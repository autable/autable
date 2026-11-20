import { ArrowLeft, ChevronRight, Download, KeyRound, Play, RefreshCw, Rocket, Trash2, Undo2, Variable, Workflow } from "lucide-react";
import { useEffect, useRef, useState, type FormEvent, type ReactNode } from "react";
import { useTranslation } from "react-i18next";
import {
  workflowSourceURL,
  type AuthUser,
  type WorkflowDefinition,
  type WorkflowDetail,
  type WorkflowRun,
  type WorkflowRunStatus,
  type WorkflowSettings,
  type WorkflowVersion
} from "../api";
import type { WorkflowTab } from "../appState";
import { useWorkflowRun, useWorkflowRuns } from "../hooks/useWorkflowRuns";
import { normalizeLanguage } from "../i18n";
import { Page, SectionHeading } from "../shell/Page";
import { RouteLink, useNavigate } from "../shell/navigation";
import {
  Badge,
  Button,
  Callout,
  Card,
  cn,
  ConfirmDialog,
  Dialog,
  DialogClose,
  DialogContent,
  EmptyState,
  Field,
  Input,
  NativeSelect,
  Skeleton,
  StatusBadge,
  StatusDot,
  Switch,
  Table,
  Tabs,
  TabsList,
  TabsTrigger,
  TBody,
  Td,
  Th,
  THead,
  Tooltip,
  Tr
} from "../ui";
import {
  formatDuration,
  formatRelativeTime,
  formatTimestamp,
  isActiveRun,
  manualTriggers,
  runDuration,
  runStatuses,
  runStatusTone
} from "../workflowFormat";
import { formatSize, PublishedStatus } from "./AppWorkspace";
import { WorkflowRunDetail } from "./WorkflowRunDetail";
import { WorkflowRunDialog } from "./WorkflowRunDialog";
import { TriggerCards, TriggerIcons, TriggerSummaryList } from "./WorkflowTriggers";

export type WorkflowWorkspaceProps = {
  busy: boolean;
  currentUser: AuthUser | null;
  databaseName: string;
  detail: WorkflowDetail | null;
  workflows: WorkflowDefinition[];
  workflowsReady: boolean;
  /** The workflow, tab and run named by the route. */
  workflowName: string;
  tab: WorkflowTab;
  runID?: number;
  onDelete: () => Promise<boolean>;
  onPublish: (version: number) => Promise<boolean>;
  onRemoveSecret: (key: string) => Promise<boolean>;
  onRemoveVar: (key: string) => Promise<boolean>;
  onRunStarted: () => void;
  onSaveSecret: (key: string, value: string) => Promise<boolean>;
  onSaveSettings: (settings: Pick<WorkflowSettings, "timeout_seconds" | "history_retention_days">) => Promise<boolean>;
  onSaveVar: (key: string, value: string) => Promise<boolean>;
  onSetEnabled: (enabled: boolean) => Promise<boolean>;
};

/** Admin workflows: the list of a database's workflows, or one workflow's page. */
export function WorkflowWorkspace(props: WorkflowWorkspaceProps) {
  if (props.workflowName) {
    return props.detail && props.detail.workflow.name === props.workflowName ? <WorkflowDetailView {...props} detail={props.detail} /> : <WorkflowLoading />;
  }
  if (!props.workflowsReady) {
    return <WorkflowLoading />;
  }
  return <WorkflowListView databaseName={props.databaseName} workflows={props.workflows} />;
}

function WorkflowLoading() {
  const { t } = useTranslation();
  return (
    <Page title={t("nav.workflows")}>
      <div role="status" aria-label={t("workflow.loading")} className="flex flex-col gap-3">
        <Skeleton className="h-11" />
        <Skeleton className="h-11" />
        <Skeleton className="h-11" />
      </div>
    </Page>
  );
}

/** A square tile with the workflow icon, used wherever a workflow is listed. */
export function WorkflowTile({ size = "md" }: { size?: "md" | "lg" }) {
  return (
    <span
      aria-hidden
      className={cn("flex shrink-0 items-center justify-center rounded-inner bg-chip text-description", size === "lg" ? "size-10" : "size-8")}
    >
      <Workflow className={size === "lg" ? "size-5" : "size-4"} strokeWidth={1.75} />
    </span>
  );
}

export function EnabledStatus({ workflow }: { workflow: Pick<WorkflowDefinition, "enabled"> }) {
  const { t } = useTranslation();
  return workflow.enabled ? (
    <StatusBadge tone="success" label={t("workflow.enabled")} />
  ) : (
    <StatusBadge tone="neutral" label={t("workflow.disabled")} />
  );
}

