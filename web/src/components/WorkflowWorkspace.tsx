import { Button, Field as FluentField, Input, Text, Textarea } from "@fluentui/react-components";
import { PlayRegular, SaveRegular } from "@fluentui/react-icons";
import type {
  WorkflowDefinition,
  WorkflowInstanceDeclaration,
  WorkflowNodeInfo,
  WorkflowPort,
  WorkflowRunResponse
} from "../api";

type WorkflowWorkspaceProps = {
  databaseName: string;
  onExecute: () => void;
  onSave: () => void;
  onUpdateInputsJSON: (text: string) => void;
  onSelectRunKey: (historyKey: string) => void;
  onUpdateInstanceConfig: (
    kind: "secrets" | "variables",
    instanceID: string,
    name: string,
    value: string
  ) => void;
  onUpdateScript: (script: string) => void;
  selectedRun: WorkflowRunResponse | null;
  inputsText: string;
  workflow?: WorkflowDefinition;
  workflowInstances:
    | { ok: true; value: Record<string, WorkflowInstanceDeclaration> }
    | { ok: false; error: string };
  workflowNodes: WorkflowNodeInfo[];
  workflowRuns: WorkflowRunResponse[];
};

export function WorkflowWorkspace({
  databaseName,
  onExecute,
  onSave,
  onUpdateInputsJSON,
  onSelectRunKey,
  onUpdateInstanceConfig,
  onUpdateScript,
  selectedRun,
  inputsText,
  workflow,
  workflowInstances,
  workflowNodes,
  workflowRuns
}: WorkflowWorkspaceProps) {
  const canWriteWorkflow = (workflow?.permission_level ?? 2) >= 2;
  const workflowNodesByType = new Map(workflowNodes.map((node) => [node.type, node]));

  return (
    <div className="split-view">
      <div className="editor-pane">
        <div className="section-header">
          <div>
            <Text weight="semibold">{workflow?.name ?? "workflow"}.js</Text>
            <Text size={200}>{databaseName} workflow</Text>
          </div>
          <Button icon={<SaveRegular />} appearance="primary" onClick={onSave} disabled={!canWriteWorkflow}>
            Save
          </Button>
        </div>
        <Textarea
          className="code-editor"
          value={workflow?.script ?? ""}
          onChange={(_, data) => onUpdateScript(data.value)}
          resize="none"
          disabled={!canWriteWorkflow}
          aria-label="Workflow JavaScript"
        />
        <div className="workflow-config-grid">
          <label className="field-stack">
            <span>Inputs JSON</span>
            <Textarea
              className="json-editor"
              value={inputsText}
              onChange={(_, data) => onUpdateInputsJSON(data.value)}
              resize="none"
              aria-label="Workflow Inputs JSON"
            />
          </label>
          <InstanceConfigEditor
            canWriteWorkflow={canWriteWorkflow}
            onUpdateInstanceConfig={onUpdateInstanceConfig}
            workflow={workflow}
            workflowInstances={workflowInstances}
            workflowNodesByType={workflowNodesByType}
          />
        </div>
      </div>
      <div className="history-pane">
        <Text weight="semibold">Nodes</Text>
        <div className="node-list" aria-label="Workflow nodes">
          {workflowNodes.map((node) => (
            <div key={node.type} className={node.trigger ? "node-item trigger" : "node-item"}>
              <div className="node-title">
                <span>{node.type}</span>
                <span>{node.stateless ? "stateless" : "stateful"}</span>
              </div>
              <Text size={200}>{node.description ?? node.display_name}</Text>
              <div className="node-ports">
                <span>in {formatPorts(node.inputs)}</span>
                <span>out {formatPorts(node.outputs)}</span>
                <span>vars {formatPorts(node.variables ?? [])}</span>
                <span>secrets {formatPorts(node.secrets ?? [])}</span>
              </div>
            </div>
          ))}
        </div>
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
                </div>
              );
            })
          ) : (
            <Text size={200}>{workflowInstances.error}</Text>
          )}
        </div>
        <Text weight="semibold">Run flow</Text>
        {workflowRuns.length > 0 && (
          <div className="run-history-list" aria-label="Workflow run history">
            {workflowRuns.map((run) => (
              <button
                key={run.history_key}
                className={run.history_key === selectedRun?.history_key ? "run-history-item selected" : "run-history-item"}
                type="button"
                onClick={() => onSelectRunKey(run.history_key)}
              >
                <span>{run.history_key}</span>
                <span>{new Date(run.run.timestamp).toLocaleString()}</span>
              </button>
            ))}
          </div>
        )}
        <div className="flow-line" aria-label="Workflow run flow">
          {selectedRun && selectedRun.run.steps.length > 0 ? (
            <>
              <RunPayload title="Run input" value={selectedRun.run.inputs ?? {}} />
              {selectedRun.run.steps.map((step, index) => (
                <div key={`${step.node_id}-${index}`} className={step.error ? "flow-step error" : "flow-step"}>
                  <div className="flow-step-title">
                    <Text weight="semibold">{step.node_id}</Text>
                    {step.node_type && <Text size={200}>{step.node_type}</Text>}
                    {step.error && <Text size={200}>{step.error}</Text>}
                  </div>
                  <div className="flow-step-payloads">
                    <RunPayload title="Input" value={step.input ?? {}} />
                    <RunPayload title="Output" value={step.output ?? {}} />
                  </div>
                </div>
              ))}
              <RunPayload title="Run output" value={selectedRun.run.outputs ?? {}} />
              {selectedRun.run.error && <div className="flow-step error">{selectedRun.run.error}</div>}
            </>
          ) : (
            <span className="flow-empty">No runs yet</span>
          )}
        </div>
        <Button icon={<PlayRegular />} onClick={onExecute} disabled={!workflow?.id || !canWriteWorkflow}>
          Run
        </Button>
      </div>
    </div>
  );
}

