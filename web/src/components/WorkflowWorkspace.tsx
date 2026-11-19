import { BookOpen, History, Pencil, Play, Workflow, Zap } from "lucide-react";
import { useEffect, useMemo, useState, type ReactNode } from "react";
import { useTranslation } from "react-i18next";
import { fetchRunners } from "../api";
import type {
  RunnersResponse,
  WorkflowDefinition,
  WorkflowInstanceDeclaration,
  WorkflowNodeInfo,
  WorkflowPort,
  WorkflowRunResponse
} from "../api";
import { workflowEditorExtraLibs } from "../editorTypes";
import type { WorkflowTriggerDeclaration } from "../workflowInstances";
import {
  Badge,
  Button,
  Card,
  cn,
  Dialog,
  DialogContent,
  DialogTrigger,
  EmptyState,
  Field,
  Input,
  Label,
  NativeSelect,
  Popover,
  PopoverContent,
  PopoverTrigger,
  StatusDot,
  Switch,
  Tabs,
  TabsList,
  TabsTrigger
} from "../ui";
import { JavaScriptEditor } from "./JavaScriptEditorLazy";

type WorkflowWorkspaceProps = {
  activeTab: WorkflowTab;
  databaseName: string;
  language: string;
  onExecute: () => void;
  onSave: () => void;
  onSaveInstanceConfig: (
    instanceID: string,
    variables: Record<string, string>,
    secrets: Record<string, string>,
    runnerName: string
  ) => void | Promise<void>;
  onSelectTab: (tab: WorkflowTab) => void;
  onSelectRunKey: (historyKey: string) => void;
  onSetHistoryRetention: (days: number | null) => void;
  onSetTimeoutSeconds: (seconds: number | null) => void;
  onToggleEnabled: (enabled: boolean) => void;
  onUpdateScript: (script: string) => void;
  selectedRun: WorkflowRunResponse | null;
  workflow?: WorkflowDefinition;
  workflowInstances:
    | { ok: true; value: Record<string, WorkflowInstanceDeclaration> }
    | { ok: false; error: string };
  workflowTrigger?: WorkflowTriggerDeclaration;
  workflowNodes: WorkflowNodeInfo[];
  workflowRuns: WorkflowRunResponse[];
};

export type WorkflowTab = "editor" | "history";

type WorkflowRunListItem = {
  id: string;
  title: string;
  subtitle?: string;
  input?: Record<string, unknown>;
  output?: Record<string, unknown>;
  error?: string;
};

/**
 * The workflow page (admins): the script in Monaco on the left, settings and
 * the declared instances on the right; History shows past runs, the steps of
 * the selected run and each step's input and output.
 */
