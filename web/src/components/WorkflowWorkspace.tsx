import { useEffect, useMemo, useState, type ReactNode } from "react";
import {
  Button,
  Dialog,
  DialogBody,
  DialogContent,
  DialogSurface,
  DialogTitle,
  DialogTrigger,
  Field as FluentField,
  Input,
  Menu,
  MenuButton,
  MenuItem,
  MenuList,
  MenuPopover,
  MenuTrigger,
  Popover,
  PopoverSurface,
  PopoverTrigger,
  Select,
  Switch,
  Tab,
  TabList,
  Text,
} from "@fluentui/react-components";
import { EditRegular, HistoryRegular, InfoRegular, PlayRegular, SaveRegular } from "@fluentui/react-icons";
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
import { AIScriptAssistant } from "./AIScriptAssistant";
import { JavaScriptEditor } from "./JavaScriptEditor";

type WorkflowWorkspaceProps = {
  aiEnabled: boolean;
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

export function WorkflowWorkspace({
  aiEnabled,
  activeTab,
  databaseName,
  language,
  onExecute,
  onSave,
  onSaveInstanceConfig,
  onSelectTab,
  onSelectRunKey,
  onSetHistoryRetention,
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
    <div className="workflow-workspace">
      <div className="section-header workflow-section-header">
        <div>
          <Text weight="semibold">{workflow?.name ?? t("common.workflow")}.js</Text>
          <Text size={200}>{t("workflow.workflowLabel", { database: databaseName })}</Text>
        </div>
        <div className="workflow-header-actions">
          {aiEnabled && (
            <AIScriptAssistant
              canWrite={canWriteWorkflow}
              kind="workflow"
              language={language}
              resourceID={workflow?.id}
              script={workflow?.script ?? ""}
              onApply={onUpdateScript}
            />
          )}
          <NodeCatalogDialog language={language} workflowNodes={workflowNodes} />
          <HistoryRetentionSelect
            canWriteWorkflow={canWriteWorkflow}
            onSetHistoryRetention={onSetHistoryRetention}
            workflow={workflow}
          />
          <Switch
            checked={workflow?.enabled ?? true}
            label={t("common.enabled")}
            onChange={(_, data) => onToggleEnabled(Boolean(data.checked))}
            disabled={!workflow?.id || !canWriteWorkflow}
          />
          <Button icon={<PlayRegular />} onClick={onExecute} disabled={!workflow?.id || !canWriteWorkflow}>
            {t("common.run")}
          </Button>
          <Button icon={<SaveRegular />} appearance="primary" onClick={onSave} disabled={!canWriteWorkflow}>
            {t("common.save")}
          </Button>
        </div>
      </div>

      <TabList
        selectedValue={activeTab}
        onTabSelect={(_, data) => onSelectTab(data.value as WorkflowTab)}
        aria-label={t("workflow.workspaceTabs")}
      >
        <Tab value="editor">{t("workflow.editor")}</Tab>
        <Tab value="history">{t("common.history")}</Tab>
      </TabList>

      {activeTab === "editor" ? (
        <div className="split-view workflow-editor-tab">
          <div className="editor-pane">
            <JavaScriptEditor
              canWrite={canWriteWorkflow}
              extraLibs={editorExtraLibs}
              label={t("workflow.workflowScriptLabel")}
              onChange={onUpdateScript}
              path={`workflow-${workflow?.id || "new"}.js`}
              testID="workflow-js-editor"
              value={workflow?.script ?? ""}
            />
          </div>
          <div className="history-pane">
            <Text weight="semibold">{t("workflow.instances")}</Text>
            <div className="node-list" aria-label={t("workflow.instances")}>
              {workflowInstances.ok ? (
                Object.entries(workflowInstances.value).map(([instanceID, instance]) => {
                  const ports = effectiveInstancePorts(instance, workflowNodesByType);
                  const remoteCapable = runnersInfo?.remote_node_types.includes(instance.node) ?? false;
                  const boundRunner = workflow?.runners?.[instanceID] ?? "";
                  return (
                    <div key={instanceID} className="node-item">
                      <div className="node-title">
                        <span>{instanceID}</span>
                        <span>{instance.node}</span>
                      </div>
                      <div className="node-ports">
                        <span>{t("workflow.vars", { ports: formatPorts(ports.variables, t) })}</span>
                        <span>{t("workflow.secrets", { ports: formatPorts(ports.secrets, t) })}</span>
                        {boundRunner !== "" && <span>{t("workflow.boundRunner", { name: boundRunner })}</span>}
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
                  );
                })
              ) : (
                <Text size={200}>{workflowInstances.error}</Text>
              )}
            </div>
          </div>
        </div>
      ) : (
        <WorkflowHistoryView
          onSelectRunKey={onSelectRunKey}
          selectedRun={selectedRun}
          workflowRuns={workflowRuns}
        />
      )}
    </div>
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
  const selectedRunLabel = selectedRun ? new Date(selectedRun.run.timestamp).toLocaleString() : t("workflow.noRunsYet");

  return (
    <div className="workflow-history-tab">
      <div className="workflow-run-left">
        <Menu>
          <MenuTrigger disableButtonEnhancement>
            <MenuButton
              className="workflow-run-history-button"
              icon={<HistoryRegular />}
              disabled={workflowRuns.length === 0}
              aria-label={t("workflow.historyList")}
            >
              {selectedRunLabel}
            </MenuButton>
          </MenuTrigger>
          <MenuPopover className="workflow-run-history-menu">
            <MenuList className="workflow-run-history-menu-list">
              {workflowRuns.map((run) => {
                const runLabel = new Date(run.run.timestamp).toLocaleString();
                return (
                  <MenuItem
                    key={run.history_key}
                    onClick={() => {
                      onSelectRunKey(run.history_key);
                      setSelectedRunItemID("run-input");
                    }}
                  >
                    {runLabel}
                  </MenuItem>
                );
              })}
            </MenuList>
          </MenuPopover>
        </Menu>
        {workflowRuns.length > 0 ? (
          <div className="workflow-run-node-list" aria-label={t("workflow.runList")}>
            {runItems.map((item, index) => (
              <button
                key={item.id}
                type="button"
                className={runNodeItemClassName(item, item.id === selectedItem?.id)}
                onClick={() => setSelectedRunItemID(item.id)}
              >
                <span className="workflow-run-node-index">{index + 1}</span>
                <span className="workflow-run-node-main">
                  <span>{item.title}</span>
                  {item.subtitle && <span>{item.subtitle}</span>}
                  {item.error && <span>{item.error}</span>}
                </span>
              </button>
            ))}
          </div>
        ) : (
          <div className="workflow-run-empty">
            <span className="flow-empty">{t("workflow.noRunsYet")}</span>
          </div>
        )}
      </div>
      <div className="workflow-run-inspector" aria-label={t("workflow.inspector")}>
        {workflowRuns.length > 0 && selectedItem ? (
          <>
            <div className="workflow-run-inspector-header">
              <Text weight="semibold">{selectedItem.title}</Text>
              {selectedItem.subtitle && <Text size={200}>{selectedItem.subtitle}</Text>}
              {selectedItem.error && <Text size={200}>{selectedItem.error}</Text>}
            </div>
            <RunPayloadTabs item={selectedItem} />
          </>
        ) : (
          <Text size={200}>{t("workflow.selectRunItem")}</Text>
        )}
      </div>
    </div>
  );
}

const historyRetentionOptions = [1, 7, 30, 90, 365];

function HistoryRetentionSelect({
  canWriteWorkflow,
  onSetHistoryRetention,
  workflow
}: {
  canWriteWorkflow: boolean;
  onSetHistoryRetention: (days: number | null) => void;
  workflow?: WorkflowDefinition;
}) {
  const { t } = useTranslation();
  const retention = workflow?.history_retention_days ?? null;
  return (
    <Select
      aria-label={t("workflow.historyRetention")}
      title={t("workflow.historyRetention")}
      value={retention === null ? "forever" : String(retention)}
      onChange={(_, data) => onSetHistoryRetention(data.value === "forever" ? null : Number(data.value))}
      disabled={!workflow?.id || !canWriteWorkflow}
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
    </Select>
  );
}

function NodeCatalogDialog({ language, workflowNodes }: { language: string; workflowNodes: WorkflowNodeInfo[] }) {
  const { t } = useTranslation();
  const [selectedType, setSelectedType] = useState(workflowNodes[0]?.type ?? "");
  const selectedNode = workflowNodes.find((node) => node.type === selectedType) ?? workflowNodes[0];
  const documentation = selectedNode ? nodeDocumentation(selectedNode, language) : "";

  return (
    <Dialog>
      <DialogTrigger disableButtonEnhancement>
        <Button icon={<InfoRegular />} aria-label={t("workflow.nodesButton")}>
          {t("workflow.nodes")}
        </Button>
      </DialogTrigger>
      <DialogSurface
        className="node-catalog-dialog"
        aria-label={t("workflow.nodeCatalog")}
        style={{
          width: "calc(100vw - 24px)",
          maxWidth: "none"
        }}
      >
        <DialogBody>
          <DialogTitle>{t("workflow.nodeCatalog")}</DialogTitle>
          <DialogContent className="node-catalog-dialog-content">
            <div className="node-catalog-menu" aria-label={t("workflow.nodeCatalogList")}>
              {workflowNodes.map((node) => (
                <Button
                  key={node.type}
                  appearance={node.type === selectedNode?.type ? "secondary" : "subtle"}
                  className={node.trigger ? "node-catalog-menu-item trigger" : "node-catalog-menu-item"}
                  onClick={() => setSelectedType(node.type)}
                >
                  <span>{node.type}</span>
                </Button>
              ))}
            </div>
            <div className="node-doc-panel" aria-label={t("workflow.nodeDocumentation")}>
              {selectedNode ? (
                <>
                  <div className="node-doc-heading">
                    <Text weight="semibold">{selectedNode.type}</Text>
                    <Text size={200}>
                      {selectedNode.trigger
                        ? t("workflow.nodeKinds.trigger")
                        : selectedNode.stateless
                          ? t("workflow.nodeKinds.stateless")
                          : t("workflow.nodeKinds.stateful")}
                    </Text>
                  </div>
                  <MarkdownDocument content={documentation} />
                </>
              ) : (
                <Text size={200}>{t("workflow.noNodesAvailable")}</Text>
              )}
            </div>
          </DialogContent>
        </DialogBody>
      </DialogSurface>
    </Dialog>
  );
}

function runListItems(
  runResponse: WorkflowRunResponse | null,
  t: ReturnType<typeof useTranslation>["t"]
): WorkflowRunListItem[] {
  if (!runResponse) {
    return [];
  }
  const runInputTitle = t("workflow.runInput");
  const runOutputTitle = t("workflow.runOutput");
  return [
    {
      id: "run-input",
      title: runInputTitle,
      input: runResponse.run.inputs ?? {}
    },
    ...runResponse.run.steps.map((step, index): WorkflowRunListItem => {
      const id = `step-${index}`;
      const subtitleParts = [step.node_type, step.runner ? `@ ${step.runner}` : ""].filter(Boolean);
      return {
        id,
        title: step.node_id,
        subtitle: subtitleParts.length > 0 ? subtitleParts.join(" ") : undefined,
        input: step.input ?? {},
        output: step.output ?? {},
        error: step.error
      };
    }),
    {
      id: "run-output",
      title: runOutputTitle,
      output: runResponse.run.outputs ?? {},
      error: runResponse.run.error
    }
  ];
}

function runNodeItemClassName(item: WorkflowRunListItem, selected: boolean): string {
  return [
    "workflow-run-node-item",
    selected ? "selected" : "",
    item.error ? "error" : ""
  ].filter(Boolean).join(" ");
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

  const boundRunner = workflow?.runners?.[instanceID] ?? "";
  const connectedNames = [...new Set((runnersInfo?.runners ?? []).map((runner) => runner.name))];
  const runnerOptions = connectedNames.includes(boundRunner) || boundRunner === ""
    ? connectedNames
    : [boundRunner, ...connectedNames];

  function resetDrafts() {
    setVariableDrafts(
      Object.fromEntries(
        ports.variables.map((port) => [port.name, workflow?.variables?.[instanceConfigKey(instanceID, port.name)] ?? ""])
      )
    );
    setSecretDrafts(
      Object.fromEntries(
        ports.secrets.map((port) => [
          port.name,
          secretMask(workflow?.secrets?.[instanceConfigKey(instanceID, port.name)] ?? 0)
        ])
      )
    );
    setDirtySecrets({});
    setRunnerDraft(boundRunner);
  }

  async function saveDrafts() {
    const changedSecrets = Object.fromEntries(
      Object.entries(secretDrafts).filter(([name, value]) => dirtySecrets[name] && value !== "")
    );
    await onSaveInstanceConfig(instanceID, variableDrafts, changedSecrets, remoteCapable ? runnerDraft : boundRunner);
    setOpen(false);
  }

  return (
    <Popover
      open={open}
      onOpenChange={(_, data) => {
        if (data.open) {
          resetDrafts();
        }
        setOpen(data.open);
      }}
    >
      <PopoverTrigger disableButtonEnhancement>
        <Button icon={<EditRegular />} aria-label={t("workflow.editConfig", { id: instanceID })} disabled={!canWriteWorkflow} />
      </PopoverTrigger>
      <PopoverSurface className="instance-config-popover" aria-label={t("workflow.instanceConfig", { id: instanceID })}>
        <div className="instance-config-popover-header">
          <div>
            <span>{instanceID}</span>
            <span>{instanceNode}</span>
          </div>
        </div>
        {remoteCapable && (
          <FluentField label={t("workflow.runnerLabel")}>
            <Select
              aria-label={t("workflow.runnerSelect", { instance: instanceID })}
              value={runnerDraft}
              onChange={(_, data) => setRunnerDraft(data.value)}
              disabled={!canWriteWorkflow}
            >
              <option value="">{t("workflow.runnerServer")}</option>
              {runnerOptions.map((name) => (
                <option key={name} value={name}>
                  {connectedNames.includes(name) ? name : t("workflow.runnerOffline", { name })}
                </option>
              ))}
            </Select>
          </FluentField>
        )}
        {ports.variables.map((port) => (
          <FluentField key={`variable-${instanceID}-${port.name}`} label={port.name}>
            <Input
              aria-label={t("workflow.variableLabel", { instance: instanceID, name: port.name })}
              value={variableDrafts[port.name] ?? ""}
              onChange={(_, data) => setVariableDrafts((current) => ({ ...current, [port.name]: data.value }))}
              disabled={!canWriteWorkflow}
            />
          </FluentField>
        ))}
        {ports.secrets.map((port) => {
          const length = workflow?.secrets?.[instanceConfigKey(instanceID, port.name)] ?? 0;
          return (
            <FluentField key={`secret-${instanceID}-${port.name}`} label={port.name}>
              <Input
                aria-label={t("workflow.secretLabel", { instance: instanceID, name: port.name })}
                type="password"
                placeholder={t("workflow.enterSecretValue")}
                value={secretDrafts[port.name] ?? ""}
                onFocus={() => {
                  if (!dirtySecrets[port.name] && length > 0) {
                    setSecretDrafts((current) => ({ ...current, [port.name]: "" }));
                  }
                }}
                onChange={(_, data) => {
                  setDirtySecrets((current) => ({ ...current, [port.name]: true }));
                  setSecretDrafts((current) => ({ ...current, [port.name]: data.value }));
                }}
                disabled={!canWriteWorkflow}
              />
            </FluentField>
          );
        })}
        <div className="instance-config-actions">
          <Button appearance="primary" icon={<SaveRegular />} onClick={saveDrafts} disabled={!canWriteWorkflow}>
            {t("workflow.saveConfig")}
          </Button>
        </div>
      </PopoverSurface>
    </Popover>
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
    <div className="run-payload-tabs">
      <TabList
        selectedValue={activePayload.id}
        onTabSelect={(_, data) => setSelectedPayload(data.value as "input" | "output")}
        aria-label={t("workflow.payloadTabs")}
      >
        {payloads.map((payload) => (
          <Tab key={payload.id} value={payload.id}>
            {payload.label}
          </Tab>
        ))}
      </TabList>
      <div className="run-payload-editor">
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
    </div>
  );
}
