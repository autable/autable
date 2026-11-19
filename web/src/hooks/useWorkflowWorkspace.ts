import type { Notify } from "../notifications";
import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { replaceResource } from "../appState";
import {
  deleteWorkflow,
  loadWorkflowRun,
  listWorkflowRuns,
  listWorkflows,
  loadWorkflowNodes,
  runWorkflow,
  saveWorkflow,
  type WorkflowDefinition,
  type WorkflowNodeInfo,
  type WorkflowRunResponse
} from "../api";
import {
  evaluateWorkflowInstances,
  evaluateWorkflowTrigger,
  type WorkflowInstanceResult,
  type WorkflowTriggerDeclaration
} from "../workflowInstances";

type UseWorkflowWorkspaceOptions = {
  currentUserID?: string;
  databaseName: string;
  tableName: string;
  onStatus: Notify;
};

const workflowEvaluationDelayMs = 5000;

export function useWorkflowWorkspace({
  currentUserID,
  databaseName,
  tableName,
  onStatus
}: UseWorkflowWorkspaceOptions) {
  const { t } = useTranslation();
  const emptyWorkflowInstances: WorkflowInstanceResult = { ok: false, error: t("workflow.noWorkflowSelected") };
  const [workflows, setWorkflows] = useState<WorkflowDefinition[]>([]);
  const [workflowNodes, setWorkflowNodes] = useState<WorkflowNodeInfo[]>([]);
  const [selectedWorkflowID, setSelectedWorkflowID] = useState(0);
  const [workflowRuns, setWorkflowRuns] = useState<WorkflowRunResponse[]>([]);
  const [selectedWorkflowRunKey, setSelectedWorkflowRunKey] = useState("");
  const [newWorkflowName, setNewWorkflowName] = useState("");
  const [workflowInstances, setWorkflowInstances] = useState<WorkflowInstanceResult>(emptyWorkflowInstances);
  const [workflowTrigger, setWorkflowTrigger] = useState<WorkflowTriggerDeclaration | undefined>(undefined);
  const [resourcesReady, setResourcesReady] = useState(false);

  const selectedWorkflow = workflows.find((item) => item.id === selectedWorkflowID) ?? workflows[0];
  const selectedWorkflowRun =
    workflowRuns.find((run) => run.history_key === selectedWorkflowRunKey) ?? workflowRuns[0] ?? null;

  useEffect(() => {
    evaluateSelectedWorkflowScript();
  }, [databaseName, selectedWorkflow?.id]);

  useEffect(() => {
    if (!selectedWorkflow) {
      setWorkflowInstances(emptyWorkflowInstances);
      setWorkflowTrigger(undefined);
      return;
    }
    const timeoutID = window.setTimeout(evaluateSelectedWorkflowScript, workflowEvaluationDelayMs);
    return () => window.clearTimeout(timeoutID);
  }, [databaseName, selectedWorkflow?.id, selectedWorkflow?.script]);

  function evaluateSelectedWorkflowScript() {
    if (!selectedWorkflow) {
      setWorkflowInstances(emptyWorkflowInstances);
      setWorkflowTrigger(undefined);
      return;
    }
    const info = {
      workflow_id: selectedWorkflow.id,
      database_name: databaseName
    };
    const nextInstances = evaluateWorkflowInstances(selectedWorkflow.script, info);
    setWorkflowInstances((current) => (nextInstances.ok || !current.ok ? nextInstances : current));
    const nextTrigger = evaluateWorkflowTrigger(selectedWorkflow.script, info);
    if (nextTrigger.ok) {
      setWorkflowTrigger(nextTrigger.value ?? undefined);
    }
  }

  useEffect(() => {
    let cancelled = false;
    if (!databaseName || !currentUserID) {
      clearResources();
      return () => {
        cancelled = true;
      };
    }
    void loadResources(databaseName).catch(() => undefined);
    return () => {
      cancelled = true;
    };

    async function loadResources(dbName: string) {
      setResourcesReady(false);
      const [nextWorkflows, nextWorkflowNodes] = await Promise.all([listWorkflows(dbName), loadWorkflowNodes()]);
      if (cancelled) {
        return;
      }
      applyResources(nextWorkflows, nextWorkflowNodes);
      setResourcesReady(true);
    }
  }, [currentUserID, databaseName]);

  useEffect(() => {
    let cancelled = false;
    if (!currentUserID || !selectedWorkflow?.id) {
      setWorkflowRuns([]);
      setSelectedWorkflowRunKey("");
      return () => {
        cancelled = true;
      };
    }
    void listWorkflowRuns(selectedWorkflow.id)
      .then((runs) => {
        if (!cancelled) {
          applyWorkflowRuns(runs);
        }
      })
      .catch(() => {
        if (!cancelled) {
          setWorkflowRuns([]);
          setSelectedWorkflowRunKey("");
        }
      });
    return () => {
      cancelled = true;
    };
  }, [currentUserID, selectedWorkflow?.id]);

  useEffect(() => {
    let cancelled = false;
    if (!currentUserID || !selectedWorkflow?.id || !selectedWorkflowRunKey || !selectedWorkflowRun?.summary) {
      return () => {
        cancelled = true;
      };
    }
    void loadWorkflowRun(selectedWorkflow.id, selectedWorkflowRunKey)
      .then((run) => {
        if (cancelled) {
          return;
        }
        setWorkflowRuns((current) =>
          current.map((item) => (item.history_key === run.history_key ? run : item))
        );
      })
      .catch((error) => {
        if (!cancelled) {
          onStatus(error instanceof Error ? error.message : t("status.workflowRunFailed"), "error");
        }
      });
    return () => {
      cancelled = true;
    };
  }, [currentUserID, selectedWorkflow?.id, selectedWorkflowRunKey, selectedWorkflowRun?.summary]);

  async function loadWorkflowRuns(workflowID: number, preferredRunKey = "") {
    const runs = await listWorkflowRuns(workflowID);
    const newestFirst = applyWorkflowRuns(runs, preferredRunKey);
    return newestFirst;
  }

  function applyWorkflowRuns(runs: WorkflowRunResponse[], preferredRunKey = "") {
    const newestFirst = [...runs].reverse();
    // Merge against the live state, not the render closure: a concurrent list
    // refresh must never clobber a run detail that already finished loading.
    setWorkflowRuns((current) => mergeWorkflowRuns(current, newestFirst));
    const preferredExists = preferredRunKey && newestFirst.some((run) => run.history_key === preferredRunKey);
    setSelectedWorkflowRunKey(preferredExists ? preferredRunKey : newestFirst[0]?.history_key ?? "");
    return newestFirst;
  }

  async function refreshWorkflowRuns(preferredRunKey = "", workflowID = selectedWorkflow?.id ?? 0) {
    if (!currentUserID || !workflowID) {
      setWorkflowRuns([]);
      setSelectedWorkflowRunKey("");
      return [];
    }
    return loadWorkflowRuns(workflowID, preferredRunKey);
  }

  function applyResources(nextWorkflows: WorkflowDefinition[], nextWorkflowNodes: WorkflowNodeInfo[]) {
    setWorkflows(nextWorkflows);
    setWorkflowNodes(nextWorkflowNodes);
    setSelectedWorkflowID((current) =>
      nextWorkflows.some((item) => item.id === current) ? current : nextWorkflows[0]?.id ?? 0
    );
  }

  function clearResources() {
    applyResources([], []);
    setResourcesReady(false);
    setWorkflowRuns([]);
    setSelectedWorkflowRunKey("");
  }

  async function refreshResources(nextDatabaseName = databaseName) {
    if (!currentUserID || !nextDatabaseName) {
      clearResources();
      return;
    }
    const [nextWorkflows, nextWorkflowNodes] = await Promise.all([
      listWorkflows(nextDatabaseName),
      loadWorkflowNodes()
    ]);
    applyResources(nextWorkflows, nextWorkflowNodes);
    setResourcesReady(true);
  }

  async function persistWorkflow() {
    if (!selectedWorkflow) {
      return;
    }
    try {
      const saved = await saveWorkflow(databaseName, selectedWorkflow);
      setWorkflows((current) => replaceResource(current, saved));
      setSelectedWorkflowID(saved.id ?? 0);
      onStatus(t("status.savedWorkflow", { id: saved.id }));
    } catch (error) {
      onStatus(error instanceof Error ? error.message : t("status.workflowSaveFailed"), "error");
    }
  }

  async function createWorkflow() {
    const name = newWorkflowName.trim();
    if (!databaseName) {
      onStatus(t("status.selectDatabaseBeforeWorkflow"));
      return;
    }
    if (!name) {
      onStatus(t("status.workflowNameRequired"));
      return;
    }
    try {
      const targetTable = tableName ? JSON.stringify(tableName) : '""';
      const saved = await saveWorkflow(databaseName, {
        database_name: databaseName,
        name,
        script: defaultWorkflowScript(targetTable),
        enabled: true,
        secrets: {},
        variables: {}
      });
      setWorkflows((current) => replaceResource(current, saved));
      setSelectedWorkflowID(saved.id ?? 0);
      setWorkflowRuns([]);
      setSelectedWorkflowRunKey("");
      setNewWorkflowName("");
      onStatus(t("status.createdWorkflow", { name: saved.name }));
      return saved;
    } catch (error) {
      onStatus(error instanceof Error ? error.message : t("status.workflowCreationFailed"), "error");
      return undefined;
    }
  }

  async function executeWorkflow() {
    if (!selectedWorkflow?.id) {
      onStatus(t("status.saveWorkflowBeforeRunning"));
      return;
    }
    try {
      const response = await runWorkflow(selectedWorkflow.id, {});
      await refreshWorkflowRuns(response.history_key, selectedWorkflow.id);
      // The run response already carries the full run detail; seed it so the
      // history panel does not depend on the summary-upgrade fetch.
      setWorkflowRuns((current) =>
        current.map((item) => (item.history_key === response.history_key ? response : item))
      );
      if (response.run.error) {
        onStatus(t("status.workflowFailed", { error: response.run.error }));
        return response;
      }
      onStatus(t("status.workflowRunSaved", { key: response.history_key }));
      return response;
    } catch (error) {
      onStatus(error instanceof Error ? error.message : t("status.workflowRunFailed"), "error");
      return undefined;
    }
  }

  async function toggleSelectedWorkflowEnabled(enabled: boolean) {
    if (!selectedWorkflow) {
      return;
    }
    try {
      const saved = await saveWorkflow(databaseName, { ...selectedWorkflow, enabled });
      setWorkflows((current) => replaceResource(current, saved));
      setSelectedWorkflowID(saved.id ?? 0);
      onStatus(t("status.workflowStatus", { name: saved.name, status: enabled ? t("common.enabled") : t("common.disabled") }));
    } catch (error) {
      onStatus(error instanceof Error ? error.message : t("status.workflowStatusUpdateFailed"), "error");
    }
  }

  async function setSelectedWorkflowHistoryRetention(days: number | null) {
    if (!selectedWorkflow) {
      return;
    }
    try {
      const saved = await saveWorkflow(databaseName, { ...selectedWorkflow, history_retention_days: days });
      setWorkflows((current) => replaceResource(current, saved));
      setSelectedWorkflowID(saved.id ?? 0);
      onStatus(t("status.workflowHistoryRetentionUpdated", { name: saved.name }));
    } catch (error) {
      onStatus(error instanceof Error ? error.message : t("status.workflowStatusUpdateFailed"), "error");
    }
  }

  async function setSelectedWorkflowTimeoutSeconds(seconds: number | null) {
    if (!selectedWorkflow) {
      return;
    }
    try {
      const saved = await saveWorkflow(databaseName, { ...selectedWorkflow, timeout_seconds: seconds });
      setWorkflows((current) => replaceResource(current, saved));
      setSelectedWorkflowID(saved.id ?? 0);
      onStatus(t("status.workflowTimeoutUpdated", { name: saved.name }));
    } catch (error) {
      onStatus(error instanceof Error ? error.message : t("status.workflowStatusUpdateFailed"), "error");
    }
  }

  async function renameSelectedWorkflow(name: string, workflowID = selectedWorkflow?.id ?? 0) {
    const trimmed = name.trim();
    const workflow = workflows.find((item) => item.id === workflowID) ?? selectedWorkflow;
    if (!workflow || !trimmed) {
      onStatus(t("status.workflowNameRequired"));
      return;
    }
    try {
      const saved = await saveWorkflow(databaseName, { ...workflow, name: trimmed });
      setWorkflows((current) => replaceResource(current, saved));
      setSelectedWorkflowID(saved.id ?? 0);
      onStatus(t("status.renamedWorkflow", { name: saved.name }));
    } catch (error) {
      onStatus(error instanceof Error ? error.message : t("status.workflowRenameFailed"), "error");
    }
  }

  async function deleteSelectedWorkflow(workflowID = selectedWorkflow?.id ?? 0) {
    const workflow = workflows.find((item) => item.id === workflowID) ?? selectedWorkflow;
    if (!workflow?.id) {
      return;
    }
    try {
      await deleteWorkflow(workflow.id);
      setWorkflows((current) => current.filter((item) => item.id !== workflow.id));
      setSelectedWorkflowID(workflows.find((item) => item.id !== workflow.id)?.id ?? 0);
      setWorkflowRuns([]);
      setSelectedWorkflowRunKey("");
      onStatus(t("status.deletedWorkflow", { name: workflow.name }));
    } catch (error) {
      onStatus(error instanceof Error ? error.message : t("status.workflowDeleteFailed"), "error");
    }
  }

  function updateSelectedWorkflowScript(script: string) {
    setWorkflows((current) =>
      current.map((item) => (item.id === selectedWorkflow?.id ? { ...item, script } : item))
    );
  }

  async function saveSelectedWorkflowInstanceConfig(
    instanceID: string,
    variables: Record<string, string>,
    secrets: Record<string, string>,
    runnerName: string
  ) {
    if (!selectedWorkflow) {
      return;
    }
    const prefix = `${instanceID}.`;
    const nextVariables = { ...(selectedWorkflow.variables ?? {}) };
    for (const [name, value] of Object.entries(variables)) {
      nextVariables[prefix + name] = value;
    }
    const nextSecretValues: Record<string, string> = {};
    for (const [name, value] of Object.entries(secrets)) {
      if (value !== "") {
        nextSecretValues[prefix + name] = value;
      }
    }
    const nextRunners = { ...(selectedWorkflow.runners ?? {}) };
    if (runnerName === "") {
      delete nextRunners[instanceID];
    } else {
      nextRunners[instanceID] = runnerName;
    }
    try {
      const saved = await saveWorkflow(databaseName, {
        ...selectedWorkflow,
        variables: nextVariables,
        secret_values: nextSecretValues,
        runners: nextRunners
      });
      setWorkflows((current) => replaceResource(current, saved));
      setSelectedWorkflowID(saved.id ?? 0);
      onStatus(t("status.savedInstanceConfig", { id: instanceID }));
    } catch (error) {
      onStatus(error instanceof Error ? error.message : t("status.workflowConfigSaveFailed"), "error");
    }
  }

  return {
    newWorkflowName,
    selectedWorkflow,
    selectedWorkflowRun,
    resourcesReady,
    workflowInstances,
    workflowTrigger,
    workflowNodes,
    workflowRuns,
    workflows,
    clearResources,
    createWorkflow,
    deleteSelectedWorkflow,
    executeWorkflow,
    persistWorkflow,
    renameSelectedWorkflow,
    refreshResources,
    refreshWorkflowRuns,
    setNewWorkflowName,
    setSelectedWorkflowHistoryRetention,
    setSelectedWorkflowTimeoutSeconds,
    setSelectedWorkflowID,
    setSelectedWorkflowRunKey,
    toggleSelectedWorkflowEnabled,
    saveSelectedWorkflowInstanceConfig,
    updateSelectedWorkflowScript
  };
}

