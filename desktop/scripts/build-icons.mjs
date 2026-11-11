import { createHash } from 'node:crypto';
import { access, copyFile, mkdir, mkdtemp, readFile, rm, writeFile } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { dirname, join, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
import { runIconsTool } from 'app-builder-lib/out/toolsets/icons.js';

const desktop=resolve(dirname(fileURLToPath(import.meta.url)),'..');

export async function buildIcons() {
  const source=resolve(desktop,'../frontend/src/app/icon.svg'),output=join(desktop,'build');
  const digest=createHash('sha256').update(await readFile(source)).digest('hex')+'\n';
  const receipt=join(output,'icon-source.sha256');
  const files=['icon.icns','icon.ico','icon.png'];
  try {
    if(await readFile(receipt,'utf8')===digest) {
      await Promise.all(files.map(file=>access(join(output,file))));
      return;
    }
  } catch { /* Generate missing or outdated assets from the canonical mark. */ }
  const temporary=await mkdtemp(join(tmpdir(),'lester-icons-'));
  try {
    await mkdir(output,{recursive:true});
    // electron-builder pins and verifies the converter bundle's SHA-256.
    // Its SVG renderer produces the platform formats without native macOS tools.
    for(const format of ['icns','ico','set']) {
      const directory=join(temporary,format);
      await mkdir(directory);
      await runIconsTool({inputFile:source,outputFormat:format,outDir:directory});
    }
    for(const format of ['icns','ico'])await copyFile(join(temporary,format,`icon.${format}`),join(output,`icon.${format}`));
    await copyFile(join(temporary,'set','512x512.png'),join(output,'icon.png'));
    await writeFile(receipt,digest);
  } finally { await rm(temporary,{recursive:true,force:true}); }
}

if(process.argv[1] && resolve(process.argv[1])===fileURLToPath(import.meta.url)) {
  await buildIcons();
  console.log('Prepared desktop icons from frontend/src/app/icon.svg.');
}
