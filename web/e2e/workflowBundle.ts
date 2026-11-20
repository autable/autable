import { tarGzip } from "./appBundle";

// Builds the gzip-compressed tar that `autablectl workflow push` uploads (see
// docs/design/workflows-v2.md): the manifest, dist/workflow.js assigning
// globalThis.__workflow = { triggers, run }, and the project source. Specs
// create workflows through the real API with it, without the CLI or esbuild.

export type WorkflowBundleOptions = {
  database: string;
  name: string;
  title: string;
  // Returned in every run's output, so a spec can tell versions apart.
  release: string;
  // Trigger declarations as the SDK helpers produce them
  // (docs/design/workflows-host-api.md); a manual trigger by default.
  triggers?: Record<string, unknown>;
  sdk?: string;
};

export const defaultManualTrigger = {
  kind: "manual",
  fields: [
    { name: "note", label: "Note", type: "string" },
    { name: "count", label: "Count", type: "number" },
    { name: "urgent", label: "Urgent", type: "boolean" }
  ]
};

export function workflowBundle({ database, name, title, release, triggers = { start: defaultManualTrigger }, sdk = "1.0" }: WorkflowBundleOptions): Buffer {
  const manifest = JSON.stringify({ database, name, title, sdk });
  // The run logs through console and the host log, reads a secret and a var,
  // and returns its input; host calls only happen inside run, because they
  // throw while the server reads the triggers on publish.
  const script = [
    "globalThis.__workflow = {",
    `  triggers: ${JSON.stringify(triggers)},`,
    "  run: async (event) => {",
    "    const host = globalThis.__autable_host;",
    '    console.log("starting run", event.runId);',
    '    host.log("warn", "checked input", { keys: Object.keys(event.input || {}) });',
    // An input "fail" makes the run throw, for specs that need a failed run.
    "    if (event.input && event.input.fail) {",
    '      host.log("error", "giving up", { reason: event.input.fail });',
    "      throw new Error(String(event.input.fail));",
    "    }",
    "    return {",
    "      ok: true,",
    `      release: ${JSON.stringify(release)},`,
    "      trigger: event.trigger,",
    "      input: event.input,",
    '      hasToken: host.secret("API_TOKEN") !== undefined,',
    '      region: host.variable("REGION") || null',
    "    };",
    "  }",
    "};",
    ""
  ].join("\n");
  return tarGzip({
    "autable.workflow.json": manifest,
    "dist/workflow.js": script,
    "source/autable.workflow.json": manifest,
    "source/src/index.ts": `// ${title}\nexport const release = ${JSON.stringify(release)};\n`
  });
}
