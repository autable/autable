import { Suspense, lazy } from "react";
import { useTranslation } from "react-i18next";
import type { JavaScriptEditorProps } from "./JavaScriptEditor";

// Monaco is only needed by the workflow editor, so it is fetched on
// first render rather than shipped in the app bundle. setupMonaco resolves
// before the editor mounts so the bundled copy wins over the CDN loader.
const MonacoBackedEditor = lazy(async () => {
  const [{ setupMonaco }, editorModule] = await Promise.all([
    import("../monacoLocal"),
    import("./JavaScriptEditor")
  ]);
  await setupMonaco();
  return { default: editorModule.JavaScriptEditor };
});

export function JavaScriptEditor(props: JavaScriptEditorProps) {
  const { t } = useTranslation();
  return (
    <Suspense
      fallback={
        <div className="flex min-h-[24rem] flex-1 items-center justify-center rounded-container border border-border bg-elevated text-13 text-muted lg:min-h-0">
          {t("common.loadingEditor")}
        </div>
      }
    >
      <MonacoBackedEditor {...props} />
    </Suspense>
  );
}
