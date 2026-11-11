import { createHash, randomUUID } from 'node:crypto';
import { join, basename } from 'node:path';
import { mkdir } from 'node:fs/promises';
import { DesktopError, publicError } from './errors.mjs';
import { assertRunnerRequest, boundedObject, object, UUID } from './policy.mjs';

export function profileHash(origin = '', account = '') {
  return createHash('sha256').update(`${origin}\n${account}`).digest('hex').slice(0, 32);
}
function text(value, max = 240) { return typeof value === 'string' && value.length > 0 && value.length <= max && !value.includes('\0'); }
function modelMetadata(input, previous) {
  boundedObject(input, 128 << 10);
  const allowed = new Set(['id','name','provider','protocol','endpoint','model_id','config','api_key','share_with_cloud']);
  if (Object.keys(input).some(key => !allowed.has(key))) throw new DesktopError('INVALID_MODEL');
  if (input.id && (!UUID.test(input.id) || !previous)) throw new DesktopError('MODEL_NOT_FOUND');
  for (const key of ['name','provider','protocol','endpoint','model_id']) if (!text(input[key], key === 'endpoint' ? 2048 : 240)) throw new DesktopError('INVALID_MODEL');
  let url; try { url = new URL(input.endpoint); } catch { throw new DesktopError('INVALID_MODEL_ENDPOINT'); }
  if (!['http:','https:'].includes(url.protocol) || url.username || url.password || url.hash || url.search) throw new DesktopError('INVALID_MODEL_ENDPOINT');
  if (input.config !== undefined && !object(input.config)) throw new DesktopError('INVALID_MODEL');
  if (input.api_key !== undefined && (typeof input.api_key !== 'string' || input.api_key.length > 65536)) throw new DesktopError('INVALID_SECRET');
  if (input.share_with_cloud !== undefined && typeof input.share_with_cloud !== 'boolean') throw new DesktopError('INVALID_MODEL');
  return {id:previous?.id ?? randomUUID(), name:input.name, source:'local', provider:input.provider, protocol:input.protocol, endpoint:url.href.replace(/\/$/,''), model_id:input.model_id, config:input.config ?? {}, share_with_cloud:input.share_with_cloud ?? previous?.share_with_cloud ?? false};
}

