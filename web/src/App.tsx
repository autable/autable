import { useEffect, useMemo, useState } from "react";
import { Text, Toolbar, ToolbarButton, Tooltip } from "@fluentui/react-components";
import { ArrowClockwiseRegular } from "@fluentui/react-icons";
import {
  type EditableGridCell,
  type GridCell,
  GridCellKind,
  type Item
} from "@glideapps/glide-data-grid";
import { AuthDialog } from "./components/AuthDialog";
import { compactMembers, replaceRole, rowDraftFromRecord } from "./appState";
import { FormWorkspace } from "./components/FormWorkspace";
import { compactRoleGrants, PermissionPanel } from "./components/PermissionPanel";
import { TableWorkspace } from "./components/TableWorkspace";
import { WorkflowWorkspace } from "./components/WorkflowWorkspace";
import { WorkspaceNavigation, type WorkspaceView } from "./components/WorkspaceNavigation";
import { useWorkflowFormWorkspace } from "./hooks/useWorkflowFormWorkspace";
import { buildTableColumns, rowRecordToValues } from "./tableGrid";
import { applyTableView } from "./tableViews";
import {
  createDatabase,
  createRole,
  createRow,
  createTable,
  deleteRow,
  listOIDCProviders,
  listRowHistory,
  listRoles,
  listRows,
  loadCurrentUser,
  loadMetadata,
  login,
  logout,
  oidcStartURL,
  register,
  saveRoleGrants,
  saveRoleMembers,
  updateTableMetadata,
  updateRow,
  type AuthUser,
  type Catalog,
  type DatabaseMetadata,
  type OIDCProvider,
  type PermissionGrant,
  type RowChange,
  type RoleDefinition,
  type TableMetadata,
  type TableViewFilter,
  type TableView,
  type TableViewSort
} from "./api";

type View = WorkspaceView;

const emptyDatabase: DatabaseMetadata = { name: "", sqlite_path: "", tables: [] };
const emptyTable: TableMetadata = { name: "", display_name: "", fields: [], views: [] };
const emptyCatalog: Catalog = { databases: [] };

