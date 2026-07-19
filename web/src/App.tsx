import { useEffect, useMemo, useRef, useState } from "react";
import { Text, Toolbar, ToolbarButton, Tooltip } from "@fluentui/react-components";
import {
  ArrowClockwiseRegular,
  DatabaseRegular,
  DocumentFlowchartRegular,
  DocumentTableRegular,
  FormRegular
} from "@fluentui/react-icons";
import { useTranslation } from "react-i18next";
import { AuthDialog } from "./components/AuthDialog";
import { FormWorkspace } from "./components/FormWorkspace";
import { PermissionPanel } from "./components/PermissionPanel";
import { PublishedFormPage } from "./components/PublishedFormPage";
import { LoginPage } from "./components/LoginPage";
import { TableWorkspace } from "./components/TableWorkspace";
import { WorkflowWorkspace, type WorkflowTab } from "./components/WorkflowWorkspace";
import { WorkspaceEmptyState } from "./components/WorkspaceEmptyState";
import { WorkspaceNavigation, type WorkspaceView } from "./components/WorkspaceNavigation";
import { useNotifier } from "./notifications";
import { usePermissionWorkspace } from "./hooks/usePermissionWorkspace";
import { useTableWorkspace } from "./hooks/useTableWorkspace";
import { useWorkflowFormWorkspace } from "./hooks/useWorkflowFormWorkspace";
import { buildWorkspacePath, parseWorkspaceRoute, type WorkspaceRoute } from "./appState";
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
  type TableMetadata,
} from "./api";
import { appLanguages, normalizeLanguage, type AppLanguage } from "./i18n";

type View = WorkspaceView;

const emptyDatabase: DatabaseMetadata = { name: "", tables: [] };
const emptyTable: TableMetadata = { name: "", display_name: "", fields: [], views: [] };
const emptyCatalog: Catalog = { databases: [] };

export function App() {
  if (window.location.pathname === "/login") {
    return <LoginPage />;
  }
  const publishedFormToken = publishedFormTokenFromPath();
  return publishedFormToken ? <PublishedFormPage token={publishedFormToken} /> : <WorkspaceApp />;
}

