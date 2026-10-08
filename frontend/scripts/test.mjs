import { execFileSync } from "node:child_process";
import { mkdtempSync, readdirSync, rmSync } from "node:fs";
import { createRequire } from "node:module";
import { tmpdir } from "node:os";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

const root = dirname(dirname(fileURLToPath(import.meta.url)));
const require = createRequire(import.meta.url);
const tests = readdirSync(join(root, "src/lib")).filter(name => name.endsWith(".test.ts"));
const output = mkdtempSync(join(tmpdir(), "lester-web-tests-"));
try {
  execFileSync(process.execPath, [require.resolve("typescript/bin/tsc"), "--module", "commonjs", "--moduleResolution", "node", "--target", "es2023", "--esModuleInterop", "--skipLibCheck", "--strict", "--outDir", output, ...tests.map(name => join(root, "src/lib", name))], { cwd: root, stdio: "inherit" });
  execFileSync(process.execPath, ["--test", ...tests.map(name => join(output, name.replace(/\.ts$/, ".js")))], { cwd: root, stdio: "inherit" });
} catch (error) {
  process.exitCode = error.status || 1;
} finally {
  rmSync(output, { recursive: true, force: true });
}
