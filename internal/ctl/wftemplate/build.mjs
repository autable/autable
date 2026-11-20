// Bundles src/index.ts into dist/workflow.js, the one file autable runs.
// The server evaluates it as a script and reads globalThis.__workflow,
// which must hold the `triggers` and `run` exports: an IIFE with the
// global name __workflow does exactly that.
import { build } from "esbuild";

await build({
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
  charset: "utf8",
  sourcemap: false,
  logLevel: "info",
});
