import { spawnSync } from 'node:child_process';
import { mkdirSync, chmodSync, renameSync, rmSync } from 'node:fs';
import { dirname, join, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

const desktop=resolve(dirname(fileURLToPath(import.meta.url)),'..'),repository=resolve(desktop,'..');
export function buildRunner({platform=process.platform,arch=process.arch,go=process.env.LESTER_GO_BINARY || 'go'}={}) {
  const target=platform==='win32'?'linux':platform;
  if(!['darwin','linux'].includes(target)||!['x64','arm64'].includes(arch))throw new Error(`Unsupported runner target ${platform}/${arch}`);
  const output=join(desktop,'resources','runner',`lester-runner-${target}-${arch}`),temporary=output+`.${process.pid}.building`;
  mkdirSync(dirname(output),{recursive:true});
  const result=spawnSync(go,['build','-trimpath','-ldflags=-s -w','-o',temporary,'./cmd/desktop-runner'],{cwd:join(repository,'backend'),env:{...process.env,CGO_ENABLED:'0',GOOS:target,GOARCH:arch==='x64'?'amd64':'arm64'},stdio:'inherit'});
  if(result.error || result.status!==0){rmSync(temporary,{force:true});throw new Error(result.error?.message || `Runner build failed (${result.status})`);}
  chmodSync(temporary,0o755);renameSync(temporary,output);return output;
}
if(process.argv[1] && resolve(process.argv[1])===fileURLToPath(import.meta.url)) {
  const flag=name=>process.argv.find(arg=>arg.startsWith(`--${name}=`))?.split('=')[1];
  const targets=process.argv.includes('--all')?[['darwin','arm64'],['darwin','x64'],['linux','arm64'],['linux','x64']]:[[flag('platform') ?? process.platform,flag('arch') ?? process.arch]];
  try { for(const[platform,arch]of targets)console.log(buildRunner({platform,arch})); }
  catch(error){console.error(error.message);process.exitCode=1;}
}
