import { type FormEvent, useEffect, useMemo, useState } from "react";
import { replaceResource } from "../appState";
import {
  createRow,
  listForms,
  listWorkflowRuns,
  listWorkflows,
  loadWorkflowNodes,
  runWorkflow,
  saveForm,
  saveWorkflow,
  type FormDefinition,
  type WorkflowDefinition,
  type WorkflowNodeInfo,
  type WorkflowRunResponse
} from "../api";
import { renderFormScript, type FormElement } from "../formRuntime";
import { rowRecordToValues } from "../tableGrid";
import { parseAnyMap, parseStringMap, stringMapToJSON } from "../workflowConfig";

type UseWorkflowFormWorkspaceOptions = {
  currentUserID?: string;
  databaseName: string;
  tableName: string;
  onStatus: (message: string) => void;
  onSubmittedRow: (targetTableName: string, row: ReturnType<typeof rowRecordToValues>) => void;
};

export function useWorkflowFormWorkspace({
  currentUserID,
  databaseName,
  tableName,
  onStatus,
  onSubmittedRow
}: UseWorkflowFormWorkspaceOptions) {
  const [workflows, setWorkflows] = useState<WorkflowDefinition[]>([]);
  const [workflowNodes, setWorkflowNodes] = useState<WorkflowNodeInfo[]>([]);
  const [forms, setForms] = useState<FormDefinition[]>([]);
  const [selectedWorkflowID, setSelectedWorkflowID] = useState(0);
  const [selectedFormID, setSelectedFormID] = useState(0);
  const [workflowRuns, setWorkflowRuns] = useState<WorkflowRunResponse[]>([]);
  const [selectedWorkflowRunKey, setSelectedWorkflowRunKey] = useState("");
  const [formValues, setFormValues] = useState<Record<string, string>>({});
  const [workflowInputsText, setWorkflowInputsText] = useState("{}");
  const [workflowSecretsText, setWorkflowSecretsText] = useState("{}");
  const [workflowVariablesText, setWorkflowVariablesText] = useState("{}");
  const [newWorkflowName, setNewWorkflowName] = useState("");
  const [newFormName, setNewFormName] = useState("");

  const selectedWorkflow = workflows.find((item) => item.id === selectedWorkflowID) ?? workflows[0];
  const selectedForm = forms.find((item) => item.id === selectedFormID) ?? forms[0];
  const selectedWorkflowRun =
    workflowRuns.find((run) => run.history_key === selectedWorkflowRunKey) ?? workflowRuns[0] ?? null;
  const renderedForm = useMemo(() => renderFormScript(selectedForm?.script ?? ""), [selectedForm?.script]);

  useEffect(() => {
    setFormValues({});
  }, [selectedForm?.id, selectedForm?.script]);

  useEffect(() => {
    setWorkflowInputsText("{}");
    setWorkflowSecretsText(stringMapToJSON(selectedWorkflow?.secrets ?? {}));
    setWorkflowVariablesText(stringMapToJSON(selectedWorkflow?.variables ?? {}));
  }, [selectedWorkflow?.id]);

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
      onStatus(`Workflow saved as #${saved.id}`);
    } catch (error) {
      onStatus(error instanceof Error ? error.message : "Workflow save failed");
    }
  }

  async function createWorkflow() {
    const name = newWorkflowName.trim();
    if (!databaseName) {
      onStatus("Select a database before creating a workflow");
      return;
    }
    if (!name) {
      onStatus("Workflow name is required");
      return;
    }
    try {
      const saved = await saveWorkflow(databaseName, {
        database_name: databaseName,
        name,
        script: "function run(info) {\n  const echoed = info.node('echo', { value: info.inputs.name });\n  return { message: echoed.value };\n}",
        secrets: {},
        variables: {}
      });
      setWorkflows((current) => replaceResource(current, saved));
      setSelectedWorkflowID(saved.id ?? 0);
      setWorkflowRuns([]);
      setSelectedWorkflowRunKey("");
      setNewWorkflowName("");
      onStatus(`Created workflow ${saved.name}`);
    } catch (error) {
      onStatus(error instanceof Error ? error.message : "Workflow creation failed");
    }
  }

  async function executeWorkflow() {
    if (!selectedWorkflow?.id) {
      onStatus("Save workflow before running");
      return;
    }
    const parsedInputs = parseAnyMap(workflowInputsText);
    if (!parsedInputs.ok) {
      onStatus(parsedInputs.error);
      return;
    }
    try {
      const response = await runWorkflow(selectedWorkflow.id, parsedInputs.value);
      setWorkflowRuns((current) => [response, ...current.filter((run) => run.history_key !== response.history_key)]);
      setSelectedWorkflowRunKey(response.history_key);
      if (response.run.error) {
        onStatus(`Workflow failed: ${response.run.error}`);
        return;
      }
      onStatus(`Workflow run saved: ${response.history_key}`);
    } catch (error) {
      onStatus(error instanceof Error ? error.message : "Workflow run failed");
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
      onStatus(`Form saved as #${saved.id}`);
    } catch (error) {
      onStatus(error instanceof Error ? error.message : "Form save failed");
    }
  }

  async function createForm() {
    const name = newFormName.trim();
    if (!databaseName) {
      onStatus("Select a database before creating a form");
      return;
    }
    if (!name) {
      onStatus("Form name is required");
      return;
    }
    try {
      const targetTable = tableName ? JSON.stringify(tableName) : "undefined";
      const saved = await saveForm(databaseName, {
        database_name: databaseName,
        name,
        script: `root.append(api.input({ name: 'name', label: 'Name' }), api.submit('Submit', { table: ${targetTable} }));`
      });
      setForms((current) => replaceResource(current, saved));
      setSelectedFormID(saved.id ?? 0);
      setFormValues({});
      setNewFormName("");
      onStatus(`Created form ${saved.name}`);
    } catch (error) {
      onStatus(error instanceof Error ? error.message : "Form creation failed");
    }
  }

  async function submitRenderedForm(submitElement?: Extract<FormElement, { kind: "submit" }>, event?: FormEvent<HTMLFormElement>) {
    event?.preventDefault();
    const targetTableName = submitElement?.tableName || tableName;
    if (!databaseName || !targetTableName) {
      onStatus("Select a target table before submitting the form");
      return;
    }
    const values = Object.fromEntries(
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
    try {
      const saved = await createRow(databaseName, targetTableName, values);
      onSubmittedRow(targetTableName, rowRecordToValues(saved));
      onStatus(`Form created ${targetTableName} record ${saved.record_id}`);
    } catch (error) {
      onStatus(error instanceof Error ? error.message : "Form submit failed");
    }
  }

  function updateSelectedWorkflowScript(script: string) {
    setWorkflows((current) =>
      current.map((item) => (item.id === selectedWorkflow?.id ? { ...item, script } : item))
    );
  }

  function updateSelectedWorkflowJSON(kind: "secrets" | "variables", text: string) {
    if (kind === "secrets") {
      setWorkflowSecretsText(text);
    } else {
      setWorkflowVariablesText(text);
    }
    const parsed = parseStringMap(text);
    if (!parsed.ok) {
      onStatus(parsed.error);
      return;
    }
    onStatus("Workflow config updated");
    setWorkflows((current) =>
      current.map((item) => (item.id === selectedWorkflow?.id ? { ...item, [kind]: parsed.value } : item))
    );
  }

  function updateWorkflowInputsJSON(text: string) {
    setWorkflowInputsText(text);
    const parsed = parseAnyMap(text);
    if (!parsed.ok) {
      onStatus(parsed.error);
      return;
    }
    onStatus("Workflow inputs updated");
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
    workflowInputsText,
    workflowNodes,
    workflowRuns,
    workflowSecretsText,
    workflows,
    workflowVariablesText,
    clearResources,
    createForm,
    createWorkflow,
    executeWorkflow,
    persistForm,
    persistWorkflow,
    refreshResources,
    setNewFormName,
    setNewWorkflowName,
    setSelectedFormID,
    setSelectedWorkflowID,
    setSelectedWorkflowRunKey,
    submitRenderedForm,
    updateFormValue,
    updateSelectedFormScript,
    updateSelectedWorkflowJSON,
    updateSelectedWorkflowScript,
    updateWorkflowInputsJSON
  };
}
