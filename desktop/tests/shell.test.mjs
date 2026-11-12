import test from 'node:test';
import assert from 'node:assert/strict';
import { EventEmitter, once } from 'node:events';
import { spawn } from 'node:child_process';
import { mkdtemp, readFile, rm, stat } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { pathToFileURL } from 'node:url';
import { randomBytes, createCipheriv, createDecipheriv } from 'node:crypto';
import { DesktopStore } from '../main/storage.mjs';
import { RunnerRPC } from '../main/sidecar.mjs';
import { DesktopController, profileHash } from '../main/controller.mjs';
import { assertRunnerRequest, assertTrustedSender, allowedCloudNavigation, allowedLocalNavigation, clampBounds, externalURL, validateCallback } from '../main/policy.mjs';
import { createIPCHandler } from '../main/ipc.mjs';
import { wslPath } from '../main/launch.mjs';
import { createSessionRenewer } from '../main/auth-session.mjs';
import { CloudWorkspaceView } from '../main/cloud-view.mjs';

function vault(available=true) {
  const key=randomBytes(32);
  return {isEncryptionAvailable:()=>available,getSelectedStorageBackend:()=>available?'gnome_libsecret':'basic_text',encryptString:value=>{const iv=randomBytes(12),cipher=createCipheriv('aes-256-gcm',key,iv);const data=Buffer.concat([cipher.update(value,'utf8'),cipher.final()]);return Buffer.concat([iv,cipher.getAuthTag(),data]);},decryptString:encoded=>{const decipher=createDecipheriv('aes-256-gcm',key,encoded.subarray(0,12));decipher.setAuthTag(encoded.subarray(12,28));return Buffer.concat([decipher.update(encoded.subarray(28)),decipher.final()]).toString('utf8');}};
}
async function store(t,available=true) {const directory=await mkdtemp(join(tmpdir(),'lester-shell-'));t.after(()=>rm(directory,{recursive:true,force:true}));return new DesktopStore(directory,vault(available)).load();}
function child(code) {return spawn(process.execPath,['-e',code],{stdio:['pipe','pipe','pipe']});}
class FakeRunner extends EventEmitter {
  constructor(){super();this.requests=[];this.closed=false;}
  async request(method,params){this.requests.push({method,params});return method==='project.open'?{id:'project',...params}:{};}
  async close(){this.closed=true;}
}

