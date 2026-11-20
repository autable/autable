import { existsSync, mkdirSync, writeFileSync } from "node:fs";
import { dirname, isAbsolute, join, relative, sep } from "node:path";
import { fileURLToPath } from "node:url";
import { defineConfig, type Plugin } from "vite";
import react from "@vitejs/plugin-react";

// `autablectl app dev` sets AUTABLE_SERVER and AUTABLE_TOKEN, so the dev
// server forwards /api to autable as you. Built apps call /api on the
// server that hosts them and need neither.
const server = process.env.AUTABLE_SERVER;
const token = process.env.AUTABLE_TOKEN;
const projectDir = dirname(fileURLToPath(import.meta.url));

export default defineConfig({
  // Relative asset URLs: autable serves the same build at the published
  // address and at every preview address.
  base: "./",
  plugins: [react(), recordBuildInputs()],
  resolve: {
    // Shared code outside this project (a lib/ of the repository) imports
    // the SDK as "@autable/app"; tsconfig.json maps it the same way.
    alias: { "@autable/app": join(projectDir, "src/autable/autable.ts") },
  },
  server: {
    // Let the dev server read shared files elsewhere in the repository.
    fs: { allow: [repositoryRoot(projectDir)] },
    proxy: server
      ? {
          "/api": {
            target: server,
            changeOrigin: true,
            headers: token ? { Authorization: `Bearer ${token}` } : {},
          },
        }
      : undefined,
  },
});

// Writes .autable/build-inputs.json: every module the build used, relative
// to this directory. `autablectl app push` uploads the files outside it (a
// shared lib/ in the same git repository) with the source; see AGENTS.md.
function recordBuildInputs(): Plugin {
  return {
    name: "autable-build-inputs",
    apply: "build",
    generateBundle() {
      const inputs = new Set<string>();
      for (const id of this.getModuleIds()) {
        const file = id.split("?")[0].replaceAll("\\", "/");
        // Skip virtual modules ("\0..."), which are not files, and
        // dependencies, which npm install restores.
        if (!id.startsWith("\0") && isAbsolute(file) && !file.includes("/node_modules/")) {
          inputs.add(relative(projectDir, file).split(sep).join("/"));
        }
      }
      mkdirSync(join(projectDir, ".autable"), { recursive: true });
      writeFileSync(join(projectDir, ".autable/build-inputs.json"), JSON.stringify({ inputs: [...inputs].sort() }, null, 2) + "\n");
    },
  };
}

// The enclosing git work tree, else this directory.
function repositoryRoot(start: string): string {
  for (let dir = start; ; dir = dirname(dir)) {
    if (existsSync(join(dir, ".git"))) return dir;
    if (dirname(dir) === dir) return start;
  }
}
