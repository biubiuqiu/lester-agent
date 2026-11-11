import { app, BrowserWindow, WebContentsView, dialog, ipcMain, safeStorage, session, shell } from 'electron';
import { join, dirname } from 'node:path';
import { fileURLToPath } from 'node:url';
import { DesktopStore } from './storage.mjs';
import { DesktopController } from './controller.mjs';
import { launchRunner, wslPath } from './launch.mjs';
import { CloudWorkspaceView } from './cloud-view.mjs';
import { createCloudBridge } from './cloud-bridge.mjs';
import { createIPCHandler } from './ipc.mjs';
import { createSessionRenewer } from './auth-session.mjs';
import { allowedLocalNavigation, externalURL, validateCallback } from './policy.mjs';

const directory=dirname(fileURLToPath(import.meta.url)),rendererPath=join(directory,'../renderer/index.html');
let window,controller,cloud,cloudView,closing=false;
const pendingCallbacks=[];
const emit=event=>{if(window&&!window.isDestroyed())window.webContents.send('lester:event',event);};
const handleCallback=url=>{if(!validateCallback(url))return;if(cloud)cloud.onCallback(url).catch(()=>emit({type:'cloud-auth-error',error:{code:'CLOUD_AUTH_FAILED',message:'Cloud sign-in could not be completed.'}}));else pendingCallbacks.push(url);};
if(!app.requestSingleInstanceLock())app.quit();
else {
  app.on('open-url',(event,url)=>{event.preventDefault();handleCallback(url);});
  app.on('second-instance',(_event,argv)=>{for(const value of argv)if(validateCallback(value))handleCallback(value);if(window){if(window.isMinimized())window.restore();window.focus();}});
  for(const value of process.argv)if(validateCallback(value))pendingCallbacks.push(value);
  app.whenReady().then(start).catch(()=>{dialog.showErrorBox('Lester','The desktop workspace could not start. Existing local data has been preserved.');app.quit();});
}

async function start() {
  if(process.defaultApp)app.setAsDefaultProtocolClient('lester-desktop',process.execPath,[join(directory,'..')]);
  else app.setAsDefaultProtocolClient('lester-desktop');
  const dataDirectory=app.getPath('userData'),storage=await new DesktopStore(dataDirectory,safeStorage).load();
  window=new BrowserWindow({width:1380,height:900,minWidth:760,minHeight:540,show:false,title:'Lester',icon:join(directory,'../build/icon.png'),backgroundColor:'#faf9f6',webPreferences:{preload:join(directory,'../preload/index.cjs'),contextIsolation:true,nodeIntegration:false,sandbox:true,webSecurity:true,allowRunningInsecureContent:false}});
  window.removeMenu();
  const localSession=window.webContents.session;
  localSession.setPermissionRequestHandler((_contents,_permission,callback)=>callback(false));
  localSession.setPermissionCheckHandler(()=>false);
  localSession.webRequest.onHeadersReceived((details,callback)=>{
    const headers={...details.responseHeaders};
    if(details.resourceType==='mainFrame' && details.url.startsWith('file:'))headers['Content-Security-Policy']=["default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data: blob:; font-src 'self'; connect-src 'none'; frame-src 'self' blob: about:; object-src 'none'; base-uri 'none'; form-action 'none'"];
    callback({responseHeaders:headers});
  });
  const openExternal=async value=>shell.openExternal(externalURL(value));
  // Explicit shell links use the trusted preload. Generated previews cannot open
  // the system browser or navigate a child frame to network/arbitrary file URLs.
  window.webContents.setWindowOpenHandler(()=>({action:'deny'}));
  window.webContents.on('will-frame-navigate',event=>{if(!allowedLocalNavigation(event.url,rendererPath,event.isMainFrame))event.preventDefault();});
  window.webContents.on('will-redirect',event=>{if(!allowedLocalNavigation(event.url,rendererPath,event.isMainFrame))event.preventDefault();});
  window.webContents.on('will-attach-webview',event=>event.preventDefault());
  controller=new DesktopController({storage,directory:dataDirectory,platform:process.platform,onEvent:emit,pathForRunner:async path=>process.platform==='win32'?wslPath(path):path,chooseDirectory:async()=>{const result=await dialog.showOpenDialog(window,{title:'Open project folder',properties:['openDirectory','createDirectory']});return result.canceled?null:result.filePaths[0];},createRunner:()=>launchRunner({resources:app.isPackaged?join(process.resourcesPath,'runner'):join(directory,'../resources/runner')})});
  cloud=createCloudBridge({storage,shell,session,renewSession:createSessionRenewer({BrowserWindow}),onEvent:event=>{emit(event);controller.emitState();},onAuthChanged:async()=>{cloudView?.close();await controller.setIdentity(cloud.getState());},getRunner:()=>controller.runner});
  controller.attachCloud(cloud);
  cloudView=new CloudWorkspaceView({window,WebContentsView,session,openExternal,getCloudState:()=>cloud.getState(),onEvent:emit});
  ipcMain.handle('lester:desktop',createIPCHandler({getWindow:()=>window,rendererPath,controller,cloud,view:cloudView,shell}));
  window.on('resize',()=>cloudView.clamp());
  window.once('ready-to-show',()=>window.show());
  window.on('closed',()=>{cloudView?.close();window=null;});
  await window.loadFile(rendererPath);
  await controller.initialize();
  for(const callback of pendingCallbacks.splice(0))handleCallback(callback);
}

app.on('before-quit',event=>{
  if(closing||!controller)return;
  event.preventDefault();closing=true;cloudView?.close();cloud?.disconnect?.();
  controller.close().finally(()=>app.quit());
});
app.on('window-all-closed',()=>app.quit());
