import { useEffect, useState } from "react";
import { Text, Toolbar, ToolbarButton, Tooltip } from "@fluentui/react-components";
import { ArrowClockwiseRegular } from "@fluentui/react-icons";
import { useTranslation } from "react-i18next";
import { AuthDialog } from "./components/AuthDialog";
import { FormWorkspace } from "./components/FormWorkspace";
import { PermissionPanel } from "./components/PermissionPanel";
import { PublishedFormPage } from "./components/PublishedFormPage";
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
import { appLanguages, normalizeLanguage, type AppLanguage } from "./i18n";

type View = WorkspaceView;

const emptyDatabase: DatabaseMetadata = { name: "", sqlite_path: "", tables: [] };
const emptyTable: TableMetadata = { name: "", display_name: "", fields: [], views: [] };
const emptyCatalog: Catalog = { databases: [] };

export function App() {
  const publishedFormToken = publishedFormTokenFromPath();
  return publishedFormToken ? <PublishedFormPage token={publishedFormToken} /> : <WorkspaceApp />;
}

function WorkspaceApp() {
  const { i18n, t } = useTranslation();
  const [catalog, setCatalog] = useState<Catalog>(emptyCatalog);
  const [view, setView] = useState<View>("table");
  const [selectedDatabaseName, setSelectedDatabaseName] = useState("");
  const [selectedTable, setSelectedTable] = useState("");
  const [selectedTableView, setSelectedTableView] = useState("all");
  const [openViewPanelRequest, setOpenViewPanelRequest] = useState(0);
  const [authEmail, setAuthEmail] = useState("");
  const [authPassword, setAuthPassword] = useState("");
  const [currentUser, setCurrentUser] = useState<AuthUser | null>(null);
  const [authReady, setAuthReady] = useState(false);
  const [oidcProviders, setOIDCProviders] = useState<OIDCProvider[]>([]);
  const [authDialogOpen, setAuthDialogOpen] = useState(false);
  const [newDatabaseName, setNewDatabaseName] = useState("");
  const [newTableName, setNewTableName] = useState("");
  const [status, setStatus] = useState(t("status.ready"));
  const language = normalizeLanguage(i18n.resolvedLanguage ?? i18n.language);

  const database =
    catalog.databases.find((item) => item.name === selectedDatabaseName) ?? catalog.databases[0] ?? emptyDatabase;
  const table = database.tables.find((item) => item.name === selectedTable) ?? database.tables[0] ?? emptyTable;
  const tableWorkspace = useTableWorkspace({
    currentUserID: currentUser?.id,
    databaseName: database.name,
    selectedTableView,
    table,
    tables: database.tables,
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
    workflowTrigger,
    workflowInstances,
    workflowNodes,
    workflowRuns,
    workflows,
  } = workflowFormWorkspace;
  const permissionWorkspace = usePermissionWorkspace({
    currentUserID: currentUser?.id,
    database,
    onStatus: setStatus
  });
  const {
    memberSearchResults,
    newRoleMemberEmail,
    newRoleName,
    roleDraftGrants,
    roleDraftMemberUsers,
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
          setStatus(t("status.signedInAs", { email: user.email }));
        }
      })
      .catch((error) => {
        if (!cancelled) {
          setStatus(error instanceof Error ? error.message : t("status.currentUserLoadFailed"));
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
          setStatus(error instanceof Error ? error.message : t("status.metadataLoadFailed"));
        }
      });
    return () => {
      cancelled = true;
    };
  }, [authReady, currentUser?.id]);

  async function refreshMetadata() {
    if (!currentUser) {
      setStatus(t("status.loginBeforeRefresh"));
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
      setStatus(t("status.metadataRefreshed"));
    } catch (error) {
      setStatus(error instanceof Error ? error.message : t("status.metadataRefreshFailed"));
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
      setStatus(t("status.databaseNameRequired"));
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
      setStatus(t("status.createdDatabase", { name: saved.name }));
    } catch (error) {
      setStatus(error instanceof Error ? error.message : t("status.databaseCreationFailed"));
    }
  }

  async function createTableFromSidebar() {
    if (!database.name) {
      setStatus(t("status.selectDatabaseBeforeTable"));
      return;
    }
    const name = newTableName.trim();
    if (!name) {
      setStatus(t("status.tableNameRequired"));
      return;
    }
    try {
      const saved = await createTable(
        database.name,
        {
          name,
          display_name: name,
          fields: [],
          views: []
        }
      );
      const nextCatalog = await loadMetadata();
      setCatalog(nextCatalog);
      setSelectedTable(saved.name);
      setSelectedTableView("all");
      tableWorkspace.resetRows("all");
      setNewTableName("");
      setStatus(t("status.createdTable", { database: database.name, table: saved.name }));
    } catch (error) {
      setStatus(error instanceof Error ? error.message : t("status.tableCreationFailed"));
    }
  }

  async function registerUser() {
    try {
      const user = await register(authEmail, authPassword);
      setCurrentUser(user);
      setAuthReady(true);
      await refreshCatalogAfterAuth();
      setStatus(t("status.signedInAs", { email: user.email }));
    } catch (error) {
      setStatus(error instanceof Error ? error.message : t("status.registrationFailed"));
    }
  }

  async function loginUser() {
    try {
      const user = await login(authEmail, authPassword);
      setCurrentUser(user);
      setAuthReady(true);
      await refreshCatalogAfterAuth();
      setStatus(t("status.signedInAs", { email: user.email }));
    } catch (error) {
      setStatus(error instanceof Error ? error.message : t("status.loginFailed"));
    }
  }

  async function logoutUser() {
    try {
      await logout();
      setCurrentUser(null);
      setAuthReady(true);
      applyCatalogSelection(emptyCatalog, "");
      setStatus(t("status.signedOut"));
    } catch (error) {
      setStatus(error instanceof Error ? error.message : t("status.logoutFailed"));
    }
  }

  function loginWithOIDC(providerName: string) {
    window.location.assign(oidcStartURL(providerName));
  }

  function cycleLanguage() {
    const currentIndex = appLanguages.indexOf(language);
    void i18n.changeLanguage(appLanguages[(currentIndex + 1) % appLanguages.length] as AppLanguage);
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

  async function openTableViewPanelFromSidebar(tableName: string) {
    setView("table");
    setSelectedTable(tableName);
    setOpenViewPanelRequest((current) => current + 1);
    if (tableName !== table.name) {
      setSelectedTableView("all");
      setStatus(t("status.selectTableBeforeView"));
      return;
    }
    await tableWorkspace.createDefaultViewFromSidebar();
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
        onOpenTableViewPanel={openTableViewPanelFromSidebar}
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
        onDeleteForm={workflowFormWorkspace.deleteSelectedForm}
        onDeleteWorkflow={workflowFormWorkspace.deleteSelectedWorkflow}
        onRenameForm={workflowFormWorkspace.renameSelectedForm}
        onRenameWorkflow={workflowFormWorkspace.renameSelectedWorkflow}
      />

      <main className="workspace">
        <header className="topbar">
          <div className="workspace-title">
            <Text weight="semibold">
              {database.name || t("common.noDatabase")}
              {view === "table" && table.name ? ` / ${table.display_name || table.name}` : ""}
              {view === "workflow" && selectedWorkflow ? ` / ${selectedWorkflow.name}` : ""}
              {view === "form" && selectedForm ? ` / ${selectedForm.name}` : ""}
              {view === "permission" ? ` / ${t("common.permission").toLowerCase()}` : ""}
            </Text>
          </div>
          <Toolbar aria-label={t("common.workspaceActions")}>
            <ToolbarButton aria-label={t("language.switch")} onClick={cycleLanguage}>
              {t("language.toggleLabel")}
            </ToolbarButton>
            <Tooltip content={t("common.refreshMetadata")} relationship="label">
              <ToolbarButton
                aria-label={t("common.refreshMetadata")}
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
              displayedRows={tableWorkspace.displayedRows}
              newFieldFormula={tableWorkspace.newFieldFormula}
              newFieldName={tableWorkspace.newFieldName}
              newFieldType={tableWorkspace.newFieldType}
              newFormulaValueType={tableWorkspace.newFormulaValueType}
              newRelationTable={tableWorkspace.newRelationTable}
              newViewBase={tableWorkspace.newViewBase}
              newViewFilterField={tableWorkspace.newViewFilterField}
              newViewFilterOp={tableWorkspace.newViewFilterOp}
              newViewFilterValue={tableWorkspace.newViewFilterValue}
              newViewSortDirection={tableWorkspace.newViewSortDirection}
              newViewSortField={tableWorkspace.newViewSortField}
              onAddRow={tableWorkspace.addDraftRow}
              onAddField={tableWorkspace.addFieldFromCanvas}
              onDeleteField={tableWorkspace.deleteFieldFromCanvas}
              onDeleteSelectedRow={tableWorkspace.deleteSelectedRow}
              onLoadHistory={tableWorkspace.loadSelectedRowHistory}
              onNewFieldFormulaChange={tableWorkspace.setNewFieldFormula}
              onNewFieldNameChange={tableWorkspace.setNewFieldName}
              onNewFieldTypeChange={tableWorkspace.setNewFieldType}
              onNewFormulaValueTypeChange={tableWorkspace.setNewFormulaValueType}
              onNewRelationTableChange={tableWorkspace.setNewRelationTable}
              onNewViewBaseChange={tableWorkspace.setNewViewBase}
              onNewViewFilterFieldChange={tableWorkspace.setNewViewFilterField}
              onNewViewFilterOpChange={tableWorkspace.setNewViewFilterOp}
              onNewViewFilterValueChange={tableWorkspace.setNewViewFilterValue}
              onNewViewSortDirectionChange={tableWorkspace.setNewViewSortDirection}
              onNewViewSortFieldChange={tableWorkspace.setNewViewSortField}
              onRowsChange={tableWorkspace.editGridRows}
              onSelectGridCell={tableWorkspace.selectGridCell}
              onSelectRecordID={tableWorkspace.setSelectedRecordID}
              onSelectedRowValueChange={tableWorkspace.updateSelectedRowDraft}
              onUpdateFieldFormula={tableWorkspace.updateFieldFormulaFromCanvas}
              onUpdateSelectedRow={tableWorkspace.updateSelectedRowFromEditor}
              onUpdateSelectedView={tableWorkspace.updateSelectedViewFromCanvas}
              rowHistory={tableWorkspace.rowHistory}
              relationDetail={tableWorkspace.relationDetail}
              onCloseRelationDetail={() => tableWorkspace.setRelationDetail(null)}
              rows={tableWorkspace.rows}
              selectedRecordID={tableWorkspace.selectedRecordID}
              selectedRowDraft={tableWorkspace.selectedRowDraft}
              selectedTableView={selectedTableView}
              table={table}
              tables={database.tables}
              openViewPanelRequest={openViewPanelRequest}
            />
          )}

          {view === "workflow" && (
            <WorkflowWorkspace
              databaseName={database.name}
              onExecute={workflowFormWorkspace.executeWorkflow}
              onSave={workflowFormWorkspace.persistWorkflow}
              onSaveInstanceConfig={workflowFormWorkspace.saveSelectedWorkflowInstanceConfig}
              onSelectRunKey={workflowFormWorkspace.setSelectedWorkflowRunKey}
              onUpdateScript={workflowFormWorkspace.updateSelectedWorkflowScript}
              onToggleEnabled={workflowFormWorkspace.toggleSelectedWorkflowEnabled}
              language={language}
              workflowTrigger={workflowTrigger}
              workflowInstances={workflowInstances}
              selectedRun={selectedWorkflowRun}
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
              onPublish={workflowFormWorkspace.publishSelectedForm}
              onSave={workflowFormWorkspace.persistForm}
              onSubmit={workflowFormWorkspace.submitRenderedForm}
              onUnpublish={workflowFormWorkspace.unpublishSelectedForm}
              onUpdateScript={workflowFormWorkspace.updateSelectedFormScript}
              renderedForm={renderedForm}
              tables={database.tables}
            />
          )}

          {view === "permission" && (
            <PermissionPanel
              database={database}
              forms={forms}
              grants={roleDraftGrants}
              members={roleDraftMembers}
              memberOptions={memberSearchResults}
              memberUsers={roleDraftMemberUsers}
              newMemberEmail={newRoleMemberEmail}
              onAddMember={permissionWorkspace.addRoleMember}
              onGrantChange={permissionWorkspace.updateRoleGrant}
              onMemberRemove={permissionWorkspace.removeRoleMember}
              onNewMemberEmailChange={permissionWorkspace.setNewRoleMemberEmail}
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

function publishedFormTokenFromPath(): string {
  const match = /^\/forms\/([^/]+)$/.exec(window.location.pathname);
  return match ? decodeURIComponent(match[1]) : "";
}
