import { Button, Text, Textarea } from "@fluentui/react-components";
import { PlayRegular, SaveRegular } from "@fluentui/react-icons";
import type { WorkflowDefinition, WorkflowNodeInfo, WorkflowRunResponse } from "../api";

type WorkflowWorkspaceProps = {
  databaseName: string;
  onExecute: () => void;
  onSave: () => void;
  onUpdateInputsJSON: (text: string) => void;
  onSelectRunKey: (historyKey: string) => void;
  onUpdateConfigJSON: (kind: "secrets" | "variables", text: string) => void;
  onUpdateScript: (script: string) => void;
  selectedRun: WorkflowRunResponse | null;
  inputsText: string;
  variablesText: string;
  secretsText: string;
  workflow?: WorkflowDefinition;
  workflowNodes: WorkflowNodeInfo[];
  workflowRuns: WorkflowRunResponse[];
};

export function WorkflowWorkspace({
  databaseName,
  onExecute,
  onSave,
  onUpdateInputsJSON,
  onSelectRunKey,
  onUpdateConfigJSON,
  onUpdateScript,
  selectedRun,
  inputsText,
  secretsText,
  variablesText,
  workflow,
  workflowNodes,
  workflowRuns
}: WorkflowWorkspaceProps) {
  const canWriteWorkflow = (workflow?.permission_level ?? 2) >= 2;

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
          <label className="field-stack">
            <span>Variables JSON</span>
            <Textarea
              className="json-editor"
              value={variablesText}
              onChange={(_, data) => onUpdateConfigJSON("variables", data.value)}
              resize="none"
              disabled={!canWriteWorkflow}
              aria-label="Workflow Variables JSON"
            />
          </label>
          <label className="field-stack">
            <span>Secrets JSON</span>
            <Textarea
              className="json-editor"
              value={secretsText}
              onChange={(_, data) => onUpdateConfigJSON("secrets", data.value)}
              resize="none"
              disabled={!canWriteWorkflow}
              aria-label="Workflow Secrets JSON"
            />
          </label>
        </div>
      </div>
      <div className="history-pane">
        <Text weight="semibold">Nodes</Text>
        <div className="node-list">
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
              </div>
            </div>
          ))}
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
            selectedRun.run.steps.map((step, index) => (
              <span key={`${step.node_id}-${index}`} className={step.error ? "flow-step error" : "flow-step"}>
                {step.error ? `${step.node_id}: ${step.error}` : step.node_id}
              </span>
            ))
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

function formatPorts(ports: Array<{ name: string; type: string }>): string {
  if (ports.length === 0) {
    return "none";
  }
  return ports.map((port) => `${port.name}:${port.type}`).join(", ");
}
