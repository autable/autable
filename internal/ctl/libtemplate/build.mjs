// Builds the library: dist/lib.js, which autable evaluates when a workflow
// imports "@lib/<name>" (an IIFE assigning __lib, the module's exports),
// and dist/index.d.ts, the type declarations `autablectl workflow sdk`
// vendors into importing projects.
import { existsSync, mkdirSync, readFileSync, writeFileSync } from "node:fs";
import { dirname, join, relative, resolve, sep } from "node:path";
import { build } from "esbuild";

const { name } = JSON.parse(readFileSync("autable.lib.json", "utf8"));

// Imports of other libraries ("@lib/<name>") are not bundled: each becomes
// a call of __autable_lib("@lib/<name>"), which loads that library's
// published version at run time.
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
  outfile: "dist/lib.js",
  bundle: true,
  format: "iife",
  globalName: "__lib",
  platform: "browser",
  target: "es2022",
  // The runtime (goja) has no async iteration; esbuild rewrites it.
  supported: { "async-generator": false, "for-await": false },
  alias: { "@autable/workflow": "./src/autable/index.ts" },
  plugins: [autableLibraries],
  charset: "utf8",
  sourcemap: false,
  metafile: true,
  logLevel: "info",
});

// Every file the bundle was built from, relative to this directory, for
// `autablectl lib push` (shared files outside the project; see AGENTS.md).
const inputs = Object.keys(result.metafile.inputs).filter((input) => !/^[\w-]{2,}:/.test(input));
mkdirSync(".autable", { recursive: true });
writeFileSync(".autable/build-inputs.json", JSON.stringify({ inputs }, null, 2) + "\n");

// tsc wrote one declaration file per module to .autable/types. Join the
// ones src/index.ts reaches into dist/index.d.ts: index becomes
// declare module "@lib/<name>", every other file an internal module whose
// relative imports are rewritten to it.
const typesDir = resolve(".autable/types");
const moduleName = `@lib/${name}`;
const moduleId = (file) => {
  const rel = relative(typesDir, file).split(sep).join("/").replace(/\.d\.ts$/, "");
  return rel === "index" ? moduleName : `${moduleName}/__internal/${rel}`;
};
const resolveTypes = (from, specifier) => {
  const base = resolve(dirname(from), specifier.replace(/\.(m?js|ts)$/, ""));
  const found = [`${base}.d.ts`, join(base, "index.d.ts")].find((candidate) => existsSync(candidate));
  if (!found) throw new Error(`types: cannot resolve ${specifier} from ${relative(typesDir, from)}`);
  return found;
};
const blocks = [];
const queue = [join(typesDir, "index.d.ts")];
const seen = new Set();
while (queue.length > 0) {
  const file = queue.shift();
  if (seen.has(file)) continue;
  seen.add(file);
  const text = readFileSync(file, "utf8")
    .replace(/(\bfrom\s*|\bimport\s*\(\s*|\bimport\s+)(["'])(\.{1,2}\/[^"']*)\2/g, (_, prefix, quote, specifier) => {
      const target = resolveTypes(file, specifier);
      queue.push(target);
      return `${prefix}${quote}${moduleId(target)}${quote}`;
    })
    // Declarations inside declare module are ambient already.
    .replace(/^(export )?declare /gm, "$1")
    .replace(/^\/\/# sourceMappingURL=.*$/gm, "")
    .trim();
  blocks.push(`declare module "${moduleId(file)}" {\n${text}\n}\n`);
}
writeFileSync("dist/index.d.ts", `// Type declarations of the autable library ${moduleName}.\n\n${blocks.join("\n")}`);