export function WorkflowWorkspace({
  activeTab,
  databaseName,
  language,
  onExecute,
  onSave,
  onSaveInstanceConfig,
  onSelectTab,
  onSelectRunKey,
  onSetHistoryRetention,
  onSetTimeoutSeconds,
  onToggleEnabled,
  onUpdateScript,
  selectedRun,
  workflow,
  workflowInstances,
  workflowTrigger,
  workflowNodes,
  workflowRuns
}: WorkflowWorkspaceProps) {
  const { t } = useTranslation();
  const canWriteWorkflow = (workflow?.permission_level ?? 2) >= 2;
  const enabled = workflow?.enabled ?? true;
  const workflowNodesByType = new Map(workflowNodes.map((node) => [node.type, node]));
  const [runnersInfo, setRunnersInfo] = useState<RunnersResponse | null>(null);
  useEffect(() => {
    if (activeTab !== "editor") {
      return;
    }
    let cancelled = false;
    fetchRunners(databaseName)
      .then((info) => {
        if (!cancelled) {
          setRunnersInfo(info);
        }
      })
      .catch(() => {
        if (!cancelled) {
          setRunnersInfo(null);
        }
      });
    return () => {
      cancelled = true;
    };
  }, [activeTab, databaseName, workflow?.id]);
  const editorExtraLibs = useMemo(
    () =>
      workflowEditorExtraLibs({
        workflowNodes,
        workflowInstances: workflowInstances.ok ? workflowInstances.value : undefined,
        workflowTrigger
      }),
    [workflowInstances, workflowNodes, workflowTrigger]
  );

  return (
    <div className="flex min-h-full flex-col lg:h-full">
      <div className="shrink-0 px-4 pt-4 md:px-8 md:pt-7">
        <div className="flex flex-wrap items-center justify-between gap-x-6 gap-y-3">
          <div className="flex min-w-0 items-center gap-3">
            <span className="hidden size-10 shrink-0 items-center justify-center rounded-inner bg-chip text-description sm:flex" aria-hidden>
              <Workflow className="size-5" strokeWidth={1.75} />
            </span>
            <div className="min-w-0">
              <h1 className="truncate text-20 font-medium tracking-tight text-fg">{workflow?.name ?? t("common.workflow")}</h1>
              <p className="flex items-center gap-2 text-13 text-muted">
                <span className="truncate">{t("workflow.workflowLabel", { database: databaseName })}</span>
                <span className="inline-flex items-center gap-1.5 text-12">
                  <span className={cn("size-1.5 rounded-full", enabled ? "bg-success" : "bg-subtle")} aria-hidden />
                  {enabled ? t("common.enabled") : t("workflow.disabled")}
                </span>
              </p>
            </div>
          </div>
          <div className="flex flex-wrap items-center gap-2">
            <NodeCatalogDialog language={language} workflowNodes={workflowNodes} />
            <Button size="sm" onClick={onExecute} disabled={!workflow?.id || !canWriteWorkflow}>
              <Play aria-hidden />
              {t("common.run")}
            </Button>
            <Button size="sm" variant="cta" onClick={onSave} disabled={!canWriteWorkflow}>
              {t("common.save")}
            </Button>
          </div>
        </div>
        <Tabs value={activeTab} onValueChange={(value) => onSelectTab(value as WorkflowTab)} className="mt-4">
          <TabsList aria-label={t("workflow.workspaceTabs")}>
            <TabsTrigger value="editor">{t("workflow.editor")}</TabsTrigger>
            <TabsTrigger value="history">
              <History aria-hidden />
              {t("common.history")}
            </TabsTrigger>
          </TabsList>
        </Tabs>
      </div>

      <div className="flex min-h-0 flex-1 flex-col px-4 pb-4 pt-3 md:px-8 md:pb-6">
        {activeTab === "editor" ? (
          <div className="grid min-h-0 flex-1 gap-4 lg:grid-cols-[minmax(0,1fr)_20rem]">
            <JavaScriptEditor
              canWrite={canWriteWorkflow}
              extraLibs={editorExtraLibs}
              label={t("workflow.workflowScriptLabel")}
              onChange={onUpdateScript}
              path={`workflow-${workflow?.id || "new"}.js`}
              testID="workflow-js-editor"
              value={workflow?.script ?? ""}
            />
            <div className="flex min-h-0 flex-col gap-4 lg:overflow-y-auto">
              <WorkflowSettings
                canWriteWorkflow={canWriteWorkflow}
                onSetHistoryRetention={onSetHistoryRetention}
                onSetTimeoutSeconds={onSetTimeoutSeconds}
                onToggleEnabled={onToggleEnabled}
                workflow={workflow}
              />
              <Card>
                <div className="px-4 pb-2 pt-3.5">
                  <h2 className="text-13 font-medium text-fg">{t("workflow.instances")}</h2>
                </div>
                <div className="flex flex-col gap-2 px-3 pb-3" aria-label={t("workflow.instances")} role="list">
                  {workflowInstances.ok ? (
                    Object.keys(workflowInstances.value).length === 0 ? (
                      <p className="px-1 pb-1 text-12 text-muted">{t("workflow.noInstances")}</p>
                    ) : (
                      Object.entries(workflowInstances.value).map(([instanceID, instance]) => {
                        const ports = effectiveInstancePorts(instance, workflowNodesByType);
                        const remoteCapable = runnersInfo?.remote_node_types.includes(instance.node) ?? false;
                        const boundRunner = workflow?.runners?.[instanceID] ?? "";
                        return (
                          <div key={instanceID} role="listitem" className="rounded-inner border border-border-soft bg-surface/40 px-3 py-2.5">
                            <div className="flex items-start justify-between gap-2">
                              <div className="min-w-0">
                                <p className="truncate font-mono text-13 font-medium text-fg">{instanceID}</p>
                                <p className="truncate font-mono text-12 text-muted">{instance.node}</p>
                              </div>
                              {(ports.variables.length > 0 || ports.secrets.length > 0 || remoteCapable) && (
                                <InstanceConfigPopover
                                  canWriteWorkflow={canWriteWorkflow}
                                  instanceID={instanceID}
                                  instanceNode={instance.node}
                                  onSaveInstanceConfig={onSaveInstanceConfig}
                                  ports={ports}
                                  remoteCapable={remoteCapable}
                                  runnersInfo={runnersInfo}
                                  workflow={workflow}
                                />
                              )}
                            </div>
                            <div className="mt-1.5 flex flex-col gap-0.5 text-12 text-description">
                              <span className="truncate">{t("workflow.vars", { ports: formatPorts(ports.variables, t) })}</span>
                              <span className="truncate">{t("workflow.secrets", { ports: formatPorts(ports.secrets, t) })}</span>
                            </div>
                            {boundRunner !== "" && (
                              <Badge className="mt-2">
                                <span>{t("workflow.boundRunner", { name: boundRunner })}</span>
                              </Badge>
                            )}
                          </div>
                        );
                      })
                    )
                  ) : (
                    <p className="rounded-inner bg-error-bg px-3 py-2 font-mono text-12 text-error">{workflowInstances.error}</p>
                  )}
                </div>
              </Card>
            </div>
          </div>
        ) : (
          <WorkflowHistoryView onSelectRunKey={onSelectRunKey} selectedRun={selectedRun} workflowRuns={workflowRuns} />
        )}
      </div>
    </div>
  );
}

