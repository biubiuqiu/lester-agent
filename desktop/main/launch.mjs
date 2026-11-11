import { spawn } from 'node:child_process';
import { access, constants } from 'node:fs/promises';
import { join } from 'node:path';
import { DesktopError } from './errors.mjs';
import { RunnerRPC } from './sidecar.mjs';

export function runnerName(platform,arch) { return `lester-runner-${platform}-${arch}`; }
export function wslPath(path) {
  if(typeof path!=='string'||!path||path.includes('\0'))throw new DesktopError('INVALID_LOCAL_PATH');
  const match=/^([a-z]):[\\/](.*)$/i.exec(path);
  if(!match)throw new DesktopError('WSL_PATH_UNSUPPORTED','Choose a folder on a Windows drive.');
  return `/mnt/${match[1].toLowerCase()}/${match[2].replaceAll('\\','/')}`;
}
function cleanEnvironment(env=process.env) {
  // Provider secrets belong only in RPC configureModels, never inherited env.
  const result={};
  for(const key of ['PATH','HOME','USERPROFILE','SystemRoot','SYSTEMROOT','WINDIR','TEMP','TMP','TMPDIR','LANG','LC_ALL','LOCALAPPDATA','APPDATA','WSLENV'])if(env[key])result[key]=env[key];
  return result;
}
export async function launchRunner({resources,platform=process.platform,arch=process.arch,spawnProcess=spawn,env=process.env}) {
  const target=platform==='win32'?'linux':platform;
  if(!['darwin','linux'].includes(target)||!['x64','arm64'].includes(arch))throw new DesktopError('UNSUPPORTED_PLATFORM');
  const executable=join(resources,runnerName(target,arch));
  try { await access(executable,constants.R_OK); } catch { throw new DesktopError('RUNNER_NOT_PACKAGED','The local runner is missing. Build or reinstall the desktop application.'); }
  let child;
  if(platform==='win32') {
    // WSL2 supplies Linux process/PTY semantics. Never run user shell startup files.
    child=spawnProcess('wsl.exe',['--exec',wslPath(executable)],{stdio:['pipe','pipe','pipe'],windowsHide:true,env:cleanEnvironment(env)});
  } else child=spawnProcess(executable,[],{stdio:['pipe','pipe','pipe'],windowsHide:true,env:cleanEnvironment(env)});
  return new RunnerRPC(child);
}