export function LastRun({ run }: { run?: WorkflowRun }) {
  const { t, i18n } = useTranslation();
  if (!run) {
    return <span className="text-13 text-subtle">{t("workflow.neverRun")}</span>;
  }
  const language = normalizeLanguage(i18n.resolvedLanguage ?? i18n.language);
  return (
    <span className="inline-flex items-center gap-2 whitespace-nowrap text-13 text-description" title={formatTimestamp(run.created_at)}>
      <StatusDot tone={runStatusTone(run.status)} pulse={isActiveRun(run.status)} label={t(`workflow.status.${run.status}`)} />
      <span>{t(`workflow.status.${run.status}`)}</span>
      <span className="text-muted">{formatRelativeTime(run.created_at, language)}</span>
    </span>
  );
}

function WorkflowListView({ databaseName, workflows }: { databaseName: string; workflows: WorkflowDefinition[] }) {
  const { t } = useTranslation();
  return (
    <Page width="wide" title={t("nav.workflows")} description={t("workflow.listLabel", { database: databaseName })}>
      {workflows.length === 0 ? (
        <Card>
          <EmptyState
            icon={Workflow}
            title={t("workflow.emptyTitle")}
            description={t("workflow.emptyDescription")}
            action={<WorkflowCommandHelp databaseName={databaseName} />}
          />
        </Card>
      ) : (
        <>
          <Card className="overflow-hidden">
            <Table aria-label={t("workflow.table")}>
              <THead>
                <tr>
                  <Th>{t("workflow.columns.workflow")}</Th>
                  <Th>{t("workflow.columns.status")}</Th>
                  <Th className="hidden md:table-cell">{t("workflow.columns.triggers")}</Th>
                  <Th className="hidden lg:table-cell">{t("workflow.columns.lastRun")}</Th>
                  <Th className="w-10" />
                </tr>
              </THead>
              <TBody>
                {workflows.map((workflow) => (
                  <Tr key={workflow.id} className="relative" aria-label={workflow.name}>
                    <Td className="py-2.5">
                      <span className="flex min-w-0 items-center gap-3">
                        <WorkflowTile />
                        <span className="min-w-0">
                          <RouteLink
                            route={{ databaseName, view: "workflow", workflowName: workflow.name }}
                            className="block truncate text-14 font-medium text-fg after:absolute after:inset-0"
                          >
                            {workflow.title || workflow.name}
                          </RouteLink>
                          <code className="block truncate font-mono text-12 text-muted">{workflow.name}</code>
                          <TriggerIcons triggers={workflow.triggers} className="mt-1 md:hidden" />
                        </span>
                      </span>
                    </Td>
                    <Td>
                      <span className="flex flex-col gap-1">
                        <EnabledStatus workflow={workflow} />
                        <span className="text-12 text-muted">
                          {workflow.published_version > 0 ? t("app.versionLabel", { version: workflow.published_version }) : t("app.notPublished")}
                        </span>
                      </span>
                    </Td>
                    <Td className="hidden max-w-[22rem] py-2.5 md:table-cell">
                      <TriggerSummaryList triggers={workflow.triggers} />
                    </Td>
                    <Td className="hidden lg:table-cell">
                      <LastRun run={workflow.last_run} />
                    </Td>
                    <Td className="text-right">
                      <ChevronRight className="ml-auto size-4 text-subtle" aria-hidden />
                    </Td>
                  </Tr>
                ))}
              </TBody>
            </Table>
          </Card>
          <div className="mt-8">
            <SectionHeading title={t("workflow.howToTitle")} />
            <Card className="px-5 py-4">
              <WorkflowCommandHelp databaseName={databaseName} />
            </Card>
          </div>
        </>
      )}
    </Page>
  );
}

function WorkflowCommandHelp({ databaseName }: { databaseName: string }) {
  const { t } = useTranslation();
  return (
    <div className="flex w-full max-w-xl flex-col gap-2 text-left">
      <CommandStep label={t("workflow.stepInit")} command={`autablectl workflow init my-workflow --database ${databaseName} --name my-workflow`} />
      <CommandStep label={t("workflow.stepPush")} command={"cd my-workflow\nautablectl workflow push --publish"} />
      <CommandStep label={t("workflow.skillHint")} command="autablectl skill install" />
      <CommandStep label={t("workflow.skillExportHint")} command="autablectl skill export ./autable-skill" />
    </div>
  );
}

function CommandStep({ label, command }: { label: string; command: string }) {
  return (
    <div className="flex flex-col gap-1">
      <p className="text-13 text-muted">{label}</p>
      <pre className="overflow-x-auto rounded-inner bg-chip px-3 py-2 font-mono text-12 leading-relaxed text-fg">{command}</pre>
    </div>
  );
}

type DetailProps = WorkflowWorkspaceProps & { detail: WorkflowDetail };

