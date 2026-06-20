import { spawn } from "node:child_process";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { copyFileSync, mkdirSync, rmSync, writeFileSync } from "node:fs";

const e2eDir = dirname(fileURLToPath(import.meta.url));
const webDir = resolve(e2eDir, "..");
const rootDir = resolve(webDir, "..");
const runtimeDir = join(e2eDir, ".runtime");
const repositoryPath = join(runtimeDir, "workspace");
const metadataPath = join(repositoryPath, "metadata", "main.yml");
const configPath = join(runtimeDir, "config.yml");

rmSync(runtimeDir, { force: true, recursive: true });
mkdirSync(join(runtimeDir, "data"), { recursive: true });
mkdirSync(join(repositoryPath, "metadata"), { recursive: true });
copyFileSync(join(e2eDir, "fixtures", "metadata.yml"), metadataPath);
writeFileSync(
  configPath,
  [
    "server:",
    '  address: "127.0.0.1:18081"',
    "data:",
    `  path: "${join(runtimeDir, "data")}"`,
    "repository:",
    `  path: "${repositoryPath}"`,
    "oidc:",
    "  providers: []",
    ""
  ].join("\n")
);

const child = spawn(
  "go",
  ["run", "./cmd/autable", "-config", configPath],
  {
    cwd: rootDir,
    env: { ...process.env, GOTOOLCHAIN: "local" },
    stdio: "inherit"
  }
);

const shutdown = () => {
  child.kill("SIGTERM");
};

process.on("SIGINT", shutdown);
process.on("SIGTERM", shutdown);
child.on("exit", (code, signal) => {
  if (signal) {
    process.exit(0);
  }
  process.exit(code ?? 0);
});