test('OS encrypted secrets survive reload and public metadata rejects nested credentials',async t=>{
  const storage=await store(t);await storage.setSecret('provider','private-provider-value');
  const raw=await readFile(storage.file,'utf8');assert.ok(!raw.includes('private-provider-value'));
  // Windows reports synthesized mode bits; POSIX chmod assertions apply only
  // on filesystems where the storage permission contract is meaningful.
  if(process.platform!=='win32')assert.equal((await stat(storage.file)).mode&0o777,0o600);
  const restored=await new DesktopStore(storage.directory,storage.safe).load();assert.equal(restored.getSecret('provider'),'private-provider-value');
  await assert.rejects(storage.setSetting('models',[{config:{nested:{api_key:'rejected'}}}]),{code:'PRIVATE_SETTING_REJECTED'});
});
test('unavailable native keyring uses session memory without plaintext persistence',async t=>{
  const storage=await store(t,false);assert.deepEqual(await storage.setSecret('api-key','session-private-value'),{persisted:false});
  assert.equal(storage.getSecret('api-key'),'session-private-value');assert.ok(!(await readFile(storage.file,'utf8')).includes('session-private-value'));
  assert.equal((await new DesktopStore(storage.directory,storage.safe).load()).getSecret('api-key'),null);
});
test('RPC handles fragmented UTF-8 frames, concurrent replies, events and reverse calls',async t=>{
  const process=child(`let buffer='';process.stdin.on('data',chunk=>{buffer+=chunk;let end;while((end=buffer.indexOf('\\n'))>=0){const frame=JSON.parse(buffer.slice(0,end));buffer=buffer.slice(end+1);if(frame.method==='ping'){const response=Buffer.from(JSON.stringify({jsonrpc:'2.0',id:frame.id,result:{text:'你好'}})+'\\n');const split=response.indexOf(Buffer.from('你好'))+1;process.stdout.write(response.subarray(0,split));setTimeout(()=>process.stdout.write(response.subarray(split)),5);}else if(frame.method==='reverse'){process.stdout.write(JSON.stringify({jsonrpc:'2.0',id:'incoming',method:'cloud.model.stream',params:{model_id:'fixture'}})+'\\n');process.stdout.write(JSON.stringify({jsonrpc:'2.0',method:'event',params:{type:'ready'}})+'\\n');process.stdout.write(JSON.stringify({jsonrpc:'2.0',id:frame.id,result:true})+'\\n');}}});`);
  const rpc=new RunnerRPC(process);t.after(()=>rpc.close());
  assert.deepEqual(await rpc.request('ping'),{text:'你好'});
  let received;rpc.handleRequest('cloud.model.stream',params=>{received=params;return{done:true};});
  const event=once(rpc,'event');assert.equal(await rpc.request('reverse'),true);assert.deepEqual((await event)[0],{type:'ready'});
  await new Promise(resolve=>setImmediate(resolve));assert.deepEqual(received,{model_id:'fixture'});
});
test('RPC timeout reports unknown result and never replays mutations',async t=>{
  const process=child(`process.stdin.on('data',()=>{});setInterval(()=>{},1000);`),rpc=new RunnerRPC(process,{timeout:25});t.after(()=>rpc.close());
  await assert.rejects(rpc.request('conversation.send',{id:'original'}),{code:'RESULT_UNKNOWN'});assert.equal(rpc.pending.size,0);
});
test('RPC rejects pending work on exit and kills oversized or invalid frames',async t=>{
  const exiting=child(`process.stdin.once('data',()=>process.exit(7));`),rpc=new RunnerRPC(exiting);t.after(()=>rpc.close());
  await assert.rejects(rpc.request('conversation.send'),{code:'RUNNER_DISCONNECTED'});
  const invalid=child(`process.stdin.once('data',()=>process.stdout.write('not-json\\n'));`),broken=new RunnerRPC(invalid);t.after(()=>broken.close());
  await assert.rejects(broken.request('ping'),{code:'RUNNER_PROTOCOL_ERROR'});
  const huge=child(`process.stdin.once('data',()=>process.stdout.write('x'.repeat(1025)));`),limited=new RunnerRPC(huge,{maxFrame:1024});t.after(()=>limited.close());
  await assert.rejects(limited.request('ping'),{code:'RUNNER_PROTOCOL_ERROR'});
});
test('IPC accepts only the exact local main frame and method allowlist',async()=>{
  const rendererPath='/app/renderer/index.html',frame={url:pathToFileURL(rendererPath).href},contents={isDestroyed:()=>false,mainFrame:frame},window={webContents:contents},event={sender:contents,senderFrame:frame};
  assert.doesNotThrow(()=>assertTrustedSender(event,window,rendererPath));
  for(const changed of [{sender:{}},{senderFrame:{url:frame.url}},{senderFrame:{url:'https://cloud.test/app'}}])assert.throws(()=>assertTrustedSender({...event,...changed},window,rendererPath),{code:'UNTRUSTED_FRAME'});
  for(const url of ['file:///app/renderer/other.html','file:///app/renderer/index.html?inject=1','https://cloud.test/app']){frame.url=url;assert.throws(()=>assertTrustedSender(event,window,rendererPath),{code:'UNTRUSTED_FRAME'});}
  frame.url=pathToFileURL(rendererPath).href;
  let calls=0;const controller={getState:()=>{calls++;return{safe:true};}};
  const handle=createIPCHandler({getWindow:()=>window,rendererPath,controller,cloud:{},view:{},shell:{}});
  assert.deepEqual(await handle(event,{action:'getState',args:[]}),{ok:true,value:{safe:true}});
  assert.equal((await handle({...event,senderFrame:{url:'https://cloud.test/app'}},{action:'getState',args:[]})).error.code,'UNTRUSTED_FRAME');assert.equal(calls,1);
  assert.equal((await handle(event,{action:'constructor',args:[]})).error.code,'METHOD_NOT_ALLOWED');
  for(const method of ['initialize','configureModels','project.open','models.generate','models.stream','__proto__'])assert.throws(()=>assertRunnerRequest(method,{}),{code:'METHOD_NOT_ALLOWED'});
  for(const key of ['state_dir','profile_id','device_id','api_key','credential','project_path','root'])assert.throws(()=>assertRunnerRequest('conversation.create',{[key]:'/etc'}),{code:'METHOD_NOT_ALLOWED'});
});
test('profiles isolate databases/models and keep device identity stable per account',async t=>{
  const storage=await store(t),runners=[],controller=new DesktopController({storage,directory:storage.directory,createRunner:async()=>{const runner=new FakeRunner();runners.push(runner);return runner;},chooseDirectory:async()=>'/approved/folder'});
  await controller.setIdentity({origin:'https://one.test',account_id:'alice'});const alice=controller.profileID,device=controller.deviceID;
  const model=await controller.saveModel({name:'Local',provider:'openai',protocol:'openai-compatible',endpoint:'https://provider.test/v1',model_id:'test',api_key:'private-key',share_with_cloud:true});
  assert.equal(model.source,'local');assert.equal(model.has_api_key,true);assert.equal(model.api_key,undefined);
  assert.ok(!(await readFile(storage.file,'utf8')).includes('private-key'));
  const configured=runners[0].requests.findLast(item=>item.method==='configureModels');assert.equal(configured.params.models[0].credential,'private-key');
  assert.equal((await controller.chooseProject()).path,'/approved/folder');
  await controller.setIdentity({origin:'https://one.test',account_id:'bob'});assert.notEqual(controller.profileID,alice);assert.notEqual(controller.deviceID,device);assert.equal(controller.models().length,0);assert.equal(runners[0].closed,true);
  const init=runners[1].requests.find(item=>item.method==='initialize').params;assert.equal(init.profile_id,profileHash('https://one.test','bob'));assert.ok(init.state_dir.endsWith(init.profile_id));
  await controller.setIdentity({origin:'https://one.test',account_id:'alice'});assert.equal(controller.profileID,alice);assert.equal(controller.deviceID,device);assert.equal(controller.models().length,1);
  await assert.rejects(controller.saveModel({...model,has_api_key:undefined}),{code:'INVALID_MODEL'});
  await controller.close();
});
test('local preview frames cannot navigate to network URLs, arbitrary files or the privileged renderer',()=>{
  const rendererPath=join(tmpdir(),'lester-navigation','index.html');
  const mainURL=pathToFileURL(rendererPath).href,previewURL=pathToFileURL(join(tmpdir(),'lester-navigation','preview.html')).href;
  assert.equal(allowedLocalNavigation(mainURL,rendererPath,true),true);
  assert.equal(allowedLocalNavigation(previewURL+'#nonce',rendererPath,false),true);
  for(const value of ['about:blank','about:srcdoc'])assert.equal(allowedLocalNavigation(value,rendererPath,false),true);
  for(const value of ['https://cloud.test/app','file:///etc/passwd',mainURL,previewURL+'?inject=1','javascript:alert(1)','blob:null/document'])assert.equal(allowedLocalNavigation(value,rendererPath,false),false);
  for(const value of ['about:blank',previewURL,mainURL+'?inject=1'])assert.equal(allowedLocalNavigation(value,rendererPath,true),false);
});
test('cloud pane navigation, bounds, external URL and WSL paths stay confined',()=>{
  const origin='https://cloud.test';assert.equal(allowedCloudNavigation(origin+'/app/c/123e4567-e89b-42d3-a456-426614174000',origin),true);assert.equal(allowedCloudNavigation(origin+'/app/settings/memory',origin),true);assert.equal(allowedCloudNavigation(origin+'/app/settings/memory-admin',origin),false);
  for(const url of [origin+'/api/v1/secret',origin+'/login','file:///etc/passwd','https://evil.test/app','https://cloud.test.evil/app'])assert.equal(allowedCloudNavigation(url,origin),false);
  assert.deepEqual(clampBounds({x:-10,y:30,width:2000,height:2000},{width:900,height:600}),{x:0,y:30,width:900,height:570});
  assert.throws(()=>clampBounds({x:NaN,y:0,width:1,height:1},{width:900,height:600}),{code:'INVALID_VIEW_BOUNDS'});
  for(const url of ['file:///etc/passwd','javascript:alert(1)','http://remote.test','https://user:secret@cloud.test'])assert.throws(()=>externalURL(url),{code:'INVALID_EXTERNAL_URL'});
  assert.equal(validateCallback('lester-desktop://auth/callback?code=abc&state=def'),true);assert.equal(validateCallback('lester://auth/callback'),false);
  assert.equal(wslPath('C:\\Users\\Alice\\Project'),'\/mnt/c/Users/Alice/Project');assert.throws(()=>wslPath('\\\\wsl$\\Ubuntu\\home\\alice'),{code:'WSL_PATH_UNSUPPORTED'});
  assert.throws(()=>wslPath('/host/path'),{code:'WSL_PATH_UNSUPPORTED'});
});
test('cloud view keeps remote content sandboxed without preload or local bridge',async()=>{
  let preferences,lastURL,view;
  class RemoteView {
    constructor(options){preferences=options.webPreferences;view=this;this.webContents=new EventEmitter();Object.assign(this.webContents,{session:{setPermissionRequestHandler(){},setPermissionCheckHandler(){}},setWindowOpenHandler:handler=>{this.popup=handler;},loadURL:async url=>{lastURL=url;},isDestroyed:()=>false,close:()=>{this.closed=true;}});}
    setBounds(bounds){this.bounds=bounds;}
  }
  const window={getContentSize:()=>[1000,700],contentView:{addChildView(){},removeChildView(){}}},origin='https://cloud.test';
  const pane=new CloudWorkspaceView({window,WebContentsView:RemoteView,getCloudState:()=>({origin,signed_in:true,partition:'persist:lester-cloud-test'}),openExternal:async()=>{}});
  const id='123e4567-e89b-42d3-a456-426614174000';await pane.open(id,{x:200,y:20,width:2000,height:2000});
  assert.equal(lastURL,origin+'/app/c/'+id);assert.equal(preferences.preload,undefined);assert.equal(preferences.nodeIntegration,false);assert.equal(preferences.sandbox,true);assert.equal(preferences.contextIsolation,true);
  assert.deepEqual(view.bounds,{x:200,y:20,width:800,height:680});
  let prevented=false;view.webContents.emit('will-navigate',{preventDefault(){prevented=true;}},'file:///etc/passwd');assert.equal(prevented,true);
  pane.close();assert.equal(view.closed,true);
});
test('session renewer uses the shared cloud Web Lock and reports expiry without exposing cookies',async()=>{
  let status='ready',options,script,lastURL,destroyed=0;
  class HelperWindow {
    constructor(value){options=value;this.webContents=new EventEmitter();Object.assign(this.webContents,{setWindowOpenHandler(){},executeJavaScript:async value=>{script=value;return status;}});}
    async loadURL(value){lastURL=value;}
    isDestroyed(){return false;}
    destroy(){destroyed++;}
  }
  let clock=100000;
  const renew=createSessionRenewer({BrowserWindow:HelperWindow,now:()=>clock});
  const input={origin:'https://cloud.test',partition:'persist:lester-cloud-test'};
  assert.deepEqual(await renew(input),{status:'ready'});assert.equal(lastURL,'https://cloud.test/docs');assert.equal(options.webPreferences.preload,undefined);assert.equal(options.webPreferences.sandbox,true);
  assert.match(script,/navigator\.locks\.request\('lester-session-renewal'/);assert.match(script,/credentials:'include'/);assert.doesNotMatch(script,/document\.cookie/);
  status='expired';assert.deepEqual(await renew(input),{status:'ready'});assert.equal(destroyed,1);clock+=30001;
  await assert.rejects(renew(input),{code:'CLOUD_SIGN_IN_REQUIRED'});assert.equal(destroyed,2);
  status='unavailable';await assert.rejects(renew(input),{code:'CLOUD_SESSION_UNAVAILABLE'});
});