function defaultWorkflowScript(targetTable: string) {
  return `/**
 * @param {AutableWorkflowDefinitionInfo} info
 * @returns {Record<string, string | AutableWorkflowInstanceDeclaration>}
 */
function instances(info) {
  return {
    row_change: 'table.record.changed'
  };
}

/**
 * @param {AutableWorkflowDefinitionInfo} info
 * @returns {AutableWorkflowTriggerDeclaration}
 */
function trigger(info) {
  return {
    instance: 'row_change',
    params: {
      table: ${targetTable},
      operations: ['create', 'update', 'delete']
    }
  };
}

/**
 * @param {AutableWorkflowRunInfo} info
 * @returns {Record<string, unknown>}
 */
function run(info) {
  return {
    database: info.inputs.database,
    table: info.inputs.table,
    record_id: info.inputs.record_id,
    operation: info.inputs.operation,
    diff: info.inputs.diff
  };
}`;
}

// Prefer runs whose detail already finished loading over their incoming
// summaries, so a concurrent list refresh never wipes loaded outputs.
export function mergeWorkflowRuns(
  current: WorkflowRunResponse[],
  incoming: WorkflowRunResponse[]
): WorkflowRunResponse[] {
  const loadedRuns = new Map(current.filter((run) => !run.summary).map((run) => [run.history_key, run]));
  return incoming.map((run) => (run.summary ? loadedRuns.get(run.history_key) ?? run : run));
}