function WorkflowDetailView(props: DetailProps) {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const { busy, databaseName, detail, tab } = props;
  const { workflow } = detail;
  const [runOpen, setRunOpen] = useState(false);
  const runnable = workflow.published_version > 0 && manualTriggers(workflow).length > 0;
  const route = (nextTab: WorkflowTab, runID?: number) => ({
    databaseName,
    view: "workflow" as const,
    workflowName: workflow.name,
    workflowTab: nextTab,
    workflowRunID: runID
  });

  return (
    <Page
      width="wide"
      title={workflow.title || workflow.name}
      description={t("workflow.detailLabel", { name: workflow.name, database: databaseName })}
      icon={<Workflow />}
      back={
        <Button asChild variant="quiet" size="sm" className="-ml-2.5">
          <RouteLink route={{ databaseName, view: "workflow" }}>
            <ArrowLeft aria-hidden />
            {t("workflow.backToList")}
          </RouteLink>
        </Button>
      }
      actions={
        <>
          <ConfirmDialog
            trigger={
              <Button variant="danger-quiet" disabled={busy}>
                <Trash2 aria-hidden />
                {t("workflow.delete")}
              </Button>
            }
            title={t("workflow.deleteTitle")}
            description={t("workflow.deleteConfirm", { name: workflow.title || workflow.name })}
            confirmLabel={t("common.delete")}
            onConfirm={async () => {
              if (await props.onDelete()) {
                navigate({ databaseName, view: "workflow" });
              }
            }}
          />
          <Tooltip content={runnable ? t("workflow.run.tooltip") : t("workflow.run.unavailable")}>
            <span tabIndex={runnable ? undefined : 0}>
              <Button variant="cta" disabled={!runnable} onClick={() => setRunOpen(true)}>
                <Play aria-hidden />
                {t("workflow.run.action")}
              </Button>
            </span>
          </Tooltip>
        </>
      }
    >
      <Tabs value={tab} onValueChange={(value) => navigate(route(value as WorkflowTab))} className="mb-6">
        <TabsList aria-label={t("workflow.sections")} className="max-w-full overflow-x-auto">
          <TabsTrigger value="overview">{t("workflow.tabs.overview")}</TabsTrigger>
          <TabsTrigger value="runs">{t("workflow.tabs.runs")}</TabsTrigger>
          <TabsTrigger value="versions">{t("workflow.tabs.versions")}</TabsTrigger>
          <TabsTrigger value="config">{t("workflow.tabs.config")}</TabsTrigger>
        </TabsList>
      </Tabs>
      {tab === "overview" && <OverviewTab {...props} onRun={runnable ? () => setRunOpen(true) : undefined} />}
      {tab === "runs" && (
        <RunsTab
          currentUser={props.currentUser}
          databaseName={databaseName}
          workflow={workflow}
          runID={props.runID}
          onSelectRun={(id) => navigate(route("runs", id))}
        />
      )}
      {tab === "versions" && <VersionsTab {...props} />}
      {tab === "config" && <ConfigTab {...props} />}
      {runnable && (
        <WorkflowRunDialog
          key={`${workflow.id}-${workflow.published_version}`}
          databaseName={databaseName}
          workflow={workflow}
          open={runOpen}
          onOpenChange={setRunOpen}
          onStarted={() => props.onRunStarted()}
          onOpenRun={(run) => {
            setRunOpen(false);
            navigate(route("runs", run.id));
          }}
        />
      )}
    </Page>
  );
}

function SideItem({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div className="flex min-h-9 items-center justify-between gap-3 px-4 py-2">
      <dt className="text-13 text-muted">{label}</dt>
      <dd className="min-w-0 text-right text-13 text-fg">{children}</dd>
    </div>
  );
}

