import { useEffect, useState } from "react";
import { Text, Toolbar, ToolbarButton, Tooltip } from "@fluentui/react-components";
import { ArrowClockwiseRegular } from "@fluentui/react-icons";
import { AuthDialog } from "./components/AuthDialog";
import { FormWorkspace } from "./components/FormWorkspace";
import { PermissionPanel } from "./components/PermissionPanel";
import { TableWorkspace } from "./components/TableWorkspace";
import { WorkflowWorkspace } from "./components/WorkflowWorkspace";
import { WorkspaceNavigation, type WorkspaceView } from "./components/WorkspaceNavigation";
import { usePermissionWorkspace } from "./hooks/usePermissionWorkspace";
import { useTableWorkspace } from "./hooks/useTableWorkspace";
import { useWorkflowFormWorkspace } from "./hooks/useWorkflowFormWorkspace";
import {
  createDatabase,
  createTable,
  listOIDCProviders,
  loadCurrentUser,
  loadMetadata,
  login,
  logout,
  oidcStartURL,
  register,
  type AuthUser,
  type Catalog,
  type DatabaseMetadata,
  type OIDCProvider,
  type TableMetadata,
} from "./api";

type View = WorkspaceView;

const emptyDatabase: DatabaseMetadata = { name: "", sqlite_path: "", tables: [] };
const emptyTable: TableMetadata = { name: "", display_name: "", fields: [], views: [] };
const emptyCatalog: Catalog = { databases: [] };

