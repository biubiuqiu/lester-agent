import { buildRunner } from './build-runner.mjs';
import { buildIcons } from './build-icons.mjs';
import { spawnSync } from 'node:child_process';
import { dirname, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
export default async context => {
  const desktop=resolve(dirname(fileURLToPath(import.meta.url)),'..');
  const assets=spawnSync(process.execPath,['scripts/sync-design-assets.mjs'],{cwd:desktop,stdio:'inherit'});
  if(assets.error || assets.status!==0)throw new Error('Design asset preparation failed');
  await buildIcons();
  const arch=typeof context.arch==='string'?context.arch:({1:'x64',3:'arm64'})[context.arch];
  buildRunner({platform:context.electronPlatformName,arch});
};