function OverviewTab({ busy, databaseName, detail, onRun, onSaveSettings, onSetEnabled }: DetailProps & { onRun?: () => void }) {
  const { t } = useTranslation();
  const { workflow } = detail;
  const latest = Math.max(0, ...detail.versions.map((version) => version.version));
  return (
    <div className="grid items-start gap-6 lg:grid-cols-[minmax(0,1fr)_19rem]">
      <section aria-labelledby="workflow-triggers" className="min-w-0">
        <SectionHeading id="workflow-triggers" title={t("workflow.trigger.title")} />
        {workflow.published_version === 0 ? (
          <Callout tone="info" title={t("workflow.unpublishedTitle")}>
            {t("workflow.unpublishedDescription")}
          </Callout>
        ) : workflow.triggers.length === 0 ? (
          <Card className="px-5 py-6 text-center text-13 text-muted">{t("workflow.trigger.noneDeclared")}</Card>
        ) : (
          <TriggerCards triggers={workflow.triggers} onRun={onRun} />
        )}
        {!workflow.enabled && workflow.published_version > 0 && <p className="mt-3 text-12 text-muted">{t("workflow.disabledHint")}</p>}
      </section>
      <div className="flex min-w-0 flex-col gap-6">
        <section aria-labelledby="workflow-status">
          <SectionHeading id="workflow-status" title={t("workflow.statusTitle")} />
          <Card>
            <div className="flex items-start justify-between gap-3 px-4 py-3">
              <span className="min-w-0">
                <label htmlFor="workflow-enabled" className="block text-13 font-medium text-fg">
                  {t("workflow.enabled")}
                </label>
                <span className="block text-12 text-muted">{t("workflow.enabledHint")}</span>
              </span>
              <Switch id="workflow-enabled" checked={workflow.enabled} disabled={busy} onCheckedChange={(checked) => void onSetEnabled(checked)} />
            </div>
            <dl className="divide-y divide-border-soft border-t border-border-soft">
              <SideItem label={t("app.publishedVersion")}>
                <PublishedStatus version={workflow.published_version} />
              </SideItem>
              <SideItem label={t("app.latestVersion")}>{latest ? t("app.versionLabel", { version: latest }) : t("app.none")}</SideItem>
              <SideItem label={t("workflow.columns.lastRun")}>
                {workflow.last_run ? (
                  <RouteLink
                    route={{ databaseName, view: "workflow", workflowName: workflow.name, workflowTab: "runs", workflowRunID: workflow.last_run.id }}
                    className="rounded-inner hover:underline"
                  >
                    <LastRun run={workflow.last_run} />
                  </RouteLink>
                ) : (
                  <LastRun />
                )}
              </SideItem>
            </dl>
          </Card>
        </section>
        <SettingsCard key={`${workflow.timeout_seconds}-${workflow.history_retention_days}`} busy={busy} workflow={workflow} onSave={onSaveSettings} />
      </div>
    </div>
  );
}

function SettingsCard({ busy, workflow, onSave }: { busy: boolean; workflow: WorkflowDefinition; onSave: DetailProps["onSaveSettings"] }) {
  const { t } = useTranslation();
  const [timeout, setTimeoutValue] = useState(String(workflow.timeout_seconds ?? ""));
  const [retention, setRetention] = useState(String(workflow.history_retention_days ?? ""));
  const [errors, setErrors] = useState<{ timeout?: string; retention?: string }>({});
  const dirty = timeout !== String(workflow.timeout_seconds ?? "") || retention !== String(workflow.history_retention_days ?? "");

  function submit(event: FormEvent) {
    event.preventDefault();
    const timeoutSeconds = Number(timeout);
    const retentionDays = Number(retention);
    const nextErrors: typeof errors = {};
    if (!Number.isInteger(timeoutSeconds) || timeoutSeconds < 1 || timeoutSeconds > 86_400) {
      nextErrors.timeout = t("workflow.settings.timeoutRange");
    }
    if (!Number.isInteger(retentionDays) || retentionDays < 1 || retentionDays > 3650) {
      nextErrors.retention = t("workflow.settings.retentionRange");
    }
    setErrors(nextErrors);
    if (Object.keys(nextErrors).length === 0) {
      void onSave({ timeout_seconds: timeoutSeconds, history_retention_days: retentionDays });
    }
  }

  const timeoutMs = Number(timeout) * 1000;
  return (
    <section aria-labelledby="workflow-settings">
      <SectionHeading id="workflow-settings" title={t("workflow.settings.title")} />
      <Card>
        <form noValidate className="flex flex-col gap-4 px-4 py-4" onSubmit={submit} aria-label={t("workflow.settings.title")}>
          <Field
            label={t("workflow.settings.timeout")}
            hint={Number.isFinite(timeoutMs) && timeoutMs > 0 ? t("workflow.settings.timeoutHint", { duration: formatDuration(timeoutMs) }) : undefined}
            error={errors.timeout}
          >
            {(field) => (
              <Input {...field} inputSize="sm" type="number" min={1} max={86400} value={timeout} onChange={(event) => setTimeoutValue(event.target.value)} />
            )}
          </Field>
          <Field label={t("workflow.settings.retention")} hint={t("workflow.settings.retentionHint")} error={errors.retention}>
            {(field) => (
              <Input {...field} inputSize="sm" type="number" min={1} max={3650} value={retention} onChange={(event) => setRetention(event.target.value)} />
            )}
          </Field>
          <div className="flex justify-end">
            <Button type="submit" size="sm" variant="primary" disabled={!dirty || busy}>
              {t("workflow.settings.save")}
            </Button>
          </div>
        </form>
      </Card>
    </section>
  );
}

