import { useMemo, type FormEvent } from "react";
import { Button, Input, Select, Text } from "@fluentui/react-components";
import { DismissRegular, SaveRegular, TabDesktopLinkRegular } from "@fluentui/react-icons";
import type { FormDefinition } from "../api";
import { formEditorExtraLibs } from "../editorTypes";
import type { FormElement, FormRenderResult } from "../formRuntime";
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
  renderedForm
}: FormWorkspaceProps) {
  const canWriteForm = (form?.permission_level ?? 2) >= 2;
  const publishedLink = form?.published_token ? `${window.location.origin}/forms/${form.published_token}` : "";
  const editorExtraLibs = useMemo(() => formEditorExtraLibs(), []);

  return (
    <div className="split-view">
      <div className="editor-pane">
        <div className="section-header">
          <div>
            <Text weight="semibold">{form?.name ?? "form"}.js</Text>
            <Text size={200}>{databaseName} form</Text>
          </div>
          <div className="form-actions">
            {publishedLink ? (
              <Button icon={<DismissRegular />} onClick={onUnpublish} disabled={!canWriteForm || !form?.id}>
                Unpublish
              </Button>
            ) : (
              <Button icon={<TabDesktopLinkRegular />} onClick={onPublish} disabled={!canWriteForm || !form?.id}>
                Publish
              </Button>
            )}
            <Button icon={<SaveRegular />} appearance="primary" onClick={onSave} disabled={!canWriteForm}>
              Save
            </Button>
          </div>
        </div>
        <div className="form-editor-body">
          {publishedLink && (
            <Input aria-label="Published form link" readOnly value={publishedLink} />
          )}
          <JavaScriptEditor
            canWrite={canWriteForm}
            extraLibs={editorExtraLibs}
            label="Form JavaScript"
            onChange={onUpdateScript}
            path={`form-${form?.id || "new"}.js`}
            testID="form-js-editor"
            value={form?.script ?? ""}
          />
        </div>
      </div>
      <form className="form-preview" onSubmit={(event) => onSubmit(undefined, event)}>
        <Text weight="semibold">Preview</Text>
        {renderedForm.error && <Text className="form-error">{renderedForm.error}</Text>}
        {renderedForm.elements.map((element) => {
          if (element.kind === "input") {
            return (
              <label key={element.name} className="field-stack">
                <span>{element.label}</span>
                <Input
                  type={element.inputType}
                  value={formValues[element.name] ?? ""}
                  onChange={(_, data) => onFormValueChange(element.name, data.value)}
                />
              </label>
            );
          }
          if (element.kind === "select") {
            return (
              <label key={element.name} className="field-stack">
                <span>{element.label}</span>
                <Select
                  value={formValues[element.name] ?? element.options[0] ?? ""}
                  onChange={(_, data) => onFormValueChange(element.name, data.value)}
                >
                  {element.options.map((option) => (
                    <option key={option}>{option}</option>
                  ))}
                </Select>
              </label>
            );
          }
          if (element.kind === "html") {
            return <div key={element.html} className="form-html" dangerouslySetInnerHTML={{ __html: element.html }} />;
          }
          return (
            <Button key={element.label} type="button" appearance="primary" onClick={() => void onSubmit(element)}>
              {element.label}
            </Button>
          );
        })}
      </form>
    </div>
  );
}