export function App() {
  const [catalog, setCatalog] = useState<Catalog>(emptyCatalog);
  const [rows, setRows] = useState<Array<Record<string, unknown>>>([]);
  const [rowsViewName, setRowsViewName] = useState("all");
  const [view, setView] = useState<View>("table");
  const [selectedDatabaseName, setSelectedDatabaseName] = useState("");
  const [selectedTable, setSelectedTable] = useState("");
  const [selectedTableView, setSelectedTableView] = useState("all");
  const [roles, setRoles] = useState<RoleDefinition[]>([]);
  const [selectedRoleName, setSelectedRoleName] = useState("");
  const [authEmail, setAuthEmail] = useState("");
  const [authPassword, setAuthPassword] = useState("");
  const [currentUser, setCurrentUser] = useState<AuthUser | null>(null);
  const [authReady, setAuthReady] = useState(false);
  const [oidcProviders, setOIDCProviders] = useState<OIDCProvider[]>([]);
  const [selectedRecordID, setSelectedRecordID] = useState(0);
  const [rowHistory, setRowHistory] = useState<RowChange[]>([]);
  const [authDialogOpen, setAuthDialogOpen] = useState(false);
  const [newDatabaseName, setNewDatabaseName] = useState("");
  const [newTableName, setNewTableName] = useState("");
  const [newFieldName, setNewFieldName] = useState("");
  const [newFieldType, setNewFieldType] = useState("text");
  const [newFieldRequired, setNewFieldRequired] = useState(false);
  const [newViewName, setNewViewName] = useState("");
  const [newViewBase, setNewViewBase] = useState("all");
  const [newViewFilterField, setNewViewFilterField] = useState("");
  const [newViewFilterOp, setNewViewFilterOp] = useState<TableViewFilter["op"]>("eq");
  const [newViewFilterValue, setNewViewFilterValue] = useState("");
  const [newViewSortField, setNewViewSortField] = useState("");
  const [newViewSortDirection, setNewViewSortDirection] = useState<TableViewSort["direction"]>("asc");
  const [newRoleName, setNewRoleName] = useState("");
  const [roleDraftGrants, setRoleDraftGrants] = useState<PermissionGrant[]>([]);
  const [roleDraftMembers, setRoleDraftMembers] = useState<string[]>([]);
  const [newRoleMemberID, setNewRoleMemberID] = useState("");
  const [status, setStatus] = useState("Ready");

  const database =
    catalog.databases.find((item) => item.name === selectedDatabaseName) ?? catalog.databases[0] ?? emptyDatabase;
  const table = database.tables.find((item) => item.name === selectedTable) ?? database.tables[0] ?? emptyTable;
  const activeFields = table.fields.filter((field) => !field.deleted);
  const activeFieldNames = useMemo(() => activeFields.map((field) => field.name), [table.fields]);
  const workflowFormWorkspace = useWorkflowFormWorkspace({
    currentUserID: currentUser?.id,
    databaseName: database.name,
    tableName: table.name,
    onStatus: setStatus,
    onSubmittedRow: (targetTableName, row) => {
      if (targetTableName === table.name) {
        setRows((current) => [...current, row]);
        setRowsViewName("local");
        setSelectedRecordID(Number(row.record_id));
        setRowHistory([]);
      }
    }
  });
  const {
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
    workflowVariablesText
  } = workflowFormWorkspace;
  const selectedRole = roles.find((item) => item.name === selectedRoleName) ?? roles[0];
  const displayedRows = useMemo(
    () => (rowsViewName === selectedTableView ? rows : applyTableView(rows, table.views ?? [], selectedTableView)),
    [rows, rowsViewName, table.views, selectedTableView]
  );
  const displayedRecordIDs = useMemo(
    () => displayedRows.map((row) => Number(row.record_id)).filter((recordID) => Number.isFinite(recordID)),
    [displayedRows]
  );
  const selectedRow = useMemo(
    () => displayedRows.find((row) => Number(row.record_id) === selectedRecordID) ?? null,
    [displayedRows, selectedRecordID]
  );
  const [selectedRowDraft, setSelectedRowDraft] = useState<Record<string, string>>({});

  useEffect(() => {
    setSelectedRowDraft(rowDraftFromRecord(selectedRow, activeFieldNames));
  }, [activeFieldNames, selectedRow]);

  useEffect(() => {
    setRoleDraftGrants(selectedRole?.grants ?? []);
    setRoleDraftMembers(selectedRole?.members ?? []);
    setNewRoleMemberID("");
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
    if (!database.name || !currentUser) {
      setRoles([]);
      setSelectedRoleName("");
      return () => {
        cancelled = true;
      };
    }
    void listRoles(database.name)
      .then((nextRoles) => {
        if (cancelled) {
          return;
        }
        setRoles(nextRoles);
        setSelectedRoleName(nextRoles[0]?.name ?? "");
      })
      .catch(() => {
        if (!cancelled) {
          setRoles([]);
          setSelectedRoleName("");
        }
      });
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
        if (cancelled) {
          return;
        }
        setCurrentUser(user);
        if (user) {
          setStatus(`Signed in as ${user.email}`);
        }
      })
      .catch((error) => {
        if (!cancelled) {
          setStatus(error instanceof Error ? error.message : "Current user load failed");
        }
      })
      .finally(() => {
        if (!cancelled) {
          setAuthReady(true);
        }
      });
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
    if (!authReady) {
      return () => {
        cancelled = true;
      };
    }
    if (!currentUser) {
      applyCatalogSelection(emptyCatalog, "");
      setRoles([]);
      workflowFormWorkspace.clearResources();
      return () => {
        cancelled = true;
      };
    }
    void loadMetadata()
      .then((nextCatalog) => {
        if (!cancelled) {
          applyCatalogSelection(nextCatalog);
        }
      })
      .catch((error) => {
        if (!cancelled) {
          setStatus(error instanceof Error ? error.message : "Metadata load failed");
        }
      });
    return () => {
      cancelled = true;
    };
  }, [authReady, currentUser?.id]);

  useEffect(() => {
    let cancelled = false;
    if (!currentUser || !database.name || !table.name) {
      setRows([]);
      setRowsViewName(selectedTableView);
      return () => {
        cancelled = true;
      };
    }
    void listRows(database.name, table.name, selectedTableView)
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

  const columns = useMemo(
    () => buildTableColumns(activeFields),
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
      const saved = await updateRow(database.name, table.name, recordID, { [field]: nextValue });
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
      setStatus(error instanceof Error ? error.message : "Row update failed");
    }
  }

  async function refreshMetadata() {
    if (!currentUser) {
      setStatus("Login before refreshing workspace metadata");
      return;
    }
    try {
      const nextCatalog = await loadMetadata();
      const dbName = applyCatalogSelection(nextCatalog, selectedDatabaseName);
      if (dbName) {
        const [nextRoles] = await Promise.all([
          listRoles(dbName).catch(() => []),
          workflowFormWorkspace.refreshResources(dbName)
        ]);
        setRoles(nextRoles);
        setSelectedRoleName(nextRoles[0]?.name ?? "");
      }
      setStatus("Metadata and db-level resources refreshed");
    } catch (error) {
      setStatus(error instanceof Error ? error.message : "Metadata refresh failed");
    }
  }

  function applyCatalogSelection(nextCatalog: Catalog, preferredDatabaseName = selectedDatabaseName) {
    setCatalog(nextCatalog);
    const dbName = nextCatalog.databases.some((item) => item.name === preferredDatabaseName)
      ? preferredDatabaseName
      : nextCatalog.databases[0]?.name ?? "";
    const nextDatabase = nextCatalog.databases.find((item) => item.name === dbName);
    setSelectedDatabaseName(dbName);
    setSelectedTable(nextDatabase?.tables[0]?.name ?? "");
    setSelectedTableView("all");
    setRows([]);
    setRowsViewName("all");
    setRowHistory([]);
    return dbName;
  }

  async function refreshCatalogAfterAuth() {
    const nextCatalog = await loadMetadata();
    applyCatalogSelection(nextCatalog);
  }

  async function createDatabaseFromSidebar() {
    const name = newDatabaseName.trim();
    if (!name) {
      setStatus("Database name is required");
      return;
    }
    try {
      const saved = await createDatabase({ name, sqlite_path: `./data/${name}.sqlite` });
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
      const saved = await createTable(
        database.name,
        {
          name,
          display_name: name,
          fields: [{ name: "name", type: "text", required: true, deleted: false }],
          views: []
        }
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

  async function persistTableMetadata(nextTable: TableMetadata, successMessage: string, nextViewName = selectedTableView) {
    if (!database.name || !table.name) {
      setStatus("Select a table before updating metadata");
      return;
    }
    try {
      await updateTableMetadata(database.name, table.name, nextTable);
      const nextCatalog = await loadMetadata();
      setCatalog(nextCatalog);
      setSelectedTable(nextTable.name);
      setSelectedTableView(nextViewName);
      setRows([]);
      setRowsViewName(nextViewName);
      setRowHistory([]);
      setStatus(successMessage);
    } catch (error) {
      setStatus(error instanceof Error ? error.message : "Table metadata update failed");
    }
  }

  async function addFieldFromCanvas() {
    const name = newFieldName.trim();
    if (!name) {
      setStatus("Field name is required");
      return;
    }
    if (name === "record_id" || table.fields.some((field) => field.name === name && !field.deleted)) {
      setStatus(`Field ${name} already exists`);
      return;
    }
    const nextTable = {
      ...table,
      fields: [...table.fields, { name, type: newFieldType, required: newFieldRequired, deleted: false }]
    };
    await persistTableMetadata(nextTable, `Added field ${name}`);
    setNewFieldName("");
    setNewFieldType("text");
    setNewFieldRequired(false);
  }

  async function deleteFieldFromCanvas(fieldName: string) {
    const nextTable = {
      ...table,
      fields: table.fields.map((field) => (field.name === fieldName ? { ...field, deleted: true } : field))
    };
    await persistTableMetadata(nextTable, `Deleted field ${fieldName}`);
  }

  async function createViewFromCanvas() {
    const name = newViewName.trim();
    if (!name) {
      setStatus("View name is required");
      return;
    }
    if (name === "all" || table.views.some((viewDef) => viewDef.name === name)) {
      setStatus(`View ${name} already exists`);
      return;
    }
    const filters: TableViewFilter[] = newViewFilterField
      ? [
          {
            field: newViewFilterField,
            op: newViewFilterOp,
            value: newViewFilterOp === "not_empty" ? undefined : newViewFilterValue
          }
        ]
      : [];
    const sorts: TableViewSort[] = newViewSortField
      ? [{ field: newViewSortField, direction: newViewSortDirection }]
      : [];
    const nextView: TableView = {
      name,
      display_name: name,
      base_view: newViewBase === "all" ? undefined : newViewBase,
      filters,
      sorts
    };
    const nextTable = { ...table, views: [...(table.views ?? []), nextView] };
    await persistTableMetadata(nextTable, `Created view ${name}`, name);
    setNewViewName("");
    setNewViewBase("all");
    setNewViewFilterField("");
    setNewViewFilterOp("eq");
    setNewViewFilterValue("");
    setNewViewSortField("");
    setNewViewSortDirection("asc");
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
      const saved = await createRole(database.name, name);
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
      await saveRoleGrants(database.name, selectedRole.name, compactRoleGrants(roleDraftGrants, database));
      const saved = await saveRoleMembers(database.name, selectedRole.name, compactMembers(roleDraftMembers));
      setRoles((current) => replaceRole(current, saved));
      setSelectedRoleName(saved.name);
      setRoleDraftMembers(saved.members ?? []);
      setStatus(`Saved role ${saved.name}`);
    } catch (error) {
      setStatus(error instanceof Error ? error.message : "Role save failed");
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
      const saved = await createRow(database.name, table.name, values);
      setRows((current) => [...current, rowRecordToValues(saved)]);
      setRowsViewName("local");
      setSelectedRecordID(saved.record_id);
      setRowHistory([]);
      setStatus(`Created record ${saved.record_id}`);
    } catch (error) {
      setStatus(error instanceof Error ? error.message : "Row creation failed");
    }
  }

  function updateSelectedRowDraft(fieldName: string, value: string) {
    setSelectedRowDraft((current) => ({ ...current, [fieldName]: value }));
  }

  async function updateSelectedRowFromEditor() {
    if (!selectedRecordID) {
      setStatus("Select a row before saving changes");
      return;
    }
    try {
      const saved = await updateRow(database.name, table.name, selectedRecordID, selectedRowDraft);
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
      setStatus(error instanceof Error ? error.message : "Row update failed");
    }
  }

  async function deleteSelectedRow() {
    if (!selectedRecordID) {
      setStatus("Select a row before deleting");
      return;
    }
    try {
      const deleted = await deleteRow(database.name, table.name, selectedRecordID);
      setRows((current) => current.filter((item) => Number(item.record_id) !== deleted.record_id));
      setRowsViewName("local");
      setSelectedRecordID(0);
      setRowHistory([]);
      setStatus(`Deleted record ${deleted.record_id}`);
    } catch (error) {
      setStatus(error instanceof Error ? error.message : "Row deletion failed");
    }
  }

  async function registerUser() {
    try {
      const user = await register(authEmail, authPassword);
      setCurrentUser(user);
      setAuthReady(true);
      await refreshCatalogAfterAuth();
      setStatus(`Signed in as ${user.email}`);
    } catch (error) {
      setStatus(error instanceof Error ? error.message : "Registration failed");
    }
  }

  async function loginUser() {
    try {
      const user = await login(authEmail, authPassword);
      setCurrentUser(user);
      setAuthReady(true);
      await refreshCatalogAfterAuth();
      setStatus(`Signed in as ${user.email}`);
    } catch (error) {
      setStatus(error instanceof Error ? error.message : "Login failed");
    }
  }

  async function logoutUser() {
    try {
      await logout();
      setCurrentUser(null);
      setAuthReady(true);
      applyCatalogSelection(emptyCatalog, "");
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
      const changes = await listRowHistory(database.name, table.name, selectedRecordID);
      setRowHistory(changes);
      setStatus(`Loaded ${changes.length} history entries for record ${selectedRecordID}`);
    } catch (error) {
      setRowHistory([]);
      setStatus(error instanceof Error ? error.message : "Row history failed");
    }
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

  function addRoleMember() {
    const memberID = newRoleMemberID.trim();
    if (!memberID) {
      setStatus("Role member user id is required");
      return;
    }
    setRoleDraftMembers((current) => compactMembers([...current, memberID]));
    setNewRoleMemberID("");
  }

  function removeRoleMember(memberID: string) {
    setRoleDraftMembers((current) => current.filter((item) => item !== memberID));
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
        newFormName={newFormName}
        newRoleName={newRoleName}
        newTableName={newTableName}
        newWorkflowName={newWorkflowName}
        onCreateDatabase={createDatabaseFromSidebar}
        onCreateForm={workflowFormWorkspace.createForm}
        onCreateRole={createRoleFromSidebar}
        onCreateTable={createTableFromSidebar}
        onCreateWorkflow={workflowFormWorkspace.createWorkflow}
        onLogout={logoutUser}
        onNewDatabaseNameChange={setNewDatabaseName}
        onNewFormNameChange={workflowFormWorkspace.setNewFormName}
        onNewRoleNameChange={setNewRoleName}
        onNewTableNameChange={setNewTableName}
        onNewWorkflowNameChange={workflowFormWorkspace.setNewWorkflowName}
        onOpenLogin={() => setAuthDialogOpen(true)}
        onSelectDatabaseSection={selectDatabaseSection}
        onSelectFormID={workflowFormWorkspace.setSelectedFormID}
        onSelectRoleName={setSelectedRoleName}
        onSelectTable={setSelectedTable}
        onSelectTableView={setSelectedTableView}
        onSelectWorkflowID={workflowFormWorkspace.setSelectedWorkflowID}
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
              <ToolbarButton
                aria-label="Refresh metadata"
                icon={<ArrowClockwiseRegular />}
                onClick={refreshMetadata}
                disabled={!currentUser}
              />
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
              newFieldName={newFieldName}
              newFieldRequired={newFieldRequired}
              newFieldType={newFieldType}
              newViewBase={newViewBase}
              newViewFilterField={newViewFilterField}
              newViewFilterOp={newViewFilterOp}
              newViewFilterValue={newViewFilterValue}
              newViewName={newViewName}
              newViewSortDirection={newViewSortDirection}
              newViewSortField={newViewSortField}
              onAddRow={addDraftRow}
              onAddField={addFieldFromCanvas}
              onCellEdited={editCell}
              onCreateView={createViewFromCanvas}
              onDeleteField={deleteFieldFromCanvas}
              onDeleteSelectedRow={deleteSelectedRow}
              onLoadHistory={loadSelectedRowHistory}
              onNewFieldNameChange={setNewFieldName}
              onNewFieldRequiredChange={setNewFieldRequired}
              onNewFieldTypeChange={setNewFieldType}
              onNewViewBaseChange={setNewViewBase}
              onNewViewFilterFieldChange={setNewViewFilterField}
              onNewViewFilterOpChange={setNewViewFilterOp}
              onNewViewFilterValueChange={setNewViewFilterValue}
              onNewViewNameChange={setNewViewName}
              onNewViewSortDirectionChange={setNewViewSortDirection}
              onNewViewSortFieldChange={setNewViewSortField}
              onSelectRecordID={setSelectedRecordID}
              onSelectTableView={setSelectedTableView}
              onSelectedRowValueChange={updateSelectedRowDraft}
              onUpdateSelectedRow={updateSelectedRowFromEditor}
              rowHistory={rowHistory}
              rows={rows}
              selectedRecordID={selectedRecordID}
              selectedRowDraft={selectedRowDraft}
              selectedTableView={selectedTableView}
              table={table}
            />
          )}

          {view === "workflow" && (
            <WorkflowWorkspace
              databaseName={database.name}
              onExecute={workflowFormWorkspace.executeWorkflow}
              onSave={workflowFormWorkspace.persistWorkflow}
              onSelectRunKey={workflowFormWorkspace.setSelectedWorkflowRunKey}
              onUpdateConfigJSON={workflowFormWorkspace.updateSelectedWorkflowJSON}
              onUpdateInputsJSON={workflowFormWorkspace.updateWorkflowInputsJSON}
              onUpdateScript={workflowFormWorkspace.updateSelectedWorkflowScript}
              inputsText={workflowInputsText}
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
              onFormValueChange={workflowFormWorkspace.updateFormValue}
              onSave={workflowFormWorkspace.persistForm}
              onSubmit={workflowFormWorkspace.submitRenderedForm}
              onUpdateScript={workflowFormWorkspace.updateSelectedFormScript}
              renderedForm={renderedForm}
            />
          )}

          {view === "permission" && (
            <PermissionPanel
              database={database}
              forms={forms}
              grants={roleDraftGrants}
              members={roleDraftMembers}
              newMemberID={newRoleMemberID}
              onAddMember={addRoleMember}
              onGrantChange={updateRoleGrant}
              onMemberRemove={removeRoleMember}
              onNewMemberIDChange={setNewRoleMemberID}
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
