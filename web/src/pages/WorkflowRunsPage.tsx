import { ChevronRight, Play, Workflow } from "lucide-react";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import type { AuthUser, WorkflowDefinition } from "../api";
import { WorkflowTile } from "../components/WorkflowWorkspace";
import { JsonBlock } from "../components/WorkflowRunDetail";
import { WorkflowRunDialog } from "../components/WorkflowRunDialog";
import { useOwnWorkflowRuns, type OwnRun } from "../hooks/useWorkflowRuns";
import { Page, SectionHeading } from "../shell/Page";
import { Button, Callout, Card, EmptyState, Skeleton, StatusBadge } from "../ui";
import { formatDuration, formatJSON, formatTimestamp, isActiveRun, manualTriggers, runDuration, runStatusTone } from "../workflowFormat";

/**
 * Workflows for people with a run grant: the workflows they may start, each
 * with a form built from its manual trigger, and the runs they started with
 * their status and output. Code, settings and other runs stay with admins.
 */
export function WorkflowRunsPage({
  currentUser,
  databaseName,
  workflows,
  workflowsReady
}: {
  currentUser: AuthUser | null;
  databaseName: string;
  workflows: WorkflowDefinition[];
  workflowsReady: boolean;
}) {
  const { t } = useTranslation();
  const own = useOwnWorkflowRuns(currentUser?.id, databaseName);
  const [running, setRunning] = useState<WorkflowDefinition | null>(null);
  const titles = new Map(workflows.map((workflow) => [workflow.name, workflow.title || workflow.name]));

  return (
    <Page title={t("nav.workflows")} description={t("workflow.member.description", { database: databaseName })}>
      <section aria-labelledby="member-workflows" className="mb-10">
        <SectionHeading id="member-workflows" title={t("workflow.member.available")} />
        {!workflowsReady ? (
          <div className="grid gap-3 sm:grid-cols-2">
            <Skeleton className="h-28" />
            <Skeleton className="h-28" />
          </div>
        ) : workflows.length === 0 ? (
          <Card>
            <EmptyState icon={Workflow} title={t("workflow.emptyTitle")} description={t("workflow.member.empty")} />
          </Card>
        ) : (
          <ul aria-label={t("workflow.member.list")} className="grid gap-3 sm:grid-cols-2">
            {workflows.map((workflow) => {
              const title = workflow.title || workflow.name;
              const fields = manualTriggers(workflow).flatMap((trigger) => trigger.fields ?? []);
              return (
                <li key={workflow.id}>
                  <Card className="flex h-full flex-col gap-4 p-4">
                    <div className="flex items-start gap-3">
                      <WorkflowTile size="lg" />
                      <div className="min-w-0 flex-1">
                        <p className="truncate text-14 font-medium text-fg">{title}</p>
                        <p className="truncate font-mono text-12 text-subtle">{workflow.name}</p>
                      </div>
                    </div>
                    <div className="mt-auto flex items-center justify-between gap-2">
                      <span className="truncate text-12 text-muted">
                        {fields.length > 0
                          ? t("workflow.member.inputs", { names: fields.map((field) => field.label || field.name).join(", ") })
                          : t("workflow.member.noInputs")}
                      </span>
                      <Button size="sm" aria-label={t("workflow.member.runWorkflow", { name: title })} onClick={() => setRunning(workflow)}>
                        <Play aria-hidden />
                        {t("workflow.run.action")}
                      </Button>
                    </div>
                  </Card>
                </li>
              );
            })}
          </ul>
        )}
      </section>
      <section aria-labelledby="member-runs">
        <SectionHeading id="member-runs" title={t("workflow.member.yourRuns")} />
        <Card className="overflow-hidden">
          {!own.ready && own.runs.length === 0 ? (
            <div className="flex flex-col gap-2 p-3">
              <Skeleton className="h-12" />
            </div>
          ) : own.runs.length === 0 ? (
            <p className="px-5 py-6 text-center text-13 text-muted">{t("workflow.member.noRuns")}</p>
          ) : (
            <ul aria-label={t("workflow.member.yourRuns")} className="divide-y divide-border-soft">
              {own.runs.map((run) => (
                <OwnRunRow key={`${run.workflow}:${run.id}`} run={run} title={titles.get(run.workflow) ?? run.workflow} />
              ))}
            </ul>
          )}
        </Card>
      </section>
      {running && (
        <WorkflowRunDialog
          key={running.id}
          databaseName={databaseName}
          workflow={running}
          open
          onOpenChange={(open) => !open && setRunning(null)}
          onStarted={(run) => own.remember(running.name, run)}
        />
      )}
    </Page>
  );
}

function OwnRunRow({ run, title }: { run: OwnRun; title: string }) {
  const { t } = useTranslation();
  const duration = runDuration(run);
  const hasResult = run.output !== undefined || Boolean(run.error);
  const summary = (
    <span className="flex min-w-0 flex-1 items-center gap-3">
      <span className="min-w-0 flex-1">
        <span className="flex items-center gap-2">
          <span className="truncate text-13 font-medium text-fg">{title}</span>
          <span className="text-12 tabular-nums text-subtle">#{run.id}</span>
        </span>
        <span className="block truncate text-12 text-muted">
          {formatTimestamp(run.created_at)}
          {duration !== undefined && ` · ${formatDuration(duration)}`}
        </span>
      </span>
      <StatusBadge tone={runStatusTone(run.status)} label={t(`workflow.status.${run.status}`)} pulse={isActiveRun(run.status)} />
    </span>
  );
  if (!hasResult) {
    return (
      <li aria-label={t("workflow.member.runLabel", { name: title, id: run.id })} className="flex items-center gap-3 px-4 py-3 pl-10">
        {summary}
      </li>
    );
  }
  return (
    <li aria-label={t("workflow.member.runLabel", { name: title, id: run.id })}>
      <details className="group">
        <summary className="flex cursor-pointer list-none items-center gap-3 px-4 py-3 hover:bg-hover [&::-webkit-details-marker]:hidden">
          <ChevronRight className="size-4 shrink-0 text-subtle transition-transform duration-150 group-open:rotate-90" aria-hidden />
          {summary}
        </summary>
        <div className="flex flex-col gap-3 px-4 pb-4 pl-11">
          {run.error && <Callout tone="danger">{run.error}</Callout>}
          {run.output !== undefined && <JsonBlock label={t("workflow.runs.output")} text={formatJSON(run.output)} />}
        </div>
      </details>
    </li>
  );
}