// Electron resources are injected so security and persistence can be tested in Node.
export class DesktopController {
  constructor({storage, directory, createRunner, chooseDirectory, pathForRunner = async path => path, onEvent = () => {}, platform = process.platform}) {
    Object.assign(this,{storage,directory,createRunner,chooseDirectory,pathForRunner,onEvent,platform});
    this.runner=null; this.runnerState={status:'starting'}; this.profileID=''; this.deviceID=''; this.cloud=null; this.transition=Promise.resolve();
  }
  attachCloud(cloud) { this.cloud=cloud; }
  async initialize() { await this.setIdentity(this.cloud?.getState() ?? {}); return this.getState(); }
  getState() {
    return {profile_id:this.profileID,device_id:this.deviceID,models:this.models(),runner:this.runnerState,cloud:this.cloud?.getState() ?? {status:'disconnected'},secret_storage:{available:this.storage.secureAvailable(),persistence:this.storage.secureAvailable()?'encrypted':'session'},platform:this.platform};
  }
  emitState() { this.onEvent({type:'state',state:this.getState()}); }
  models(profile = this.profileID) {
    return this.storage.getSetting(`models:${profile}`,[]).map(model => ({...model,has_api_key:!!this.storage.getSecret(this.secretKey(model.id,profile)),credential_persisted:this.storage.secureAvailable() && !!this.storage.data.secrets[this.secretKey(model.id,profile)]}));
  }
  secretKey(id,profile = this.profileID) { return `model:${profile}:${id}`; }
  setIdentity(identity = {}) {
    const run = async () => {
      const origin=identity.origin ?? '', account=identity.account_id ?? identity.account?.id ?? identity.user?.id ?? '';
      const profile=profileHash(origin,account);
      if(this.profileID===profile && this.runner && !this.runner.closed) { this.cloud?.attachRunner?.(this.runner);await this.configureRunner(); this.emitState(); return; }
      this.cloud?.disconnect?.();
      await this.runner?.close(); this.runner=null;
      this.profileID=profile;
      const devices=this.storage.getSetting('profileDevices',{});
      this.deviceID=devices[profile] ?? randomUUID();
      if(!devices[profile]) { devices[profile]=this.deviceID; await this.storage.setSetting('profileDevices',devices); }
      this.runnerState={status:'starting'};this.emitState();
      try {
        const stateDirectory=join(this.directory,'profiles',profile); await mkdir(stateDirectory,{recursive:true,mode:0o700});
        const runner=await this.createRunner({profile_id:profile,device_id:this.deviceID,stateDirectory}); this.runner=runner;
        runner.on('event',event=>{if(this.runner===runner)this.onEvent({type:'runner',event});});
        runner.on('notification',message=>{if(this.runner===runner && ['terminal.data','terminal.exit'].includes(message.method))this.onEvent({type:message.method,...message.params});});
        runner.on('disconnected',error=>{if(this.runner===runner){this.runnerState={status:'error',error};this.emitState();}});
        await runner.request('initialize',{state_dir:await this.pathForRunner(stateDirectory),profile_id:profile,device_id:this.deviceID});
        this.cloud?.attachRunner?.(runner); await this.configureRunner(); this.runnerState={status:'ready'};
      } catch(error) { this.runnerState={status:'error',error:publicError(error)}; }
      this.emitState();
    };
    const next=this.transition.then(run,run);this.transition=next.catch(()=>{});return next;
  }
  async configureRunner() {
    if(!this.runner || this.runner.closed)return;
    const runner=this.runner;
    const models=this.models().map(({has_api_key,credential_persisted,share_with_cloud,...model})=>({...model,credential:this.storage.getSecret(this.secretKey(model.id)) ?? ''}));
    const remote=await this.cloud?.listModels?.().catch(()=>[]) ?? [];
    const cloudModels=Array.isArray(remote)?remote:(remote.models ?? remote.data ?? []);
    for(const model of cloudModels) {
      if(model.source==='local' || !UUID.test(model.id?.replace(/^cloud:/,'')))continue;
      models.push({id:`cloud:${model.id.replace(/^cloud:/,'')}`,name:model.name ?? model.model_id ?? 'Cloud model',source:'cloud',provider:model.provider ?? 'cloud',protocol:model.protocol ?? 'cloud',model_id:model.id.replace(/^cloud:/,''),endpoint:'',config:{}});
    }
    if(runner!==this.runner || runner.closed)throw new DesktopError('PROFILE_CHANGED');
    await runner.request('configureModels',{models});
    await this.cloud?.sync?.().catch(()=>{});
  }
  async request(method,params={}) { assertRunnerRequest(method,params); if(this.runnerState.status!=='ready' || !this.runner || this.runner.closed)throw new DesktopError('RUNNER_UNAVAILABLE');return this.runner.request(method,params); }
  async chooseProject() {
    if(this.runnerState.status!=='ready')throw new DesktopError('RUNNER_UNAVAILABLE');
    const profile=this.profileID, path=await this.chooseDirectory(); if(!path)return null;
    if(profile!==this.profileID)throw new DesktopError('PROFILE_CHANGED');
    return this.runner.request('project.open',{name:basename(path),path:await this.pathForRunner(path)});
  }
  async saveModel(input) {
    const profile=this.profileID,models=this.storage.getSetting(`models:${profile}`,[]), previous=models.find(model=>model.id===input?.id);
    const model=modelMetadata(input,previous);
    if(previous && this.storage.getSecret(this.secretKey(model.id,profile)) && !input.api_key && ['provider','protocol','endpoint'].some(key=>previous[key]!==model[key]))throw new DesktopError('NEW_CREDENTIAL_REQUIRED','Enter a new API key when changing the provider or endpoint.');
    // Persist validated public metadata first; setSetting rejects nested secret keys.
    const next=previous?models.map(item=>item.id===model.id?model:item):[...models,model];
    if(next.length>128)throw new DesktopError('TOO_MANY_MODELS');
    await this.storage.setSetting(`models:${profile}`,next);
    if(input.api_key)await this.storage.setSecret(this.secretKey(model.id,profile),input.api_key);
    if(profile!==this.profileID)throw new DesktopError('PROFILE_CHANGED');
    try { await this.configureRunner(); } finally { this.emitState(); }
    return this.models().find(item=>item.id===model.id);
  }
  async deleteModel(id) {
    if(!UUID.test(id))throw new DesktopError('INVALID_MODEL');
    const profile=this.profileID,models=this.storage.getSetting(`models:${profile}`,[]);
    if(!models.some(model=>model.id===id))throw new DesktopError('MODEL_NOT_FOUND');
    await this.storage.setSetting(`models:${profile}`,models.filter(model=>model.id!==id));await this.storage.deleteSecret(this.secretKey(id,profile));
    if(profile!==this.profileID)throw new DesktopError('PROFILE_CHANGED');
    try { await this.configureRunner(); } finally { this.emitState(); }
    return {deleted:true};
  }
  async close() { await this.runner?.close();this.runner=null; }
}
