import { useEffect, useMemo, useRef, useState } from "react";
import { Database, Table2 } from "lucide-react";
import { useTranslation } from "react-i18next";
import { AppWorkspace } from "./components/AppWorkspace";
import { AuthScreen } from "./components/AuthScreen";
import { CreateNameDialog } from "./components/CreateNamePopover";
import { LibraryWorkspace } from "./components/LibraryWorkspace";
import { LoginPage } from "./components/LoginPage";
import { PermissionPanel } from "./components/PermissionPanel";
import { RunnersPanel } from "./components/RunnersPanel";
import { TableWorkspace } from "./components/TableWorkspace";
import { WorkflowWorkspace } from "./components/WorkflowWorkspace";
import { useNotifier } from "./notifications";
import { useAppWorkspace } from "./hooks/useAppWorkspace";
import { useLibraryWorkspace } from "./hooks/useLibraryWorkspace";
import { usePermissionWorkspace } from "./hooks/usePermissionWorkspace";
import { useTableWorkspace } from "./hooks/useTableWorkspace";
import { useWorkflowWorkspace } from "./hooks/useWorkflowWorkspace";
import { buildWorkspacePath, parseWorkspaceRoute, type WorkspaceRoute, type WorkspaceViewName } from "./appState";
import {
  createDatabase,
  createTable,
  loadAuthConfig,
  loadCurrentUser,
  loadMetadata,
  login,
  logout,
  oidcStartURL,
  register,
  updateTableMetadata,
  type AuthUser,
  type Catalog,
  type DatabaseMetadata,
  type OIDCProvider,
  type TableMetadata
} from "./api";
import { AdminPage } from "./pages/AdminPage";
import { HomePage } from "./pages/HomePage";
import { WorkflowRunsPage } from "./pages/WorkflowRunsPage";
import { AppShell } from "./shell/AppShell";
import { NavigationProvider } from "./shell/navigation";
import { Page } from "./shell/Page";
import type { SidebarProps } from "./shell/Sidebar";
import { Button, Card, EmptyState, Spinner, ThemeProvider, ToastProvider, TooltipProvider } from "./ui";

type View = WorkspaceViewName;

const emptyDatabase: DatabaseMetadata = { name: "", tables: [] };
const emptyTable: TableMetadata = { name: "", display_name: "", fields: [], views: [] };
const emptyCatalog: Catalog = { databases: [] };
// The database a bare "/" opens: the one used last on this browser.
const lastDatabaseKey = "autable.database";

export function App() {
  return (
    <ThemeProvider>
      <TooltipProvider>
        <ToastProvider>{window.location.pathname === "/login" ? <LoginPage /> : <WorkspaceApp />}</ToastProvider>
      </TooltipProvider>
    </ThemeProvider>
  );
}

