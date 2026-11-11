import { EventEmitter } from 'node:events';
import { randomUUID } from 'node:crypto';
import { StringDecoder } from 'node:string_decoder';
import { DesktopError } from './errors.mjs';
import { object } from './policy.mjs';

export class RunnerRPC extends EventEmitter {
  constructor(child,{maxFrame=8<<20,maxPending=64,timeout=30000}={}) {
    super(); this.child=child;this.maxFrame=maxFrame;this.maxPending=maxPending;this.timeout=timeout;this.buffer='';this.decoder=new StringDecoder('utf8');this.pending=new Map();this.incoming=new Map();this.handlers=new Map();this.closed=false;
    child.stdout.on('data',chunk=>this.receive(chunk));
    child.stdout.on('end',()=>{if(this.buffer.trim())this.fail(new DesktopError('RUNNER_PROTOCOL_ERROR'));});
    // Diagnostic output is never forwarded to the renderer or serialized with credentials.
    child.stderr?.on('data',()=>this.emit('diagnostic'));
    child.once('error',()=>this.fail(new DesktopError('RUNNER_UNAVAILABLE')));
    child.once('exit',(code,signal)=>{this.fail(new DesktopError('RUNNER_DISCONNECTED','The local runner disconnected. Check the original receipt before resending.'));this.emit('exit',{code,signal});});
  }
  write(value) {
    if(this.closed||!this.child.stdin.writable) throw new DesktopError('RUNNER_DISCONNECTED');
    let frame;try{frame=JSON.stringify(value)+'\n'}catch{throw new DesktopError('INVALID_REQUEST')}
    if(Buffer.byteLength(frame)>this.maxFrame)throw new DesktopError('REQUEST_TOO_LARGE');
    this.child.stdin.write(frame,error=>{if(error)this.fail(new DesktopError('RUNNER_DISCONNECTED'));});
  }
  request(method,params={},options={}) {
    if(this.closed)return Promise.reject(new DesktopError('RUNNER_DISCONNECTED'));
    if(this.pending.size>=this.maxPending)return Promise.reject(new DesktopError('RUNNER_BUSY'));
    const id=randomUUID();
    return new Promise((resolve,reject)=>{
      const timer=setTimeout(()=>{this.pending.delete(id);reject(new DesktopError('RESULT_UNKNOWN','The request result is unknown. Check its original receipt before retrying.'));},options.timeout??this.timeout);timer.unref?.();
      this.pending.set(id,{resolve,reject,timer});
      try{this.write({jsonrpc:'2.0',id,method,params})}catch(error){clearTimeout(timer);this.pending.delete(id);reject(error)}
    });
  }
  notify(method,params={}) { this.write({jsonrpc:'2.0',method,params}); }
  handleRequest(method,handler) {this.handlers.set(method,handler);return()=>this.handlers.delete(method);}
  respond(id,result=null,error=null) {
    const active=this.incoming.get(id);if(!active)return false;this.incoming.delete(id);
    this.write(error?{jsonrpc:'2.0',id,error}:{jsonrpc:'2.0',id,result});return true;
  }
  receive(chunk) {
    if(this.closed)return;
    this.buffer+=this.decoder.write(chunk);
    for(;;){const end=this.buffer.indexOf('\n');if(end<0)break;const line=this.buffer.slice(0,end);this.buffer=this.buffer.slice(end+1);if(!line.trim())continue;
      if(Buffer.byteLength(line)>this.maxFrame){this.fail(new DesktopError('RUNNER_PROTOCOL_ERROR'));return;}
      let frame;try{frame=JSON.parse(line)}catch{this.fail(new DesktopError('RUNNER_PROTOCOL_ERROR'));return;}
      if(!object(frame)||frame.jsonrpc!=='2.0'){this.fail(new DesktopError('RUNNER_PROTOCOL_ERROR'));return;}
      if(typeof frame.method==='string'){
        if(Object.hasOwn(frame,'id'))this.handleIncoming(frame);
        else {if(frame.method==='cloud.model.cancel')this.incoming.get(frame.params?.request_id)?.abort();this.emit('notification',{method:frame.method,params:frame.params});if(frame.method==='event')this.emit('event',frame.params);}
      }else{
        const pending=this.pending.get(frame.id);if(!pending)continue;this.pending.delete(frame.id);clearTimeout(pending.timer);
        if(object(frame.error))pending.reject(new DesktopError('RUNNER_REQUEST_FAILED','The local runner rejected the request.'));
        else if(Object.hasOwn(frame,'result'))pending.resolve(frame.result);
        else pending.reject(new DesktopError('RUNNER_PROTOCOL_ERROR'));
      }
    }
    if(Buffer.byteLength(this.buffer)>this.maxFrame)this.fail(new DesktopError('RUNNER_PROTOCOL_ERROR'));
  }
  handleIncoming(frame) {
    if(typeof frame.id!=='string'||frame.id.length>160||this.incoming.has(frame.id)||this.incoming.size>=this.maxPending){this.fail(new DesktopError('RUNNER_PROTOCOL_ERROR'));return;}
    const controller=new AbortController();this.incoming.set(frame.id,controller);const handler=this.handlers.get(frame.method);
    if(!handler){this.respond(frame.id,null,{code:-32601,message:'Method not available'});return;}
    Promise.resolve().then(()=>handler(frame.params,{id:frame.id,signal:controller.signal})).then(result=>this.respond(frame.id,result??null),()=>this.respond(frame.id,null,{code:-32603,message:'Model bridge request failed'})).catch(()=>{});
  }
  fail(error) {
    if(this.closed)return;this.closed=true;
    for(const item of this.pending.values()){clearTimeout(item.timer);item.reject(error)}this.pending.clear();
    for(const controller of this.incoming.values())controller.abort();this.incoming.clear();this.emit('disconnected',{code:error.code});
    if(error.code==='RUNNER_PROTOCOL_ERROR')this.child.kill();
  }
  async close() {
    if(this.closed)return;this.fail(new DesktopError('RUNNER_DISCONNECTED'));
    this.child.stdin.end();
    await new Promise(resolve=>{if(this.child.exitCode!==null){resolve();return}const timer=setTimeout(()=>{this.child.kill();resolve();},3000);timer.unref?.();this.child.once('exit',()=>{clearTimeout(timer);resolve()});});
  }
}
