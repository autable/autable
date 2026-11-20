import { ExternalLink, Play } from "lucide-react";
import { useEffect, useState, type FormEvent } from "react";
import { useTranslation } from "react-i18next";
import { startWorkflowRun, type WorkflowDefinition, type WorkflowManualTrigger, type WorkflowRun } from "../api";
import { Button, Callout, Checkbox, Dialog, DialogContent, Field, Input, NativeSelect, StatusBadge, Textarea } from "../ui";
import {
  formatJSON,
  initialManualValues,
  isActiveRun,
  manualInputFromForm,
  manualInputFromJSON,
  manualTriggers,
  runStatusTone,
  type ManualFormValues
} from "../workflowFormat";
import { JsonBlock } from "./WorkflowRunDetail";

type WorkflowRunDialogProps = {
  databaseName: string;
  workflow: WorkflowDefinition;
  open: boolean;
  onOpenChange: (open: boolean) => void;
  /** Called with every run started from the dialog. */
  onStarted?: (run: WorkflowRun) => void;
  /** Admins can jump from the result to the run's page. */
  onOpenRun?: (run: WorkflowRun) => void;
};

/**
 * Starts a workflow's manual trigger. The form is built from the trigger's
 * declared fields; a trigger without fields takes a JSON object. The dialog
 * waits for the run (the server waits up to 60 s) and shows its result.
 */
export function WorkflowRunDialog({ databaseName, workflow, open, onOpenChange, onStarted, onOpenRun }: WorkflowRunDialogProps) {
  const { t } = useTranslation();
  const triggers = manualTriggers(workflow);
  const [triggerName, setTriggerName] = useState(triggers[0]?.name ?? "");
  const trigger = triggers.find((item) => item.name === triggerName) ?? triggers[0];
  const fields = trigger?.fields ?? [];
  const [values, setValues] = useState<ManualFormValues>(() => initialManualValues(fields));
  const [jsonText, setJSONText] = useState("{}");
  const [errors, setErrors] = useState<Record<string, string>>({});
  const [running, setRunning] = useState(false);
  const [failure, setFailure] = useState("");
  const [result, setResult] = useState<{ run: WorkflowRun; finished: boolean } | null>(null);

  useEffect(() => {
    if (open) {
      setResult(null);
      setFailure("");
      setErrors({});
    }
  }, [open]);

  function selectTrigger(name: string) {
    setTriggerName(name);
    const next = triggers.find((item) => item.name === name);
    setValues(initialManualValues(next?.fields ?? []));
    setErrors({});
    setResult(null);
  }

  async function submit(event: FormEvent) {
    event.preventDefault();
    if (!trigger) {
      return;
    }
    let input: Record<string, unknown>;
    if (fields.length > 0) {
      const built = manualInputFromForm(fields, values);
      if (Object.keys(built.errors).length > 0) {
        setErrors(Object.fromEntries(Object.keys(built.errors).map((name) => [name, t("workflow.run.numberRequired")])));
        return;
      }
      input = built.input;
    } else {
      const parsed = manualInputFromJSON(jsonText);
      if (!parsed.input) {
        setErrors({ $json: t(parsed.error === "json" ? "workflow.run.invalidJSON" : "workflow.run.objectRequired") });
        return;
      }
      input = parsed.input;
    }
    setErrors({});
    setFailure("");
    setResult(null);
    setRunning(true);
    try {
      const started = await startWorkflowRun(databaseName, workflow.name, {
        trigger: triggers.length > 1 ? trigger.name : undefined,
        input,
        wait: true
      });
      setResult(started);
      onStarted?.(started.run);
    } catch (runError) {
      setFailure(runError instanceof Error ? runError.message : t("status.workflowRunFailed"));
    } finally {
      setRunning(false);
    }
  }

  const title = workflow.title || workflow.name;
  const formID = `workflow-run-${workflow.id}`;
  return (
    <Dialog open={open} onOpenChange={(next) => !running && onOpenChange(next)}>
      <DialogContent
        title={t("workflow.run.title", { name: title })}
        description={t("workflow.run.description")}
        className="max-w-lg"
        footer={
          <>
            <Button onClick={() => onOpenChange(false)} disabled={running}>
              {t("common.close")}
            </Button>
            <Button type="submit" form={formID} variant="cta" loading={running} disabled={!trigger}>
              <Play aria-hidden />
              {result ? t("workflow.run.again") : t("workflow.run.start")}
            </Button>
          </>
        }
      >
        <form id={formID} aria-label={t("workflow.run.form")} className="flex flex-col gap-4" onSubmit={(event) => void submit(event)}>
          {!trigger && <Callout tone="warning">{t("workflow.run.noManualTrigger")}</Callout>}
          {triggers.length > 1 && (
            <Field label={t("workflow.run.trigger")}>
              {(field) => (
                <NativeSelect {...field} value={trigger?.name ?? ""} onChange={(event) => selectTrigger(event.target.value)}>
                  {triggers.map((item) => (
                    <option key={item.name} value={item.name}>
                      {item.name}
                    </option>
                  ))}
                </NativeSelect>
              )}
            </Field>
          )}
          {trigger && fields.length > 0 && <ManualFields trigger={trigger} values={values} errors={errors} onChange={setValues} />}
          {trigger && fields.length === 0 && (
            <Field label={t("workflow.run.input")} hint={t("workflow.run.inputHint")} error={errors.$json}>
              {(field) => (
                <Textarea
                  {...field}
                  value={jsonText}
                  spellCheck={false}
                  className="min-h-28 font-mono text-12"
                  onChange={(event) => setJSONText(event.target.value)}
                />
              )}
            </Field>
          )}
        </form>
        {failure && (
          <Callout tone="danger" className="mt-4" title={t("workflow.run.startFailed")}>
            {failure}
          </Callout>
        )}
        {result && <RunResult result={result} onOpenRun={onOpenRun} />}
      </DialogContent>
    </Dialog>
  );
}

