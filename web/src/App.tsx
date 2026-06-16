import { type FormEvent, useEffect, useMemo, useState } from "react";
import {
  Button,
  Input,
  Label,
  Select,
  Tab,
  TabList,
  Text,
  Textarea,
  Toolbar,
  ToolbarButton,
  Tooltip
} from "@fluentui/react-components";
import {
  AddRegular,
  ArrowClockwiseRegular,
  DatabaseRegular,
  PlayRegular,
  SaveRegular
} from "@fluentui/react-icons";
import DataEditor, {
  type EditableGridCell,
  type GridCell,
  GridCellKind,
  type GridColumn,
  type Item
} from "@glideapps/glide-data-grid";
import { demoCatalog, initialForms, initialRows, initialWorkflows } from "./demoData";
import { renderFormScript } from "./formRuntime";
import {
  createRow,
  listForms,
  listWorkflows,
  loadMetadata,
  login,
  logout,
  register,
  runWorkflow,
  saveForm,
  saveWorkflow,
  updateRow,
  type AuthUser,
  type Catalog,
  type FormDefinition,
  type TableView,
  type WorkflowDefinition,
  type WorkflowRunResponse
} from "./api";

type View = "table" | "workflow" | "form";

export function App() {
  const [catalog, setCatalog] = useState<Catalog>(demoCatalog);
  const [rows, setRows] = useState(initialRows);
  const [view, setView] = useState<View>("table");
  const [selectedTable, setSelectedTable] = useState("contacts");
  const [selectedTableView, setSelectedTableView] = useState("all");
  const [workflows, setWorkflows] = useState<WorkflowDefinition[]>(initialWorkflows);
  const [forms, setForms] = useState<FormDefinition[]>(initialForms);
  const [selectedWorkflowID, setSelectedWorkflowID] = useState(initialWorkflows[0]?.id ?? 0);
  const [selectedFormID, setSelectedFormID] = useState(initialForms[0]?.id ?? 0);
  const [authEmail, setAuthEmail] = useState("");
  const [authPassword, setAuthPassword] = useState("");
  const [currentUser, setCurrentUser] = useState<AuthUser | null>(null);
  const [lastWorkflowRun, setLastWorkflowRun] = useState<WorkflowRunResponse | null>(null);
  const [formValues, setFormValues] = useState<Record<string, string>>({});
  const [workflowSecretsText, setWorkflowSecretsText] = useState("{}");
  const [workflowVariablesText, setWorkflowVariablesText] = useState("{}");
  const [status, setStatus] = useState("Ready");

  const database = catalog.databases[0];
  const table = database.tables.find((item) => item.name === selectedTable) ?? database.tables[0];
  const activeFields = table.fields.filter((field) => !field.deleted);
  const selectedWorkflow = workflows.find((item) => item.id === selectedWorkflowID) ?? workflows[0];
  const selectedForm = forms.find((item) => item.id === selectedFormID) ?? forms[0];
  const displayedRows = useMemo(
    () => applyTableView(rows, table.views ?? [], selectedTableView),
    [rows, table.views, selectedTableView]
  );
  const selectedWorkflowRun =
    lastWorkflowRun?.run.workflow_id === selectedWorkflow?.id ? lastWorkflowRun : null;
  const renderedForm = useMemo(() => renderFormScript(selectedForm?.script ?? ""), [selectedForm?.script]);

  useEffect(() => {
    setFormValues({});
  }, [selectedForm?.id, selectedForm?.script]);

  useEffect(() => {
    setWorkflowSecretsText(stringMapToJSON(selectedWorkflow?.secrets ?? {}));
    setWorkflowVariablesText(stringMapToJSON(selectedWorkflow?.variables ?? {}));
  }, [selectedWorkflow?.id]);

  const columns = useMemo<GridColumn[]>(
    () => [
      { id: "record_id", title: "record_id", width: 96 },
      ...activeFields.map((field) => ({
        id: field.name,
        title: field.required ? `${field.name} *` : field.name,
        width: Math.max(128, field.name.length * 14)
      }))
    ],
    [activeFields]
  );

  const getCellContent = ([columnIndex, rowIndex]: Item): GridCell => {
    const column = columns[columnIndex];
    const row = displayedRows[rowIndex];
    const value = row?.[String(column.id)] ?? "";
    return {
      kind: GridCellKind.Text,
      allowOverlay: true,
      displayData: String(value),
      data: String(value)
    };
  };

  async function editCell([columnIndex, rowIndex]: Item, newValue: EditableGridCell) {
    const column = columns[columnIndex];
    const field = String(column.id);
    const row = displayedRows[rowIndex];
    if (!row || field === "record_id" || newValue.kind !== GridCellKind.Text) {
      return;
    }
    const recordID = Number(row.record_id);
    const nextValue = newValue.data;
    setRows((current) =>
      current.map((item) => (Number(item.record_id) === recordID ? { ...item, [field]: nextValue } : item))
    );
    try {
      const saved = await updateRow(
        database.name,
        table.name,
        recordID,
        { [field]: nextValue },
        currentUser ? undefined : "demo-user"
      );
      setRows((current) =>
        current.map((item) =>
          Number(item.record_id) === saved.record_id ? { record_id: saved.record_id, ...saved.values } : item
        )
      );
      setStatus(`Updated record ${saved.record_id}`);
    } catch (error) {
      setStatus(error instanceof Error ? `Local edit: ${error.message}` : "Local edit saved");
    }
  }

  async function refreshMetadata() {
    try {
      const nextCatalog = await loadMetadata();
      setCatalog(nextCatalog);
      const dbName = nextCatalog.databases[0]?.name;
      if (dbName) {
        const userID = currentUser ? undefined : "demo-user";
        const [nextWorkflows, nextForms] = await Promise.all([listWorkflows(dbName, userID), listForms(dbName, userID)]);
        setWorkflows(nextWorkflows);
        setForms(nextForms);
        setSelectedWorkflowID(nextWorkflows[0]?.id ?? 0);
        setSelectedFormID(nextForms[0]?.id ?? 0);
      }
      setStatus("Metadata and db-level resources refreshed");
    } catch (error) {
      setStatus(error instanceof Error ? error.message : "Metadata refresh failed");
    }
  }

  async function addDraftRow() {
    const values = Object.fromEntries(activeFields.map((field) => [field.name, field.name === "status" ? "Review" : ""]));
    values.name = `New record ${rows.length + 1}`;
    try {
      const saved = await createRow(database.name, table.name, values, currentUser ? undefined : "demo-user");
      setRows((current) => [...current, { record_id: saved.record_id, ...saved.values }]);
      setStatus(`Created record ${saved.record_id}`);
    } catch (error) {
      const localID = Math.max(0, ...rows.map((row) => Number(row.record_id))) + 1;
      setRows((current) => [...current, { record_id: localID, ...values }]);
      setStatus(error instanceof Error ? `Local draft: ${error.message}` : "Local draft added");
    }
  }

  async function persistWorkflow() {
    if (!selectedWorkflow) {
      return;
    }
    try {
      const saved = await saveWorkflow(database.name, selectedWorkflow, currentUser ? undefined : "demo-user");
      setWorkflows((current) => replaceResource(current, saved));
      setSelectedWorkflowID(saved.id ?? 0);
      setStatus(`Workflow saved as #${saved.id}`);
    } catch (error) {
      setStatus(error instanceof Error ? error.message : "Workflow save failed");
    }
  }

  async function executeWorkflow() {
    if (!selectedWorkflow?.id) {
      setStatus("Save workflow before running");
      return;
    }
    const sampleRow = rows[0] ?? {};
    try {
      const response = await runWorkflow(selectedWorkflow.id, {
        ...sampleRow,
        record_id: Number(sampleRow.record_id ?? 1)
      }, currentUser ? undefined : "demo-user");
      setLastWorkflowRun(response);
      if (response.run.error) {
        setStatus(`Workflow failed: ${response.run.error}`);
        return;
      }
      setStatus(`Workflow run saved: ${response.history_key}`);
    } catch (error) {
      setStatus(error instanceof Error ? error.message : "Workflow run failed");
    }
  }

  async function persistForm() {
    if (!selectedForm) {
      return;
    }
    try {
      const saved = await saveForm(database.name, selectedForm, currentUser ? undefined : "demo-user");
      setForms((current) => replaceResource(current, saved));
      setSelectedFormID(saved.id ?? 0);
      setStatus(`Form saved as #${saved.id}`);
    } catch (error) {
      setStatus(error instanceof Error ? error.message : "Form save failed");
    }
  }

  async function submitRenderedForm(event?: FormEvent<HTMLFormElement>) {
    event?.preventDefault();
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
    if (!currentUser) {
      const localID = Math.max(0, ...rows.map((row) => Number(row.record_id))) + 1;
      setRows((current) => [...current, { record_id: localID, ...values }]);
      setStatus("Local form submitted");
      return;
    }
    try {
      const saved = await createRow(database.name, table.name, values);
      setRows((current) => [...current, { record_id: saved.record_id, ...saved.values }]);
      setStatus(`Form created record ${saved.record_id}`);
    } catch (error) {
      const localID = Math.max(0, ...rows.map((row) => Number(row.record_id))) + 1;
      setRows((current) => [...current, { record_id: localID, ...values }]);
      setStatus(error instanceof Error ? `Local form: ${error.message}` : "Local form submitted");
    }
  }

  async function registerUser() {
    try {
      const user = await register(authEmail, authPassword);
      setCurrentUser(user);
      setStatus(`Signed in as ${user.email}`);
    } catch (error) {
      setStatus(error instanceof Error ? error.message : "Registration failed");
    }
  }

  async function loginUser() {
    try {
      const user = await login(authEmail, authPassword);
      setCurrentUser(user);
      setStatus(`Signed in as ${user.email}`);
    } catch (error) {
      setStatus(error instanceof Error ? error.message : "Login failed");
    }
  }

  async function logoutUser() {
    try {
      await logout();
      setCurrentUser(null);
      setStatus("Signed out");
    } catch (error) {
      setStatus(error instanceof Error ? error.message : "Logout failed");
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
      setStatus(parsed.error);
      return;
    }
    setStatus("Workflow config updated");
    setWorkflows((current) =>
      current.map((item) => (item.id === selectedWorkflow?.id ? { ...item, [kind]: parsed.value } : item))
    );
  }

  function updateSelectedFormScript(script: string) {
    setForms((current) => current.map((item) => (item.id === selectedForm?.id ? { ...item, script } : item)));
  }

  function updateFormValue(name: string, value: string) {
    setFormValues((current) => ({ ...current, [name]: value }));
  }

  return (
    <div className="app-shell">
      <aside className="sidebar">
        <div className="brand">
          <DatabaseRegular />
          <Text weight="semibold">codetable</Text>
        </div>
        <div className="auth-panel">
          <Label htmlFor="auth-email">Email</Label>
          <Input
            id="auth-email"
            type="email"
            value={authEmail}
            onChange={(_, data) => setAuthEmail(data.value)}
            disabled={currentUser !== null}
          />
          <Label htmlFor="auth-password">Password</Label>
          <Input
            id="auth-password"
            type="password"
            value={authPassword}
            onChange={(_, data) => setAuthPassword(data.value)}
            disabled={currentUser !== null}
          />
          {currentUser ? (
            <Button onClick={logoutUser}>{currentUser.email}</Button>
          ) : (
            <div className="auth-actions">
              <Button onClick={loginUser}>Login</Button>
              <Button appearance="primary" onClick={registerUser}>
                Register
              </Button>
            </div>
          )}
        </div>
        <Label htmlFor="table-select">Table</Label>
        <Select id="table-select" value={selectedTable} onChange={(_, data) => setSelectedTable(data.value)}>
          {database.tables.map((item) => (
            <option key={item.name} value={item.name}>
              {item.display_name || item.name}
            </option>
          ))}
        </Select>
        <Label htmlFor="view-select">View</Label>
        <Select id="view-select" value={selectedTableView} onChange={(_, data) => setSelectedTableView(data.value)}>
          <option value="all">All records</option>
          {(table.views ?? []).map((item) => (
            <option key={item.name} value={item.name}>
              {item.display_name || item.name}
            </option>
          ))}
        </Select>
        <div className="metadata-block">
          <Text size={200}>{database.name}</Text>
          <Text size={200}>{database.sqlite_path}</Text>
        </div>
      </aside>

      <main className="workspace">
        <header className="topbar">
          <TabList selectedValue={view} onTabSelect={(_, data) => setView(data.value as View)}>
            <Tab value="table">Table</Tab>
            <Tab value="workflow">Workflow</Tab>
            <Tab value="form">Form</Tab>
          </TabList>
          <Toolbar aria-label="Workspace actions">
            <Tooltip content="Refresh metadata" relationship="label">
              <ToolbarButton aria-label="Refresh metadata" icon={<ArrowClockwiseRegular />} onClick={refreshMetadata} />
            </Tooltip>
            <Tooltip content="Create row" relationship="label">
              <ToolbarButton aria-label="Create row" icon={<AddRegular />} onClick={addDraftRow} />
            </Tooltip>
          </Toolbar>
        </header>

        <section className="content-band">
          {view === "table" && (
            <div className="table-view">
              <div className="section-header">
                <div>
                  <Text weight="semibold">{table.display_name || table.name}</Text>
                  <Text size={200}>
                    {displayedRows.length} of {rows.length} records
                  </Text>
                </div>
                <Button icon={<AddRegular />} appearance="primary" onClick={addDraftRow}>
                  Row
                </Button>
              </div>
              <div className="grid-host">
                <DataEditor
                  getCellContent={getCellContent}
                  onCellEdited={editCell}
                  columns={columns}
                  rows={displayedRows.length}
                  rowMarkers="number"
                  smoothScrollX
                  smoothScrollY
                  width="100%"
                  height="100%"
                />
              </div>
            </div>
          )}

          {view === "workflow" && (
            <div className="split-view">
              <div className="editor-pane">
                <div className="section-header">
                  <div>
                    <Text weight="semibold">{selectedWorkflow?.name ?? "workflow"}.js</Text>
                    <Text size={200}>{database.name} workflow</Text>
                  </div>
                  <Button icon={<SaveRegular />} appearance="primary" onClick={persistWorkflow}>
                    Save
                  </Button>
                </div>
                <Textarea
                  className="code-editor"
                  value={selectedWorkflow?.script ?? ""}
                  onChange={(_, data) => updateSelectedWorkflowScript(data.value)}
                  resize="none"
                  aria-label="Workflow JavaScript"
                />
                <div className="workflow-config-grid">
                  <label className="field-stack">
                    <span>Variables JSON</span>
                    <Textarea
                      className="json-editor"
                      value={workflowVariablesText}
                      onChange={(_, data) => updateSelectedWorkflowJSON("variables", data.value)}
                      resize="none"
                      aria-label="Workflow Variables JSON"
                    />
                  </label>
                  <label className="field-stack">
                    <span>Secrets JSON</span>
                    <Textarea
                      className="json-editor"
                      value={workflowSecretsText}
                      onChange={(_, data) => updateSelectedWorkflowJSON("secrets", data.value)}
                      resize="none"
                      aria-label="Workflow Secrets JSON"
                    />
                  </label>
                </div>
              </div>
              <div className="history-pane">
                <Text weight="semibold">Workflows</Text>
                <div className="resource-list">
                  {workflows.map((item) => (
                    <button
                      key={item.id ?? item.name}
                      className={item.id === selectedWorkflow?.id ? "resource-item selected" : "resource-item"}
                      type="button"
                      onClick={() => setSelectedWorkflowID(item.id ?? 0)}
                    >
                      {item.name}
                    </button>
                  ))}
                </div>
                <Text weight="semibold">Run flow</Text>
                <div className="flow-line" aria-label="Workflow run flow">
                  {selectedWorkflowRun && selectedWorkflowRun.run.steps.length > 0 ? (
                    selectedWorkflowRun.run.steps.map((step, index) => (
                      <span key={`${step.node_id}-${index}`} className={step.error ? "flow-step error" : "flow-step"}>
                        {step.error ? `${step.node_id}: ${step.error}` : step.node_id}
                      </span>
                    ))
                  ) : (
                    <span className="flow-empty">No runs yet</span>
                  )}
                </div>
                <Button icon={<PlayRegular />} onClick={executeWorkflow} disabled={!selectedWorkflow?.id}>
                  Run
                </Button>
              </div>
            </div>
          )}

          {view === "form" && (
            <div className="split-view">
              <div className="editor-pane">
                <div className="section-header">
                  <div>
                    <Text weight="semibold">{selectedForm?.name ?? "form"}.js</Text>
                    <Text size={200}>{database.name} form</Text>
                  </div>
                  <Button icon={<SaveRegular />} appearance="primary" onClick={persistForm}>
                    Save
                  </Button>
                </div>
                <Textarea
                  className="code-editor"
                  value={selectedForm?.script ?? ""}
                  onChange={(_, data) => updateSelectedFormScript(data.value)}
                  resize="none"
                  aria-label="Form JavaScript"
                />
              </div>
              <form className="form-preview" onSubmit={submitRenderedForm}>
                <Text weight="semibold">Forms</Text>
                <div className="resource-list">
                  {forms.map((item) => (
                    <button
                      key={item.id ?? item.name}
                      className={item.id === selectedForm?.id ? "resource-item selected" : "resource-item"}
                      type="button"
                      onClick={() => setSelectedFormID(item.id ?? 0)}
                    >
                      {item.name}
                    </button>
                  ))}
                </div>
                {renderedForm.error && <Text className="form-error">{renderedForm.error}</Text>}
                {renderedForm.elements.map((element) => {
                  if (element.kind === "input") {
                    return (
                      <label key={element.name} className="field-stack">
                        <span>{element.label}</span>
                        <Input
                          type={element.inputType}
                          required={element.required}
                          value={formValues[element.name] ?? ""}
                          onChange={(_, data) => updateFormValue(element.name, data.value)}
                        />
                      </label>
                    );
                  }
                  if (element.kind === "select") {
                    return (
                      <label key={element.name} className="field-stack">
                        <span>{element.label}</span>
                        <Select
                          value={formValues[element.name] ?? element.options[0] ?? ""}
                          onChange={(_, data) => updateFormValue(element.name, data.value)}
                        >
                          {element.options.map((option) => (
                            <option key={option}>{option}</option>
                          ))}
                        </Select>
                      </label>
                    );
                  }
                  if (element.kind === "html") {
                    return <div key={element.html} className="form-html" dangerouslySetInnerHTML={{ __html: element.html }} />;
                  }
                  return (
                    <Button key={element.label} type="button" appearance="primary" onClick={() => void submitRenderedForm()}>
                      {element.label}
                    </Button>
                  );
                })}
              </form>
            </div>
          )}
        </section>

        <footer className="statusbar">{status}</footer>
      </main>
    </div>
  );
}

