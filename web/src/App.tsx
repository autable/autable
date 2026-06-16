import { type FormEvent, useEffect, useMemo, useState } from "react";
import { Text, Toolbar, ToolbarButton, Tooltip } from "@fluentui/react-components";
import { AddRegular, ArrowClockwiseRegular } from "@fluentui/react-icons";
import {
  type EditableGridCell,
  type GridCell,
  GridCellKind,
  type GridColumn,
  type Item
} from "@glideapps/glide-data-grid";
import { demoCatalog, initialForms, initialRows, initialWorkflowNodes, initialWorkflows } from "./demoData";
import { AuthDialog } from "./components/AuthDialog";
import { FormWorkspace } from "./components/FormWorkspace";
import { compactRoleGrants, PermissionPanel } from "./components/PermissionPanel";
import { TableWorkspace } from "./components/TableWorkspace";
import { WorkflowWorkspace } from "./components/WorkflowWorkspace";
import { WorkspaceNavigation, type WorkspaceView } from "./components/WorkspaceNavigation";
import { renderFormScript } from "./formRuntime";
import {
  createDatabase,
  createRole,
  createRow,
  createTable,
  listOIDCProviders,
  listForms,
  listRowHistory,
  listRoles,
  listRows,
  listWorkflowRuns,
  listWorkflows,
  loadCurrentUser,
  loadMetadata,
  loadWorkflowNodes,
  login,
  logout,
  oidcStartURL,
  register,
  runWorkflow,
  saveForm,
  saveRoleGrants,
  saveWorkflow,
  updateRow,
  type AuthUser,
  type Catalog,
  type DatabaseMetadata,
  type FormDefinition,
  type OIDCProvider,
  type PermissionGrant,
  type RowChange,
  type RowRecord,
  type RoleDefinition,
  type TableMetadata,
  type TableView,
  type WorkflowDefinition,
  type WorkflowNodeInfo,
  type WorkflowRunResponse
} from "./api";

type View = WorkspaceView;

const emptyDatabase: DatabaseMetadata = { name: "", sqlite_path: "", tables: [] };
const emptyTable: TableMetadata = { name: "", display_name: "", fields: [], views: [] };