function InstanceConfigEditor({
  canWriteWorkflow,
  onUpdateInstanceConfig,
  workflow,
  workflowInstances,
  workflowNodesByType
}: {
  canWriteWorkflow: boolean;
  onUpdateInstanceConfig: (
    kind: "secrets" | "variables",
    instanceID: string,
    name: string,
    value: string
  ) => void;
  workflow?: WorkflowDefinition;
  workflowInstances:
    | { ok: true; value: Record<string, WorkflowInstanceDeclaration> }
    | { ok: false; error: string };
  workflowNodesByType: Map<string, WorkflowNodeInfo>;
}) {
  if (!workflowInstances.ok) {
    return (
      <div className="instance-config-panel">
        <Text weight="semibold">Instance config</Text>
        <Text size={200}>{workflowInstances.error}</Text>
      </div>
    );
  }
  const entries = Object.entries(workflowInstances.value)
    .map(([instanceID, instance]) => ({
      instanceID,
      instance,
      ports: effectiveInstancePorts(instance, workflowNodesByType)
    }))
    .filter(({ ports }) => ports.variables.length > 0 || ports.secrets.length > 0);
  if (entries.length === 0) {
    return (
      <div className="instance-config-panel">
        <Text weight="semibold">Instance config</Text>
        <Text size={200}>No variables or secrets declared</Text>
      </div>
    );
  }
  return (
    <div className="instance-config-panel" aria-label="Workflow instance config">
      <Text weight="semibold">Instance config</Text>
      {entries.map(({ instanceID, instance, ports }) => (
        <div className="instance-config-group" key={instanceID}>
          <div className="node-title">
            <span>{instanceID}</span>
            <span>{instance.node}</span>
          </div>
          {ports.variables.map((port) => (
            <FluentField key={`variable-${instanceID}-${port.name}`} label={port.name}>
              <Input
                aria-label={`Variable ${instanceID}.${port.name}`}
                value={workflow?.variables?.[instanceConfigKey(instanceID, port.name)] ?? ""}
                onChange={(_, data) => onUpdateInstanceConfig("variables", instanceID, port.name, data.value)}
                disabled={!canWriteWorkflow}
              />
            </FluentField>
          ))}
          {ports.secrets.map((port) => (
            <FluentField key={`secret-${instanceID}-${port.name}`} label={port.name}>
              <Input
                aria-label={`Secret ${instanceID}.${port.name}`}
                type="password"
                value={workflow?.secrets?.[instanceConfigKey(instanceID, port.name)] ?? ""}
                onChange={(_, data) => onUpdateInstanceConfig("secrets", instanceID, port.name, data.value)}
                disabled={!canWriteWorkflow}
              />
            </FluentField>
          ))}
        </div>
      ))}
    </div>
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