function replaceResource<T extends { id?: number }>(items: T[], saved: T): T[] {
  if (!saved.id) {
    return items;
  }
  if (!items.some((item) => item.id === saved.id)) {
    return [...items, saved];
  }
  return items.map((item) => (item.id === saved.id ? saved : item));
}

function stringMapToJSON(values: Record<string, string>): string {
  const sorted = Object.fromEntries(Object.entries(values).sort(([left], [right]) => left.localeCompare(right)));
  return JSON.stringify(sorted, null, 2);
}

function parseStringMap(text: string): { ok: true; value: Record<string, string> } | { ok: false; error: string } {
  let parsed: unknown;
  try {
    parsed = JSON.parse(text);
  } catch (error) {
    return { ok: false, error: error instanceof Error ? error.message : "Invalid JSON" };
  }
  if (!parsed || Array.isArray(parsed) || typeof parsed !== "object") {
    return { ok: false, error: "Workflow config must be a JSON object" };
  }
  const values: Record<string, string> = {};
  for (const [key, value] of Object.entries(parsed)) {
    if (typeof value !== "string") {
      return { ok: false, error: `Workflow config value for ${key} must be a string` };
    }
    values[key] = value;
  }
  return { ok: true, value: values };
}

function applyTableView(rows: Array<Record<string, unknown>>, views: TableView[], selectedView: string) {
  if (selectedView === "all") {
    return rows;
  }
  const resolved = resolveTableView(views, selectedView, new Set());
  if (!resolved) {
    return rows;
  }
  const filtered = rows.filter((row) =>
    resolved.filters.every((filter) => {
      const value = rowValue(row, filter.field);
      if (filter.op === "eq") {
        return String(value) === String(filter.value);
      }
      if (filter.op === "contains") {
        return String(value).toLowerCase().includes(String(filter.value ?? "").toLowerCase());
      }
      if (filter.op === "not_empty") {
        return value !== undefined && value !== null && String(value).trim() !== "";
      }
      return false;
    })
  );
  return [...filtered].sort((left, right) => {
    for (const sortDef of resolved.sorts) {
      const leftValue = String(rowValue(left, sortDef.field));
      const rightValue = String(rowValue(right, sortDef.field));
      if (leftValue === rightValue) {
        continue;
      }
      return sortDef.direction === "desc" ? rightValue.localeCompare(leftValue) : leftValue.localeCompare(rightValue);
    }
    return Number(left.record_id ?? 0) - Number(right.record_id ?? 0);
  });
}

function resolveTableView(views: TableView[], name: string, visiting: Set<string>): TableView | undefined {
  const view = views.find((item) => item.name === name);
  if (!view || visiting.has(name)) {
    return undefined;
  }
  visiting.add(name);
  if (!view.base_view) {
    visiting.delete(name);
    return view;
  }
  const base = resolveTableView(views, view.base_view, visiting);
  visiting.delete(name);
  if (!base) {
    return view;
  }
  return {
    ...view,
    filters: [...base.filters, ...view.filters],
    sorts: [...base.sorts, ...view.sorts]
  };
}

function rowValue(row: Record<string, unknown>, field: string) {
  return row[field];
}
