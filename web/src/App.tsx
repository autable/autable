import { useEffect, useMemo, useRef, useState } from "react";
import { Database, Table2, Workflow } from "lucide-react";
import { useTranslation } from "react-i18next";
import { AppWorkspace } from "./components/AppWorkspace";
import { AuthScreen } from "./components/AuthScreen";
import { CreateNameDialog } from "./components/CreateNamePopover";
import { LoginPage } from "./components/LoginPage";
import { PermissionPanel } from "./components/PermissionPanel";
import { RunnersPanel } from "./components/RunnersPanel";
import { TableWorkspace } from "./components/TableWorkspace";
import { WorkflowWorkspace, type WorkflowTab } from "./components/WorkflowWorkspace";
import { useNotifier } from "./notifications";
import { useAppWorkspace } from "./hooks/useAppWorkspace";
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
import { normalizeLanguage } from "./i18n";
import { AdminPage } from "./pages/AdminPage";
import { HomePage } from "./pages/HomePage";
import { WorkflowRunsPage } from "./pages/WorkflowRunsPage";
import { AppShell } from "./shell/AppShell";
import { FluentScope } from "./shell/FluentScope";
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
  const { i18n, t } = useTranslation();
  const language = normalizeLanguage(i18n.resolvedLanguage ?? i18n.language);
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
  const workflowWorkspace = useWorkflowWorkspace({
    currentUserID: currentUser?.id,
    databaseName: database.name,
    isAdmin,
    tableName: table.name,
    onStatus: notify
  });
  const {
    newWorkflowName,
    resourcesReady,
    selectedWorkflow,
    selectedWorkflowRun,
    workflowTrigger,
    workflowInstances,
    workflowNodes,
    workflowRuns,
    workflows
  } = workflowWorkspace;
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
  const selectedWorkflowTab: WorkflowTab =
    workspaceRoute?.databaseName === database.name && workspaceRoute.view === "workflow"
      ? workspaceRoute.workflowTab ?? "editor"
      : "editor";

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
      workspaceRoute.workflowID &&
      resourcesReady &&
      !workflows.some((item) => item.id === workspaceRoute.workflowID)
    ) {
      navigateWorkspace(null, "replace");
      return;
    }
    if (workspaceRoute.view === "app" && workspaceRoute.appName && appsReady && !apps.some((item) => item.name === workspaceRoute.appName)) {
      navigateWorkspace({ databaseName: database.name, view: "app" }, "replace");
      return;
    }
    if (workspaceRoute.view === "permission" && workspaceRoute.roleName && rolesReady && !roles.some((item) => item.name === workspaceRoute.roleName)) {
      navigateWorkspace(null, "replace");
      return;
    }
    if (workspaceRoute.view === "workflow" && workspaceRoute.workflowID && workflows.some((item) => item.id === workspaceRoute.workflowID)) {
      workflowWorkspace.setSelectedWorkflowID(workspaceRoute.workflowID);
    }
    if (
      workspaceRoute.view === "workflow" &&
      workspaceRoute.workflowTab === "history" &&
      workspaceRoute.workflowRunKey &&
      workflowRuns.some((run) => run.history_key === workspaceRoute.workflowRunKey)
    ) {
      workflowWorkspace.setSelectedWorkflowRunKey(workspaceRoute.workflowRunKey);
    }
    if (workspaceRoute.view === "permission" && workspaceRoute.roleName && roles.some((item) => item.name === workspaceRoute.roleName)) {
      permissionWorkspace.setSelectedRoleName(workspaceRoute.roleName);
    }
  }, [apps, appsReady, database.name, locationPath, resourcesReady, roles, rolesReady, workflowRuns, workflows, workspaceRoute]);

  useEffect(() => {
    if (selectedWorkflowTab !== "history" || !selectedWorkflow?.id) {
      return;
    }
    // A deep link names a workflow; until that selection is applied the
    // current selection is still the default first workflow, and fetching
    // its runs would race the real target and show the wrong history.
    if (workspaceRoute?.view === "workflow" && workspaceRoute.workflowID && workspaceRoute.workflowID !== selectedWorkflow.id) {
      return;
    }
    void workflowWorkspace.refreshWorkflowRuns(workspaceRoute?.workflowRunKey ?? "", selectedWorkflow.id);
  }, [selectedWorkflow?.id, selectedWorkflowTab]);

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
      workflowWorkspace.clearResources();
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
          workflowWorkspace.refreshResources(dbName),
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

  async function executeWorkflowFromWorkspace() {
    const response = await workflowWorkspace.executeWorkflow();
    if (response?.history_key && selectedWorkflow?.id) {
      navigateWorkspace(
        {
          databaseName: database.name,
          view: "workflow",
          workflowID: selectedWorkflow.id,
          workflowTab: "history",
          workflowRunKey: response.history_key
        },
        "push"
      );
    }
  }

  function selectWorkflowTabFromWorkspace(tab: WorkflowTab) {
    if (!selectedWorkflow?.id) {
      navigateWorkspace({ databaseName: database.name, view: "workflow" }, "push");
      return;
    }
    navigateWorkspace(
      {
        databaseName: database.name,
        view: "workflow",
        workflowID: selectedWorkflow.id,
        workflowTab: tab,
        workflowRunKey: tab === "history" ? selectedWorkflowRun?.history_key : undefined
      },
      "push"
    );
  }

  function selectWorkflowRunFromWorkspace(historyKey: string) {
    workflowWorkspace.setSelectedWorkflowRunKey(historyKey);
    if (!selectedWorkflow?.id) {
      return;
    }
    navigateWorkspace(
      {
        databaseName: database.name,
        view: "workflow",
        workflowID: selectedWorkflow.id,
        workflowTab: "history",
        workflowRunKey: historyKey
      },
      "push"
    );
  }

  function selectAppFromNavigation(name: string) {
    navigateWorkspace({ databaseName: database.name, view: "app", appName: name }, "push");
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

  async function createWorkflowFromSidebar() {
    const saved = await workflowWorkspace.createWorkflow();
    if (saved?.id) {
      setView("workflow");
      navigateWorkspace({ databaseName: database.name, view: "workflow", workflowID: saved.id }, "push");
    }
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
    selectedWorkflowID: selectedWorkflow?.id,
    roles,
    selectedRoleName: selectedRole?.name,
    newDatabaseName,
    onNewDatabaseNameChange: setNewDatabaseName,
    onCreateDatabase: createDatabaseFromSidebar,
    newTableName,
    onNewTableNameChange: setNewTableName,
    onCreateTable: createTableFromSidebar,
    newWorkflowName,
    onNewWorkflowNameChange: workflowWorkspace.setNewWorkflowName,
    onCreateWorkflow: createWorkflowFromSidebar,
    onDeleteWorkflow: workflowWorkspace.deleteSelectedWorkflow,
    onRenameWorkflow: workflowWorkspace.renameSelectedWorkflow,
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
        <FluentScope className="contents">
          <RunnersPanel databaseName={database.name} open={runnersOpen && Boolean(database.name)} onOpenChange={setRunnersOpen} />
        </FluentScope>
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
        return isAdmin && selectedWorkflow ? selectedWorkflow.name : t("nav.workflows");
      case "permission":
        return t("nav.permissions");
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
          <AdminPage apps={apps} database={database} onOpenRunners={() => setRunnersOpen(true)} roles={roles} workflows={workflows} />
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
          return <WorkflowRunsPage databaseName={database.name} onRun={workflowWorkspace.runGrantedWorkflow} workflows={workflows} />;
        }
        return selectedWorkflow ? (
          <FluentScope className="h-full min-h-0 overflow-hidden p-4 md:px-8 md:py-6">
            <WorkflowWorkspace
              activeTab={selectedWorkflowTab}
              databaseName={database.name}
              onExecute={executeWorkflowFromWorkspace}
              onSave={workflowWorkspace.persistWorkflow}
              onSaveInstanceConfig={workflowWorkspace.saveSelectedWorkflowInstanceConfig}
              onSelectRunKey={selectWorkflowRunFromWorkspace}
              onSelectTab={selectWorkflowTabFromWorkspace}
              onUpdateScript={workflowWorkspace.updateSelectedWorkflowScript}
              onSetHistoryRetention={workflowWorkspace.setSelectedWorkflowHistoryRetention}
              onSetTimeoutSeconds={workflowWorkspace.setSelectedWorkflowTimeoutSeconds}
              onToggleEnabled={workflowWorkspace.toggleSelectedWorkflowEnabled}
              language={language}
              workflowTrigger={workflowTrigger}
              workflowInstances={workflowInstances}
              selectedRun={selectedWorkflowRun}
              workflow={selectedWorkflow}
              workflowNodes={workflowNodes}
              workflowRuns={workflowRuns}
            />
          </FluentScope>
        ) : (
          <Page title={t("nav.workflows")}>
            <Card>
              <EmptyState icon={Workflow} title={t("empty.noWorkflowTitle")} description={t("empty.noWorkflowDescription")} />
            </Card>
          </Page>
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
          <FluentScope className="h-full min-h-0 overflow-auto p-4 md:px-8 md:py-6">
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
          </FluentScope>
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
  if ((nextView === "permission" || nextView === "admin") && !isAdmin) {
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