function WorkspaceApp() {
  const { t } = useTranslation();
  const [locationPath, setLocationPath] = useState(() => window.location.pathname);
  const [catalog, setCatalog] = useState<Catalog>(emptyCatalog);
  const [view, setView] = useState<View>("home");
  const [selectedDatabaseName, setSelectedDatabaseName] = useState("");
  const [selectedTable, setSelectedTable] = useState("");
  const [selectedTableView, setSelectedTableView] = useState("all");
  const [openViewPanelRequest, setOpenViewPanelRequest] = useState(0);
  const [currentUser, setCurrentUser] = useState<AuthUser | null>(null);
  const [authReady, setAuthReady] = useState(false);
  const [metadataReady, setMetadataReady] = useState(false);
  const [passwordAuthEnabled, setPasswordAuthEnabled] = useState(true);
  const [oidcProviders, setOIDCProviders] = useState<OIDCProvider[]>([]);
  const [newDatabaseName, setNewDatabaseName] = useState("");
  const [newTableName, setNewTableName] = useState("");
  const [runnersOpen, setRunnersOpen] = useState(false);
  const { notify } = useNotifier();
  const workspaceRoute = useMemo(() => parseWorkspaceRoute(locationPath), [locationPath]);
  const workspaceRouteRef = useRef<WorkspaceRoute | null>(workspaceRoute);
  workspaceRouteRef.current = workspaceRoute;
  // Global admins (from the server config) write structure and code; all
  // other users only get the data, apps and workflows granted to them.
  const isAdmin = Boolean(currentUser?.is_admin);

  const database = catalog.databases.find((item) => item.name === selectedDatabaseName) ?? emptyDatabase;
  const table = database.tables.find((item) => item.name === selectedTable) ?? database.tables[0] ?? emptyTable;

  // Views (including the built-in "all") arrive permission-filtered from the
  // server; if the current selection is not among them, fall back to the
  // first authorized view instead of rendering a denied grid.
  useEffect(() => {
    if (!table.name || !database.name || view !== "table") {
      return;
    }
    const viewNames = (table.views ?? []).map((viewDef) => viewDef.name);
    if (viewNames.length === 0 || viewNames.includes(selectedTableView)) {
      return;
    }
    setSelectedTableView(viewNames[0]);
    navigateWorkspace(buildRouteForSelection(database.name, "table", table.name, viewNames[0]), "replace");
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [database.name, table.name, table.views, selectedTableView, view]);
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
    onStatus: notify
  });
  const workflowRoute = workspaceRoute?.databaseName === database.name && workspaceRoute.view === "workflow" ? workspaceRoute : null;
  const selectedWorkflowName = workflowRoute?.workflowName ?? "";
  const workflowWorkspace = useWorkflowWorkspace({
    currentUserID: currentUser?.id,
    databaseName: database.name,
    isAdmin,
    workflowName: selectedWorkflowName,
    onStatus: notify
  });
  const { workflows, workflowsReady } = workflowWorkspace;
  const selectedWorkflow = workflows.find((item) => item.name === selectedWorkflowName);
  const selectedAppName =
    workspaceRoute?.databaseName === database.name && workspaceRoute.view === "app" ? workspaceRoute.appName ?? "" : "";
  const appWorkspace = useAppWorkspace({
    currentUserID: currentUser?.id,
    databaseName: database.name,
    appName: selectedAppName,
    onStatus: notify
  });
  const { apps, appsReady } = appWorkspace;
  const selectedApp = apps.find((item) => item.name === selectedAppName);
  const libraryRoute = workspaceRoute?.databaseName === database.name && workspaceRoute.view === "library" ? workspaceRoute : null;
  const selectedLibraryName = libraryRoute?.libraryName ?? "";
  // Libraries are instance-wide; the list loads once an admin opens the page.
  const libraryWorkspace = useLibraryWorkspace({
    enabled: isAdmin && Boolean(currentUser?.id) && (view === "library" || view === "admin"),
    libraryName: selectedLibraryName,
    onStatus: notify
  });
  const selectedLibrary = libraryWorkspace.libraries.find((item) => item.name === selectedLibraryName);
  const permissionWorkspace = usePermissionWorkspace({
    currentUserID: currentUser?.id,
    database,
    isAdmin,
    onStatus: notify
  });
  const {
    memberSearchResults,
    newRoleMemberQuery,
    newRoleName,
    roleDraftGrants,
    roleDraftMemberUsers,
    roleDraftMembers,
    rolesReady,
    roles,
    selectedRole
  } = permissionWorkspace;

  useEffect(() => {
    function handlePopState() {
      setLocationPath(window.location.pathname);
    }
    window.addEventListener("popstate", handlePopState);
    return () => window.removeEventListener("popstate", handlePopState);
  }, []);

  useEffect(() => {
    if (!selectedDatabaseName) {
      return;
    }
    if (!catalog.databases.some((item) => item.name === selectedDatabaseName)) {
      setSelectedDatabaseName("");
      setSelectedTable("");
      setSelectedTableView("all");
      return;
    }
    window.localStorage.setItem(lastDatabaseKey, selectedDatabaseName);
    if (!database.tables.some((item) => item.name === selectedTable)) {
      setSelectedTable(database.tables[0]?.name ?? "");
      setSelectedTableView("all");
    }
  }, [catalog.databases, database.tables, selectedDatabaseName, selectedTable]);

  useEffect(() => {
    if (!workspaceRoute) {
      clearWorkspaceSelection();
      if (locationPath !== "/") {
        navigateWorkspace(null, "replace");
        return;
      }
      // "/" is not a page of its own: it opens a database's home once the
      // catalog is known, or stays as the empty state when there is none.
      if (metadataReady && catalog.databases.length > 0) {
        const remembered = window.localStorage.getItem(lastDatabaseKey);
        const target = catalog.databases.find((item) => item.name === remembered) ?? catalog.databases[0];
        navigateWorkspace({ databaseName: target.name, view: "home" }, "replace");
      }
      return;
    }
    const nextDatabase = catalog.databases.find((item) => item.name === workspaceRoute.databaseName);
    if (!nextDatabase) {
      if (metadataReady) {
        navigateWorkspace(null, "replace");
      }
      return;
    }
    const nextView = allowedWorkspaceView(isAdmin, workspaceRoute.view);
    if (nextView !== workspaceRoute.view) {
      navigateWorkspace({ databaseName: nextDatabase.name, view: nextView }, "replace");
      return;
    }
    setSelectedDatabaseName(nextDatabase.name);
    setView(nextView);
    if (nextView === "table") {
      const requestedTableName = workspaceRoute.tableName;
      const nextTable = requestedTableName
        ? nextDatabase.tables.find((item) => item.name === requestedTableName)
        : nextDatabase.tables[0];
      if (requestedTableName && !nextTable) {
        navigateWorkspace(null, "replace");
        return;
      }
      if (workspaceRoute.tableViewName && !tableHasView(nextTable, workspaceRoute.tableViewName)) {
        navigateWorkspace(null, "replace");
        return;
      }
      setSelectedTable(nextTable?.name ?? "");
      setSelectedTableView(workspaceRoute.tableViewName ?? "all");
    }
  }, [catalog.databases, isAdmin, locationPath, metadataReady, workspaceRoute]);

  useEffect(() => {
    if (!workspaceRoute || workspaceRoute.databaseName !== database.name) {
      return;
    }
    if (
      workspaceRoute.view === "workflow" &&
      workspaceRoute.workflowName &&
      workflowsReady &&
      (!isAdmin || !workflows.some((item) => item.name === workspaceRoute.workflowName))
    ) {
      // Only admins open a workflow's page; others run workflows from the list.
      navigateWorkspace({ databaseName: database.name, view: "workflow" }, "replace");
      return;
    }
    if (workspaceRoute.view === "app" && workspaceRoute.appName && appsReady && !apps.some((item) => item.name === workspaceRoute.appName)) {
      navigateWorkspace({ databaseName: database.name, view: "app" }, "replace");
      return;
    }
    if (
      workspaceRoute.view === "library" &&
      workspaceRoute.libraryName &&
      libraryWorkspace.librariesReady &&
      !libraryWorkspace.libraries.some((item) => item.name === workspaceRoute.libraryName)
    ) {
      navigateWorkspace({ databaseName: database.name, view: "library" }, "replace");
      return;
    }
    if (workspaceRoute.view === "permission" && workspaceRoute.roleName && rolesReady && !roles.some((item) => item.name === workspaceRoute.roleName)) {
      navigateWorkspace(null, "replace");
      return;
    }
    if (workspaceRoute.view === "permission" && workspaceRoute.roleName && roles.some((item) => item.name === workspaceRoute.roleName)) {
      permissionWorkspace.setSelectedRoleName(workspaceRoute.roleName);
    }
  }, [apps, appsReady, database.name, isAdmin, libraryWorkspace.libraries, libraryWorkspace.librariesReady, locationPath, roles, rolesReady, workflows, workflowsReady, workspaceRoute]);

  useEffect(() => {
    let cancelled = false;
    void loadCurrentUser()
      .then((user) => {
        if (cancelled) {
          return;
        }
        setCurrentUser(user);
      })
      .catch((error) => {
        if (!cancelled) {
          notify(error instanceof Error ? error.message : t("status.currentUserLoadFailed"), "error");
        }
      })
      .finally(() => {
        if (!cancelled) {
          setAuthReady(true);
        }
      });
    void loadAuthConfig()
      .then((authConfig) => {
        if (!cancelled) {
          setPasswordAuthEnabled(authConfig.password_enabled);
          setOIDCProviders(authConfig.oidc_providers);
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
      setMetadataReady(false);
      applyCatalogSelection(emptyCatalog, "");
      permissionWorkspace.clearRoles();
      return () => {
        cancelled = true;
      };
    }
    void loadMetadata()
      .then((nextCatalog) => {
        if (!cancelled) {
          applyCatalogSelection(nextCatalog, workspaceRouteRef.current?.databaseName || selectedDatabaseName);
          setMetadataReady(true);
        }
      })
      .catch((error) => {
        if (!cancelled) {
          notify(error instanceof Error ? error.message : t("status.metadataLoadFailed"), "error");
        }
      });
    return () => {
      cancelled = true;
    };
  }, [authReady, currentUser?.id]);

  async function refreshMetadata() {
    if (!currentUser) {
      notify(t("status.loginBeforeRefresh"));
      return;
    }
    try {
      const nextCatalog = await loadMetadata();
      const dbName = applyCatalogSelection(nextCatalog, selectedDatabaseName);
      setMetadataReady(true);
      if (dbName) {
        await Promise.all([
          permissionWorkspace.refreshRoles(dbName),
          workflowWorkspace.refreshWorkflows(dbName),
          appWorkspace.refreshApps(dbName)
        ]);
      }
      notify(t("status.metadataRefreshed"), "success");
    } catch (error) {
      notify(error instanceof Error ? error.message : t("status.metadataRefreshFailed"), "error");
    }
  }

  function applyCatalogSelection(nextCatalog: Catalog, preferredDatabaseName = selectedDatabaseName) {
    setCatalog(nextCatalog);
    const route = workspaceRouteRef.current;
    const routeDatabaseName = route?.databaseName ?? "";
    const dbName =
      routeDatabaseName && nextCatalog.databases.some((item) => item.name === routeDatabaseName)
        ? routeDatabaseName
        : preferredDatabaseName && nextCatalog.databases.some((item) => item.name === preferredDatabaseName)
          ? preferredDatabaseName
          : "";
    const nextDatabase = nextCatalog.databases.find((item) => item.name === dbName);
    const nextView = route?.databaseName === dbName ? allowedWorkspaceView(isAdmin, route.view) : "home";
    const nextTable = nextDatabase?.tables.find((item) => item.name === route?.tableName) ?? nextDatabase?.tables[0] ?? emptyTable;
    setSelectedDatabaseName(dbName);
    setView(nextView);
    setSelectedTable(nextTable.name);
    const nextTableView = nextView === "table" && tableHasView(nextTable, route?.tableViewName) ? route?.tableViewName ?? "all" : "all";
    setSelectedTableView(nextTableView);
    tableWorkspace.resetRows(nextTableView);
    return dbName;
  }

  async function createDatabaseFromSidebar() {
    const name = newDatabaseName.trim();
    if (!name) {
      notify(t("status.databaseNameRequired"));
      return;
    }
    try {
      const saved = await createDatabase({ name });
      const nextCatalog = await loadMetadata();
      setCatalog(nextCatalog);
      setSelectedDatabaseName(saved.name);
      setSelectedTable(saved.tables[0]?.name ?? "");
      setSelectedTableView("all");
      tableWorkspace.resetRows("all");
      navigateWorkspace({ databaseName: saved.name, view: "home" }, "push");
      setNewDatabaseName("");
      notify(t("status.createdDatabase", { name: saved.name }), "success");
    } catch (error) {
      notify(error instanceof Error ? error.message : t("status.databaseCreationFailed"), "error");
    }
  }

  async function createTableFromSidebar() {
    if (!database.name) {
      notify(t("status.selectDatabaseBeforeTable"));
      return;
    }
    const name = newTableName.trim();
    if (!name) {
      notify(t("status.tableNameRequired"));
      return;
    }
    try {
      const saved = await createTable(database.name, {
        name,
        display_name: name,
        fields: [],
        views: []
      });
      const nextCatalog = await loadMetadata();
      setCatalog(nextCatalog);
      setSelectedTable(saved.name);
      setSelectedTableView("all");
      tableWorkspace.resetRows("all");
      navigateWorkspace(buildRouteForSelection(database.name, "table", saved.name, "all"), "push");
      setNewTableName("");
      notify(t("status.createdTable", { database: database.name, table: saved.name }), "success");
    } catch (error) {
      notify(error instanceof Error ? error.message : t("status.tableCreationFailed"), "error");
    }
  }

  async function registerUser(email: string, password: string, displayName: string) {
    try {
      const user = await register(email, password, displayName);
      setCurrentUser(user);
      setAuthReady(true);
      notify(t("status.signedInAs", { name: user.display_name }), "success");
      return true;
    } catch (error) {
      notify(error instanceof Error ? error.message : t("status.registrationFailed"), "error");
      return false;
    }
  }

  async function loginUser(email: string, password: string) {
    try {
      const user = await login(email, password);
      setCurrentUser(user);
      setAuthReady(true);
      notify(t("status.signedInAs", { name: user.display_name }), "success");
      return true;
    } catch (error) {
      notify(error instanceof Error ? error.message : t("status.loginFailed"), "error");
      return false;
    }
  }

  async function logoutUser() {
    try {
      await logout();
      setCurrentUser(null);
      setAuthReady(true);
      applyCatalogSelection(emptyCatalog, "");
      notify(t("status.signedOut"));
    } catch (error) {
      notify(error instanceof Error ? error.message : t("status.logoutFailed"), "error");
    }
  }

  function selectAppFromNavigation(name: string) {
    navigateWorkspace({ databaseName: database.name, view: "app", appName: name }, "push");
  }

  async function deleteSelectedLibrary() {
    if (await libraryWorkspace.removeLibrary()) {
      navigateWorkspace({ databaseName: database.name, view: "library" }, "replace");
    }
  }

  async function deleteSelectedApp() {
    if (await appWorkspace.removeApp()) {
      navigateWorkspace({ databaseName: database.name, view: "app" }, "replace");
    }
  }

  function clearWorkspaceSelection() {
    setSelectedDatabaseName("");
    setSelectedTable("");
    setSelectedTableView("all");
    setView("home");
    tableWorkspace.resetRows("all");
  }

  function navigateWorkspace(route: WorkspaceRoute | null, mode: "push" | "replace" = "push") {
    const nextPath = buildWorkspacePath(route);
    if (window.location.pathname === nextPath) {
      setLocationPath(nextPath);
      return;
    }
    window.history[mode === "replace" ? "replaceState" : "pushState"](null, "", nextPath);
    setLocationPath(nextPath);
  }

  function buildRouteForSelection(
    databaseName: string,
    nextView: View,
    tableName = table.name,
    tableViewName = selectedTableView
  ): WorkspaceRoute | null {
    if (!databaseName) {
      return null;
    }
    if (nextView === "table") {
      return { databaseName, view: "table", tableName, tableViewName };
    }
    return { databaseName, view: nextView };
  }

  async function createTableView(tableName: string, viewName: string) {
    const name = viewName.trim();
    if (!name) {
      notify(t("status.viewNameRequired"));
      return;
    }
    const targetTable = database.tables.find((item) => item.name === tableName);
    if (!database.name || !targetTable) {
      notify(t("status.selectTableBeforeView"));
      return;
    }
    if (name === "all" || (targetTable.views ?? []).some((viewDef) => viewDef.name === name)) {
      notify(t("status.viewAlreadyExists", { name }));
      return;
    }
    const nextTable = {
      ...targetTable,
      views: [...(targetTable.views ?? []), { name, display_name: name, sorts: [] }]
    };
    try {
      await updateTableMetadata(database.name, tableName, nextTable);
      const nextCatalog = await loadMetadata();
      setCatalog(nextCatalog);
      setSelectedTable(tableName);
      setSelectedTableView(name);
      tableWorkspace.resetRows(name);
      navigateWorkspace(buildRouteForSelection(database.name, "table", tableName, name), "push");
      notify(t("status.createdView", { name }), "success");
    } catch (error) {
      notify(error instanceof Error ? error.message : t("status.tableMetadataUpdateFailed"), "error");
      return;
    }
    setView("table");
    setOpenViewPanelRequest((current) => current + 1);
  }

  if (!authReady) {
    return (
      <div className="flex h-full items-center justify-center text-subtle">
        <Spinner />
      </div>
    );
  }

  if (!currentUser) {
    return (
      <AuthScreen
        onLogin={loginUser}
        onRegister={registerUser}
        onOIDCLogin={(providerName) => window.location.assign(oidcStartURL(providerName))}
        passwordEnabled={passwordAuthEnabled}
        providers={oidcProviders}
      />
    );
  }

  const sidebar: SidebarProps = {
    catalog,
    currentUser,
    database,
    isAdmin,
    view,
    tableName: table.name,
    workflows,
    selectedWorkflowName,
    roles,
    selectedRoleName: selectedRole?.name,
    newDatabaseName,
    onNewDatabaseNameChange: setNewDatabaseName,
    onCreateDatabase: createDatabaseFromSidebar,
    newTableName,
    onNewTableNameChange: setNewTableName,
    onCreateTable: createTableFromSidebar,
    newRoleName,
    onNewRoleNameChange: permissionWorkspace.setNewRoleName,
    onCreateRole: permissionWorkspace.createRoleFromSidebar,
    onOpenRunners: () => setRunnersOpen(true),
    onLogout: logoutUser,
    onRefresh: refreshMetadata
  };

  return (
    <NavigationProvider navigate={(route) => navigateWorkspace(route, "push")}>
      <AppShell sidebar={sidebar} title={pageTitle()}>
        {renderContent()}
      </AppShell>
      {isAdmin && (
        <RunnersPanel databaseName={database.name} open={runnersOpen && Boolean(database.name)} onOpenChange={setRunnersOpen} />
      )}
    </NavigationProvider>
  );

  function pageTitle(): string {
    if (!database.name) {
      return "autable";
    }
    switch (view) {
      case "table":
        return table.display_name || table.name || database.name;
      case "app":
        return selectedApp ? selectedApp.title || selectedApp.name : t("nav.apps");
      case "workflow":
        return isAdmin && selectedWorkflow ? selectedWorkflow.title || selectedWorkflow.name : t("nav.workflows");
      case "permission":
        return t("nav.permissions");
      case "library":
        return selectedLibrary ? selectedLibrary.title || selectedLibrary.name : t("library.title");
      case "admin":
        return t("admin.title");
      default:
        return database.name;
    }
  }

  function renderContent() {
    if (!metadataReady) {
      return null;
    }
    if (!database.name) {
      return (
        <NoDatabase
          isAdmin={isAdmin}
          name={newDatabaseName}
          onNameChange={setNewDatabaseName}
          onCreate={createDatabaseFromSidebar}
        />
      );
    }
    switch (view) {
      case "home":
        return <HomePage apps={apps} appsReady={appsReady} database={database} isAdmin={isAdmin} />;
      case "admin":
        return isAdmin ? (
          <AdminPage
            apps={apps}
            database={database}
            libraries={libraryWorkspace.libraries}
            onOpenRunners={() => setRunnersOpen(true)}
            roles={roles}
            workflows={workflows}
          />
        ) : null;
      case "library":
        return isAdmin ? (
          <LibraryWorkspace
            busy={libraryWorkspace.busy}
            currentUser={currentUser}
            databaseName={database.name}
            detail={libraryWorkspace.detail}
            libraries={libraryWorkspace.libraries}
            librariesReady={libraryWorkspace.librariesReady}
            libraryName={selectedLibraryName}
            onDelete={() => void deleteSelectedLibrary()}
            onPublish={(version) => void libraryWorkspace.publishVersion(version)}
          />
        ) : null;
      case "table":
        return table.name ? (
          <TableWorkspace
            columns={tableWorkspace.columns}
            databaseName={database.name}
            displayedRows={tableWorkspace.displayedRows}
            isAdmin={isAdmin}
            newFieldFormula={tableWorkspace.newFieldFormula}
            newFieldName={tableWorkspace.newFieldName}
            newFieldMultiple={tableWorkspace.newFieldMultiple}
            newFieldOptions={tableWorkspace.newFieldOptions}
            newFieldType={tableWorkspace.newFieldType}
            newFormulaValueType={tableWorkspace.newFormulaValueType}
            newRelationTable={tableWorkspace.newRelationTable}
            newViewBase={tableWorkspace.newViewBase}
            newViewQuery={tableWorkspace.newViewQuery}
            newViewSortDirection={tableWorkspace.newViewSortDirection}
            newViewSortField={tableWorkspace.newViewSortField}
            onAddRow={tableWorkspace.addDraftRow}
            onAddField={tableWorkspace.addFieldFromCanvas}
            onCreateView={(viewName) => void createTableView(table.name, viewName)}
            onDeleteField={tableWorkspace.deleteFieldFromCanvas}
            onDeleteSelectedRow={tableWorkspace.deleteSelectedRow}
            onLoadHistory={tableWorkspace.loadSelectedRowHistory}
            onNewFieldFormulaChange={tableWorkspace.setNewFieldFormula}
            onNewFieldNameChange={tableWorkspace.setNewFieldName}
            onNewFieldOptionsChange={tableWorkspace.setNewFieldOptions}
            onNewFieldMultipleChange={tableWorkspace.setNewFieldMultiple}
            onNewFieldTypeChange={tableWorkspace.setNewFieldType}
            onNewFormulaValueTypeChange={tableWorkspace.setNewFormulaValueType}
            onNewRelationTableChange={tableWorkspace.setNewRelationTable}
            onNewViewBaseChange={tableWorkspace.setNewViewBase}
            onNewViewQueryChange={tableWorkspace.setNewViewQuery}
            onNewViewSortDirectionChange={tableWorkspace.setNewViewSortDirection}
            onNewViewSortFieldChange={tableWorkspace.setNewViewSortField}
            onTemporarySortChange={tableWorkspace.setTemporarySort}
            onMoveFieldPosition={tableWorkspace.moveFieldPosition}
            onLoadMoreRows={tableWorkspace.loadMoreRows}
            onSearchTextChange={tableWorkspace.setSearchText}
            onRowsChange={tableWorkspace.editGridRows}
            onSelectGridCell={tableWorkspace.selectGridCell}
            onSelectRecordID={tableWorkspace.setSelectedRecordID}
            onSelectedRowValueChange={tableWorkspace.updateSelectedRowDraft}
            onUpdateFieldFormula={tableWorkspace.updateFieldFormulaFromCanvas}
            onUpdateFieldOptions={tableWorkspace.updateFieldOptionsFromCanvas}
            onImported={tableWorkspace.reloadAfterImport}
            onOpenRelationCell={tableWorkspace.openRelationDetail}
            onUpdateSelectedRow={tableWorkspace.updateSelectedRowFromEditor}
            onUpdateSelectedView={tableWorkspace.updateSelectedViewFromCanvas}
            rowHistory={tableWorkspace.rowHistory}
            relationDetail={tableWorkspace.relationDetail}
            onCloseRelationDetail={() => tableWorkspace.setRelationDetail(null)}
            rows={tableWorkspace.rows}
            searchText={tableWorkspace.searchText}
            totalRows={tableWorkspace.total}
            selectedRecordID={tableWorkspace.selectedRecordID}
            selectedRowDraft={tableWorkspace.selectedRowDraft}
            selectedTableView={selectedTableView}
            table={table}
            tables={database.tables}
            temporarySort={tableWorkspace.temporarySort}
            openViewPanelRequest={openViewPanelRequest}
          />
        ) : (
          <Page title={database.name}>
            <Card>
              <EmptyState
                icon={Table2}
                title={t("empty.noTableTitle")}
                description={t(isAdmin ? "empty.noTableDescription" : "empty.noGrantedTableDescription")}
              />
            </Card>
          </Page>
        );
      case "workflow":
        if (!isAdmin) {
          return (
            <WorkflowRunsPage currentUser={currentUser} databaseName={database.name} workflows={workflows} workflowsReady={workflowsReady} />
          );
        }
        return (
          <WorkflowWorkspace
            busy={workflowWorkspace.busy}
            currentUser={currentUser}
            databaseName={database.name}
            detail={workflowWorkspace.detail}
            workflows={workflows}
            workflowsReady={workflowsReady}
            workflowName={selectedWorkflowName}
            tab={workflowRoute?.workflowTab ?? "overview"}
            runID={workflowRoute?.workflowRunID}
            onDelete={workflowWorkspace.removeWorkflow}
            onPublish={workflowWorkspace.publishVersion}
            onRemoveSecret={workflowWorkspace.removeSecret}
            onRemoveVar={workflowWorkspace.removeVar}
            onRunStarted={() => void workflowWorkspace.noteRunStarted()}
            onSaveSecret={workflowWorkspace.saveSecret}
            onSaveSettings={workflowWorkspace.saveSettings}
            onSaveVar={workflowWorkspace.saveVar}
            onSetEnabled={workflowWorkspace.setEnabled}
          />
        );
      case "app":
        return (
          <AppWorkspace
            apps={apps}
            appsReady={appsReady}
            busy={appWorkspace.busy}
            currentUser={currentUser}
            databaseName={database.name}
            detail={appWorkspace.detail}
            isAdmin={isAdmin}
            onDelete={deleteSelectedApp}
            onPublish={appWorkspace.publishVersion}
            onSelectApp={selectAppFromNavigation}
            selectedAppName={selectedAppName}
          />
        );
      case "permission":
        return isAdmin ? (
            <PermissionPanel
              apps={apps}
              database={database}
              grants={roleDraftGrants}
              members={roleDraftMembers}
              memberOptions={memberSearchResults}
              memberUsers={roleDraftMemberUsers}
              memberWorkflows={permissionWorkspace.roleDraftMemberWorkflows}
              newMemberQuery={newRoleMemberQuery}
              onAddMember={permissionWorkspace.addRoleMember}
              onAddWorkflowMember={permissionWorkspace.addWorkflowMember}
              onGrantChange={permissionWorkspace.updateRoleGrant}
              onMemberRemove={permissionWorkspace.removeRoleMember}
              onNewMemberQueryChange={permissionWorkspace.setNewRoleMemberQuery}
              onSave={permissionWorkspace.persistRoleGrants}
              role={selectedRole}
              workflows={workflows}
            />
        ) : null;
    }
  }
}

function NoDatabase({
  isAdmin,
  name,
  onNameChange,
  onCreate
}: {
  isAdmin: boolean;
  name: string;
  onNameChange: (value: string) => void;
  onCreate: () => void;
}) {
  const { t } = useTranslation();
  const [open, setOpen] = useState(false);
  return (
    <Page title={t("common.noDatabase")}>
      <Card>
        <EmptyState
          icon={Database}
          title={t("empty.noDatabaseTitle")}
          description={isAdmin ? t("empty.noDatabaseDescription") : t("empty.noGrantedDatabaseDescription")}
          action={
            isAdmin && (
              <Button variant="cta" onClick={() => setOpen(true)}>
                {t("nav.createDb")}
              </Button>
            )
          }
        />
      </Card>
      {isAdmin && (
        <CreateNameDialog
          open={open}
          onOpenChange={setOpen}
          title={t("nav.newDatabaseTitle")}
          description={t("nav.newDatabaseDescription")}
          inputLabel={t("nav.newDatabaseName")}
          placeholder={t("nav.placeholderDatabase")}
          name={name}
          onNameChange={onNameChange}
          onSave={onCreate}
        />
      )}
    </Page>
  );
}

function allowedWorkspaceView(isAdmin: boolean, nextView: View): View {
  if ((nextView === "permission" || nextView === "admin" || nextView === "library") && !isAdmin) {
    return "home";
  }
  return nextView;
}

function tableHasView(table: TableMetadata | undefined, viewName: string | undefined): boolean {
  if (!viewName || viewName === "all") {
    return true;
  }
  return Boolean(table?.views.some((item) => item.name === viewName));
}
