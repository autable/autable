import { useMemo, useState, type ReactNode } from "react";
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
  Popover,
  PopoverSurface,
  PopoverTrigger,
  Switch,
  Tab,
  TabList,
  Text,
} from "@fluentui/react-components";
import { EditRegular, InfoRegular, PlayRegular, SaveRegular } from "@fluentui/react-icons";
import { Background, Controls, ReactFlow, type Edge, type Node } from "@xyflow/react";
import type {
  WorkflowDefinition,
  WorkflowInstanceDeclaration,
  WorkflowNodeInfo,
  WorkflowPort,
  WorkflowRunResponse
} from "../api";
import { workflowEditorExtraLibs } from "../editorTypes";
import type { WorkflowTriggerDeclaration } from "../workflowInstances";
import { JavaScriptEditor } from "./JavaScriptEditor";

type WorkflowWorkspaceProps = {
  databaseName: string;
  language: string;
  onExecute: () => void;
  onSave: () => void;
  onSaveInstanceConfig: (
    instanceID: string,
    variables: Record<string, string>,
    secrets: Record<string, string>
  ) => void | Promise<void>;
  onSelectRunKey: (historyKey: string) => void;
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

type WorkflowTab = "editor" | "history";

type WorkflowFlowNodeData = {
  label: ReactNode;
  title: string;
  subtitle?: string;
  input?: Record<string, unknown>;
  output?: Record<string, unknown>;
  error?: string;
};

type WorkflowFlowNode = Node<WorkflowFlowNodeData>;

export function WorkflowWorkspace({
  databaseName,
  language,
  onExecute,
  onSave,
  onSaveInstanceConfig,
  onSelectRunKey,
  onToggleEnabled,
  onUpdateScript,
  selectedRun,
  workflow,
  workflowInstances,
  workflowTrigger,
  workflowNodes,
  workflowRuns
}: WorkflowWorkspaceProps) {
  const canWriteWorkflow = (workflow?.permission_level ?? 2) >= 2;
  const workflowNodesByType = new Map(workflowNodes.map((node) => [node.type, node]));
  const [activeTab, setActiveTab] = useState<WorkflowTab>("editor");
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
          <Text weight="semibold">{workflow?.name ?? "workflow"}.js</Text>
          <Text size={200}>{databaseName} workflow</Text>
        </div>
        <div className="workflow-header-actions">
          <NodeCatalogDialog language={language} workflowNodes={workflowNodes} />
          <Switch
            checked={workflow?.enabled ?? true}
            label="Enabled"
            onChange={(_, data) => onToggleEnabled(Boolean(data.checked))}
            disabled={!workflow?.id || !canWriteWorkflow}
          />
          <Button icon={<PlayRegular />} onClick={onExecute} disabled={!workflow?.id || !canWriteWorkflow}>
            Run
          </Button>
          <Button icon={<SaveRegular />} appearance="primary" onClick={onSave} disabled={!canWriteWorkflow}>
            Save
          </Button>
        </div>
      </div>

      <TabList
        selectedValue={activeTab}
        onTabSelect={(_, data) => setActiveTab(data.value as WorkflowTab)}
        aria-label="Workflow workspace tabs"
      >
        <Tab value="editor">Editor</Tab>
        <Tab value="history">History</Tab>
      </TabList>

      {activeTab === "editor" ? (
        <div className="split-view workflow-editor-tab">
          <div className="editor-pane">
            <JavaScriptEditor
              canWrite={canWriteWorkflow}
              extraLibs={editorExtraLibs}
              label="Workflow JavaScript"
              onChange={onUpdateScript}
              path={`workflow-${workflow?.id || "new"}.js`}
              testID="workflow-js-editor"
              value={workflow?.script ?? ""}
            />
          </div>
          <div className="history-pane">
            <Text weight="semibold">Instances</Text>
            <div className="node-list" aria-label="Workflow instances">
              {workflowInstances.ok ? (
                Object.entries(workflowInstances.value).map(([instanceID, instance]) => {
                  const ports = effectiveInstancePorts(instance, workflowNodesByType);
                  return (
                    <div key={instanceID} className="node-item">
                      <div className="node-title">
                        <span>{instanceID}</span>
                        <span>{instance.node}</span>
                      </div>
                      <div className="node-ports">
                        <span>vars {formatPorts(ports.variables)}</span>
                        <span>secrets {formatPorts(ports.secrets)}</span>
                      </div>
                      {(ports.variables.length > 0 || ports.secrets.length > 0) && (
                        <InstanceConfigPopover
                          canWriteWorkflow={canWriteWorkflow}
                          instanceID={instanceID}
                          instanceNode={instance.node}
                          onSaveInstanceConfig={onSaveInstanceConfig}
                          ports={ports}
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
  const [selectedNodeID, setSelectedNodeID] = useState("run-input");
  const { nodes, edges } = useMemo(() => runFlowElements(selectedRun), [selectedRun]);
  const selectedNode = nodes.find((node) => node.id === selectedNodeID) ?? nodes[0];

  return (
    <div className="workflow-history-tab">
      <div className="run-history-list" aria-label="Workflow run history">
        {workflowRuns.length > 0 ? (
          workflowRuns.map((run) => (
            <button
              key={run.history_key}
              className={run.history_key === selectedRun?.history_key ? "run-history-item selected" : "run-history-item"}
              type="button"
              onClick={() => {
                onSelectRunKey(run.history_key);
                setSelectedNodeID("run-input");
              }}
            >
              <span>{run.history_key}</span>
              <span>{new Date(run.run.timestamp).toLocaleString()}</span>
            </button>
          ))
        ) : (
          <span className="flow-empty">No runs yet</span>
        )}
      </div>
      <div className="workflow-run-flow-shell">
        <div className="workflow-run-flow" aria-label="Workflow run flow">
          {selectedRun ? (
            <ReactFlow
              nodes={nodes}
              edges={edges}
              fitView
              nodesDraggable={false}
              nodesConnectable={false}
              onNodeClick={(_, node) => setSelectedNodeID(node.id)}
              proOptions={{ hideAttribution: true }}
            >
              <Background />
              <Controls showInteractive={false} />
            </ReactFlow>
          ) : (
            <span className="flow-empty">No runs yet</span>
          )}
        </div>
        <div className="workflow-run-inspector" aria-label="Workflow node inspector">
          {selectedNode ? (
            <>
              <div className="workflow-run-inspector-header">
                <Text weight="semibold">{selectedNode.data.title}</Text>
                {selectedNode.data.subtitle && <Text size={200}>{selectedNode.data.subtitle}</Text>}
                {selectedNode.data.error && <Text size={200}>{selectedNode.data.error}</Text>}
              </div>
              <div className="flow-step-payloads">
                {selectedNode.data.input && <RunPayload title="Input" value={selectedNode.data.input} />}
                {selectedNode.data.output && <RunPayload title="Output" value={selectedNode.data.output} />}
              </div>
            </>
          ) : (
            <Text size={200}>Select a node</Text>
          )}
        </div>
      </div>
    </div>
  );
}

function NodeCatalogDialog({ language, workflowNodes }: { language: string; workflowNodes: WorkflowNodeInfo[] }) {
  const [selectedType, setSelectedType] = useState(workflowNodes[0]?.type ?? "");
  const selectedNode = workflowNodes.find((node) => node.type === selectedType) ?? workflowNodes[0];
  const documentation = selectedNode ? nodeDocumentation(selectedNode, language) : "";

  return (
    <Dialog>
      <DialogTrigger disableButtonEnhancement>
        <Button icon={<InfoRegular />} aria-label="Workflow nodes">
          Nodes
        </Button>
      </DialogTrigger>
      <DialogSurface
        className="node-catalog-dialog"
        aria-label="Workflow node catalog"
        style={{
          width: "calc(100vw - 24px)",
          maxWidth: "none"
        }}
      >
        <DialogBody>
          <DialogTitle>Workflow nodes</DialogTitle>
          <DialogContent className="node-catalog-dialog-content">
            <div className="node-catalog-menu" aria-label="Workflow node catalog list">
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
            <div className="node-doc-panel" aria-label="Workflow node documentation">
              {selectedNode ? (
                <>
                  <div className="node-doc-heading">
                    <Text weight="semibold">{selectedNode.type}</Text>
                    <Text size={200}>{selectedNode.trigger ? "trigger" : selectedNode.stateless ? "stateless" : "stateful"}</Text>
                  </div>
                  <MarkdownDocument content={documentation} />
                </>
              ) : (
                <Text size={200}>No workflow nodes available</Text>
              )}
            </div>
          </DialogContent>
        </DialogBody>
      </DialogSurface>
    </Dialog>
  );
}

function runFlowElements(runResponse: WorkflowRunResponse | null): { nodes: WorkflowFlowNode[]; edges: Edge[] } {
  if (!runResponse) {
    return { nodes: [], edges: [] };
  }
  const nodes: WorkflowFlowNode[] = [
    {
      id: "run-input",
      type: "input",
      position: { x: 0, y: 0 },
      data: {
        title: "Run input",
        input: runResponse.run.inputs ?? {},
        label: <FlowNodeLabel title="Run input" subtitle={runResponse.history_key} />
      }
    },
    ...runResponse.run.steps.map((step, index): WorkflowFlowNode => {
      const id = `step-${index}`;
      return {
        id,
        position: { x: 260 * (index + 1), y: 0 },
        className: step.error ? "workflow-flow-node-error" : undefined,
        data: {
          title: step.node_id,
          subtitle: step.node_type,
          input: step.input ?? {},
          output: step.output ?? {},
          error: step.error,
          label: <FlowNodeLabel title={step.node_id} subtitle={step.node_type} error={step.error} />
        }
      };
    }),
    {
      id: "run-output",
      type: "output",
      position: { x: 260 * (runResponse.run.steps.length + 1), y: 0 },
      className: runResponse.run.error ? "workflow-flow-node-error" : undefined,
      data: {
        title: "Run output",
        output: runResponse.run.outputs ?? {},
        error: runResponse.run.error,
        label: <FlowNodeLabel title="Run output" subtitle={runResponse.run.error} error={runResponse.run.error} />
      }
    }
  ];
  const edges = nodes.slice(0, -1).map((node, index): Edge => ({
    id: `${node.id}-${nodes[index + 1].id}`,
    source: node.id,
    target: nodes[index + 1].id,
    type: "smoothstep"
  }));
  return { nodes, edges };
}

function FlowNodeLabel({ title, subtitle, error }: { title: string; subtitle?: string; error?: string }) {
  return (
    <div className="workflow-flow-node-label">
      <span>{title}</span>
      {subtitle && <span>{subtitle}</span>}
      {error && <span>{error}</span>}
    </div>
  );
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
  workflow
}: {
  canWriteWorkflow: boolean;
  instanceID: string;
  instanceNode: string;
  onSaveInstanceConfig: (
    instanceID: string,
    variables: Record<string, string>,
    secrets: Record<string, string>
  ) => void | Promise<void>;
  ports: { variables: WorkflowPort[]; secrets: WorkflowPort[] };
  workflow?: WorkflowDefinition;
}) {
  const [open, setOpen] = useState(false);
  const [variableDrafts, setVariableDrafts] = useState<Record<string, string>>({});
  const [secretDrafts, setSecretDrafts] = useState<Record<string, string>>({});
  const [dirtySecrets, setDirtySecrets] = useState<Record<string, boolean>>({});

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
  }

  async function saveDrafts() {
    const changedSecrets = Object.fromEntries(
      Object.entries(secretDrafts).filter(([name, value]) => dirtySecrets[name] && value !== "")
    );
    await onSaveInstanceConfig(instanceID, variableDrafts, changedSecrets);
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
        <Button icon={<EditRegular />} aria-label={`Edit config ${instanceID}`} disabled={!canWriteWorkflow} />
      </PopoverTrigger>
      <PopoverSurface className="instance-config-popover" aria-label={`Instance config ${instanceID}`}>
        <div className="instance-config-popover-header">
          <div>
            <span>{instanceID}</span>
            <span>{instanceNode}</span>
          </div>
        </div>
        {ports.variables.map((port) => (
          <FluentField key={`variable-${instanceID}-${port.name}`} label={port.name}>
            <Input
              aria-label={`Variable ${instanceID}.${port.name}`}
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
                aria-label={`Secret ${instanceID}.${port.name}`}
                type="password"
                placeholder="Enter secret value"
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
            Save config
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

function formatPorts(ports: Array<{ name: string; type: string }>): string {
  if (ports.length === 0) {
    return "none";
  }
  return ports.map((port) => `${port.name}:${port.type}`).join(", ");
}

function instanceConfigKey(instanceID: string, name: string): string {
  return `${instanceID}.${name}`;
}

function secretMask(length: number): string {
  return length > 0 ? "x".repeat(length) : "";
}

function RunPayload({ title, value }: { title: string; value: Record<string, unknown> }) {
  return (
    <div className="flow-payload">
      <Text size={200} weight="semibold">
        {title}
      </Text>
      <pre>{JSON.stringify(value, null, 2)}</pre>
    </div>
  );
}
