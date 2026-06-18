import { useMemo, type FormEvent } from "react";
import { Button, Input, Text } from "@fluentui/react-components";
import { DismissRegular, SaveRegular, TabDesktopLinkRegular } from "@fluentui/react-icons";
import { useTranslation } from "react-i18next";
import type { FormDefinition, TableMetadata } from "../api";
import { formEditorExtraLibs } from "../editorTypes";
import type { FormElement, FormRenderResult } from "../formRuntime";
import { FormPreviewFields } from "./FormPreviewFields";
import { JavaScriptEditor } from "./JavaScriptEditor";

type FormWorkspaceProps = {
  databaseName: string;
  form?: FormDefinition;
  formValues: Record<string, string>;
  onFormValueChange: (name: string, value: string) => void;
  onPublish: () => void;
  onSave: () => void;
  onSubmit: (submitElement?: Extract<FormElement, { kind: "submit" }>, event?: FormEvent<HTMLFormElement>) => void | Promise<void>;
  onUnpublish: () => void;
  onUpdateScript: (script: string) => void;
  renderedForm: FormRenderResult;
  tables: TableMetadata[];
};

export function FormWorkspace({
  databaseName,
  form,
  formValues,
  onFormValueChange,
  onPublish,
  onSave,
  onSubmit,
  onUnpublish,
  onUpdateScript,
  renderedForm,
  tables
}: FormWorkspaceProps) {
  const { t } = useTranslation();
  const canWriteForm = (form?.permission_level ?? 2) >= 2;
  const publishedLink = form?.published_token ? `${window.location.origin}/forms/${form.published_token}` : "";
  const editorExtraLibs = useMemo(() => formEditorExtraLibs(), []);

  return (
    <div className="split-view">
      <div className="editor-pane form-editor-pane">
        <div className="section-header form-section-header">
          <div>
            <Text weight="semibold">{form?.name ?? t("common.form")}.js</Text>
            <Text size={200}>{databaseName} {t("common.form").toLowerCase()}</Text>
          </div>
          <div className="form-actions">
            {publishedLink ? (
              <Button icon={<DismissRegular />} onClick={onUnpublish} disabled={!canWriteForm || !form?.id}>
                {t("form.unpublish")}
              </Button>
            ) : (
              <Button icon={<TabDesktopLinkRegular />} onClick={onPublish} disabled={!canWriteForm || !form?.id}>
                {t("form.publish")}
              </Button>
            )}
            <Button icon={<SaveRegular />} appearance="primary" onClick={onSave} disabled={!canWriteForm}>
              {t("common.save")}
            </Button>
          </div>
        </div>
        <div className="form-editor-body">
          {publishedLink && (
            <Input aria-label={t("form.publishedFormLink")} readOnly value={publishedLink} />
          )}
          <JavaScriptEditor
            canWrite={canWriteForm}
            extraLibs={editorExtraLibs}
            label={t("form.formScriptLabel")}
            onChange={onUpdateScript}
            path={`form-${form?.id || "new"}.js`}
            testID="form-js-editor"
            value={form?.script ?? ""}
          />
        </div>
      </div>
      <form className="form-preview" onSubmit={(event) => onSubmit(undefined, event)}>
        <Text weight="semibold">{t("common.preview")}</Text>
        {renderedForm.error && <Text className="form-error">{renderedForm.error}</Text>}
        <FormPreviewFields
          databaseName={databaseName}
          elements={renderedForm.elements}
          formValues={formValues}
          onFormValueChange={onFormValueChange}
          onSubmit={onSubmit}
          tables={tables}
        />
      </form>
    </div>
  );
}
