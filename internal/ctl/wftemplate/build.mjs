// Bundles src/index.ts into dist/workflow.js, the one file autable runs.
// The server evaluates it as a script and reads globalThis.__workflow,
// which must hold the `triggers` and `run` exports: an IIFE with the
// global name __workflow does exactly that.
import { mkdirSync, writeFileSync } from "node:fs";
import { build } from "esbuild";

// Imports of autable libraries ("@lib/<name>") are not bundled: each becomes
// a call of __autable_lib("@lib/<name>"), which loads the library's
// published version when the workflow runs. Their types are vendored in
// src/autable/libs/ by `autablectl workflow sdk`.
const autableLibraries = {
  name: "autable-libraries",
  setup(build) {
    build.onResolve({ filter: /^@lib\// }, (args) => ({ path: args.path, namespace: "autable-lib" }));
    build.onLoad({ filter: /.*/, namespace: "autable-lib" }, (args) => ({
      contents: `module.exports = __autable_lib(${JSON.stringify(args.path)});`,
      loader: "js",
    }));
  },
};

const result = await build({
  entryPoints: ["src/index.ts"],
  outfile: "dist/workflow.js",
  bundle: true,
  format: "iife",
  globalName: "__workflow",
  // Not Node: prefer packages' browser builds, which avoid Node built-ins.
  platform: "browser",
  target: "es2022",
  // The runtime (goja) has no async iteration; esbuild rewrites it.
  supported: { "async-generator": false, "for-await": false },
  // Shared code outside this project (a lib/ of the repository) imports
  // the SDK as "@autable/workflow"; tsconfig.json maps it the same way.
  alias: { "@autable/workflow": "./src/autable/index.ts" },
  plugins: [autableLibraries],
  charset: "utf8",
  sourcemap: false,
  metafile: true,
  logLevel: "info",
});

// Every file the bundle was built from, relative to this directory.
// `autablectl workflow push` uploads the files outside it (a shared lib/
// in the same git repository) with the source; see AGENTS.md.
const inputs = Object.keys(result.metafile.inputs).filter((input) => !/^[\w-]{2,}:/.test(input));
mkdirSync(".autable", { recursive: true });
writeFileSync(".autable/build-inputs.json", JSON.stringify({ inputs }, null, 2) + "\n");
