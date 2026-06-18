import { type FormEvent, useEffect, useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import { replaceResource } from "../appState";
import {
  createRow,
  deleteForm,
  deleteWorkflow,
  listForms,
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
import { renderFormScript, type FormElement } from "../formRuntime";
import { rowRecordToValues } from "../tableGrid";
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
  onStatus: (message: string) => void;
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
  const [formValues, setFormValues] = useState<Record<string, string>>({});
  const [newWorkflowName, setNewWorkflowName] = useState("");
  const [newFormName, setNewFormName] = useState("");
  const [workflowInstances, setWorkflowInstances] = useState<WorkflowInstanceResult>(emptyWorkflowInstances);
  const [workflowTrigger, setWorkflowTrigger] = useState<WorkflowTriggerDeclaration | undefined>(undefined);

  const selectedWorkflow = workflows.find((item) => item.id === selectedWorkflowID) ?? workflows[0];
  const selectedForm = forms.find((item) => item.id === selectedFormID) ?? forms[0];
  const selectedWorkflowRun =
    workflowRuns.find((run) => run.history_key === selectedWorkflowRunKey) ?? workflowRuns[0] ?? null;
  const renderedForm = useMemo(() => renderFormScript(selectedForm?.script ?? ""), [selectedForm?.script]);

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
    setFormValues({});
  }, [selectedForm?.id, selectedForm?.script]);

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
      const [nextWorkflows, nextForms, nextWorkflowNodes] = await Promise.all([
        listWorkflows(dbName),
        listForms(dbName),
        loadWorkflowNodes()
      ]);
      if (cancelled) {
        return;
      }
      applyResources(nextWorkflows, nextForms, nextWorkflowNodes);
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
        if (cancelled) {
          return;
        }
        const newestFirst = [...runs].reverse();
        setWorkflowRuns(newestFirst);
        setSelectedWorkflowRunKey(newestFirst[0]?.history_key ?? "");
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

  function applyResources(
    nextWorkflows: WorkflowDefinition[],
    nextForms: FormDefinition[],
    nextWorkflowNodes: WorkflowNodeInfo[]
  ) {
    setWorkflows(nextWorkflows);
    setForms(nextForms);
    setWorkflowNodes(nextWorkflowNodes);
    setSelectedWorkflowID(nextWorkflows[0]?.id ?? 0);
    setSelectedFormID(nextForms[0]?.id ?? 0);
  }

  function clearResources() {
    applyResources([], [], []);
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
      onStatus(error instanceof Error ? error.message : t("status.workflowSaveFailed"));
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
    } catch (error) {
      onStatus(error instanceof Error ? error.message : t("status.workflowCreationFailed"));
    }
  }

  async function executeWorkflow() {
    if (!selectedWorkflow?.id) {
      onStatus(t("status.saveWorkflowBeforeRunning"));
      return;
    }
    try {
      const response = await runWorkflow(selectedWorkflow.id, {});
      setWorkflowRuns((current) => [response, ...current.filter((run) => run.history_key !== response.history_key)]);
      setSelectedWorkflowRunKey(response.history_key);
      if (response.run.error) {
        onStatus(t("status.workflowFailed", { error: response.run.error }));
        return;
      }
      onStatus(t("status.workflowRunSaved", { key: response.history_key }));
    } catch (error) {
      onStatus(error instanceof Error ? error.message : t("status.workflowRunFailed"));
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
      onStatus(error instanceof Error ? error.message : t("status.workflowStatusUpdateFailed"));
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
      onStatus(error instanceof Error ? error.message : t("status.workflowRenameFailed"));
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
      onStatus(error instanceof Error ? error.message : t("status.workflowDeleteFailed"));
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
      onStatus(error instanceof Error ? error.message : t("status.formSaveFailed"));
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
      onStatus(error instanceof Error ? error.message : t("status.formPublishFailed"));
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
      onStatus(error instanceof Error ? error.message : t("status.formUnpublishFailed"));
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
      onStatus(error instanceof Error ? error.message : t("status.formRenameFailed"));
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
      setFormValues({});
      onStatus(t("status.deletedForm", { name: form.name }));
    } catch (error) {
      onStatus(error instanceof Error ? error.message : t("status.formDeleteFailed"));
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
      setFormValues({});
      setNewFormName("");
      onStatus(t("status.createdForm", { name: saved.name }));
    } catch (error) {
      onStatus(error instanceof Error ? error.message : t("status.formCreationFailed"));
    }
  }

  async function submitRenderedForm(submitElement?: Extract<FormElement, { kind: "submit" }>, event?: FormEvent<HTMLFormElement>) {
    event?.preventDefault();
    if (!submitElement && !renderedForm.elements.some((element) => element.kind === "submit")) {
      return;
    }
    if (!databaseName || !renderedForm.table || !renderedForm.fields) {
      onStatus(t("status.formRenderTargetRequired"));
      return;
    }
    const inputValues = Object.fromEntries(
      renderedForm.elements.flatMap((element) => {
        if (element.kind === "input") {
          return [[element.name, formValues[element.name] ?? ""]];
        }
        if (element.kind === "select") {
          return [[element.name, formValues[element.name] ?? element.options[0] ?? ""]];
        }
        return [];
      })
    );
    const values = Object.fromEntries(
      Object.entries(renderedForm.fields).map(([inputID, fieldName]) => [fieldName, inputValues[inputID] ?? ""])
    );
    try {
      const saved = await createRow(databaseName, renderedForm.table, values);
      onSubmittedRow(renderedForm.table, rowRecordToValues(saved));
      onStatus(t("status.formCreatedRecord", { table: renderedForm.table, id: saved.record_id }));
    } catch (error) {
      onStatus(error instanceof Error ? error.message : t("status.formSubmitFailed"));
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
    secrets: Record<string, string>
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
    try {
      const saved = await saveWorkflow(databaseName, {
        ...selectedWorkflow,
        variables: nextVariables,
        secret_values: nextSecretValues
      });
      setWorkflows((current) => replaceResource(current, saved));
      setSelectedWorkflowID(saved.id ?? 0);
      onStatus(t("status.savedInstanceConfig", { id: instanceID }));
    } catch (error) {
      onStatus(error instanceof Error ? error.message : t("status.workflowConfigSaveFailed"));
    }
  }

  function updateSelectedFormScript(script: string) {
    setForms((current) => current.map((item) => (item.id === selectedForm?.id ? { ...item, script } : item)));
  }

  function updateFormValue(name: string, value: string) {
    setFormValues((current) => ({ ...current, [name]: value }));
  }

  return {
    forms,
    formValues,
    newFormName,
    newWorkflowName,
    renderedForm,
    selectedForm,
    selectedWorkflow,
    selectedWorkflowRun,
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
    setNewFormName,
    setNewWorkflowName,
    setSelectedFormID,
    setSelectedWorkflowID,
    setSelectedWorkflowRunKey,
    submitRenderedForm,
    toggleSelectedWorkflowEnabled,
    unpublishSelectedForm,
    updateFormValue,
    updateSelectedFormScript,
    saveSelectedWorkflowInstanceConfig,
    updateSelectedWorkflowScript
  };
}

function defaultWorkflowScript(targetTable: string) {
  return `/**
 * @param {CodeTableWorkflowDefinitionInfo} info
 * @returns {Record<string, string | CodeTableWorkflowInstanceDeclaration>}
 */
function instances(info) {
  return {
    row_change: 'table.record.changed'
  };
}

/**
 * @param {CodeTableWorkflowDefinitionInfo} info
 * @returns {CodeTableWorkflowTriggerDeclaration}
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
 * @param {CodeTableWorkflowRunInfo} info
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
 * @param {CodeTableFormAPI} api
 * @param {CodeTableFormRoot} root
 * @returns {CodeTableFormDefinition}
 */
function render(api, root) {
  root.append(api.input({ name: 'name', label: 'Name' }), api.submit('Submit'));
  return { table: ${targetTable}, fields: { name: 'name' } };
}`;
}