function RunsTab({
  currentUser,
  databaseName,
  workflow,
  runID,
  onSelectRun
}: {
  currentUser: AuthUser | null;
  databaseName: string;
  workflow: WorkflowDefinition;
  runID?: number;
  onSelectRun: (id: number) => void;
}) {
  const { t } = useTranslation();
  const runs = useWorkflowRuns(databaseName, workflow.name);
  const selected = useWorkflowRun(databaseName, workflow.name, runID);
  const detailRef = useRef<HTMLDivElement>(null);
  const lastRunID = workflow.last_run?.id;
  const refreshRuns = runs.refresh;

  // A run started from the Run dialog shows up without a reload.
  useEffect(() => {
    if (lastRunID && runs.ready && !runs.runs.some((run) => run.id === lastRunID)) {
      void refreshRuns();
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [lastRunID]);

  // A selected run that changed state updates its row in the list too.
  const selectedRun = selected.run;
  useEffect(() => {
    if (selectedRun && runs.runs.some((run) => run.id === selectedRun.id && run.status !== selectedRun.status)) {
      void refreshRuns();
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [selectedRun?.status]);

  return (
    <div className="grid items-start gap-6 xl:grid-cols-[minmax(0,24rem)_minmax(0,1fr)]">
      <section aria-labelledby="workflow-runs" className="min-w-0">
        <SectionHeading
          id="workflow-runs"
          title={t("workflow.runs.title")}
          action={
            <span className="flex items-center gap-1">
              <NativeSelect
                selectSize="sm"
                aria-label={t("workflow.runs.filter")}
                value={runs.status}
                className="w-36"
                onChange={(event) => runs.setStatus(event.target.value as WorkflowRunStatus | "")}
              >
                <option value="">{t("workflow.runs.allStatuses")}</option>
                {runStatuses.map((status) => (
                  <option key={status} value={status}>
                    {t(`workflow.status.${status}`)}
                  </option>
                ))}
              </NativeSelect>
              <Tooltip content={t("workflow.runs.refresh")}>
                <Button variant="quiet" size="icon-sm" aria-label={t("workflow.runs.refresh")} onClick={() => void runs.refresh()}>
                  <RefreshCw />
                </Button>
              </Tooltip>
            </span>
          }
        />
        <Card className="overflow-hidden">
          {runs.error && (
            <Callout tone="danger" className="m-3">
              {runs.error}
            </Callout>
          )}
          {!runs.ready ? (
            <div role="status" aria-label={t("workflow.runs.loading")} className="flex flex-col gap-2 p-3">
              <Skeleton className="h-12" />
              <Skeleton className="h-12" />
              <Skeleton className="h-12" />
            </div>
          ) : runs.runs.length === 0 ? (
            <EmptyState
              className="py-12"
              title={runs.status ? t("workflow.runs.noneWithStatus") : t("workflow.runs.none")}
              description={runs.status ? undefined : t("workflow.runs.noneDescription")}
            />
          ) : (
            <ul aria-label={t("workflow.runs.list")} className="divide-y divide-border-soft">
              {runs.runs.map((run) => (
                <RunListItem
                  key={run.id}
                  run={run}
                  selected={run.id === runID}
                  onSelect={() => {
                    onSelectRun(run.id);
                    window.requestAnimationFrame(() => {
                      if (window.matchMedia?.("(max-width: 1279px)").matches) {
                        detailRef.current?.scrollIntoView({ behavior: "smooth", block: "start" });
                      }
                    });
                  }}
                />
              ))}
            </ul>
          )}
          {runs.hasMore && (
            <div className="border-t border-border-soft p-2">
              <Button variant="quiet" size="sm" className="w-full" loading={runs.loadingMore} onClick={() => void runs.loadMore()}>
                {t("workflow.runs.loadMore")}
              </Button>
            </div>
          )}
        </Card>
      </section>
      <div ref={detailRef} className="min-w-0 scroll-mt-6 xl:sticky xl:top-6">
        {runID ? (
          <WorkflowRunDetail
            run={selected.run}
            error={selected.error}
            currentUser={currentUser}
            cancelling={selected.cancelling}
            onCancel={() => {
              void selected
                .cancel()
                .then(() => runs.refresh())
                .catch(() => undefined);
            }}
          />
        ) : (
          <Card className="hidden xl:block">
            <EmptyState className="py-20" title={t("workflow.runs.selectTitle")} description={t("workflow.runs.selectDescription")} />
          </Card>
        )}
      </div>
    </div>
  );
}

function RunListItem({ run, selected, onSelect }: { run: WorkflowRun; selected: boolean; onSelect: () => void }) {
  const { t } = useTranslation();
  const duration = runDuration(run);
  const label = t(`workflow.status.${run.status}`);
  return (
    <li>
      <button
        type="button"
        aria-current={selected ? "true" : undefined}
        aria-label={t("workflow.runs.itemLabel", { id: run.id, status: label })}
        onClick={onSelect}
        className={cn("flex w-full items-center gap-3 px-4 py-2.5 text-left transition-colors duration-150", selected ? "bg-chip" : "hover:bg-hover")}
      >
        <StatusDot tone={runStatusTone(run.status)} pulse={isActiveRun(run.status)} label={label} />
        <span className="min-w-0 flex-1">
          <span className="flex items-center gap-2">
            <span className="text-13 font-medium tabular-nums text-fg">#{run.id}</span>
            <code className="truncate font-mono text-12 text-description">{run.trigger}</code>
          </span>
          <span className="block truncate text-12 text-muted">
            {formatTimestamp(run.started_at || run.created_at)}
            {duration !== undefined && ` · ${formatDuration(duration)}`}
          </span>
        </span>
        <span className="shrink-0 text-12 text-muted">{label}</span>
      </button>
    </li>
  );
}

function VersionsTab({ busy, currentUser, databaseName, detail, onPublish }: DetailProps) {
  const { t } = useTranslation();
  const [rollback, setRollback] = useState<number | null>(null);
  const { workflow, sdk } = detail;
  const versions = [...detail.versions].sort((left, right) => right.version - left.version);

  function requestPublish(version: WorkflowVersion) {
    if (workflow.published_version > version.version) {
      setRollback(version.version);
      return;
    }
    void onPublish(version.version);
  }

  return (
    <section aria-labelledby="workflow-versions-heading" className="min-w-0">
      <SectionHeading
        id="workflow-versions-heading"
        title={t("app.versions")}
        action={<span className="text-12 text-subtle">{t("workflow.serverSDK", { version: sdk.version, min: sdk.min_major })}</span>}
      />
      <Card className="overflow-hidden">
        {versions.length === 0 ? (
          <p className="px-5 py-8 text-center text-13 text-muted">{t("app.noVersions")}</p>
        ) : (
          <Table aria-label={t("app.versionTable")}>
            <THead>
              <tr>
                <Th>{t("app.version")}</Th>
                <Th>{t("app.message")}</Th>
                <Th className="hidden lg:table-cell">{t("app.sdk")}</Th>
                <Th className="hidden lg:table-cell">{t("app.uploadedBy")}</Th>
                <Th className="hidden md:table-cell">{t("app.uploadedAt")}</Th>
                <Th className="hidden lg:table-cell">{t("app.size")}</Th>
                <Th className="text-right">
                  <span className="sr-only">{t("app.actions")}</span>
                </Th>
              </tr>
            </THead>
            <TBody>
              {versions.map((version) => {
                const published = version.version === workflow.published_version;
                const older = workflow.published_version > version.version;
                const source = t("app.downloadSourceVersion", { version: version.version });
                return (
                  <Tr key={version.version} aria-label={t("app.versionLabel", { version: version.version })}>
                    <Td className="whitespace-nowrap">
                      <span className="flex items-center gap-2">
                        <span className={cn("tabular-nums", published ? "font-medium text-fg" : "text-description")}>
                          {t("app.versionLabel", { version: version.version })}
                        </span>
                        {published && <StatusBadge tone="success" label={t("app.published")} />}
                      </span>
                    </Td>
                    <Td className="min-w-48 max-w-96">
                      {version.message ? <span className="line-clamp-2">{version.message}</span> : <span className="text-subtle">{t("app.noMessage")}</span>}
                    </Td>
                    <Td className="hidden whitespace-nowrap lg:table-cell">
                      <span className="flex items-center gap-2">
                        <code className="font-mono text-12">{version.sdk}</code>
                        {!version.supported && (
                          <Tooltip content={version.sdk_error ?? t("app.unsupported")}>
                            <Badge tabIndex={0} className="bg-error-bg text-error">
                              {t("app.unsupported")}
                            </Badge>
                          </Tooltip>
                        )}
                      </span>
                    </Td>
                    <Td className="hidden max-w-40 lg:table-cell">
                      <span className="block truncate text-description" title={version.created_by}>
                        {version.created_by === currentUser?.id ? currentUser.display_name : version.created_by}
                      </span>
                    </Td>
                    <Td className="hidden whitespace-nowrap text-muted md:table-cell">{formatTimestamp(version.created_at)}</Td>
                    <Td className="hidden whitespace-nowrap tabular-nums text-muted lg:table-cell">{formatSize(version.size)}</Td>
                    <Td className="whitespace-nowrap text-right">
                      <span className="inline-flex items-center justify-end gap-0.5">
                        <Tooltip content={source}>
                          <Button asChild variant="quiet" size="icon-sm">
                            <a href={workflowSourceURL(databaseName, workflow.name, version.version)} download aria-label={source}>
                              <Download aria-hidden />
                            </a>
                          </Button>
                        </Tooltip>
                        {!published && (
                          <Button
                            size="sm"
                            variant={older ? "secondary" : "primary"}
                            className="ml-1"
                            disabled={busy || !version.supported}
                            aria-label={t(older ? "app.rollbackVersion" : "app.publishVersion", { version: version.version })}
                            onClick={() => requestPublish(version)}
                          >
                            {older ? <Undo2 aria-hidden /> : <Rocket aria-hidden />}
                            {t(older ? "app.rollback" : "app.publish")}
                          </Button>
                        )}
                      </span>
                    </Td>
                  </Tr>
                );
              })}
            </TBody>
          </Table>
        )}
      </Card>
      <Dialog open={rollback !== null} onOpenChange={(open) => !open && setRollback(null)}>
        <DialogContent
          title={t("workflow.rollbackTitle")}
          description={t("workflow.rollbackConfirm", { current: workflow.published_version, version: rollback ?? 0 })}
          footer={
            <>
              <DialogClose asChild>
                <Button>{t("common.cancel")}</Button>
              </DialogClose>
              <Button
                variant="cta"
                onClick={() => {
                  if (rollback !== null) {
                    void onPublish(rollback);
                  }
                  setRollback(null);
                }}
              >
                {t("app.rollback")}
              </Button>
            </>
          }
        />
      </Dialog>
    </section>
  );
}

const keyPattern = /^[A-Za-z_][A-Za-z0-9_.-]{0,127}$/;

function ConfigTab({ busy, detail, onRemoveSecret, onRemoveVar, onSaveSecret, onSaveVar }: DetailProps) {
  return (
    <div className="grid items-start gap-6 lg:grid-cols-2">
      <SecretsCard busy={busy} secrets={detail.secrets} onRemove={onRemoveSecret} onSave={onSaveSecret} />
      <VarsCard busy={busy} vars={detail.vars} onRemove={onRemoveVar} onSave={onSaveVar} />
    </div>
  );
}

function SecretsCard({
  busy,
  secrets,
  onRemove,
  onSave
}: {
  busy: boolean;
  secrets: string[];
  onRemove: (key: string) => Promise<boolean>;
  onSave: (key: string, value: string) => Promise<boolean>;
}) {
  const { t } = useTranslation();
  const [name, setName] = useState("");
  const [value, setValue] = useState("");
  const [error, setError] = useState("");
  const valueRef = useRef<HTMLInputElement>(null);
  const replacing = secrets.includes(name.trim());

  async function submit(event: FormEvent) {
    event.preventDefault();
    const key = name.trim();
    if (!keyPattern.test(key)) {
      setError(t("workflow.config.invalidName"));
      return;
    }
    setError("");
    if (await onSave(key, value)) {
      setName("");
      setValue("");
    }
  }

  return (
    <section aria-labelledby="workflow-secrets">
      <SectionHeading id="workflow-secrets" title={t("workflow.config.secrets")} />
      <Card>
        <p className="px-4 pt-3 text-12 text-muted">{t("workflow.config.secretsHint")}</p>
        {secrets.length === 0 ? (
          <p className="px-4 py-4 text-13 text-subtle">{t("workflow.config.noSecrets")}</p>
        ) : (
          <ul aria-label={t("workflow.config.secrets")} className="mt-2 divide-y divide-border-soft border-y border-border-soft">
            {secrets.map((secret) => (
              <li key={secret} className="flex items-center gap-3 px-4 py-2">
                <KeyRound className="size-3.5 shrink-0 text-muted" aria-hidden />
                <code className="min-w-0 flex-1 truncate font-mono text-12 text-fg">{secret}</code>
                <span className="font-mono text-12 tracking-widest text-subtle" aria-hidden>
                  ••••••
                </span>
                <Button
                  size="sm"
                  variant="quiet"
                  aria-label={t("workflow.config.replaceSecret", { name: secret })}
                  onClick={() => {
                    setName(secret);
                    setValue("");
                    valueRef.current?.focus();
                  }}
                >
                  {t("workflow.config.replace")}
                </Button>
                <ConfirmDialog
                  trigger={
                    <Button size="icon-sm" variant="quiet" aria-label={t("workflow.config.deleteSecret", { name: secret })} disabled={busy}>
                      <Trash2 />
                    </Button>
                  }
                  title={t("workflow.config.deleteSecretTitle")}
                  description={t("workflow.config.deleteSecretConfirm", { name: secret })}
                  confirmLabel={t("common.delete")}
                  onConfirm={() => onRemove(secret)}
                />
              </li>
            ))}
          </ul>
        )}
        <form className="flex flex-col gap-3 px-4 py-4" aria-label={t("workflow.config.setSecret")} onSubmit={(event) => void submit(event)}>
          <div className="grid gap-3 sm:grid-cols-2">
            <Field label={t("workflow.config.secretName")} error={error}>
              {(field) => (
                <Input
                  {...field}
                  inputSize="sm"
                  value={name}
                  autoComplete="off"
                  spellCheck={false}
                  placeholder="API_TOKEN"
                  onChange={(event) => setName(event.target.value)}
                />
              )}
            </Field>
            <Field label={t("workflow.config.secretValue")}>
              {(field) => (
                <Input
                  {...field}
                  ref={valueRef}
                  inputSize="sm"
                  type="password"
                  autoComplete="new-password"
                  value={value}
                  onChange={(event) => setValue(event.target.value)}
                />
              )}
            </Field>
          </div>
          <div className="flex justify-end">
            <Button type="submit" size="sm" variant="primary" disabled={busy || !name.trim()}>
              <KeyRound aria-hidden />
              {replacing ? t("workflow.config.replaceSecretAction") : t("workflow.config.setSecret")}
            </Button>
          </div>
        </form>
      </Card>
    </section>
  );
}

function VarsCard({
  busy,
  vars,
  onRemove,
  onSave
}: {
  busy: boolean;
  vars: Record<string, string>;
  onRemove: (key: string) => Promise<boolean>;
  onSave: (key: string, value: string) => Promise<boolean>;
}) {
  const { t } = useTranslation();
  const names = Object.keys(vars).sort();
  const [drafts, setDrafts] = useState<Record<string, string>>({});
  const [name, setName] = useState("");
  const [value, setValue] = useState("");
  const [error, setError] = useState("");

  async function add(event: FormEvent) {
    event.preventDefault();
    const key = name.trim();
    if (!keyPattern.test(key)) {
      setError(t("workflow.config.invalidName"));
      return;
    }
    setError("");
    if (await onSave(key, value)) {
      setName("");
      setValue("");
    }
  }

  return (
    <section aria-labelledby="workflow-vars">
      <SectionHeading id="workflow-vars" title={t("workflow.config.vars")} />
      <Card>
        <p className="px-4 pt-3 text-12 text-muted">{t("workflow.config.varsHint")}</p>
        {names.length === 0 ? (
          <p className="px-4 py-4 text-13 text-subtle">{t("workflow.config.noVars")}</p>
        ) : (
          <ul aria-label={t("workflow.config.vars")} className="mt-2 divide-y divide-border-soft border-y border-border-soft">
            {names.map((key) => {
              const draft = drafts[key] ?? vars[key];
              const changed = draft !== vars[key];
              return (
                <li key={key} className="flex flex-wrap items-center gap-2 px-4 py-2 sm:flex-nowrap">
                  <Variable className="size-3.5 shrink-0 text-muted" aria-hidden />
                  <code className="w-32 shrink-0 truncate font-mono text-12 text-fg" title={key}>
                    {key}
                  </code>
                  <Input
                    inputSize="sm"
                    aria-label={t("workflow.config.varValue", { name: key })}
                    className="min-w-0 flex-1 font-mono text-12"
                    value={draft}
                    onChange={(event) => setDrafts((current) => ({ ...current, [key]: event.target.value }))}
                  />
                  <Button
                    size="sm"
                    variant="primary"
                    disabled={!changed || busy}
                    aria-label={t("workflow.config.saveVar", { name: key })}
                    onClick={() =>
                      void onSave(key, draft).then((saved) => {
                        if (saved) {
                          setDrafts(({ [key]: _saved, ...rest }) => rest);
                        }
                      })
                    }
                  >
                    {t("common.save")}
                  </Button>
                  <ConfirmDialog
                    trigger={
                      <Button size="icon-sm" variant="quiet" aria-label={t("workflow.config.deleteVar", { name: key })} disabled={busy}>
                        <Trash2 />
                      </Button>
                    }
                    title={t("workflow.config.deleteVarTitle")}
                    description={t("workflow.config.deleteVarConfirm", { name: key })}
                    confirmLabel={t("common.delete")}
                    onConfirm={() => onRemove(key)}
                  />
                </li>
              );
            })}
          </ul>
        )}
        <form className="flex flex-col gap-3 px-4 py-4" aria-label={t("workflow.config.addVar")} onSubmit={(event) => void add(event)}>
          <div className="grid gap-3 sm:grid-cols-2">
            <Field label={t("workflow.config.varName")} error={error}>
              {(field) => (
                <Input
                  {...field}
                  inputSize="sm"
                  value={name}
                  autoComplete="off"
                  spellCheck={false}
                  placeholder="REGION"
                  onChange={(event) => setName(event.target.value)}
                />
              )}
            </Field>
            <Field label={t("workflow.config.varValueLabel")}>
              {(field) => <Input {...field} inputSize="sm" value={value} onChange={(event) => setValue(event.target.value)} />}
            </Field>
          </div>
          <div className="flex justify-end">
            <Button type="submit" size="sm" variant="primary" disabled={busy || !name.trim()}>
              <Variable aria-hidden />
              {t("workflow.config.addVar")}
            </Button>
          </div>
        </form>
      </Card>
    </section>
  );
}