function WorkflowSettings({
  canWriteWorkflow,
  onSetHistoryRetention,
  onSetTimeoutSeconds,
  onToggleEnabled,
  workflow
}: {
  canWriteWorkflow: boolean;
  onSetHistoryRetention: (days: number | null) => void;
  onSetTimeoutSeconds: (seconds: number | null) => void;
  onToggleEnabled: (enabled: boolean) => void;
  workflow?: WorkflowDefinition;
}) {
  const { t } = useTranslation();
  const disabled = !workflow?.id || !canWriteWorkflow;
  return (
    <Card>
      <div className="flex items-center justify-between gap-3 border-b border-border-soft px-4 py-3">
        <div className="min-w-0">
          <Label htmlFor="workflow-enabled">{t("common.enabled")}</Label>
          <p className="text-12 text-muted">{t("workflow.enabledHint")}</p>
        </div>
        <Switch
          id="workflow-enabled"
          checked={workflow?.enabled ?? true}
          onCheckedChange={(checked) => onToggleEnabled(checked)}
          disabled={disabled}
        />
      </div>
      <div className="flex flex-col gap-3 px-4 py-3">
        <Field label={t("workflow.historyRetention")}>
          {(field) => (
            <HistoryRetentionSelect {...field} disabled={disabled} onSetHistoryRetention={onSetHistoryRetention} workflow={workflow} />
          )}
        </Field>
        <Field label={t("workflow.timeout")}>
          {(field) => <TimeoutSelect {...field} disabled={disabled} onSetTimeoutSeconds={onSetTimeoutSeconds} workflow={workflow} />}
        </Field>
      </div>
    </Card>
  );
}

