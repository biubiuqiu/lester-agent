import { createHash } from 'node:crypto';
import { DesktopError } from './errors.mjs';
import { allowedCloudNavigation, clampBounds, UUID } from './policy.mjs';

export function cloudPartition(origin) { return `persist:lester-cloud-${createHash('sha256').update(origin).digest('hex').slice(0,24)}`; }
const SHELL_CSS = `
.workspace-shell { --workspace-sidebar-width:0px!important; grid-template-columns:0 minmax(0,1fr)!important; }
.workspace-shell > .conversation-sidebar { visibility:hidden!important; pointer-events:none!important; max-width:0!important; overflow:hidden!important; border:0!important; }
.workspace-shell :is(.sidebar-collapse-button,.sidebar-restore-button,.mobile-menu,.mobile-close) { display:none!important; }
@media(min-width:1001px) { .workspace-shell.workspace-panel-open { grid-template-columns:0 minmax(0,1fr) min(var(--computer-panel-width,680px),calc(100vw - 360px))!important; } }
`;

export class CloudWorkspaceView {
  constructor({window,WebContentsView,session,openExternal,getCloudState,onEvent=()=>{}}) { Object.assign(this,{window,WebContentsView,session,openExternal,getCloudState,onEvent});this.view=null;this.bounds=null;this.origin=''; }
  async open(id,bounds) {
    if(!UUID.test(id))throw new DesktopError('INVALID_CONVERSATION');
    const cloud=this.getCloudState();
    if(!cloud.origin || !cloud.signed_in)throw new DesktopError('CLOUD_LOGIN_REQUIRED');
    const origin=cloud.origin;
    if(!this.view || this.origin!==origin) {
      this.close(); this.origin=origin;
      const view=new this.WebContentsView({webPreferences:{partition:cloud.partition ?? cloudPartition(origin),contextIsolation:true,nodeIntegration:false,sandbox:true,webSecurity:true,allowRunningInsecureContent:false}});this.view=view;
      const contents=view.webContents;
      contents.session?.setPermissionRequestHandler((_contents,_permission,callback)=>callback(false));
      contents.session?.setPermissionCheckHandler(()=>false);
      // A remote workspace never receives the desktop preload or local file access.
      contents.setWindowOpenHandler(({url})=>{this.openExternal(url).catch(()=>{});return{action:'deny'};});
      const navigate=(event,url)=>{if(!allowedCloudNavigation(url,origin)){event.preventDefault();this.onEvent({type:'cloud-navigation-blocked'});}};
      contents.on('will-navigate',navigate);contents.on('will-redirect',navigate);
      contents.on('did-finish-load',()=>{if(allowedCloudNavigation(contents.getURL(),origin))contents.insertCSS(SHELL_CSS).catch(()=>{});});
      contents.on('will-attach-webview',event=>event.preventDefault());
      contents.on('did-fail-load',(_event,code)=>{if(code!==-3)this.onEvent({type:'cloud-view-error',error:{code:'CLOUD_VIEW_UNAVAILABLE',message:'The cloud conversation could not be loaded.'}});});
      this.window.contentView.addChildView(view);
    }
    this.resize(bounds);
    await this.view.webContents.loadURL(`${origin}/app/c/${id}`);
    return {opened:true};
  }
  resize(bounds) {
    if(!this.view)return{opened:false};
    const [width,height]=this.window.getContentSize();this.bounds=clampBounds(bounds,{width,height});this.view.setBounds(this.bounds);return this.bounds;
  }
  clamp() { if(this.view&&this.bounds)this.resize(this.bounds); }
  close() { if(this.view){this.window.contentView.removeChildView(this.view);if(!this.view.webContents.isDestroyed())this.view.webContents.close();this.view=null;}this.origin='';return{closed:true}; }
}
