// Exercise the production standalone bundle used by the Web container.
import { spawn } from "node:child_process";
import { cpSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

const root = dirname(dirname(fileURLToPath(import.meta.url)));
cpSync(join(root, ".next/static"), join(root, ".next/standalone/.next/static"), { recursive: true });
const server = spawn(process.execPath, [join(root, ".next/standalone/server.js")], {
  cwd: root,
  env: { ...process.env, HOSTNAME: "127.0.0.1", PORT: "13020" },
  stdio: "inherit",
});
for (const signal of ["SIGINT", "SIGTERM"]) process.on(signal, () => server.kill(signal));
server.on("exit", (code) => { process.exitCode = code ?? 1; });
