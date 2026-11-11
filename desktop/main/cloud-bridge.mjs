import { createHash, randomBytes, randomUUID } from 'node:crypto';
import { hostname } from 'node:os';
import WebSocket from 'ws';
import { DesktopError } from './errors.mjs';
import { CLOUD_METHODS, UUID, boundedObject, object, validateCallback, validateCloudOrigin } from './policy.mjs';

const hash = value => createHash('sha256').update(value).digest('hex');
const profileHash = (origin, account) => hash(`${origin}\n${account}`).slice(0,32);
const maxBytes = 16 << 20;
function fail(code='CLOUD_REQUEST_FAILED') { return new DesktopError(code); }
function rows(value, key) { return Array.isArray(value) ? value : value?.[key] ?? []; }
function uuid(value) { if(typeof value!=='string'||!UUID.test(value))throw fail('INVALID_REQUEST');return value; }
function only(input, allowed) { boundedObject(input);if(Object.keys(input).some(key=>!allowed.includes(key)))throw fail('INVALID_REQUEST');return input; }
async function readJSON(response) {
  if(response.status===204)return null;
  const body=await boundedBody(response);
  try{return JSON.parse(body)}catch{throw fail('CLOUD_PROTOCOL_ERROR')}
}
async function boundedBody(response) {
  if(Number(response.headers.get('content-length'))>maxBytes){await response.body?.cancel().catch(()=>{});throw fail('CLOUD_RESPONSE_TOO_LARGE');}
  const reader=response.body?.getReader();if(!reader)return '';
  const decoder=new TextDecoder();let size=0,body='';
  try{for(;;){const {done,value}=await reader.read();if(done)break;size+=value.byteLength;if(size>maxBytes)throw fail('CLOUD_RESPONSE_TOO_LARGE');body+=decoder.decode(value,{stream:true});}return body+decoder.decode();}
  catch(error){await reader.cancel().catch(()=>{});throw error;}
  finally{reader.releaseLock();}
}