function WorkflowHistoryView({
  onSelectRunKey,
  selectedRun,
  workflowRuns
}: {
  onSelectRunKey: (historyKey: string) => void;
  selectedRun: WorkflowRunResponse | null;
  workflowRuns: WorkflowRunResponse[];
}) {
  const { t } = useTranslation();
  const [selectedRunItemID, setSelectedRunItemID] = useState("run-input");
  const runItems = useMemo(() => runListItems(selectedRun, t), [selectedRun, t]);
  const selectedItem = runItems.find((item) => item.id === selectedRunItemID) ?? runItems[0];
  const runs = useMemo(() => [...workflowRuns].sort((left, right) => right.run.timestamp - left.run.timestamp), [workflowRuns]);

  if (workflowRuns.length === 0) {
    return (
      <Card className="flex flex-1 items-center justify-center">
        <EmptyState icon={History} title={t("workflow.noRunsYet")} description={t("workflow.noRunsDescription")} />
      </Card>
    );
  }

  return (
    <div className="grid min-h-0 flex-1 gap-4 lg:grid-cols-[15rem_17rem_minmax(0,1fr)]">
      <Card className="flex min-h-0 flex-col overflow-hidden">
        <h2 className="shrink-0 px-4 pb-2 pt-3.5 text-13 font-medium text-fg">{t("workflow.runs")}</h2>
        <nav aria-label={t("workflow.historyList")} className="scroll-quiet flex max-h-64 min-h-0 flex-col gap-px overflow-y-auto px-2 pb-2 lg:max-h-none">
          {runs.map((run) => {
            const active = run.history_key === selectedRun?.history_key;
            const failed = Boolean(run.run.error) || run.run.steps.some((step) => step.error);
            return (
              <button
                key={run.history_key}
                type="button"
                aria-current={active ? "true" : undefined}
                onClick={() => {
                  onSelectRunKey(run.history_key);
                  setSelectedRunItemID("run-input");
                }}
                className={cn(
                  "flex items-center gap-2.5 rounded-inner px-2.5 py-2 text-left text-13 transition-colors duration-150",
                  active ? "bg-chip font-medium text-fg" : "text-description hover:bg-hover hover:text-fg"
                )}
              >
                <StatusDot tone={failed ? "error" : "success"} label={failed ? t("workflow.runFailed") : t("workflow.runSucceeded")} />
                <span className="min-w-0 truncate tabular-nums">{new Date(run.run.timestamp).toLocaleString()}</span>
              </button>
            );
          })}
        </nav>
      </Card>
      <Card className="flex min-h-0 flex-col overflow-hidden">
        <h2 className="shrink-0 px-4 pb-2 pt-3.5 text-13 font-medium text-fg">{t("workflow.steps")}</h2>
        {selectedRun ? (
          <ol aria-label={t("workflow.runList")} className="scroll-quiet flex min-h-0 flex-col gap-px overflow-y-auto px-2 pb-2">
            {runItems.map((item, index) => {
              const active = item.id === selectedItem?.id;
              return (
                <li key={item.id}>
                  <button
                    type="button"
                    aria-current={active ? "true" : undefined}
                    onClick={() => setSelectedRunItemID(item.id)}
                    className={cn(
                      "flex w-full items-start gap-2.5 rounded-inner px-2.5 py-2 text-left transition-colors duration-150",
                      active ? "bg-chip" : "hover:bg-hover"
                    )}
                  >
                    <span
                      className={cn(
                        "mt-0.5 flex size-5 shrink-0 items-center justify-center rounded-full text-11 font-medium tabular-nums",
                        item.error ? "bg-error-bg text-error" : "bg-chip text-description"
                      )}
                    >
                      {index + 1}
                    </span>
                    <span className="flex min-w-0 flex-col">
                      <span className={cn("truncate text-13", active ? "font-medium text-fg" : "text-description")}>{item.title}</span>
                      {item.subtitle && <span className="truncate font-mono text-11 text-muted">{item.subtitle}</span>}
                      {item.error && <span className="text-12 text-error [overflow-wrap:anywhere]">{item.error}</span>}
                    </span>
                  </button>
                </li>
              );
            })}
          </ol>
        ) : (
          <p className="px-4 pb-4 text-13 text-muted">{t("workflow.selectRunItem")}</p>
        )}
      </Card>
      <Card className="flex min-h-[22rem] flex-col overflow-hidden" aria-label={t("workflow.inspector")} role="region">
        {selectedRun && selectedItem ? (
          <>
            <div className="shrink-0 border-b border-border-soft px-4 py-3">
              <p className="text-14 font-medium text-fg">{selectedItem.title}</p>
              {selectedItem.subtitle && <p className="font-mono text-12 text-muted">{selectedItem.subtitle}</p>}
              {selectedItem.error && <p className="mt-1 text-12 text-error [overflow-wrap:anywhere]">{selectedItem.error}</p>}
            </div>
            <RunPayloadTabs item={selectedItem} />
          </>
        ) : (
          <p className="p-4 text-13 text-muted">{t("workflow.selectRunItem")}</p>
        )}
      </Card>
    </div>
  );
}

const historyRetentionOptions = [1, 7, 30, 90, 365];

function HistoryRetentionSelect({
  onSetHistoryRetention,
  workflow,
  ...props
}: {
  id: string;
  disabled: boolean;
  onSetHistoryRetention: (days: number | null) => void;
  workflow?: WorkflowDefinition;
}) {
  const { t } = useTranslation();
  const retention = workflow?.history_retention_days ?? null;
  return (
    <NativeSelect
      {...props}
      selectSize="sm"
      aria-label={t("workflow.historyRetention")}
      value={retention === null ? "forever" : String(retention)}
      onChange={(event) => onSetHistoryRetention(event.target.value === "forever" ? null : Number(event.target.value))}
    >
      <option value="forever">{t("workflow.historyRetentionForever")}</option>
      <option value="0">{t("workflow.historyRetentionNone")}</option>
      {historyRetentionOptions.map((days) => (
        <option key={days} value={String(days)}>
          {t("workflow.historyRetentionDays", { days })}
        </option>
      ))}
      {retention !== null && retention !== 0 && !historyRetentionOptions.includes(retention) && (
        <option value={String(retention)}>{t("workflow.historyRetentionDays", { days: retention })}</option>
      )}
    </NativeSelect>
  );
}

