import { spawnSync } from 'node:child_process';
import { readdirSync } from 'node:fs';
import { dirname, join, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
const desktop=resolve(dirname(fileURLToPath(import.meta.url)),'..');
const files=[];
function collect(directory){for(const entry of readdirSync(directory,{withFileTypes:true})){const path=join(directory,entry.name);if(entry.isDirectory())collect(path);else if(/\.(mjs|cjs|js)$/.test(entry.name))files.push(path);}}
for(const directory of ['main','preload','scripts'])collect(join(desktop,directory));
try{for(const entry of readdirSync(join(desktop,'renderer'))){if(/\.(mjs|cjs|js)$/.test(entry))files.push(join(desktop,'renderer',entry));}}catch{}
for(const file of files){const result=spawnSync(process.execPath,['--check',file],{stdio:'inherit'});if(result.status!==0)process.exit(result.status||1);}
console.log(`Checked ${files.length} desktop source files.`);