// The bridge is a main-process boundary. Neither browser cookies, device bearer
// credentials nor provider keys are ever part of its public state or events.
export function createCloudBridge({storage,shell,session,onEvent=()=>{},onAuthChanged=()=>{},getRunner=()=>null,renewSession,WebSocketFactory=WebSocket,now=()=>Date.now()}) {
  let origin=storage.getSetting('cloudOrigin',''), identity=origin?storage.getSetting(`cloudIdentity:${hash(origin)}`,null):null;
  let authInFlight=false, pendingLogin=null, runner=null, socket=null, stopped=false, generation=0, reconnectTimer=null, syncTimer=null, syncFlight=null, syncingAgain=false;
  let detachHandler=null, notificationHandler=null, eventHandler=null;
  const inference=new Map(), controllers=new Set(), seenInference=new Set();
  const partition=()=>`${storage.secureAvailable()?'persist:':''}lester-cloud-${hash(origin).slice(0,24)}`;
  const profile=()=>profileHash(origin,identity?.account_id??'');
  const tokenKey=()=>`desktop-device:${profile()}`;
  const token=()=>identity?storage.getSecret(tokenKey()):null;
  const getState=()=>({origin,partition:origin?partition():null,account_id:identity?.account_id??'',workspace_id:identity?.workspace_id??'',device_id:identity?.device_id??'',signed_in:!authInFlight&&!!identity&&!!token(),status:!origin?'unconfigured':!identity||!token()?'signed_out':socket?.readyState===1?'online':'offline'});
  function changed(){onEvent({type:'cloud-state',cloud:getState()});}
  async function fetchAPI(path,{method='GET',body,device=false,signal,auth=true}={}) {
    if(!origin)throw fail('CLOUD_NOT_CONFIGURED');
    const version=generation,currentOrigin=origin;
    if(auth&&!device&&renewSession)await renewSession({origin,partition:partition()});
    if(version!==generation||currentOrigin!==origin)throw fail('PROFILE_CHANGED');
    const headers={Accept:'application/json',Origin:origin};
    if(body!==undefined)headers['Content-Type']='application/json';
    if(device){if(!token())throw fail('CLOUD_SIGN_IN_REQUIRED');headers.Authorization=`Bearer ${token()}`;}
    const controller=new AbortController();controllers.add(controller);
    const abort=()=>controller.abort();signal?.addEventListener('abort',abort,{once:true});if(signal?.aborted)abort();
    const timeout=setTimeout(abort,path.endsWith('/stream')||path.endsWith('/generate')?600000:30000);timeout.unref?.();
    let response;
    try{
      response=await session.fromPartition(partition()).fetch(origin+path,{method,headers,body:body===undefined?undefined:JSON.stringify(body),credentials:device?'omit':'include',redirect:'error',cache:'no-store',signal:controller.signal});
      if(version!==generation||currentOrigin!==origin)throw fail('PROFILE_CHANGED');
      if(!response.ok){await response.body?.cancel().catch(()=>{});if(response.status===401)throw fail('CLOUD_SIGN_IN_REQUIRED');if(response.status===409)throw fail('CLOUD_CONFLICT');if(response.status===429)throw fail('CLOUD_BUSY');throw fail();}
      // Caller owns the returned stream, so its cancellation/deadline remain
      // active until its body has actually been consumed.
      const cleanup=()=>{clearTimeout(timeout);controllers.delete(controller);signal?.removeEventListener('abort',abort);};
      return {response,cleanup};
    }catch(error){clearTimeout(timeout);controllers.delete(controller);signal?.removeEventListener('abort',abort);if(error instanceof DesktopError)throw error;throw fail(method==='GET'?'CLOUD_UNAVAILABLE':'RESULT_UNKNOWN');}
  }
  async function json(path,options){const {response,cleanup}=await fetchAPI(path,options);try{return await readJSON(response)}finally{cleanup()}}
  async function configure(input){
    const value=typeof input==='string'?input:input?.origin;
    const next=validateCloudOrigin(value);
    if(next===origin)return getState();
    disconnect();authInFlight=false;pendingLogin=null;origin=next;identity=storage.getSetting(`cloudIdentity:${hash(origin)}`,null);
    await storage.setSetting('cloudOrigin',origin);changed();return getState();
  }
  async function login(){
    if(!origin)throw fail('CLOUD_NOT_CONFIGURED');
    const verifier=randomBytes(32).toString('base64url'),state=randomBytes(32).toString('base64url');
    const challenge=createHash('sha256').update(verifier).digest('base64url');
    pendingLogin={verifier,state,origin,expires:now()+5*60000};
    const target=new URL('/app/native-auth',origin);target.searchParams.set('client','desktop');target.searchParams.set('challenge',challenge);target.searchParams.set('state',state);
    try{await shell.openExternal(target.href)}catch{pendingLogin=null;throw fail('CLOUD_AUTH_FAILED')}
    return {status:'awaiting_browser'};
  }
  async function onCallback(value){
    if(!validateCallback(value))throw fail('CLOUD_AUTH_FAILED');
    const callback=new URL(value),pending=pendingLogin;
    // Consume before network I/O. An uncertain one-time exchange is never replayed.
    pendingLogin=null;
    if(!pending||pending.origin!==origin||pending.expires<now()||callback.searchParams.get('state')!==pending.state||!/^[-\w]{43}$/.test(callback.searchParams.get('code')??'')||callback.searchParams.getAll('state').length!==1||callback.searchParams.getAll('code').length!==1)throw fail('CLOUD_AUTH_FAILED');
    disconnect();authInFlight=true;changed();
    const current=generation,authOrigin=origin;
    const check=()=>{if(current!==generation||authOrigin!==origin)throw fail('PROFILE_CHANGED');};
    try{
      await json('/api/v1/auth/native/exchange',{method:'POST',auth:false,body:{code:callback.searchParams.get('code'),verifier:pending.verifier}});check();
      const me=await json('/api/v1/me');check();uuid(me?.user_id);uuid(me?.workspace_id);
      const key=profileHash(origin,me.user_id),devices=storage.getSetting('profileDevices',{}),deviceID=devices[key]??randomUUID();
      if(!devices[key]){devices[key]=deviceID;await storage.setSetting('profileDevices',devices);check();}
      const registered=await json('/api/v1/desktop/devices',{method:'POST',body:{id:deviceID,name:hostname().slice(0,100)||'Lester desktop',platform:process.platform,reauthorize:true}});check();
      let credential=registered?.token;
      if(!credential){credential=(await json(`/api/v1/desktop/devices/${deviceID}/token`,{method:'POST',body:{}}))?.token;check();}
      if(typeof credential!=='string'||!/^[-\w]{43}$/.test(credential))throw fail('CLOUD_AUTH_FAILED');
      const savedIdentity={account_id:me.user_id,workspace_id:me.workspace_id,device_id:deviceID},secretKey=`desktop-device:${key}`,identityKey=`cloudIdentity:${hash(authOrigin)}`;
      await storage.setSecret(secretKey,credential);check();
      await storage.setSetting(identityKey,savedIdentity);check();
      identity=savedIdentity;authInFlight=false;
      await onAuthChanged(getState());changed();return getState();
    }finally{if(current===generation){authInFlight=false;changed();}}
  }
  async function logout(){
    const oldOrigin=origin,oldPartition=origin?partition():null,oldKey=identity?tokenKey():null;
    disconnect();const current=generation;authInFlight=false;pendingLogin=null;
    // Do not retry a logout if the response is lost. Clearing the local scoped
    // token and partition immediately removes this application's access.
    if(oldOrigin)try{await json('/api/v1/auth/logout',{method:'POST',auth:false,body:{}})}catch{}
    if(oldKey)await storage.deleteSecret(oldKey);
    if(oldOrigin)await storage.setSetting(`cloudIdentity:${hash(oldOrigin)}`,null);
    if(current===generation&&origin===oldOrigin)identity=null;
    if(oldPartition)await session.fromPartition(oldPartition).clearStorageData();
    if(current===generation)changed();return getState();
  }
  async function listModels(){
    if(!getState().signed_in)return [];
    const value=await json('/api/v1/desktop/models',{device:true});
    return rows(value,'models').filter(model=>model.enabled!==false&&model.device_id!==identity.device_id).map(model=>({...model,source:model.source==='device'?'device':'cloud'}));
  }
  async function request(method,params={}){
    if(!CLOUD_METHODS.has(method))throw fail('METHOD_NOT_ALLOWED');boundedObject(params);
    if(!getState().signed_in)throw fail('CLOUD_SIGN_IN_REQUIRED');
    switch(method){
      case 'models.list':only(params,[]);return {models:await listModels()};
      case 'conversations.list':only(params,[]);return json('/api/v1/desktop/conversations',{device:true});
      case 'projects.list':only(params,[]);return json('/api/v1/projects');
      case 'conversations.get':only(params,['id']);return json(`/api/v1/conversations/${uuid(params.id)}`);
      case 'conversations.create':only(params,['project_id','agent_slug','title','model_deployment_id']);uuid(params.project_id);uuid(params.model_deployment_id);return json('/api/v1/conversations',{method:'POST',body:params});
      case 'conversations.send':{
        only(params,['id','content','submission_id','permission_mode']);uuid(params.submission_id);
        const {id,...body}=params;return json(`/api/v1/conversations/${uuid(id)}/messages`,{method:'POST',body});
      }
      case 'conversations.stop':only(params,['id','run_id']);return json(`/api/v1/conversations/${uuid(params.id)}/runs/${uuid(params.run_id)}/cancel`,{method:'POST',body:{}});
      case 'conversations.events':only(params,['id','after']);return json(`/api/v1/conversations/${uuid(params.id)}/events/history?after=${encodeURIComponent(params.after??0)}`);
      case 'personal.state':only(params,[]);return json('/api/v1/personal');
      case 'personal.messages':only(params,['content','request_id','project_id','model_deployment_id']);return json('/api/v1/personal/messages',{method:'POST',body:params});
      default:throw fail('METHOD_NOT_ALLOWED');
    }
  }
  async function cloudStream(params,{id,signal}){
    only(params,['model_id','request']);const modelID=uuid(params.model_id?.replace(/^cloud:/,''));boundedObject(params.request,4<<20);
    const selected=runner;if(!selected)throw fail('RUNNER_UNAVAILABLE');
    const {response,cleanup}=await fetchAPI(`/api/v1/desktop/models/${modelID}/stream`,{method:'POST',device:true,body:params.request,signal});
    const reader=response.body?.getReader();if(!reader){cleanup();throw fail('CLOUD_PROTOCOL_ERROR')}
    const decoder=new TextDecoder();let buffer='',size=0,completed=false,failed=false;
    const emit=line=>{let event;try{event=JSON.parse(line)}catch{throw fail('CLOUD_PROTOCOL_ERROR')};if(!object(event)||typeof event.type!=='string')throw fail('CLOUD_PROTOCOL_ERROR');if(event.type==='MODEL_COMPLETED')completed=true;if(event.error_kind||event.type==='RUN_FAILED')failed=true;selected.notify('cloud.model.event',{request_id:id,event});};
    try{
      for(;;){const {done,value}=await reader.read();if(done)break;size+=value.byteLength;if(size>maxBytes)throw fail('CLOUD_RESPONSE_TOO_LARGE');buffer+=decoder.decode(value,{stream:true});let end;while((end=buffer.indexOf('\n'))>=0){const line=buffer.slice(0,end);buffer=buffer.slice(end+1);if(line.trim())emit(line);}if(Buffer.byteLength(buffer)>8<<20)throw fail('CLOUD_RESPONSE_TOO_LARGE');}
      buffer+=decoder.decode();if(buffer.trim())emit(buffer);if(!completed&&!failed)throw fail('CLOUD_STREAM_INTERRUPTED');return {done:true};
    }finally{await reader.cancel().catch(()=>{});reader.releaseLock();cleanup();}
  }
  function detach(){
    detachHandler?.();detachHandler=null;
    if(runner&&notificationHandler)runner.off('notification',notificationHandler);
    if(runner&&eventHandler)runner.off('event',eventHandler);
    notificationHandler=null;eventHandler=null;
  }
  function disconnect(){
    stopped=true;generation++;clearTimeout(reconnectTimer);clearTimeout(syncTimer);reconnectTimer=null;syncTimer=null;syncingAgain=false;
    for(const controller of controllers)controller.abort();controllers.clear();
    const old=socket;socket=null;if(old){old.removeAllListeners?.();try{old.close()}catch{}}
    for(const [id,call]of inference){try{call.runner.notify('models.cancel',{id})}catch{}}
    inference.clear();detach();runner=null;
  }
  function attachRunner(next){
    disconnect();runner=next;stopped=false;const current=generation;
    detachHandler=runner.handleRequest('cloud.model.stream',cloudStream);
    notificationHandler=({method,params})=>{
      if(method!=='model.event'||!object(params))return;const call=inference.get(params.id);
      if(!call||call.runner!==runner||!object(params.event))return;
      send({type:'model.event',id:params.id,event:params.event});
    };
    eventHandler=()=>scheduleSync();runner.on('notification',notificationHandler);runner.on('event',eventHandler);
    if(getState().signed_in)connect(current);
  }
  function send(value){
    if(!socket||socket.readyState!==1)return false;
    const encoded=JSON.stringify(value);if(Buffer.byteLength(encoded)>8<<20||socket.bufferedAmount>8<<20){socket.close();return false;}
    try{socket.send(encoded);return true}catch{return false}
  }
  function connect(current){
    if(stopped||current!==generation||!runner||!getState().signed_in)return;
    const address=new URL(`/api/v1/desktop/devices/${identity.device_id}/connect`,origin);address.protocol=address.protocol==='https:'?'wss:':'ws:';
    const ws=new WebSocketFactory(address.href,{headers:{Authorization:`Bearer ${token()}`,Origin:origin},maxPayload:8<<20,followRedirects:false,handshakeTimeout:15000});socket=ws;
    ws.on('open',()=>{if(current!==generation){ws.close();return;}changed();scheduleSync(0);});
    ws.on('message',data=>{
      if(current!==generation||socket!==ws)return;
      let frame;try{frame=JSON.parse(data.toString())}catch{ws.close();return;}
      if(!object(frame)||typeof frame.id!=='string'||frame.id.length>160){ws.close();return;}
      if(frame.type==='model.cancel'){const call=inference.get(frame.id);if(call)try{call.runner.notify('models.cancel',{id:frame.id})}catch{};inference.delete(frame.id);return;}
      if(frame.type!=='model.request')return;
      if(seenInference.has(frame.id)||seenInference.size>=8192){send({type:'model.done',id:frame.id,error_kind:'protocol'});return;}
      seenInference.add(frame.id);
      const models=storage.getSetting(`models:${profile()}`,[]);
      if(inference.size>=16||!models.some(m=>m.id===frame.model_key&&m.share_with_cloud===true)||!object(frame.request)){
        send({type:'model.done',id:frame.id,error_kind:'transport'});return;
      }
      const activeRunner=runner;inference.set(frame.id,{runner:activeRunner,model_key:frame.model_key});
      activeRunner.request(frame.stream?'models.stream':'models.generate',{id:frame.id,model_id:frame.model_key,request:frame.request},{timeout:605000}).then(response=>{
        if(!inference.delete(frame.id)||current!==generation)return;
        send({type:'model.done',id:frame.id,...(frame.stream?{}:{response})});
      },()=>{if(inference.delete(frame.id)&&current===generation)send({type:'model.done',id:frame.id,error_kind:'transport'});});
    });
    ws.on('error',()=>{});
    ws.on('close',()=>{
      if(socket!==ws||current!==generation)return;socket=null;
      for(const [id,call]of inference){try{call.runner.notify('models.cancel',{id})}catch{}}
      inference.clear();changed();
      // Reconnect the transport only. Previously accepted inference and tools
      // are deliberately absent from this reconnect path.
      if(!stopped){reconnectTimer=setTimeout(()=>connect(current),5000);reconnectTimer.unref?.();}
    });
  }
  function scheduleSync(delay=500){
    if(stopped||!getState().signed_in)return;
    clearTimeout(syncTimer);syncTimer=setTimeout(()=>{sync().catch(()=>onEvent({type:'cloud-sync-error',error:{code:'CLOUD_SYNC_UNAVAILABLE'}}));},delay);syncTimer.unref?.();
  }
  async function sync(){
    if(syncFlight){syncingAgain=true;return syncFlight;}
    const current=generation,selected=runner;
    if(stopped||!selected||!getState().signed_in)return;
    const operation=async()=>{
      const shared=storage.getSetting(`models:${profile()}`,[]).filter(model=>model.share_with_cloud===true);
      for(const [id,call]of inference){if(!shared.some(model=>model.id===call.model_key)){try{call.runner.notify('models.cancel',{id})}catch{};inference.delete(id);send({type:'model.done',id,error_kind:'transport'});}}
      const models=shared.map(model=>({id:model.id,name:model.name,source:'local',provider:model.provider,model_id:model.model_id,context_window:Number(model.config?.context_window)||0,output_reserve:Number(model.config?.output_reserve)||0}));
      await json(`/api/v1/desktop/devices/${identity.device_id}/models`,{method:'PUT',device:true,body:{models}});
      const local=rows(await selected.request('conversation.list',{}),'conversations');
      if(current!==generation||selected!==runner)return;
      const settingKey=`directory:${profile()}`,receipts=storage.getSetting(settingKey,{}),metadata=[];
      for(const conversation of local){
        const item={id:conversation.id,project_name:conversation.project_name??'',title:(conversation.title||'Untitled').slice(0,240),status:conversation.status??'idle',updated_at:conversation.updated_at};
        if(!UUID.test(item.id)||!item.updated_at)continue;
        const digest=hash(JSON.stringify(item)),old=receipts[item.id];
        const revision=old?.digest===digest?old.revision:Math.max((old?.revision??0)+1,now());
        receipts[item.id]={digest,revision};metadata.push({...item,revision});
      }
      await storage.setSetting(settingKey,receipts);
      for(let offset=0;offset<metadata.length;offset+=200){if(current!==generation)return;await json(`/api/v1/desktop/devices/${identity.device_id}/conversations`,{method:'PUT',device:true,body:{conversations:metadata.slice(offset,offset+200)}});}
    };
    syncFlight=operation();try{await syncFlight}finally{syncFlight=null;if(current===generation&&!stopped){const delay=syncingAgain?500:30000;syncingAgain=false;scheduleSync(delay);}}
  }
  return {configure,login,logout,getState,listModels,request,onCallback,attachRunner,disconnect,sync};
}