const timeoutOptions = [60, 300, 1800, 21600, 86400];

function TimeoutSelect({
  onSetTimeoutSeconds,
  workflow,
  ...props
}: {
  id: string;
  disabled: boolean;
  onSetTimeoutSeconds: (seconds: number | null) => void;
  workflow?: WorkflowDefinition;
}) {
  const { t } = useTranslation();
  const timeout = workflow?.timeout_seconds ?? null;
  const label = (seconds: number) =>
    seconds % 3600 === 0
      ? t("workflow.timeoutHours", { hours: seconds / 3600 })
      : t("workflow.timeoutMinutes", { minutes: Math.max(1, Math.round(seconds / 60)) });
  return (
    <NativeSelect
      {...props}
      selectSize="sm"
      aria-label={t("workflow.timeout")}
      value={timeout === null ? "default" : String(timeout)}
      onChange={(event) => onSetTimeoutSeconds(event.target.value === "default" ? null : Number(event.target.value))}
    >
      <option value="default">{t("workflow.timeoutDefault")}</option>
      {timeoutOptions.map((seconds) => (
        <option key={seconds} value={String(seconds)}>
          {label(seconds)}
        </option>
      ))}
      {timeout !== null && !timeoutOptions.includes(timeout) && (
        <option value={String(timeout)}>{t("workflow.timeoutSeconds", { seconds: timeout })}</option>
      )}
    </NativeSelect>
  );
}

function nodeKind(node: WorkflowNodeInfo, t: ReturnType<typeof useTranslation>["t"]): string {
  return node.trigger ? t("workflow.nodeKinds.trigger") : node.stateless ? t("workflow.nodeKinds.stateless") : t("workflow.nodeKinds.stateful");
}

/** The node catalog: every node type on the left, its documentation on the right. */
function NodeCatalogDialog({ language, workflowNodes }: { language: string; workflowNodes: WorkflowNodeInfo[] }) {
  const { t } = useTranslation();
  const [selectedType, setSelectedType] = useState(workflowNodes[0]?.type ?? "");
  const selectedNode = workflowNodes.find((node) => node.type === selectedType) ?? workflowNodes[0];
  const documentation = selectedNode ? nodeDocumentation(selectedNode, language) : "";

  return (
    <Dialog>
      <DialogTrigger asChild>
        <Button size="sm" aria-label={t("workflow.nodesButton")}>
          <BookOpen aria-hidden />
          {t("workflow.nodes")}
        </Button>
      </DialogTrigger>
      <DialogContent
        title={t("workflow.nodeCatalog")}
        description={t("workflow.nodeCatalogDescription")}
        className="h-[85vh] w-[calc(100vw-1.5rem)] max-w-none"
        bodyClassName="flex flex-1 flex-col pb-5 md:grid md:grid-cols-[17rem_minmax(0,1fr)] md:gap-5"
      >
        <nav
          aria-label={t("workflow.nodeCatalogList")}
          className="scroll-quiet flex max-h-48 shrink-0 flex-col gap-px overflow-y-auto rounded-container border border-border-soft p-1.5 md:max-h-none md:min-h-0"
        >
          {workflowNodes.map((node) => {
            const active = node.type === selectedNode?.type;
            return (
              <button
                key={node.type}
                type="button"
                aria-current={active ? "true" : undefined}
                onClick={() => setSelectedType(node.type)}
                className={cn(
                  "flex items-center gap-2 rounded-inner px-2.5 py-1.5 text-left font-mono text-12 transition-colors duration-150",
                  active ? "bg-chip font-medium text-fg" : "text-description hover:bg-hover hover:text-fg"
                )}
              >
                <span className="min-w-0 flex-1 truncate">{node.type}</span>
                {node.trigger && <Zap className="size-3.5 shrink-0 text-running" aria-hidden />}
              </button>
            );
          })}
        </nav>
        <div aria-label={t("workflow.nodeDocumentation")} role="region" className="mt-4 min-h-0 flex-1 overflow-y-auto md:mt-0">
          {selectedNode ? (
            <>
              <div className="mb-3 flex flex-wrap items-center gap-2">
                <p className="font-mono text-15 font-medium text-fg">{selectedNode.type}</p>
                <Badge>{nodeKind(selectedNode, t)}</Badge>
              </div>
              <MarkdownDocument content={documentation} />
            </>
          ) : (
            <p className="text-13 text-muted">{t("workflow.noNodesAvailable")}</p>
          )}
        </div>
      </DialogContent>
    </Dialog>
  );
}