function WorkspaceApp() {
  const { i18n, t } = useTranslation();
  const [locationPath, setLocationPath] = useState(() => window.location.pathname);
  const [catalog, setCatalog] = useState<Catalog>(emptyCatalog);
  const [view, setView] = useState<View>("table");
  const [selectedDatabaseName, setSelectedDatabaseName] = useState("");
  const [selectedTable, setSelectedTable] = useState("");
  const [selectedTableView, setSelectedTableView] = useState("all");
  const [openViewPanelRequest, setOpenViewPanelRequest] = useState(0);
  const [authEmail, setAuthEmail] = useState("");
  const [authDisplayName, setAuthDisplayName] = useState("");
  const [authPassword, setAuthPassword] = useState("");
  const [currentUser, setCurrentUser] = useState<AuthUser | null>(null);
  const [authReady, setAuthReady] = useState(false);
  const [metadataReady, setMetadataReady] = useState(false);
  const [passwordAuthEnabled, setPasswordAuthEnabled] = useState(true);
  const [aiEnabled, setAIEnabled] = useState(false);
  const [oidcProviders, setOIDCProviders] = useState<OIDCProvider[]>([]);
  const [authDialogOpen, setAuthDialogOpen] = useState(false);
  const [newDatabaseName, setNewDatabaseName] = useState("");
  const [newTableName, setNewTableName] = useState("");
  const [primaryNavCollapsed, setPrimaryNavCollapsed] = useState(false);
  const { Toaster, notify } = useNotifier();
  const language = normalizeLanguage(i18n.resolvedLanguage ?? i18n.language);
  const workspaceRoute = useMemo(() => parseWorkspaceRoute(locationPath), [locationPath]);
  const workspaceRouteRef = useRef<WorkspaceRoute | null>(workspaceRoute);
  workspaceRouteRef.current = workspaceRoute;

  const database = catalog.databases.find((item) => item.name === selectedDatabaseName) ?? emptyDatabase;
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
    onStatus: notify
  });
  const workflowFormWorkspace = useWorkflowFormWorkspace({
    currentUserID: currentUser?.id,
    databaseName: database.name,
    tableName: table.name,
    onStatus: notify,
    onSubmittedRow: tableWorkspace.addSubmittedRow
  });
  const {
    forms,
    formValues,
    newFormName,
    newWorkflowName,
    renderedForm,
    resourcesReady,
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
    const nextView = allowedWorkspaceView(nextDatabase, workspaceRoute.view);
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
  }, [catalog.databases, locationPath, metadataReady, workspaceRoute]);

  useEffect(() => {
    if (!workspaceRoute || workspaceRoute.databaseName !== database.name) {
      return;
    }
    if (workspaceRoute.view === "workflow" && workspaceRoute.workflowID && resourcesReady && !workflows.some((item) => item.id === workspaceRoute.workflowID)) {
      navigateWorkspace(null, "replace");
      return;
    }
    if (workspaceRoute.view === "form" && workspaceRoute.formID && resourcesReady && !forms.some((item) => item.id === workspaceRoute.formID)) {
      navigateWorkspace(null, "replace");
      return;
    }
    if (workspaceRoute.view === "permission" && workspaceRoute.roleName && rolesReady && !roles.some((item) => item.name === workspaceRoute.roleName)) {
      navigateWorkspace(null, "replace");
      return;
    }
    if (workspaceRoute.view === "workflow" && workspaceRoute.workflowID && workflows.some((item) => item.id === workspaceRoute.workflowID)) {
      workflowFormWorkspace.setSelectedWorkflowID(workspaceRoute.workflowID);
    }
    if (
      workspaceRoute.view === "workflow" &&
      workspaceRoute.workflowTab === "history" &&
      workspaceRoute.workflowRunKey &&
      workflowRuns.some((run) => run.history_key === workspaceRoute.workflowRunKey)
    ) {
      workflowFormWorkspace.setSelectedWorkflowRunKey(workspaceRoute.workflowRunKey);
    }
    if (workspaceRoute.view === "form" && workspaceRoute.formID && forms.some((item) => item.id === workspaceRoute.formID)) {
      workflowFormWorkspace.setSelectedFormID(workspaceRoute.formID);
    }
    if (workspaceRoute.view === "permission" && workspaceRoute.roleName && roles.some((item) => item.name === workspaceRoute.roleName)) {
      permissionWorkspace.setSelectedRoleName(workspaceRoute.roleName);
    }
  }, [database.name, forms, locationPath, resourcesReady, roles, rolesReady, workflowRuns, workflows, workspaceRoute]);

  useEffect(() => {
    if (selectedWorkflowTab !== "history" || !selectedWorkflow?.id) {
      return;
    }
    // A deep link names a workflow; until that selection is applied the
    // current selection is still the default first workflow, and fetching
    // its runs would race the real target and show the wrong history.
    if (
      workspaceRoute?.view === "workflow" &&
      workspaceRoute.workflowID &&
      workspaceRoute.workflowID !== selectedWorkflow.id
    ) {
      return;
    }
    void workflowFormWorkspace.refreshWorkflowRuns(workspaceRoute?.workflowRunKey ?? "", selectedWorkflow.id);
  }, [selectedWorkflow?.id, selectedWorkflowTab]);

  useEffect(() => {
    let cancelled = false;
    void loadCurrentUser()
      .then((user) => {
        if (cancelled) {
          return;
        }
        setCurrentUser(user);
        if (user) {
          notify(t("status.signedInAs", { name: user.display_name }));
        }
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
          setAIEnabled(Boolean(authConfig.ai_enabled));
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
      workflowFormWorkspace.clearResources();
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
          workflowFormWorkspace.refreshResources(dbName)
        ]);
      }
      notify(t("status.metadataRefreshed"));
    } catch (error) {
      notify(error instanceof Error ? error.message : t("status.metadataRefreshFailed"), "error");
    }
  }

  function applyCatalogSelection(nextCatalog: Catalog, preferredDatabaseName = selectedDatabaseName) {
    setCatalog(nextCatalog);
    const route = workspaceRouteRef.current;
    const routeDatabaseName = route?.databaseName ?? "";
    const dbName = routeDatabaseName && nextCatalog.databases.some((item) => item.name === routeDatabaseName)
      ? routeDatabaseName
      : preferredDatabaseName && nextCatalog.databases.some((item) => item.name === preferredDatabaseName)
      ? preferredDatabaseName
      : "";
    const nextDatabase = nextCatalog.databases.find((item) => item.name === dbName);
    const nextView = route?.databaseName === dbName ? allowedWorkspaceView(nextDatabase, route.view) : "table";
    const nextTable =
      nextDatabase?.tables.find((item) => item.name === route?.tableName) ?? nextDatabase?.tables[0] ?? emptyTable;
    setSelectedDatabaseName(dbName);
    setView(nextView);
    setSelectedTable(nextTable.name);
    const nextTableView =
      nextView === "table" && tableHasView(nextTable, route?.tableViewName) ? route?.tableViewName ?? "all" : "all";
    setSelectedTableView(nextTableView);
    tableWorkspace.resetRows(nextTableView);
    return dbName;
  }

  async function refreshCatalogAfterAuth() {
    const nextCatalog = await loadMetadata();
    applyCatalogSelection(nextCatalog);
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
      navigateWorkspace(buildRouteForSelection(saved.name, "table", saved.tables[0]?.name ?? "", "all"), "push");
      setNewDatabaseName("");
      notify(t("status.createdDatabase", { name: saved.name }));
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
      navigateWorkspace(buildRouteForSelection(database.name, "table", saved.name, "all"), "push");
      setNewTableName("");
      notify(t("status.createdTable", { database: database.name, table: saved.name }));
    } catch (error) {
      notify(error instanceof Error ? error.message : t("status.tableCreationFailed"), "error");
    }
  }

  async function registerUser() {
    try {
      const user = await register(authEmail, authPassword, authDisplayName);
      setCurrentUser(user);
      setAuthReady(true);
      await refreshCatalogAfterAuth();
      notify(t("status.signedInAs", { name: user.display_name }));
      return true;
    } catch (error) {
      notify(error instanceof Error ? error.message : t("status.registrationFailed"), "error");
      return false;
    }
  }

  async function loginUser() {
    try {
      const user = await login(authEmail, authPassword);
      setCurrentUser(user);
      setAuthReady(true);
      await refreshCatalogAfterAuth();
      notify(t("status.signedInAs", { name: user.display_name }));
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
    if (nextView === "permission" && (nextDatabase.permission_level ?? 0) < 2) {
      nextView = "table";
    }
    setSelectedDatabaseName(databaseName);
    setView(nextView);
    if (nextView === "table") {
      const nextTableName = nextDatabase.tables[0]?.name ?? "";
      setSelectedTable(nextTableName);
      setSelectedTableView("all");
    }
    navigateWorkspace(
      nextView === "table"
        ? buildRouteForSelection(databaseName, "table", nextDatabase.tables[0]?.name ?? "", "all")
        : { databaseName, view: nextView },
      "push"
    );
  }

  function selectTableViewFromNavigation(tableName: string, viewName: string) {
    setSelectedTable(tableName);
    setSelectedTableView(viewName);
    navigateWorkspace(buildRouteForSelection(database.name, "table", tableName, viewName), "push");
  }

  function selectWorkflowFromNavigation(id: number) {
    workflowFormWorkspace.setSelectedWorkflowID(id);
    navigateWorkspace({
      databaseName: database.name,
      view: "workflow",
      workflowID: id,
      workflowTab: selectedWorkflowTab
    }, "push");
  }

  async function executeWorkflowFromWorkspace() {
    const response = await workflowFormWorkspace.executeWorkflow();
    if (response?.history_key && selectedWorkflow?.id) {
      navigateWorkspace({
        databaseName: database.name,
        view: "workflow",
        workflowID: selectedWorkflow.id,
        workflowTab: "history",
        workflowRunKey: response.history_key
      }, "push");
    }
  }

  function selectWorkflowTabFromWorkspace(tab: WorkflowTab) {
    if (!selectedWorkflow?.id) {
      navigateWorkspace({ databaseName: database.name, view: "workflow" }, "push");
      return;
    }
    navigateWorkspace({
      databaseName: database.name,
      view: "workflow",
      workflowID: selectedWorkflow.id,
      workflowTab: tab,
      workflowRunKey: tab === "history" ? selectedWorkflowRun?.history_key : undefined
    }, "push");
  }

  function selectWorkflowRunFromWorkspace(historyKey: string) {
    workflowFormWorkspace.setSelectedWorkflowRunKey(historyKey);
    if (!selectedWorkflow?.id) {
      return;
    }
    navigateWorkspace({
      databaseName: database.name,
      view: "workflow",
      workflowID: selectedWorkflow.id,
      workflowTab: "history",
      workflowRunKey: historyKey
    }, "push");
  }

  function selectFormFromNavigation(id: number) {
    workflowFormWorkspace.setSelectedFormID(id);
    navigateWorkspace({ databaseName: database.name, view: "form", formID: id }, "push");
  }

  function selectRoleFromNavigation(name: string) {
    permissionWorkspace.setSelectedRoleName(name);
    navigateWorkspace({ databaseName: database.name, view: "permission", roleName: name }, "push");
  }

  function clearWorkspaceSelection() {
    setSelectedDatabaseName("");
    setSelectedTable("");
    setSelectedTableView("all");
    setView("table");
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
    if (nextView === "workflow") {
      return { databaseName, view: "workflow", workflowID: selectedWorkflow?.id };
    }
    if (nextView === "form") {
      return { databaseName, view: "form", formID: selectedForm?.id };
    }
    if (nextView === "permission") {
      return { databaseName, view: "permission", roleName: selectedRole?.name };
    }
    return { databaseName, view: "table", tableName, tableViewName };
  }

  async function createWorkflowFromSidebar() {
    const saved = await workflowFormWorkspace.createWorkflow();
    if (saved?.id) {
      setView("workflow");
      navigateWorkspace({ databaseName: database.name, view: "workflow", workflowID: saved.id }, "push");
    }
  }

  async function createFormFromSidebar() {
    const saved = await workflowFormWorkspace.createForm();
    if (saved?.id) {
      setView("form");
      navigateWorkspace({ databaseName: database.name, view: "form", formID: saved.id }, "push");
    }
  }

  async function createTableViewFromSidebar(tableName: string, viewName: string) {
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
      notify(t("status.createdView", { name }));
    } catch (error) {
      notify(error instanceof Error ? error.message : t("status.tableMetadataUpdateFailed"), "error");
      return;
    }
    setView("table");
    setOpenViewPanelRequest((current) => current + 1);
  }

  return (
    <div className={primaryNavCollapsed ? "app-shell primary-collapsed" : "app-shell"}>
      <WorkspaceNavigation
        catalog={catalog}
        collapsed={primaryNavCollapsed}
        onToggleCollapsed={() => setPrimaryNavCollapsed((value) => !value)}
        currentUser={currentUser}
        database={database}
        forms={forms}
        newDatabaseName={newDatabaseName}
        newFormName={newFormName}
        newRoleName={newRoleName}
        newTableName={newTableName}
        newWorkflowName={newWorkflowName}
        onCreateDatabase={createDatabaseFromSidebar}
        onCreateForm={createFormFromSidebar}
        onCreateRole={permissionWorkspace.createRoleFromSidebar}
        onCreateTable={createTableFromSidebar}
        onCreateWorkflow={createWorkflowFromSidebar}
        onLogout={logoutUser}
        onNewDatabaseNameChange={setNewDatabaseName}
        onNewFormNameChange={workflowFormWorkspace.setNewFormName}
        onNewRoleNameChange={permissionWorkspace.setNewRoleName}
        onNewTableNameChange={setNewTableName}
        onNewWorkflowNameChange={workflowFormWorkspace.setNewWorkflowName}
        onOpenLogin={() => setAuthDialogOpen(true)}
        onCreateTableView={createTableViewFromSidebar}
        onSelectDatabaseSection={selectDatabaseSection}
        onSelectFormID={selectFormFromNavigation}
        onSelectRoleName={selectRoleFromNavigation}
        onSelectTableView={selectTableViewFromNavigation}
        onSelectWorkflowID={selectWorkflowFromNavigation}
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
          {!database.name ? (
            <WorkspaceEmptyState
              icon={<DatabaseRegular />}
              title={t("empty.noDatabaseTitle")}
              description={t("empty.noDatabaseDescription")}
            />
          ) : (
            <>
          {view === "table" &&
            (table.name ? (
            <TableWorkspace
              columns={tableWorkspace.columns}
              databaseName={database.name}
              displayedRows={tableWorkspace.displayedRows}
              newFieldFormula={tableWorkspace.newFieldFormula}
              newFieldName={tableWorkspace.newFieldName}
              newFieldType={tableWorkspace.newFieldType}
              newFormulaValueType={tableWorkspace.newFormulaValueType}
              newRelationTable={tableWorkspace.newRelationTable}
              newViewBase={tableWorkspace.newViewBase}
              newViewQuery={tableWorkspace.newViewQuery}
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
              <WorkspaceEmptyState
                icon={<DocumentTableRegular />}
                title={t("empty.noTableTitle")}
                description={t("empty.noTableDescription")}
              />
            ))}

          {view === "workflow" &&
            (selectedWorkflow ? (
            <WorkflowWorkspace
              aiEnabled={aiEnabled}
              activeTab={selectedWorkflowTab}
              databaseName={database.name}
              onExecute={executeWorkflowFromWorkspace}
              onSave={workflowFormWorkspace.persistWorkflow}
              onSaveInstanceConfig={workflowFormWorkspace.saveSelectedWorkflowInstanceConfig}
              onSelectRunKey={selectWorkflowRunFromWorkspace}
              onSelectTab={selectWorkflowTabFromWorkspace}
              onUpdateScript={workflowFormWorkspace.updateSelectedWorkflowScript}
              onSetHistoryRetention={workflowFormWorkspace.setSelectedWorkflowHistoryRetention}
              onToggleEnabled={workflowFormWorkspace.toggleSelectedWorkflowEnabled}
              language={language}
              workflowTrigger={workflowTrigger}
              workflowInstances={workflowInstances}
              selectedRun={selectedWorkflowRun}
              workflow={selectedWorkflow}
              workflowNodes={workflowNodes}
              workflowRuns={workflowRuns}
            />
            ) : (
              <WorkspaceEmptyState
                icon={<DocumentFlowchartRegular />}
                title={t("empty.noWorkflowTitle")}
                description={t("empty.noWorkflowDescription")}
              />
            ))}

          {view === "form" &&
            (selectedForm ? (
            <FormWorkspace
              aiEnabled={aiEnabled}
              databaseName={database.name}
              form={selectedForm}
              formResult={workflowFormWorkspace.formResult}
              formValues={formValues}
              onAction={workflowFormWorkspace.executeFormAction}
              onFormValueChange={workflowFormWorkspace.updateFormValue}
              onPublish={workflowFormWorkspace.publishSelectedForm}
              onSave={workflowFormWorkspace.persistForm}
              onSubmit={workflowFormWorkspace.submitRenderedForm}
              onUnpublish={workflowFormWorkspace.unpublishSelectedForm}
              onUpdateScript={workflowFormWorkspace.updateSelectedFormScript}
              renderedForm={renderedForm}
              tables={database.tables}
            />
            ) : (
              <WorkspaceEmptyState
                icon={<FormRegular />}
                title={t("empty.noFormTitle")}
                description={t("empty.noFormDescription")}
              />
            ))}

          {view === "permission" && (
            <PermissionPanel
              database={database}
              forms={forms}
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
          )}
            </>
          )}
        </section>
      </main>

      <Toaster />

      <AuthDialog
        displayName={authDisplayName}
        email={authEmail}
        onDisplayNameChange={setAuthDisplayName}
        onEmailChange={setAuthEmail}
        onLogin={loginUser}
        onOIDCLogin={loginWithOIDC}
        onOpenChange={setAuthDialogOpen}
        onPasswordChange={setAuthPassword}
        onRegister={registerUser}
        open={authDialogOpen}
        password={authPassword}
        passwordEnabled={passwordAuthEnabled}
        providers={oidcProviders}
      />
    </div>
  );
}

function allowedWorkspaceView(database: DatabaseMetadata | undefined, nextView: View): View {
  if (nextView === "permission" && (database?.permission_level ?? 0) < 2) {
    return "table";
  }
  return nextView;
}

function tableHasView(table: TableMetadata | undefined, viewName: string | undefined): boolean {
  if (!viewName || viewName === "all") {
    return true;
  }
  return Boolean(table?.views.some((item) => item.name === viewName));
}

function publishedFormTokenFromPath(): string {
  const match = /^\/forms\/([^/]+)$/.exec(window.location.pathname);
  return match ? decodeURIComponent(match[1]) : "";
}
