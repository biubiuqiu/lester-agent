import { DesktopError } from './errors.mjs';
import { validateCloudOrigin } from './policy.mjs';

// Fixed source runs only in the configured cloud origin, under the same Web Lock
// as the workspace. HttpOnly cookies never cross into main or the local renderer.
const RENEW = `(async () => {
  const request = (path, method = 'GET') => fetch('/api/v1/auth/' + path, {method,credentials:'include',cache:'no-store',signal:AbortSignal.timeout(10000)});
  const fresh = async response => {
    if (!response.ok) return false;
    const value = await response.json();
    return Number.isFinite(Date.parse(value.access_expires_at)) && Date.parse(value.access_expires_at) > Date.now() + 120000;
  };
  const renew = async () => {
    const current = await request('session');
    if (current.ok) { if (await fresh(current)) return 'ready'; }
    else if (current.status !== 401) return 'unavailable';
    const response = await request('refresh', 'POST');
    if (response.ok) return await fresh(response) ? 'ready' : 'unavailable';
    if (response.status === 401) return 'expired';
    if (response.status === 409) {
      for (let attempt = 0; attempt < 5; attempt++) {
        await new Promise(resolve => setTimeout(resolve, 200));
        if (await fresh(await request('session'))) return 'ready';
      }
    }
    return 'unavailable';
  };
  try {
    if (!navigator.locks) return 'unavailable';
    return await navigator.locks.request('lester-session-renewal', renew);
  } catch { return 'unavailable'; }
})()`;

export function createSessionRenewer({BrowserWindow,now=()=>Date.now()}) {
  const pending=new Map(),freshUntil=new Map();
  return async function renewSession({origin,partition}) {
    origin=validateCloudOrigin(origin);
    if(typeof partition!=='string'||!/^(?:persist:)?lester-cloud-[a-z0-9-]+$/.test(partition))throw new DesktopError('INVALID_CLOUD_ORIGIN');
    if((freshUntil.get(partition) ?? 0)>now())return {status:'ready'};
    if(pending.has(partition))return pending.get(partition);
    const operation=(async()=>{
      const helper=new BrowserWindow({show:false,width:480,height:320,webPreferences:{partition,contextIsolation:true,nodeIntegration:false,sandbox:true,webSecurity:true,backgroundThrottling:false}});
      const contents=helper.webContents;
      contents.session?.setPermissionRequestHandler((_contents,_permission,callback)=>callback(false));
      contents.session?.setPermissionCheckHandler(()=>false);
      contents.setWindowOpenHandler(()=>({action:'deny'}));
      const navigation=(event,url)=>{try{const value=new URL(url);if(value.origin!==origin||value.pathname!=='/docs')event.preventDefault();}catch{event.preventDefault();}};
      contents.on('will-navigate',navigation);contents.on('will-redirect',navigation);contents.on('will-attach-webview',event=>event.preventDefault());
      let timer;
      try {
        const status=await Promise.race([(async()=>{await helper.loadURL(`${origin}/docs`);return contents.executeJavaScript(RENEW);})(),new Promise((_,reject)=>{timer=setTimeout(()=>reject(new DesktopError('CLOUD_SESSION_UNAVAILABLE')),35000);})]);
        if(status==='expired')throw new DesktopError('CLOUD_SIGN_IN_REQUIRED','Cloud sign-in expired. Sign in again.');
        if(status!=='ready')throw new DesktopError('CLOUD_SESSION_UNAVAILABLE','Cloud sign-in could not be renewed.');
        freshUntil.set(partition,now()+30000);
        return {status:'ready'};
      } catch(error) { if(error instanceof DesktopError)throw error;throw new DesktopError('CLOUD_SESSION_UNAVAILABLE','Cloud sign-in could not be renewed.'); }
      finally { clearTimeout(timer);if(!helper.isDestroyed())helper.destroy(); }
    })();
    pending.set(partition,operation);
    try{return await operation;}finally{if(pending.get(partition)===operation)pending.delete(partition);}
  };
}