function runListItems(runResponse: WorkflowRunResponse | null, t: ReturnType<typeof useTranslation>["t"]): WorkflowRunListItem[] {
  if (!runResponse) {
    return [];
  }
  return [
    {
      id: "run-input",
      title: t("workflow.runInput"),
      input: runResponse.run.inputs ?? {}
    },
    ...runResponse.run.steps.map((step, index): WorkflowRunListItem => {
      const subtitleParts = [step.node_type, step.runner ? `@ ${step.runner}` : ""].filter(Boolean);
      return {
        id: `step-${index}`,
        title: step.node_id,
        subtitle: subtitleParts.length > 0 ? subtitleParts.join(" ") : undefined,
        input: step.input ?? {},
        output: step.output ?? {},
        error: step.error
      };
    }),
    {
      id: "run-output",
      title: t("workflow.runOutput"),
      output: runResponse.run.outputs ?? {},
      error: runResponse.run.error
    }
  ];
}

function nodeDocumentation(node: WorkflowNodeInfo, language: string): string {
  return (
    node.documentation?.[language] ??
    node.documentation?.["en-US"] ??
    node.documentation?.["zh-CN"] ??
    node.description ??
    node.display_name
  );
}

function MarkdownDocument({ content }: { content: string }) {
  const lines = content.split(/\r?\n/);
  const blocks: ReactNode[] = [];
  let index = 0;

  while (index < lines.length) {
    const line = lines[index];
    if (!line.trim()) {
      index += 1;
      continue;
    }
    if (line.startsWith("```")) {
      const codeLines: string[] = [];
      index += 1;
      while (index < lines.length && !lines[index].startsWith("```")) {
        codeLines.push(lines[index]);
        index += 1;
      }
      index += 1;
      blocks.push(
        <pre key={`code-${blocks.length}`}>
          <code>{codeLines.join("\n")}</code>
        </pre>
      );
      continue;
    }
    if (line.startsWith("### ")) {
      blocks.push(<h3 key={`h3-${blocks.length}`}>{renderInlineMarkdown(line.slice(4))}</h3>);
      index += 1;
      continue;
    }
    if (line.startsWith("## ")) {
      blocks.push(<h2 key={`h2-${blocks.length}`}>{renderInlineMarkdown(line.slice(3))}</h2>);
      index += 1;
      continue;
    }
    if (line.startsWith("- ")) {
      const items: string[] = [];
      while (index < lines.length && lines[index].startsWith("- ")) {
        items.push(lines[index].slice(2));
        index += 1;
      }
      blocks.push(
        <ul key={`list-${blocks.length}`}>
          {items.map((item, itemIndex) => (
            <li key={`${item}-${itemIndex}`}>{renderInlineMarkdown(item)}</li>
          ))}
        </ul>
      );
      continue;
    }

    const paragraphLines = [line];
    index += 1;
    while (
      index < lines.length &&
      lines[index].trim() &&
      !lines[index].startsWith("## ") &&
      !lines[index].startsWith("### ") &&
      !lines[index].startsWith("- ") &&
      !lines[index].startsWith("```")
    ) {
      paragraphLines.push(lines[index]);
      index += 1;
    }
    blocks.push(<p key={`p-${blocks.length}`}>{renderInlineMarkdown(paragraphLines.join(" "))}</p>);
  }

  return <div className="markdown-doc">{blocks}</div>;
}

