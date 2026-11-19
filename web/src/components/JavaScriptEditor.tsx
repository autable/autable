import { useCallback, useEffect, useMemo } from "react";
import MonacoEditor, { useMonaco, type Monaco } from "@monaco-editor/react";
import { useTranslation } from "react-i18next";
import type { EditorExtraLib } from "../editorTypes";
import { useTheme } from "../ui";

export type JavaScriptEditorProps = {
  canWrite: boolean;
  extraLibs?: EditorExtraLib[];
  label: string;
  language?: "javascript" | "json";
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

export function JavaScriptEditor({
  canWrite,
  extraLibs = [],
  label,
  language = "javascript",
  onChange,
  path,
  testID,
  value
}: JavaScriptEditorProps) {
  const { t } = useTranslation();
  const { resolved } = useTheme();
  const monaco = useMonaco();
  const normalizedExtraLibs = useMemo(
    () => [...extraLibs].sort((left, right) => left.filePath.localeCompare(right.filePath)),
    [extraLibs]
  );

  useEffect(() => {
    if (!monaco || language !== "javascript") {
      return;
    }
    configureJavaScriptLanguage(monaco);
    installExtraLibs(monaco, normalizedExtraLibs);
  }, [language, monaco, normalizedExtraLibs]);

  const beforeMount = useCallback((nextMonaco: Monaco) => {
    defineThemes(nextMonaco);
    if (language === "javascript") {
      configureJavaScriptLanguage(nextMonaco);
      installExtraLibs(nextMonaco, normalizedExtraLibs);
    }
  }, [language, normalizedExtraLibs]);

  return (
    <div className="min-h-[24rem] min-w-0 flex-1 overflow-hidden relative rounded-container border border-border bg-elevated lg:min-h-0">
      <MonacoEditor
        beforeMount={beforeMount}
        defaultLanguage={language}
        height="100%"
        language={language}
        loading={<span className="text-13 text-muted">{t("common.loadingEditor")}</span>}
        onChange={(nextValue) => onChange(nextValue ?? "")}
        options={{
          ariaLabel: label,
          // Render suggestion/hover/parameter-hint popups with position: fixed
          // so they aren't clipped by the editor pane's overflow: hidden.
          fixedOverflowWidgets: true,
          fontFamily: 'ui-monospace, "SFMono-Regular", "Cascadia Code", Menlo, Consolas, monospace',
          fontSize: 13,
          lineNumbersMinChars: 3,
          minimap: { enabled: false },
          padding: { top: 10, bottom: 10 },
          readOnly: !canWrite,
          renderLineHighlight: "line",
          scrollBeyondLastLine: false,
          tabSize: 2,
          wordWrap: "on"
        }}
        path={path}
        theme={resolved === "dark" ? "autable-dark" : "autable-light"}
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

// Editor themes on the design tokens' surfaces, so the editor sits flush in its card.
function defineThemes(monaco: Monaco) {
  monaco.editor.defineTheme("autable-light", {
    base: "vs",
    inherit: true,
    rules: [],
    colors: {
      "editor.background": "#fdfdf8",
      "editorGutter.background": "#fdfdf8",
      "editor.lineHighlightBackground": "#00000008",
      "editorLineNumber.foreground": "#00000047"
    }
  });
  monaco.editor.defineTheme("autable-dark", {
    base: "vs-dark",
    inherit: true,
    rules: [],
    colors: {
      "editor.background": "#1f1f1f",
      "editorGutter.background": "#1f1f1f",
      "editor.lineHighlightBackground": "#ffffff0a",
      "editorLineNumber.foreground": "#ffffff4d"
    }
  });
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
