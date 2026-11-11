import { mkdir, open, readFile, rename, chmod } from 'node:fs/promises';
import { join } from 'node:path';
import { randomUUID } from 'node:crypto';
import { DesktopError } from './errors.mjs';
import { boundedObject, object } from './policy.mjs';

const PRIVATE = /(^|_)(credential|api_key|access_token|refresh_token|password|authorization|cookie|token|secret)($|_)/i;
function publicSetting(value) {
  if (Array.isArray(value)) return value.every(publicSetting);
  if (!object(value)) return true;
  return Object.entries(value).every(([key, child]) => !PRIVATE.test(key) && publicSetting(child));
}
export class DesktopStore {
  constructor(directory, safeStorage) { this.directory=directory; this.file=join(directory,'desktop-settings.json'); this.safe=safeStorage; this.data={version:1,settings:{},secrets:{}}; this.memory=new Map(); this.writes=Promise.resolve(); }
  async load() {
    await mkdir(this.directory,{recursive:true,mode:0o700}); await chmod(this.directory,0o700);
    try {
      const content=await readFile(this.file,'utf8'); if(Buffer.byteLength(content)>2<<20) throw new Error();
      const value=JSON.parse(content); if(value.version!==1 || !object(value.settings) || !object(value.secrets)) throw new Error();
      this.data=value; await chmod(this.file,0o600);
    } catch(error) { if(error.code!=='ENOENT') throw new DesktopError('SETTINGS_UNREADABLE','Desktop settings could not be opened. Existing files were preserved.'); }
    if(!this.data.settings.deviceId) { this.data.settings.deviceId=randomUUID(); await this.save(); }
    return this;
  }
  secureAvailable() { return this.safe.isEncryptionAvailable() && this.safe.getSelectedStorageBackend?.()!=='basic_text'; }
  getSetting(key,fallback=null) { return Object.hasOwn(this.data.settings,key) ? structuredClone(this.data.settings[key]) : fallback; }
  async setSetting(key,value) {
    if(typeof key!=='string'||!key||PRIVATE.test(key)||!publicSetting(value)) throw new DesktopError('PRIVATE_SETTING_REJECTED','Use encrypted storage for credentials.');
    boundedObject({value}); this.data.settings[key]=structuredClone(value); await this.save();
  }
  getSecret(key) {
    if(this.memory.has(key)) return this.memory.get(key);
    const encoded=this.data.secrets[key]; if(!encoded||!this.secureAvailable()) return null;
    try { return this.safe.decryptString(Buffer.from(encoded,'base64')); } catch { return null; }
  }
  async setSecret(key,value) {
    if(typeof key!=='string'||!key||typeof value!=='string'||value.length>65536) throw new DesktopError('INVALID_SECRET');
    this.memory.set(key,value);
    if(!this.secureAvailable()) { delete this.data.secrets[key]; await this.save(); return {persisted:false}; }
    this.data.secrets[key]=this.safe.encryptString(value).toString('base64'); await this.save(); return {persisted:true};
  }
  async deleteSecret(key) { this.memory.delete(key); delete this.data.secrets[key]; await this.save(); }
  save() {
    const snapshot=JSON.stringify(this.data);
    const commit=async()=>{
      const temporary=join(this.directory,'.settings-'+randomUUID()+'.tmp');
      const handle=await open(temporary,'wx',0o600);
      try { await handle.writeFile(snapshot); await handle.sync(); } finally { await handle.close(); }
      await rename(temporary,this.file); await chmod(this.file,0o600);
    };
    const next=this.writes.then(commit,commit); this.writes=next.catch(()=>{}); return next;
  }
}