function renderInlineMarkdown(text: string): ReactNode[] {
  return text.split(/(`[^`]+`)/g).map((part, index) => {
    if (part.startsWith("`") && part.endsWith("`")) {
      return <code key={`${part}-${index}`}>{part.slice(1, -1)}</code>;
    }
    return <span key={`${part}-${index}`}>{part}</span>;
  });
}

/** Variables, secrets and the runner of one declared instance, edited in a panel next to it. */
function InstanceConfigPopover({
  canWriteWorkflow,
  instanceID,
  instanceNode,
  onSaveInstanceConfig,
  ports,
  remoteCapable,
  runnersInfo,
  workflow
}: {
  canWriteWorkflow: boolean;
  instanceID: string;
  instanceNode: string;
  onSaveInstanceConfig: (
    instanceID: string,
    variables: Record<string, string>,
    secrets: Record<string, string>,
    runnerName: string
  ) => void | Promise<void>;
  ports: { variables: WorkflowPort[]; secrets: WorkflowPort[] };
  remoteCapable: boolean;
  runnersInfo: RunnersResponse | null;
  workflow?: WorkflowDefinition;
}) {
  const { t } = useTranslation();
  const [open, setOpen] = useState(false);
  const [variableDrafts, setVariableDrafts] = useState<Record<string, string>>({});
  const [secretDrafts, setSecretDrafts] = useState<Record<string, string>>({});
  const [dirtySecrets, setDirtySecrets] = useState<Record<string, boolean>>({});
  const [runnerDraft, setRunnerDraft] = useState("");
  const [saving, setSaving] = useState(false);

  const boundRunner = workflow?.runners?.[instanceID] ?? "";
  const connectedNames = [...new Set((runnersInfo?.runners ?? []).map((runner) => runner.name))];
  const runnerOptions = connectedNames.includes(boundRunner) || boundRunner === "" ? connectedNames : [boundRunner, ...connectedNames];

  function resetDrafts() {
    setVariableDrafts(
      Object.fromEntries(ports.variables.map((port) => [port.name, workflow?.variables?.[instanceConfigKey(instanceID, port.name)] ?? ""]))
    );
    setSecretDrafts(
      Object.fromEntries(ports.secrets.map((port) => [port.name, secretMask(workflow?.secrets?.[instanceConfigKey(instanceID, port.name)] ?? 0)]))
    );
    setDirtySecrets({});
    setRunnerDraft(boundRunner);
  }

  async function saveDrafts() {
    const changedSecrets = Object.fromEntries(Object.entries(secretDrafts).filter(([name, value]) => dirtySecrets[name] && value !== ""));
    setSaving(true);
    try {
      await onSaveInstanceConfig(instanceID, variableDrafts, changedSecrets, remoteCapable ? runnerDraft : boundRunner);
      setOpen(false);
    } finally {
      setSaving(false);
    }
  }

  return (
    <Popover
      open={open}
      onOpenChange={(next) => {
        if (next) {
          resetDrafts();
        }
        setOpen(next);
      }}
    >
      <PopoverTrigger asChild>
        <Button variant="quiet" size="icon-sm" aria-label={t("workflow.editConfig", { id: instanceID })} disabled={!canWriteWorkflow} className="-mr-1 -mt-0.5">
          <Pencil />
        </Button>
      </PopoverTrigger>
      <PopoverContent align="end" aria-label={t("workflow.instanceConfig", { id: instanceID })} className="w-80 p-0">
        <form
          onSubmit={(event) => {
            event.preventDefault();
            void saveDrafts();
          }}
        >
          <div className="border-b border-border-soft px-4 py-3">
            <p className="font-mono text-13 font-medium text-fg">{instanceID}</p>
            <p className="font-mono text-12 text-muted">{instanceNode}</p>
          </div>
          <div className="flex flex-col gap-3 px-4 py-3">
            {remoteCapable && (
              <Field label={t("workflow.runnerLabel")}>
                {(field) => (
                  <NativeSelect
                    {...field}
                    selectSize="sm"
                    aria-label={t("workflow.runnerSelect", { instance: instanceID })}
                    value={runnerDraft}
                    onChange={(event) => setRunnerDraft(event.target.value)}
                    disabled={!canWriteWorkflow}
                  >
                    <option value="">{t("workflow.runnerServer")}</option>
                    {runnerOptions.map((name) => (
                      <option key={name} value={name}>
                        {connectedNames.includes(name) ? name : t("workflow.runnerOffline", { name })}
                      </option>
                    ))}
                  </NativeSelect>
                )}
              </Field>
            )}
            {ports.variables.length > 0 && <p className="text-12 font-medium text-subtle">{t("workflow.variables")}</p>}
            {ports.variables.map((port) => (
              <Field key={`variable-${instanceID}-${port.name}`} label={<PortLabel port={port} />}>
                {(field) => (
                  <Input
                    {...field}
                    inputSize="sm"
                    aria-label={t("workflow.variableLabel", { instance: instanceID, name: port.name })}
                    value={variableDrafts[port.name] ?? ""}
                    onChange={(event) => {
                      const value = event.target.value;
                      setVariableDrafts((current) => ({ ...current, [port.name]: value }));
                    }}
                    disabled={!canWriteWorkflow}
                  />
                )}
              </Field>
            ))}
            {ports.secrets.length > 0 && <p className="text-12 font-medium text-subtle">{t("workflow.secretsTitle")}</p>}
            {ports.secrets.map((port) => {
              const length = workflow?.secrets?.[instanceConfigKey(instanceID, port.name)] ?? 0;
              return (
                <Field key={`secret-${instanceID}-${port.name}`} label={<PortLabel port={port} />}>
                  {(field) => (
                    <Input
                      {...field}
                      inputSize="sm"
                      aria-label={t("workflow.secretLabel", { instance: instanceID, name: port.name })}
                      type="password"
                      autoComplete="new-password"
                      placeholder={t("workflow.enterSecretValue")}
                      value={secretDrafts[port.name] ?? ""}
                      onFocus={() => {
                        if (!dirtySecrets[port.name] && length > 0) {
                          setSecretDrafts((current) => ({ ...current, [port.name]: "" }));
                        }
                      }}
                      onChange={(event) => {
                        const value = event.target.value;
                        setDirtySecrets((current) => ({ ...current, [port.name]: true }));
                        setSecretDrafts((current) => ({ ...current, [port.name]: value }));
                      }}
                      disabled={!canWriteWorkflow}
                    />
                  )}
                </Field>
              );
            })}
          </div>
          <div className="flex justify-end border-t border-border-soft px-4 py-3">
            <Button type="submit" size="sm" variant="cta" disabled={!canWriteWorkflow} loading={saving}>
              {t("workflow.saveConfig")}
            </Button>
          </div>
        </form>
      </PopoverContent>
    </Popover>
  );
}

function PortLabel({ port }: { port: WorkflowPort }) {
  return (
    <span className="flex items-center gap-2">
      <span className="font-mono">{port.name}</span>
      <span className="font-mono text-11 font-normal text-subtle">{port.type}</span>
    </span>
  );
}

function effectiveInstancePorts(
  instance: WorkflowInstanceDeclaration,
  workflowNodesByType: Map<string, WorkflowNodeInfo>
): { variables: WorkflowPort[]; secrets: WorkflowPort[] } {
  const node = workflowNodesByType.get(instance.node);
  return {
    variables: mergePorts(node?.variables ?? [], instance.variables ?? []),
    secrets: mergePorts(node?.secrets ?? [], instance.secrets ?? [])
  };
}

function mergePorts(defaultPorts: WorkflowPort[], instancePorts: WorkflowPort[]): WorkflowPort[] {
  const portsByName = new Map<string, WorkflowPort>();
  for (const port of [...defaultPorts, ...instancePorts]) {
    if (port.name) {
      portsByName.set(port.name, port);
    }
  }
  return [...portsByName.values()];
}

function formatPorts(ports: Array<{ name: string; type: string }>, t: ReturnType<typeof useTranslation>["t"]): string {
  if (ports.length === 0) {
    return t("common.none");
  }
  return ports.map((port) => `${port.name}:${port.type}`).join(", ");
}

function instanceConfigKey(instanceID: string, name: string): string {
  return `${instanceID}.${name}`;
}

function secretMask(length: number): string {
  return length > 0 ? "x".repeat(length) : "";
}

function RunPayloadTabs({ item }: { item: WorkflowRunListItem }) {
  const { t } = useTranslation();
  const [selectedPayload, setSelectedPayload] = useState<"input" | "output">("input");
  const payloads = [
    item.input ? { id: "input" as const, label: t("common.input"), value: item.input } : undefined,
    item.output ? { id: "output" as const, label: t("common.output"), value: item.output } : undefined
  ].filter((payload): payload is { id: "input" | "output"; label: string; value: Record<string, unknown> } => Boolean(payload));
  const activePayload = payloads.find((payload) => payload.id === selectedPayload) ?? payloads[0];

  if (!activePayload) {
    return null;
  }

  return (
    <div className="flex min-h-0 flex-1 flex-col gap-3 p-3">
      <Tabs value={activePayload.id} onValueChange={(value) => setSelectedPayload(value as "input" | "output")}>
        <TabsList aria-label={t("workflow.payloadTabs")}>
          {payloads.map((payload) => (
            <TabsTrigger key={payload.id} value={payload.id}>
              {payload.label}
            </TabsTrigger>
          ))}
        </TabsList>
      </Tabs>
      <JavaScriptEditor
        canWrite={false}
        label={activePayload.label}
        language="json"
        onChange={() => undefined}
        path={`workflow-run-${item.id}-${activePayload.id}.json`}
        testID={`workflow-run-${activePayload.id}-editor`}
        value={JSON.stringify(activePayload.value, null, 2)}
      />
    </div>
  );
}