function ManualFields({
  trigger,
  values,
  errors,
  onChange
}: {
  trigger: WorkflowManualTrigger;
  values: ManualFormValues;
  errors: Record<string, string>;
  onChange: (values: ManualFormValues) => void;
}) {
  return (
    <>
      {(trigger.fields ?? []).map((item) => {
        const label = item.label || item.name;
        const value = values[item.name];
        if (item.type === "boolean") {
          return (
            <Checkbox
              key={item.name}
              label={label}
              checked={value === true}
              onChange={(event) => onChange({ ...values, [item.name]: event.target.checked })}
            />
          );
        }
        return (
          <Field key={item.name} label={label} error={errors[item.name]}>
            {(field) => (
              <Input
                {...field}
                inputMode={item.type === "number" ? "decimal" : undefined}
                value={typeof value === "string" ? value : ""}
                onChange={(event) => onChange({ ...values, [item.name]: event.target.value })}
              />
            )}
          </Field>
        );
      })}
    </>
  );
}

function RunResult({ result, onOpenRun }: { result: { run: WorkflowRun; finished: boolean }; onOpenRun?: (run: WorkflowRun) => void }) {
  const { t } = useTranslation();
  const { run } = result;
  return (
    <section aria-label={t("workflow.run.result")} className="mt-5 flex flex-col gap-3 border-t border-border-soft pt-4">
      <div className="flex items-center justify-between gap-3">
        <span className="flex items-center gap-2 text-13 font-medium text-fg">
          {t("workflow.runs.runLabel", { id: run.id })}
          <StatusBadge tone={runStatusTone(run.status)} label={t(`workflow.status.${run.status}`)} pulse={isActiveRun(run.status)} />
        </span>
        {onOpenRun && (
          <Button size="sm" variant="quiet" onClick={() => onOpenRun(run)}>
            <ExternalLink aria-hidden />
            {t("workflow.run.openRun")}
          </Button>
        )}
      </div>
      {isActiveRun(run.status) && <p className="text-13 text-muted">{t("workflow.run.stillRunning")}</p>}
      {run.error && <Callout tone="danger">{run.error}</Callout>}
      {run.output !== undefined && <JsonBlock label={t("workflow.runs.output")} text={formatJSON(run.output)} />}
      {!run.error && run.output === undefined && !isActiveRun(run.status) && (
        <p className="text-13 text-muted">{t("workflow.runs.noOutput")}</p>
      )}
    </section>
  );
}