export function App() {
  const [catalog, setCatalog] = useState<Catalog>(emptyCatalog);
  const [view, setView] = useState<View>("table");
  const [selectedDatabaseName, setSelectedDatabaseName] = useState("");
  const [selectedTable, setSelectedTable] = useState("");
  const [selectedTableView, setSelectedTableView] = useState("all");
  const [authEmail, setAuthEmail] = useState("");
  const [authPassword, setAuthPassword] = useState("");
  const [currentUser, setCurrentUser] = useState<AuthUser | null>(null);
  const [authReady, setAuthReady] = useState(false);
  const [oidcProviders, setOIDCProviders] = useState<OIDCProvider[]>([]);
  const [authDialogOpen, setAuthDialogOpen] = useState(false);
  const [newDatabaseName, setNewDatabaseName] = useState("");
  const [newTableName, setNewTableName] = useState("");
  const [status, setStatus] = useState("Ready");

  const database =
    catalog.databases.find((item) => item.name === selectedDatabaseName) ?? catalog.databases[0] ?? emptyDatabase;
  const table = database.tables.find((item) => item.name === selectedTable) ?? database.tables[0] ?? emptyTable;
  const tableWorkspace = useTableWorkspace({
    currentUserID: currentUser?.id,
    databaseName: database.name,
    selectedTableView,
    table,
    onCatalogChanged: (nextCatalog, tableName, viewName) => {
      setCatalog(nextCatalog);
      setSelectedTable(tableName);
      setSelectedTableView(viewName);
    },
    onStatus: setStatus
  });
  const workflowFormWorkspace = useWorkflowFormWorkspace({
    currentUserID: currentUser?.id,
    databaseName: database.name,
    tableName: table.name,
    onStatus: setStatus,
    onSubmittedRow: tableWorkspace.addSubmittedRow
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
  const permissionWorkspace = usePermissionWorkspace({
    currentUserID: currentUser?.id,
    database,
    onStatus: setStatus
  });
  const {
    newRoleMemberID,
    newRoleName,
    roleDraftGrants,
    roleDraftMembers,
    roles,
    selectedRole
  } = permissionWorkspace;

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
      permissionWorkspace.clearRoles();
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

  async function refreshMetadata() {
    if (!currentUser) {
      setStatus("Login before refreshing workspace metadata");
      return;
    }
    try {
      const nextCatalog = await loadMetadata();
      const dbName = applyCatalogSelection(nextCatalog, selectedDatabaseName);
      if (dbName) {
        await Promise.all([
          permissionWorkspace.refreshRoles(dbName),
          workflowFormWorkspace.refreshResources(dbName)
        ]);
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
    tableWorkspace.resetRows("all");
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
      tableWorkspace.resetRows("all");
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
      tableWorkspace.resetRows("all");
      setNewTableName("");
      setStatus(`Created table ${database.name}.${saved.name}`);
    } catch (error) {
      setStatus(error instanceof Error ? error.message : "Table creation failed");
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
        onCreateRole={permissionWorkspace.createRoleFromSidebar}
        onCreateTable={createTableFromSidebar}
        onCreateWorkflow={workflowFormWorkspace.createWorkflow}
        onLogout={logoutUser}
        onNewDatabaseNameChange={setNewDatabaseName}
        onNewFormNameChange={workflowFormWorkspace.setNewFormName}
        onNewRoleNameChange={permissionWorkspace.setNewRoleName}
        onNewTableNameChange={setNewTableName}
        onNewWorkflowNameChange={workflowFormWorkspace.setNewWorkflowName}
        onOpenLogin={() => setAuthDialogOpen(true)}
        onSelectDatabaseSection={selectDatabaseSection}
        onSelectFormID={workflowFormWorkspace.setSelectedFormID}
        onSelectRoleName={permissionWorkspace.setSelectedRoleName}
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
              {view === "table" && `${tableWorkspace.displayedRows.length} of ${tableWorkspace.rows.length} records`}
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
              columns={tableWorkspace.columns}
              displayedRecordIDs={tableWorkspace.displayedRecordIDs}
              displayedRows={tableWorkspace.displayedRows}
              newFieldName={tableWorkspace.newFieldName}
              newFieldRequired={tableWorkspace.newFieldRequired}
              newFieldType={tableWorkspace.newFieldType}
              newViewBase={tableWorkspace.newViewBase}
              newViewFilterField={tableWorkspace.newViewFilterField}
              newViewFilterOp={tableWorkspace.newViewFilterOp}
              newViewFilterValue={tableWorkspace.newViewFilterValue}
              newViewName={tableWorkspace.newViewName}
              newViewSortDirection={tableWorkspace.newViewSortDirection}
              newViewSortField={tableWorkspace.newViewSortField}
              onAddRow={tableWorkspace.addDraftRow}
              onAddField={tableWorkspace.addFieldFromCanvas}
              onCreateView={tableWorkspace.createViewFromCanvas}
              onDeleteField={tableWorkspace.deleteFieldFromCanvas}
              onDeleteSelectedRow={tableWorkspace.deleteSelectedRow}
              onLoadHistory={tableWorkspace.loadSelectedRowHistory}
              onNewFieldNameChange={tableWorkspace.setNewFieldName}
              onNewFieldRequiredChange={tableWorkspace.setNewFieldRequired}
              onNewFieldTypeChange={tableWorkspace.setNewFieldType}
              onNewViewBaseChange={tableWorkspace.setNewViewBase}
              onNewViewFilterFieldChange={tableWorkspace.setNewViewFilterField}
              onNewViewFilterOpChange={tableWorkspace.setNewViewFilterOp}
              onNewViewFilterValueChange={tableWorkspace.setNewViewFilterValue}
              onNewViewNameChange={tableWorkspace.setNewViewName}
              onNewViewSortDirectionChange={tableWorkspace.setNewViewSortDirection}
              onNewViewSortFieldChange={tableWorkspace.setNewViewSortField}
              onRowsChange={tableWorkspace.editGridRows}
              onSelectGridCell={tableWorkspace.selectGridCell}
              onSelectRecordID={tableWorkspace.setSelectedRecordID}
              onSelectTableView={setSelectedTableView}
              onSelectedRowValueChange={tableWorkspace.updateSelectedRowDraft}
              onUpdateSelectedRow={tableWorkspace.updateSelectedRowFromEditor}
              rowHistory={tableWorkspace.rowHistory}
              rows={tableWorkspace.rows}
              selectedRecordID={tableWorkspace.selectedRecordID}
              selectedRowDraft={tableWorkspace.selectedRowDraft}
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
              onAddMember={permissionWorkspace.addRoleMember}
              onGrantChange={permissionWorkspace.updateRoleGrant}
              onMemberRemove={permissionWorkspace.removeRoleMember}
              onNewMemberIDChange={permissionWorkspace.setNewRoleMemberID}
              onSave={permissionWorkspace.persistRoleGrants}
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
