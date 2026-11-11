import { DesktopError, publicError } from './errors.mjs';
import { assertTrustedSender, boundedObject, externalURL } from './policy.mjs';

export function createIPCHandler({getWindow,rendererPath,controller,cloud,view,shell}) {
  const actions={
    getState:()=>controller.getState(),chooseProject:()=>controller.chooseProject(),request:(method,params)=>controller.request(method,params),
    saveModel:model=>controller.saveModel(model),deleteModel:id=>controller.deleteModel(id),
    configureCloud:async origin=>{view.close();const result=await cloud.configure(origin);await controller.setIdentity(cloud.getState());return result;},
    cloudLogin:()=>cloud.login(),cloudLogout:async()=>{view.close();const result=await cloud.logout();await controller.setIdentity(cloud.getState());return result;},
    cloudRequest:(method,params)=>cloud.request(method,params),openCloudConversation:(id,bounds)=>view.open(id,bounds),resizeCloudConversation:bounds=>view.resize(bounds),closeCloudConversation:()=>view.close(),
    openExternal:url=>shell.openExternal(externalURL(url)),
  };
  return async(event,payload)=>{
    try{
      assertTrustedSender(event,getWindow(),rendererPath);boundedObject(payload);
      if(!Object.hasOwn(actions,payload.action)||!Array.isArray(payload.args)||payload.args.length>3)throw new DesktopError('METHOD_NOT_ALLOWED');
      return {ok:true,value:await actions[payload.action](...payload.args)};
    }catch(error){return{ok:false,error:publicError(error)};}
  };
}
