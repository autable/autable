import { useCallback, useEffect, useMemo } from "react";
import MonacoEditor, { useMonaco, type Monaco } from "@monaco-editor/react";
import type { EditorExtraLib } from "../editorTypes";

type JavaScriptEditorProps = {
  canWrite: boolean;
  extraLibs?: EditorExtraLib[];
  label: string;
  onChange: (script: string) => void;
  path: string;
  testID: string;
  value: string;
};

type Disposable = {
  dispose: () => void;
};

const configuredMonacos = new WeakSet<object>();
const extraLibDisposables = new Map<string, { content: string; disposable: Disposable }>();

export function JavaScriptEditor({ canWrite, extraLibs = [], label, onChange, path, testID, value }: JavaScriptEditorProps) {
  const monaco = useMonaco();
  const normalizedExtraLibs = useMemo(
    () => [...extraLibs].sort((left, right) => left.filePath.localeCompare(right.filePath)),
    [extraLibs]
  );

  useEffect(() => {
    if (!monaco) {
      return;
    }
    configureJavaScriptLanguage(monaco);
    installExtraLibs(monaco, normalizedExtraLibs);
  }, [monaco, normalizedExtraLibs]);

  const beforeMount = useCallback((nextMonaco: Monaco) => {
    configureJavaScriptLanguage(nextMonaco);
    installExtraLibs(nextMonaco, normalizedExtraLibs);
  }, [normalizedExtraLibs]);

  return (
    <div className="javascript-editor-shell">
      <MonacoEditor
        beforeMount={beforeMount}
        className="javascript-editor"
        defaultLanguage="javascript"
        height="100%"
        language="javascript"
        loading={<span className="flow-empty">Loading editor</span>}
        onChange={(nextValue) => onChange(nextValue ?? "")}
        options={{
          ariaLabel: label,
          fontFamily: '"SFMono-Regular", Consolas, "Liberation Mono", monospace',
          fontSize: 13,
          lineNumbersMinChars: 3,
          minimap: { enabled: false },
          readOnly: !canWrite,
          renderLineHighlight: "line",
          scrollBeyondLastLine: false,
          tabSize: 2,
          wordWrap: "on"
        }}
        path={path}
        theme="light"
        value={value}
        wrapperProps={{
          "aria-disabled": String(!canWrite),
          "aria-label": label,
          "data-testid": testID,
          role: "group"
        }}
        width="100%"
      />
    </div>
  );
}

function configureJavaScriptLanguage(monaco: Monaco) {
  if (configuredMonacos.has(monaco)) {
    return;
  }
  configuredMonacos.add(monaco);
  const defaults = monaco.languages.typescript.javascriptDefaults;
  defaults.setCompilerOptions({
    ...defaults.getCompilerOptions(),
    allowJs: true,
    allowNonTsExtensions: true,
    checkJs: true,
    noEmit: true,
    target: monaco.languages.typescript.ScriptTarget.ES2020
  });
  defaults.setDiagnosticsOptions({
    noSemanticValidation: false,
    noSyntaxValidation: false,
    noSuggestionDiagnostics: false
  });
}

function installExtraLibs(monaco: Monaco, extraLibs: EditorExtraLib[]) {
  for (const extraLib of extraLibs) {
    const existing = extraLibDisposables.get(extraLib.filePath);
    if (existing?.content === extraLib.content) {
      continue;
    }
    existing?.disposable.dispose();
    extraLibDisposables.set(extraLib.filePath, {
      content: extraLib.content,
      disposable: monaco.languages.typescript.javascriptDefaults.addExtraLib(extraLib.content, extraLib.filePath)
    });
  }
}
