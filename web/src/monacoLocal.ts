import type * as monacoEditor from "monaco-editor";

type MonacoEnvironment = {
  getWorker: (_workerID: string, label: string) => Worker;
};

type MonacoGlobal = typeof globalThis & {
  monaco?: typeof monacoEditor;
  MonacoEnvironment: MonacoEnvironment;
};

let setupPromise: Promise<void> | undefined;

// Monaco is ~3.6 MB, so it loads when an editor first renders instead of with
// the app shell. Editors must await this before mounting: it points
// @monaco-editor/react at the bundled copy, and the loader otherwise falls
// back to fetching Monaco from a CDN.
export function setupMonaco(): Promise<void> {
  setupPromise ??= loadMonaco();
  return setupPromise;
}

async function loadMonaco(): Promise<void> {
  const [{ loader }, monaco, { default: EditorWorker }, { default: JsonWorker }, { default: TypeScriptWorker }] =
    await Promise.all([
      import("@monaco-editor/react"),
      import("monaco-editor"),
      import("monaco-editor/esm/vs/editor/editor.worker?worker"),
      import("monaco-editor/esm/vs/language/json/json.worker?worker"),
      import("monaco-editor/esm/vs/language/typescript/ts.worker?worker")
    ]);

  (globalThis as MonacoGlobal).monaco = monaco;
  (globalThis as MonacoGlobal).MonacoEnvironment = {
    getWorker(_workerID, label) {
      if (label === "json") {
        return new JsonWorker();
      }
      if (label === "javascript" || label === "typescript") {
        return new TypeScriptWorker();
      }
      return new EditorWorker();
    }
  };

  loader.config({ monaco });
}