export function App() {
  const [catalog, setCatalog] = useState<Catalog>(demoCatalog);
  const [rows, setRows] = useState(initialRows);
  const [rowsViewName, setRowsViewName] = useState("all");
  const [view, setView] = useState<View>("table");
  const [selectedDatabaseName, setSelectedDatabaseName] = useState(demoCatalog.databases[0]?.name ?? "");
  const [selectedTable, setSelectedTable] = useState("contacts");
  const [selectedTableView, setSelectedTableView] = useState("all");
  const [workflows, setWorkflows] = useState<WorkflowDefinition[]>(initialWorkflows);
  const [workflowNodes, setWorkflowNodes] = useState<WorkflowNodeInfo[]>(initialWorkflowNodes);
  const [forms, setForms] = useState<FormDefinition[]>(initialForms);
  const [roles, setRoles] = useState<RoleDefinition[]>([]);
  const [selectedWorkflowID, setSelectedWorkflowID] = useState(initialWorkflows[0]?.id ?? 0);
  const [selectedFormID, setSelectedFormID] = useState(initialForms[0]?.id ?? 0);
  const [selectedRoleName, setSelectedRoleName] = useState("");
  const [authEmail, setAuthEmail] = useState("");
  const [authPassword, setAuthPassword] = useState("");
  const [currentUser, setCurrentUser] = useState<AuthUser | null>(null);
  const [oidcProviders, setOIDCProviders] = useState<OIDCProvider[]>([]);
  const [selectedRecordID, setSelectedRecordID] = useState(0);
  const [rowHistory, setRowHistory] = useState<RowChange[]>([]);
  const [workflowRuns, setWorkflowRuns] = useState<WorkflowRunResponse[]>([]);
  const [selectedWorkflowRunKey, setSelectedWorkflowRunKey] = useState("");
  const [formValues, setFormValues] = useState<Record<string, string>>({});
  const [workflowSecretsText, setWorkflowSecretsText] = useState("{}");
  const [workflowVariablesText, setWorkflowVariablesText] = useState("{}");
  const [authDialogOpen, setAuthDialogOpen] = useState(false);
  const [newDatabaseName, setNewDatabaseName] = useState("");
  const [newTableName, setNewTableName] = useState("");
  const [newRoleName, setNewRoleName] = useState("");
  const [roleDraftGrants, setRoleDraftGrants] = useState<PermissionGrant[]>([]);
  const [status, setStatus] = useState("Ready");

  const database =
    catalog.databases.find((item) => item.name === selectedDatabaseName) ?? catalog.databases[0] ?? emptyDatabase;
  const table = database.tables.find((item) => item.name === selectedTable) ?? database.tables[0] ?? emptyTable;
  const activeFields = table.fields.filter((field) => !field.deleted);
  const selectedWorkflow = workflows.find((item) => item.id === selectedWorkflowID) ?? workflows[0];
  const selectedForm = forms.find((item) => item.id === selectedFormID) ?? forms[0];
  const selectedRole = roles.find((item) => item.name === selectedRoleName) ?? roles[0];
  const displayedRows = useMemo(
    () => (rowsViewName === selectedTableView ? rows : applyTableView(rows, table.views ?? [], selectedTableView)),
    [rows, rowsViewName, table.views, selectedTableView]
  );
  const displayedRecordIDs = useMemo(
    () => displayedRows.map((row) => Number(row.record_id)).filter((recordID) => Number.isFinite(recordID)),
    [displayedRows]
  );
  const selectedWorkflowRun =
    workflowRuns.find((run) => run.history_key === selectedWorkflowRunKey) ?? workflowRuns[0] ?? null;
  const renderedForm = useMemo(() => renderFormScript(selectedForm?.script ?? ""), [selectedForm?.script]);

  useEffect(() => {
    setFormValues({});
  }, [selectedForm?.id, selectedForm?.script]);

  useEffect(() => {
    setRoleDraftGrants(selectedRole?.grants ?? []);
  }, [selectedRole?.subject_id]);

  useEffect(() => {
    if (!catalog.databases.some((item) => item.name === selectedDatabaseName)) {
      setSelectedDatabaseName(catalog.databases[0]?.name ?? "");
      return;
    }
    if (!database.tables.some((item) => item.name === selectedTable)) {
      setSelectedTable(database.tables[0]?.name ?? "");
      setSelectedTableView("all");
    }
  }, [catalog.databases, database.tables, selectedDatabaseName, selectedTable]);

  useEffect(() => {
    let cancelled = false;
    if (!database.name) {
      setWorkflows([]);
      setForms([]);
      setRoles([]);
      return () => {
        cancelled = true;
      };
    }
    const userID = currentUser ? undefined : "demo-user";
    void Promise.all([
      listWorkflows(database.name, userID),
      listForms(database.name, userID),
      listRoles(database.name, userID).catch(() => []),
      loadWorkflowNodes()
    ])
      .then(([nextWorkflows, nextForms, nextRoles, nextWorkflowNodes]) => {
        if (cancelled) {
          return;
        }
        setWorkflows(nextWorkflows);
        setForms(nextForms);
        setRoles(nextRoles);
        setWorkflowNodes(nextWorkflowNodes);
        setSelectedWorkflowID(nextWorkflows[0]?.id ?? 0);
        setSelectedFormID(nextForms[0]?.id ?? 0);
        setSelectedRoleName(nextRoles[0]?.name ?? "");
      })
      .catch(() => undefined);
    return () => {
      cancelled = true;
    };
  }, [currentUser?.id, database.name]);

  useEffect(() => {
    if (displayedRecordIDs.length === 0) {
      setSelectedRecordID(0);
      setRowHistory([]);
      return;
    }
    if (!displayedRecordIDs.includes(selectedRecordID)) {
      setSelectedRecordID(displayedRecordIDs[0]);
      setRowHistory([]);
    }
  }, [displayedRecordIDs, selectedRecordID]);

  useEffect(() => {
    let cancelled = false;
    void loadCurrentUser()
      .then((user) => {
        if (cancelled || !user) {
          return;
        }
        setCurrentUser(user);
        setStatus(`Signed in as ${user.email}`);
      })
      .catch(() => undefined);
    void listOIDCProviders()
      .then((providers) => {
        if (!cancelled) {
          setOIDCProviders(providers);
        }
      })
      .catch(() => undefined);
    return () => {
      cancelled = true;
    };
  }, []);

  useEffect(() => {
    let cancelled = false;
    if (!database.name || !table.name) {
      setRows([]);
      setRowsViewName(selectedTableView);
      return () => {
        cancelled = true;
      };
    }
    const userID = currentUser ? undefined : "demo-user";
    void listRows(database.name, table.name, selectedTableView, userID)
      .then((nextRows) => {
        if (cancelled) {
          return;
        }
        setRows(nextRows.map(rowRecordToValues));
        setRowsViewName(selectedTableView);
      })
      .catch(() => undefined);
    return () => {
      cancelled = true;
    };
  }, [currentUser?.id, database.name, table.name, selectedTableView]);

  useEffect(() => {
    setWorkflowSecretsText(stringMapToJSON(selectedWorkflow?.secrets ?? {}));
    setWorkflowVariablesText(stringMapToJSON(selectedWorkflow?.variables ?? {}));
  }, [selectedWorkflow?.id]);

  useEffect(() => {
    let cancelled = false;
    if (!selectedWorkflow?.id) {
      setWorkflowRuns([]);
      setSelectedWorkflowRunKey("");
      return () => {
        cancelled = true;
      };
    }
    const userID = currentUser ? undefined : "demo-user";
    void listWorkflowRuns(selectedWorkflow.id, userID)
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
  }, [currentUser?.id, selectedWorkflow?.id]);

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
          Number(item.record_id) === saved.record_id ? rowRecordToValues(saved) : item
        )
      );
      setRowsViewName("local");
      setSelectedRecordID(saved.record_id);
      setRowHistory([]);
      setStatus(`Updated record ${saved.record_id}`);
    } catch (error) {
      setStatus(error instanceof Error ? `Local edit: ${error.message}` : "Local edit saved");
    }
  }

  async function refreshMetadata() {
    try {
      const nextCatalog = await loadMetadata();
      setCatalog(nextCatalog);
      const dbName = nextCatalog.databases.some((item) => item.name === selectedDatabaseName)
        ? selectedDatabaseName
        : nextCatalog.databases[0]?.name;
      if (dbName) {
        setSelectedDatabaseName(dbName);
        const userID = currentUser ? undefined : "demo-user";
        const [nextWorkflows, nextForms, nextRoles, nextWorkflowNodes] = await Promise.all([
          listWorkflows(dbName, userID),
          listForms(dbName, userID),
          listRoles(dbName, userID).catch(() => []),
          loadWorkflowNodes()
        ]);
        setWorkflows(nextWorkflows);
        setForms(nextForms);
        setRoles(nextRoles);
        setWorkflowNodes(nextWorkflowNodes);
        setSelectedWorkflowID(nextWorkflows[0]?.id ?? 0);
        setSelectedFormID(nextForms[0]?.id ?? 0);
        setSelectedRoleName(nextRoles[0]?.name ?? "");
      }
      setStatus("Metadata and db-level resources refreshed");
    } catch (error) {
      setStatus(error instanceof Error ? error.message : "Metadata refresh failed");
    }
  }

  async function createDatabaseFromSidebar() {
    const name = newDatabaseName.trim();
    if (!name) {
      setStatus("Database name is required");
      return;
    }
    try {
      const userID = currentUser ? undefined : "demo-user";
      const saved = await createDatabase({ name, sqlite_path: `./data/${name}.sqlite` }, userID);
      const nextCatalog = await loadMetadata();
      setCatalog(nextCatalog);
      setSelectedDatabaseName(saved.name);
      setSelectedTable(saved.tables[0]?.name ?? "");
      setSelectedTableView("all");
      setRows([]);
      setRowsViewName("all");
      setNewDatabaseName("");
      setStatus(`Created database ${saved.name}`);
    } catch (error) {
      setStatus(error instanceof Error ? error.message : "Database creation failed");
    }
  }

  async function createTableFromSidebar() {
    if (!database.name) {
      setStatus("Select a database before creating a table");
      return;
    }
    const name = newTableName.trim();
    if (!name) {
      setStatus("Table name is required");
      return;
    }
    try {
      const userID = currentUser ? undefined : "demo-user";
      const saved = await createTable(
        database.name,
        {
          name,
          display_name: name,
          fields: [{ name: "name", type: "text", required: true, deleted: false }],
          views: []
        },
        userID
      );
      const nextCatalog = await loadMetadata();
      setCatalog(nextCatalog);
      setSelectedTable(saved.name);
      setSelectedTableView("all");
      setRows([]);
      setRowsViewName("all");
      setNewTableName("");
      setStatus(`Created table ${database.name}.${saved.name}`);
    } catch (error) {
      setStatus(error instanceof Error ? error.message : "Table creation failed");
    }
  }

  async function createRoleFromSidebar() {
    if (!database.name) {
      setStatus("Select a database before creating a role");
      return;
    }
    const name = newRoleName.trim();
    if (!name) {
      setStatus("Role name is required");
      return;
    }
    try {
      const userID = currentUser ? undefined : "demo-user";
      const saved = await createRole(database.name, name, userID);
      setRoles((current) => replaceRole(current, saved));
      setSelectedRoleName(saved.name);
      setNewRoleName("");
      setStatus(`Created role ${saved.name}`);
    } catch (error) {
      setStatus(error instanceof Error ? error.message : "Role creation failed");
    }
  }

  async function persistRoleGrants() {
    if (!database.name || !selectedRole) {
      setStatus("Select a role before saving permissions");
      return;
    }
    try {
      const userID = currentUser ? undefined : "demo-user";
      const saved = await saveRoleGrants(database.name, selectedRole.name, compactRoleGrants(roleDraftGrants), userID);
      setRoles((current) => replaceRole(current, saved));
      setSelectedRoleName(saved.name);
      setStatus(`Saved permissions for ${saved.name}`);
    } catch (error) {
      setStatus(error instanceof Error ? error.message : "Permission save failed");
    }
  }

  async function addDraftRow() {
    if (!database.name || !table.name) {
      setStatus("Create a table before adding rows");
      return;
    }
    const values = Object.fromEntries(activeFields.map((field) => [field.name, field.name === "status" ? "Review" : ""]));
    values.name = `New record ${rows.length + 1}`;
    try {
      const saved = await createRow(database.name, table.name, values, currentUser ? undefined : "demo-user");
      setRows((current) => [...current, rowRecordToValues(saved)]);
      setRowsViewName("local");
      setSelectedRecordID(saved.record_id);
      setRowHistory([]);
      setStatus(`Created record ${saved.record_id}`);
    } catch (error) {
      const localID = Math.max(0, ...rows.map((row) => Number(row.record_id))) + 1;
      setRows((current) => [...current, { record_id: localID, ...values }]);
      setRowsViewName("local");
      setSelectedRecordID(localID);
      setRowHistory([]);
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
      setWorkflowRuns((current) => [response, ...current.filter((run) => run.history_key !== response.history_key)]);
      setSelectedWorkflowRunKey(response.history_key);
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
      setRowsViewName("local");
      setSelectedRecordID(localID);
      setRowHistory([]);
      setStatus("Local form submitted");
      return;
    }
    try {
      const saved = await createRow(database.name, table.name, values);
      setRows((current) => [...current, rowRecordToValues(saved)]);
      setRowsViewName("local");
      setSelectedRecordID(saved.record_id);
      setRowHistory([]);
      setStatus(`Form created record ${saved.record_id}`);
    } catch (error) {
      const localID = Math.max(0, ...rows.map((row) => Number(row.record_id))) + 1;
      setRows((current) => [...current, { record_id: localID, ...values }]);
      setRowsViewName("local");
      setSelectedRecordID(localID);
      setRowHistory([]);
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

  function loginWithOIDC(providerName: string) {
    window.location.assign(oidcStartURL(providerName));
  }

  async function loadSelectedRowHistory() {
    if (!selectedRecordID) {
      setStatus("Select a row before loading history");
      return;
    }
    try {
      const userID = currentUser ? undefined : "demo-user";
      const changes = await listRowHistory(database.name, table.name, selectedRecordID, userID);
      setRowHistory(changes);
      setStatus(`Loaded ${changes.length} history entries for record ${selectedRecordID}`);
    } catch (error) {
      setRowHistory([]);
      setStatus(error instanceof Error ? error.message : "Row history failed");
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

  function updateRoleGrant(scope: PermissionGrant["scope"], resource: string, field: string, level: PermissionGrant["level"]) {
    if (!selectedRole) {
      return;
    }
    setRoleDraftGrants((current) => {
      const next = current.filter((grant) => grant.scope !== scope || grant.resource !== resource || grant.field !== field);
      if (level === 0) {
        return next;
      }
      return [
        ...next,
        {
          subject_id: selectedRole.subject_id,
          scope,
          resource,
          field,
          level
        }
      ];
    });
  }

  function selectDatabaseSection(databaseName: string, nextView: View) {
    const nextDatabase = catalog.databases.find((item) => item.name === databaseName);
    if (!nextDatabase) {
      return;
    }
    setSelectedDatabaseName(databaseName);
    setView(nextView);
    if (nextView === "table") {
      setSelectedTable(nextDatabase.tables[0]?.name ?? "");
      setSelectedTableView("all");
    }
  }

  return (
    <div className="app-shell">
      <WorkspaceNavigation
        catalog={catalog}
        currentUser={currentUser}
        database={database}
        forms={forms}
        newDatabaseName={newDatabaseName}
        newRoleName={newRoleName}
        newTableName={newTableName}
        onCreateDatabase={createDatabaseFromSidebar}
        onCreateRole={createRoleFromSidebar}
        onCreateTable={createTableFromSidebar}
        onLogout={logoutUser}
        onNewDatabaseNameChange={setNewDatabaseName}
        onNewRoleNameChange={setNewRoleName}
        onNewTableNameChange={setNewTableName}
        onOpenLogin={() => setAuthDialogOpen(true)}
        onSelectDatabaseSection={selectDatabaseSection}
        onSelectFormID={setSelectedFormID}
        onSelectRoleName={setSelectedRoleName}
        onSelectTable={setSelectedTable}
        onSelectTableView={setSelectedTableView}
        onSelectWorkflowID={setSelectedWorkflowID}
        roles={roles}
        selectedForm={selectedForm}
        selectedRole={selectedRole}
        selectedTableView={selectedTableView}
        selectedWorkflow={selectedWorkflow}
        table={table}
        view={view}
        workflows={workflows}
      />

      <main className="workspace">
        <header className="topbar">
          <div className="workspace-title">
            <Text weight="semibold">
              {database.name || "No database"}
              {view === "table" && table.name ? ` / ${table.display_name || table.name}` : ""}
              {view === "workflow" && selectedWorkflow ? ` / ${selectedWorkflow.name}` : ""}
              {view === "form" && selectedForm ? ` / ${selectedForm.name}` : ""}
              {view === "permission" ? " / permissions" : ""}
            </Text>
            <Text size={200}>
              {view === "table" && `${displayedRows.length} of ${rows.length} records`}
              {view === "workflow" && `${workflows.length} workflows`}
              {view === "form" && `${forms.length} forms`}
              {view === "permission" && `${roles.length} roles`}
            </Text>
          </div>
          <Toolbar aria-label="Workspace actions">
            <Tooltip content="Refresh metadata" relationship="label">
              <ToolbarButton aria-label="Refresh metadata" icon={<ArrowClockwiseRegular />} onClick={refreshMetadata} />
            </Tooltip>
            <Tooltip content="Create row" relationship="label">
              <ToolbarButton aria-label="Create row" icon={<AddRegular />} onClick={addDraftRow} disabled={view !== "table"} />
            </Tooltip>
          </Toolbar>
        </header>

        <section className="content-band">
          {view === "table" && (
            <TableWorkspace
              columns={columns}
              displayedRecordIDs={displayedRecordIDs}
              displayedRows={displayedRows}
              getCellContent={getCellContent}
              onAddRow={addDraftRow}
              onCellEdited={editCell}
              onLoadHistory={loadSelectedRowHistory}
              onSelectRecordID={setSelectedRecordID}
              rowHistory={rowHistory}
              rows={rows}
              selectedRecordID={selectedRecordID}
              table={table}
            />
          )}

          {view === "workflow" && (
            <WorkflowWorkspace
              databaseName={database.name}
              onExecute={executeWorkflow}
              onSave={persistWorkflow}
              onSelectRunKey={setSelectedWorkflowRunKey}
              onUpdateConfigJSON={updateSelectedWorkflowJSON}
              onUpdateScript={updateSelectedWorkflowScript}
              selectedRun={selectedWorkflowRun}
              secretsText={workflowSecretsText}
              variablesText={workflowVariablesText}
              workflow={selectedWorkflow}
              workflowNodes={workflowNodes}
              workflowRuns={workflowRuns}
            />
          )}

          {view === "form" && (
            <FormWorkspace
              databaseName={database.name}
              form={selectedForm}
              formValues={formValues}
              onFormValueChange={updateFormValue}
              onSave={persistForm}
              onSubmit={submitRenderedForm}
              onUpdateScript={updateSelectedFormScript}
              renderedForm={renderedForm}
            />
          )}

          {view === "permission" && (
            <PermissionPanel
              database={database}
              forms={forms}
              grants={roleDraftGrants}
              onGrantChange={updateRoleGrant}
              onSave={persistRoleGrants}
              role={selectedRole}
              workflows={workflows}
            />
          )}
        </section>

        <footer className="statusbar">{status}</footer>
      </main>

      <AuthDialog
        email={authEmail}
        onEmailChange={setAuthEmail}
        onLogin={loginUser}
        onOIDCLogin={loginWithOIDC}
        onOpenChange={setAuthDialogOpen}
        onPasswordChange={setAuthPassword}
        onRegister={registerUser}
        open={authDialogOpen}
        password={authPassword}
        providers={oidcProviders}
      />
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

function replaceRole(items: RoleDefinition[], saved: RoleDefinition): RoleDefinition[] {
  if (!items.some((item) => item.name === saved.name)) {
    return [...items, saved];
  }
  return items.map((item) => (item.name === saved.name ? saved : item));
}

function rowRecordToValues(row: RowRecord): Record<string, unknown> {
  return { record_id: row.record_id, ...row.values };
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
