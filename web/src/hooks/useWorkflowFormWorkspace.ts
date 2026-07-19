import type { Notify } from "../notifications";
import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { replaceResource } from "../appState";
import {
  deleteForm,
  deleteWorkflow,
  listForms,
  loadWorkflowRun,
  listWorkflowRuns,
  listWorkflows,
  loadWorkflowNodes,
  publishForm,
  runWorkflow,
  saveForm,
  saveWorkflow,
  unpublishForm,
  type FormDefinition,
  type WorkflowDefinition,
  type WorkflowNodeInfo,
  type WorkflowRunResponse
} from "../api";
import { rowRecordToValues } from "../tableGrid";
import { useFormRunner } from "./useFormRunner";
import {
  evaluateWorkflowInstances,
  evaluateWorkflowTrigger,
  type WorkflowInstanceResult,
  type WorkflowTriggerDeclaration
} from "../workflowInstances";

type UseWorkflowFormWorkspaceOptions = {
  currentUserID?: string;
  databaseName: string;
  tableName: string;
  onStatus: Notify;
  onSubmittedRow: (targetTableName: string, row: ReturnType<typeof rowRecordToValues>) => void;
};

const workflowEvaluationDelayMs = 5000;

export function useWorkflowFormWorkspace({
  currentUserID,
  databaseName,
  tableName,
  onStatus,
  onSubmittedRow
}: UseWorkflowFormWorkspaceOptions) {
  const { t } = useTranslation();
  const emptyWorkflowInstances: WorkflowInstanceResult = { ok: false, error: t("workflow.noWorkflowSelected") };
  const [workflows, setWorkflows] = useState<WorkflowDefinition[]>([]);
  const [workflowNodes, setWorkflowNodes] = useState<WorkflowNodeInfo[]>([]);
  const [forms, setForms] = useState<FormDefinition[]>([]);
  const [selectedWorkflowID, setSelectedWorkflowID] = useState(0);
  const [selectedFormID, setSelectedFormID] = useState(0);
  const [workflowRuns, setWorkflowRuns] = useState<WorkflowRunResponse[]>([]);
  const [selectedWorkflowRunKey, setSelectedWorkflowRunKey] = useState("");
  const [newWorkflowName, setNewWorkflowName] = useState("");
  const [newFormName, setNewFormName] = useState("");
  const [workflowInstances, setWorkflowInstances] = useState<WorkflowInstanceResult>(emptyWorkflowInstances);
  const [workflowTrigger, setWorkflowTrigger] = useState<WorkflowTriggerDeclaration | undefined>(undefined);
  const [resourcesReady, setResourcesReady] = useState(false);

  const selectedWorkflow = workflows.find((item) => item.id === selectedWorkflowID) ?? workflows[0];
  const selectedForm = forms.find((item) => item.id === selectedFormID) ?? forms[0];
  const selectedWorkflowRun =
    workflowRuns.find((run) => run.history_key === selectedWorkflowRunKey) ?? workflowRuns[0] ?? null;
  const formRunner = useFormRunner({
    databaseName,
    script: selectedForm?.script ?? "",
    onStatus,
    onRowCreated: (targetTableName, row) => {
      onSubmittedRow(targetTableName, rowRecordToValues(row));
      onStatus(t("status.formCreatedRecord", { table: targetTableName, id: row.record_id }));
    }
  });

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
      const [nextWorkflows, nextForms, nextWorkflowNodes] = await Promise.all([
        listWorkflows(dbName),
        listForms(dbName),
        loadWorkflowNodes()
      ]);
      if (cancelled) {
        return;
      }
      applyResources(nextWorkflows, nextForms, nextWorkflowNodes);
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
    const loadedRuns = new Map(workflowRuns.filter((run) => !run.summary).map((run) => [run.history_key, run]));
    const mergedRuns = newestFirst.map((run) => loadedRuns.get(run.history_key) ?? run);
    setWorkflowRuns(mergedRuns);
    const preferredExists = preferredRunKey && mergedRuns.some((run) => run.history_key === preferredRunKey);
    setSelectedWorkflowRunKey(preferredExists ? preferredRunKey : mergedRuns[0]?.history_key ?? "");
    return mergedRuns;
  }

  async function refreshWorkflowRuns(preferredRunKey = "", workflowID = selectedWorkflow?.id ?? 0) {
    if (!currentUserID || !workflowID) {
      setWorkflowRuns([]);
      setSelectedWorkflowRunKey("");
      return [];
    }
    return loadWorkflowRuns(workflowID, preferredRunKey);
  }

  function applyResources(
    nextWorkflows: WorkflowDefinition[],
    nextForms: FormDefinition[],
    nextWorkflowNodes: WorkflowNodeInfo[]
  ) {
    setWorkflows(nextWorkflows);
    setForms(nextForms);
    setWorkflowNodes(nextWorkflowNodes);
    setSelectedWorkflowID((current) =>
      nextWorkflows.some((item) => item.id === current) ? current : nextWorkflows[0]?.id ?? 0
    );
    setSelectedFormID((current) => (nextForms.some((item) => item.id === current) ? current : nextForms[0]?.id ?? 0));
  }

  function clearResources() {
    applyResources([], [], []);
    setResourcesReady(false);
    setWorkflowRuns([]);
    setSelectedWorkflowRunKey("");
  }

  async function refreshResources(nextDatabaseName = databaseName) {
    if (!currentUserID || !nextDatabaseName) {
      clearResources();
      return;
    }
    const [nextWorkflows, nextForms, nextWorkflowNodes] = await Promise.all([
      listWorkflows(nextDatabaseName),
      listForms(nextDatabaseName),
      loadWorkflowNodes()
    ]);
    applyResources(nextWorkflows, nextForms, nextWorkflowNodes);
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

  async function persistForm() {
    if (!selectedForm) {
      return;
    }
    try {
      const saved = await saveForm(databaseName, selectedForm);
      setForms((current) => replaceResource(current, saved));
      setSelectedFormID(saved.id ?? 0);
      onStatus(t("status.savedForm", { id: saved.id }));
    } catch (error) {
      onStatus(error instanceof Error ? error.message : t("status.formSaveFailed"), "error");
    }
  }

  async function publishSelectedForm() {
    if (!selectedForm?.id) {
      onStatus(t("status.saveFormBeforePublishing"));
      return;
    }
    try {
      const saved = await publishForm(selectedForm.id);
      setForms((current) => replaceResource(current, saved));
      setSelectedFormID(saved.id ?? 0);
      onStatus(t("status.publishedForm", { name: saved.name }));
    } catch (error) {
      onStatus(error instanceof Error ? error.message : t("status.formPublishFailed"), "error");
    }
  }

  async function unpublishSelectedForm() {
    if (!selectedForm?.id) {
      onStatus(t("status.saveFormBeforeUnpublishing"));
      return;
    }
    try {
      const saved = await unpublishForm(selectedForm.id);
      setForms((current) => replaceResource(current, saved));
      setSelectedFormID(saved.id ?? 0);
      onStatus(t("status.unpublishedForm", { name: saved.name }));
    } catch (error) {
      onStatus(error instanceof Error ? error.message : t("status.formUnpublishFailed"), "error");
    }
  }

  async function renameSelectedForm(name: string, formID = selectedForm?.id ?? 0) {
    const trimmed = name.trim();
    const form = forms.find((item) => item.id === formID) ?? selectedForm;
    if (!form || !trimmed) {
      onStatus(t("status.formNameRequired"));
      return;
    }
    try {
      const saved = await saveForm(databaseName, { ...form, name: trimmed });
      setForms((current) => replaceResource(current, saved));
      setSelectedFormID(saved.id ?? 0);
      onStatus(t("status.renamedForm", { name: saved.name }));
    } catch (error) {
      onStatus(error instanceof Error ? error.message : t("status.formRenameFailed"), "error");
    }
  }

  async function deleteSelectedForm(formID = selectedForm?.id ?? 0) {
    const form = forms.find((item) => item.id === formID) ?? selectedForm;
    if (!form?.id) {
      return;
    }
    try {
      await deleteForm(form.id);
      setForms((current) => current.filter((item) => item.id !== form.id));
      setSelectedFormID(forms.find((item) => item.id !== form.id)?.id ?? 0);
      onStatus(t("status.deletedForm", { name: form.name }));
    } catch (error) {
      onStatus(error instanceof Error ? error.message : t("status.formDeleteFailed"), "error");
    }
  }

  async function createForm() {
    const name = newFormName.trim();
    if (!databaseName) {
      onStatus(t("status.selectDatabaseBeforeForm"));
      return;
    }
    if (!name) {
      onStatus(t("status.formNameRequired"));
      return;
    }
    try {
      const targetTable = tableName ? JSON.stringify(tableName) : '""';
      const saved = await saveForm(databaseName, {
        database_name: databaseName,
        name,
        script: defaultFormScript(targetTable)
      });
      setForms((current) => replaceResource(current, saved));
      setSelectedFormID(saved.id ?? 0);
      setNewFormName("");
      onStatus(t("status.createdForm", { name: saved.name }));
      return saved;
    } catch (error) {
      onStatus(error instanceof Error ? error.message : t("status.formCreationFailed"), "error");
      return undefined;
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

  function updateSelectedFormScript(script: string) {
    setForms((current) => current.map((item) => (item.id === selectedForm?.id ? { ...item, script } : item)));
  }

  return {
    forms,
    formResult: formRunner.result,
    formValues: formRunner.values,
    newFormName,
    newWorkflowName,
    renderedForm: formRunner.rendered,
    selectedForm,
    selectedWorkflow,
    selectedWorkflowRun,
    resourcesReady,
    workflowInstances,
    workflowTrigger,
    workflowNodes,
    workflowRuns,
    workflows,
    clearResources,
    createForm,
    createWorkflow,
    deleteSelectedForm,
    deleteSelectedWorkflow,
    executeWorkflow,
    persistForm,
    publishSelectedForm,
    persistWorkflow,
    renameSelectedForm,
    renameSelectedWorkflow,
    refreshResources,
    refreshWorkflowRuns,
    setNewFormName,
    setNewWorkflowName,
    setSelectedWorkflowHistoryRetention,
    setSelectedWorkflowTimeoutSeconds,
    setSelectedFormID,
    setSelectedWorkflowID,
    setSelectedWorkflowRunKey,
    executeFormAction: formRunner.execute,
    submitRenderedForm: formRunner.submit,
    toggleSelectedWorkflowEnabled,
    unpublishSelectedForm,
    updateFormValue: formRunner.updateValue,
    updateSelectedFormScript,
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

function defaultFormScript(targetTable: string) {
  return `/**
 * @param {AutableFormAPI} api
 * @param {AutableFormRoot} root
 * @returns {AutableFormDefinition}
 */
function render(api, root) {
  root.append(
    api.input({ field: 'name', label: 'Name' }),
    api.button('Submit', async (api) => {
      const row = await api.rows.create(${targetTable}, api.values());
      api.show(row);
    })
  );
  return { table: ${targetTable} };
}`;
}
