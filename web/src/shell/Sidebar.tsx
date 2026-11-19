import {
  AppWindow,
  ChevronsUpDown,
  Database,
  House,
  Languages,
  LayoutDashboard,
  LogOut,
  MoreHorizontal,
  Pencil,
  Plus,
  RefreshCw,
  Server,
  ShieldCheck,
  Table2,
  Trash2,
  Users,
  Workflow,
  type LucideIcon
} from "lucide-react";
import { useState, type ReactNode } from "react";
import { useTranslation } from "react-i18next";
import type { AuthUser, Catalog, DatabaseMetadata, RoleDefinition, TableMetadata, WorkflowDefinition } from "../api";
import type { WorkspaceRoute, WorkspaceViewName } from "../appState";
import { CreateNameDialog, CreateNamePopover } from "../components/CreateNamePopover";
import { appLanguages, normalizeLanguage, type AppLanguage } from "../i18n";
import {
  Badge,
  Button,
  cn,
  Dialog,
  DialogClose,
  DialogContent,
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuRadioGroup,
  DropdownMenuRadioItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
  Field,
  Input,
  ThemeToggle,
  Tooltip
} from "../ui";
import { Wordmark } from "./Logo";
import { RouteLink, useNavigate } from "./navigation";

/** Sidebar rows: 8px inner radius, the selected row a solid chip. */
export const navRowClass = "flex h-8 min-w-0 items-center gap-2.5 rounded-inner px-2.5 text-13 transition-colors duration-150";
export const navRowIdleClass = "text-description hover:bg-hover hover:text-fg";
export const navRowActiveClass = "bg-chip font-medium text-fg";
const subRowClass = "h-7 gap-2 text-12";

export type SidebarProps = {
  catalog: Catalog;
  currentUser: AuthUser;
  database: DatabaseMetadata;
  // Admins get the Manage group (structure, code, access); everyone else
  // only sees the data, apps and workflows granted to them.
  isAdmin: boolean;
  view: WorkspaceViewName;
  tableName: string;
  workflows: WorkflowDefinition[];
  selectedWorkflowID?: number;
  roles: RoleDefinition[];
  selectedRoleName?: string;
  newDatabaseName: string;
  onNewDatabaseNameChange: (value: string) => void;
  onCreateDatabase: () => void;
  newTableName: string;
  onNewTableNameChange: (value: string) => void;
  onCreateTable: () => void;
  newWorkflowName: string;
  onNewWorkflowNameChange: (value: string) => void;
  onCreateWorkflow: () => void;
  onDeleteWorkflow: (id: number) => void;
  onRenameWorkflow: (name: string, id: number) => void;
  newRoleName: string;
  onNewRoleNameChange: (value: string) => void;
  onCreateRole: () => void;
  onOpenRunners: () => void;
  onLogout: () => void;
  onRefresh: () => void;
  /** Called after any navigation, so the mobile drawer can close. */
  onNavigated?: () => void;
};

/** Where a table entry leads: its first view the user can read ("all" when granted). */
export function tableRoute(databaseName: string, table: TableMetadata): WorkspaceRoute {
  return { databaseName, view: "table", tableName: table.name, tableViewName: table.views?.[0]?.name ?? "all" };
}

export function Sidebar(props: SidebarProps) {
  const { t } = useTranslation();
  const { database, isAdmin, view, onNavigated } = props;
  const db = database.name;
  return (
    <div className="flex h-full min-h-0 flex-col">
      <div className="flex h-14 shrink-0 items-center pl-4 pr-2.5">
        <RouteLink
          route={db ? { databaseName: db, view: "home" } : null}
          onClick={onNavigated}
          aria-label={t("nav.goHome")}
          className="rounded-inner"
        >
          <Wordmark />
        </RouteLink>
      </div>
      <div className="shrink-0 px-2.5 pb-3">
        <DatabaseSwitcher {...props} />
      </div>
      <nav aria-label={t("nav.primary")} className="scroll-quiet flex min-h-0 flex-1 flex-col overflow-y-auto px-2.5 pb-4">
        {db && (
          <div className="flex flex-col gap-px">
            <NavRow route={{ databaseName: db, view: "home" }} icon={House} active={view === "home"} onNavigated={onNavigated}>
              {t("nav.home")}
            </NavRow>
            <NavRow route={{ databaseName: db, view: "app" }} icon={AppWindow} active={view === "app"} onNavigated={onNavigated}>
              {t("nav.apps")}
            </NavRow>
            {!isAdmin && (
              <NavRow route={{ databaseName: db, view: "workflow" }} icon={Workflow} active={view === "workflow"} onNavigated={onNavigated}>
                {t("nav.workflows")}
              </NavRow>
            )}
          </div>
        )}
        {db && <TablesGroup {...props} />}
        {db && isAdmin && <ManageGroup {...props} />}
      </nav>
      <div className="flex shrink-0 items-center gap-0.5 border-t border-border-soft p-2.5">
        <UserMenu {...props} />
        <LanguageButton />
        <ThemeToggle />
      </div>
    </div>
  );
}

function NavRow({
  route,
  icon: Icon,
  active,
  onNavigated,
  className,
  children
}: {
  route: WorkspaceRoute;
  icon?: LucideIcon;
  active: boolean;
  onNavigated?: () => void;
  className?: string;
  children: ReactNode;
}) {
  return (
    <RouteLink
      route={route}
      onClick={onNavigated}
      aria-current={active ? "page" : undefined}
      className={cn(navRowClass, active ? navRowActiveClass : navRowIdleClass, className)}
    >
      {Icon && <Icon className="size-4 shrink-0" strokeWidth={1.75} aria-hidden />}
      <span className="min-w-0 truncate">{children}</span>
    </RouteLink>
  );
}

function GroupHeading({ title, icon: Icon, action }: { title: string; icon?: LucideIcon; action?: ReactNode }) {
  return (
    <div className="mb-0.5 mt-5 flex h-7 items-center justify-between gap-2 pl-2.5 pr-0.5 text-12 font-medium text-subtle">
      <span className="flex items-center gap-1.5">
        {Icon && <Icon className="size-3.5" strokeWidth={2} aria-hidden />}
        {title}
      </span>
      {action}
    </div>
  );
}

function TablesGroup({ database, isAdmin, view, tableName, newTableName, onNewTableNameChange, onCreateTable, onNavigated }: SidebarProps) {
  const { t } = useTranslation();
  return (
    <>
      <GroupHeading
        title={t("nav.tables")}
        action={
          isAdmin && (
            <CreateNamePopover
              title={t("nav.newTableTitle")}
              inputLabel={t("nav.newTableName")}
              placeholder={t("nav.placeholderTable")}
              name={newTableName}
              onNameChange={onNewTableNameChange}
              onSave={onCreateTable}
              trigger={
                <Button variant="quiet" size="icon-sm" className="size-6" aria-label={t("nav.createTable")}>
                  <Plus />
                </Button>
              }
            />
          )
        }
      />
      <div className="flex flex-col gap-px" aria-label={t("nav.tableList")} role="group">
        {database.tables.length === 0 && <p className="px-2.5 py-1 text-12 text-subtle">{t("nav.noTables")}</p>}
        {database.tables.map((table) => (
          <NavRow
            key={table.name}
            route={tableRoute(database.name, table)}
            icon={Table2}
            active={view === "table" && tableName === table.name}
            onNavigated={onNavigated}
          >
            {table.display_name || table.name}
          </NavRow>
        ))}
      </div>
    </>
  );
}

function ManageGroup(props: SidebarProps) {
  const { t } = useTranslation();
  const { database, view, onNavigated, onOpenRunners } = props;
  const db = database.name;
  return (
    <>
      <GroupHeading title={t("nav.manage")} icon={ShieldCheck} />
      <div className="flex flex-col gap-px">
        <NavRow route={{ databaseName: db, view: "admin" }} icon={LayoutDashboard} active={view === "admin"} onNavigated={onNavigated}>
          {t("nav.overview")}
        </NavRow>
        <NavRow route={{ databaseName: db, view: "workflow" }} icon={Workflow} active={view === "workflow"} onNavigated={onNavigated}>
          {t("nav.workflows")}
        </NavRow>
        {view === "workflow" && <WorkflowItems {...props} />}
        <NavRow route={{ databaseName: db, view: "permission" }} icon={Users} active={view === "permission"} onNavigated={onNavigated}>
          {t("nav.permissions")}
        </NavRow>
        {view === "permission" && <RoleItems {...props} />}
        <button
          type="button"
          className={cn(navRowClass, navRowIdleClass, "w-full text-left")}
          onClick={() => {
            onNavigated?.();
            onOpenRunners();
          }}
        >
          <Server className="size-4 shrink-0" strokeWidth={1.75} aria-hidden />
          {t("runners.title")}
        </button>
      </div>
    </>
  );
}

function SubList({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div role="group" aria-label={label} className="mb-1 ml-4 flex flex-col gap-px border-l border-border-soft pl-1.5 pt-0.5">
      {children}
    </div>
  );
}

function WorkflowItems({
  database,
  workflows,
  selectedWorkflowID,
  newWorkflowName,
  onNewWorkflowNameChange,
  onCreateWorkflow,
  onDeleteWorkflow,
  onRenameWorkflow,
  onNavigated
}: SidebarProps) {
  const { t } = useTranslation();
  const [rename, setRename] = useState<{ id: number; name: string } | null>(null);
  const [pendingDelete, setPendingDelete] = useState<WorkflowDefinition | null>(null);
  return (
    <SubList label={t("nav.workflowList")}>
      {workflows.map((workflow) => {
        const id = workflow.id ?? 0;
        const active = id === selectedWorkflowID;
        const enabled = workflow.enabled ?? true;
        return (
          <div key={id || workflow.name} className="group relative flex">
            <RouteLink
              route={{ databaseName: database.name, view: "workflow", workflowID: id }}
              onClick={onNavigated}
              aria-current={active ? "page" : undefined}
              className={cn(navRowClass, subRowClass, "flex-1 pr-8", active ? navRowActiveClass : navRowIdleClass)}
            >
              <span
                aria-hidden
                title={enabled ? t("common.enabled") : t("common.disabled")}
                className={cn("size-1.5 shrink-0 rounded-full", enabled ? "bg-success" : "bg-subtle")}
              />
              <span className={cn("min-w-0 truncate", !enabled && "text-muted")}>{workflow.name}</span>
            </RouteLink>
            <DropdownMenu>
              <DropdownMenuTrigger asChild>
                <Button
                  variant="quiet"
                  size="icon-sm"
                  aria-label={t("nav.workflowActions", { id })}
                  className={cn(
                    "absolute right-0.5 top-0 size-7 opacity-0 focus-visible:opacity-100 group-hover:opacity-100 data-[state=open]:opacity-100",
                    active && "opacity-100"
                  )}
                >
                  <MoreHorizontal />
                </Button>
              </DropdownMenuTrigger>
              <DropdownMenuContent align="start" onCloseAutoFocus={(event) => event.preventDefault()}>
                <DropdownMenuItem onSelect={() => setRename({ id, name: workflow.name })}>
                  <Pencil />
                  {t("common.rename")}
                </DropdownMenuItem>
                <DropdownMenuItem danger onSelect={() => setPendingDelete(workflow)}>
                  <Trash2 />
                  {t("common.delete")}
                </DropdownMenuItem>
              </DropdownMenuContent>
            </DropdownMenu>
          </div>
        );
      })}
      <CreateNamePopover
        title={t("nav.newWorkflowTitle")}
        inputLabel={t("nav.newWorkflowName")}
        placeholder={t("nav.placeholderWorkflow")}
        name={newWorkflowName}
        onNameChange={onNewWorkflowNameChange}
        onSave={onCreateWorkflow}
        trigger={
          <button type="button" className={cn(navRowClass, subRowClass, navRowIdleClass, "text-subtle")} aria-label={t("nav.createWorkflow")}>
            <Plus className="size-3.5 shrink-0" aria-hidden />
            {t("nav.newWorkflowTitle")}
          </button>
        }
      />
      <Dialog open={rename !== null} onOpenChange={(open) => !open && setRename(null)}>
        <DialogContent
          title={t("nav.renameWorkflow")}
          footer={
            <>
              <DialogClose asChild>
                <Button>{t("common.cancel")}</Button>
              </DialogClose>
              <Button
                variant="cta"
                onClick={() => {
                  if (rename) {
                    onRenameWorkflow(rename.name, rename.id);
                  }
                  setRename(null);
                }}
              >
                {t("common.save")}
              </Button>
            </>
          }
        >
          <Field label={t("nav.name")}>
            {(field) => (
              <Input
                {...field}
                aria-label={t("nav.renameResource")}
                value={rename?.name ?? ""}
                onChange={(event) => setRename((current) => (current ? { ...current, name: event.target.value } : current))}
              />
            )}
          </Field>
        </DialogContent>
      </Dialog>
      <Dialog open={pendingDelete !== null} onOpenChange={(open) => !open && setPendingDelete(null)}>
        <DialogContent
          title={t("nav.deleteWorkflowTitle")}
          description={t("nav.deleteWorkflowConfirm", { name: pendingDelete?.name ?? "" })}
          footer={
            <>
              <DialogClose asChild>
                <Button>{t("common.cancel")}</Button>
              </DialogClose>
              <Button
                variant="danger"
                onClick={() => {
                  if (pendingDelete?.id) {
                    onDeleteWorkflow(pendingDelete.id);
                  }
                  setPendingDelete(null);
                }}
              >
                {t("common.delete")}
              </Button>
            </>
          }
        />
      </Dialog>
    </SubList>
  );
}

function RoleItems({ database, roles, selectedRoleName, newRoleName, onNewRoleNameChange, onCreateRole, onNavigated }: SidebarProps) {
  const { t } = useTranslation();
  return (
    <SubList label={t("nav.roleList")}>
      {roles.map((role) => {
        const active = role.name === selectedRoleName;
        return (
          <RouteLink
            key={role.name}
            route={{ databaseName: database.name, view: "permission", roleName: role.name }}
            onClick={onNavigated}
            aria-current={active ? "page" : undefined}
            className={cn(navRowClass, subRowClass, active ? navRowActiveClass : navRowIdleClass)}
          >
            <span className="min-w-0 truncate">{role.name}</span>
          </RouteLink>
        );
      })}
      <CreateNamePopover
        title={t("nav.newRoleTitle")}
        inputLabel={t("nav.newRoleName")}
        placeholder={t("nav.placeholderRole")}
        name={newRoleName}
        onNameChange={onNewRoleNameChange}
        onSave={onCreateRole}
        trigger={
          <button type="button" className={cn(navRowClass, subRowClass, navRowIdleClass, "text-subtle")} aria-label={t("nav.createRole")}>
            <Plus className="size-3.5 shrink-0" aria-hidden />
            {t("nav.newRoleTitle")}
          </button>
        }
      />
    </SubList>
  );
}

function DatabaseSwitcher({ catalog, database, isAdmin, newDatabaseName, onNewDatabaseNameChange, onCreateDatabase, onNavigated }: SidebarProps) {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const [createOpen, setCreateOpen] = useState(false);
  return (
    <>
      <DropdownMenu>
        <DropdownMenuTrigger asChild>
          <button
            type="button"
            title={t("nav.switchDatabase")}
            className="flex h-10 w-full items-center gap-2.5 rounded-inner border border-border-soft bg-elevated/70 px-2 text-left transition-colors duration-150 hover:bg-hover data-[state=open]:bg-hover"
          >
            <span className="flex size-6 shrink-0 items-center justify-center rounded-[6px] bg-chip text-description" aria-hidden>
              <Database className="size-3.5" strokeWidth={1.75} />
            </span>
            <span className={cn("min-w-0 flex-1 truncate text-13 font-medium", database.name ? "text-fg" : "text-muted")}>
              {database.name || t("common.noDatabase")}
            </span>
            <ChevronsUpDown className="size-4 shrink-0 text-subtle" aria-hidden />
          </button>
        </DropdownMenuTrigger>
        <DropdownMenuContent
          align="start"
          className="w-[var(--radix-dropdown-menu-trigger-width)] min-w-56"
          onCloseAutoFocus={(event) => createOpen && event.preventDefault()}
        >
          <DropdownMenuLabel>{t("nav.databases")}</DropdownMenuLabel>
          <DropdownMenuRadioGroup
            value={database.name}
            onValueChange={(name) => {
              navigate({ databaseName: name, view: "home" });
              onNavigated?.();
            }}
          >
            {catalog.databases.map((item) => (
              <DropdownMenuRadioItem key={item.name} value={item.name}>
                <Database />
                <span className="truncate">{item.name}</span>
              </DropdownMenuRadioItem>
            ))}
          </DropdownMenuRadioGroup>
          {catalog.databases.length === 0 && <p className="px-2.5 py-1.5 text-12 text-subtle">{t("common.noDatabase")}</p>}
          {isAdmin && (
            <>
              <DropdownMenuSeparator />
              <DropdownMenuItem onSelect={() => setCreateOpen(true)}>
                <Plus />
                {t("nav.createDb")}
              </DropdownMenuItem>
            </>
          )}
        </DropdownMenuContent>
      </DropdownMenu>
      {isAdmin && (
        <CreateNameDialog
          open={createOpen}
          onOpenChange={setCreateOpen}
          title={t("nav.newDatabaseTitle")}
          description={t("nav.newDatabaseDescription")}
          inputLabel={t("nav.newDatabaseName")}
          placeholder={t("nav.placeholderDatabase")}
          name={newDatabaseName}
          onNameChange={onNewDatabaseNameChange}
          onSave={() => {
            onCreateDatabase();
            onNavigated?.();
          }}
        />
      )}
    </>
  );
}

export function initials(name: string): string {
  const trimmed = name.trim();
  if (!trimmed) return "?";
  const first = Array.from(trimmed)[0]!;
  return /[a-z]/i.test(first) ? first.toUpperCase() : first;
}

export function UserMenu({ currentUser, isAdmin, onLogout, onRefresh, compact }: SidebarProps & { compact?: boolean }) {
  const { t } = useTranslation();
  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <button
          type="button"
          aria-label={currentUser.display_name}
          className={cn(
            "flex min-w-0 items-center gap-2.5 rounded-inner text-left transition-colors duration-150 hover:bg-hover data-[state=open]:bg-hover",
            compact ? "rounded-full p-0.5" : "flex-1 px-2 py-1.5"
          )}
        >
          <span className="flex size-7 shrink-0 items-center justify-center rounded-full bg-chip text-12 font-medium text-description" aria-hidden>
            {initials(currentUser.display_name || currentUser.email)}
          </span>
          {!compact && (
            <span className="min-w-0 flex-1" aria-hidden>
              <span className="block truncate text-13 font-medium text-fg">{currentUser.display_name}</span>
              <span className="block truncate text-12 text-muted">{currentUser.email}</span>
            </span>
          )}
        </button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align={compact ? "end" : "start"} side={compact ? "bottom" : "top"} className="w-64">
        <DropdownMenuLabel className="flex flex-col gap-0.5">
          <span className="flex items-center gap-2 text-13 font-medium text-fg">
            <span className="truncate">{currentUser.display_name}</span>
            {isAdmin && <Badge>{t("nav.admin")}</Badge>}
          </span>
          <span className="truncate font-normal">{currentUser.email}</span>
        </DropdownMenuLabel>
        <DropdownMenuSeparator />
        <DropdownMenuItem onSelect={onRefresh}>
          <RefreshCw />
          {t("common.refreshMetadata")}
        </DropdownMenuItem>
        <DropdownMenuSeparator />
        <DropdownMenuItem onSelect={onLogout}>
          <LogOut />
          {t("common.logout")}
        </DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  );
}

export function LanguageButton() {
  const { t, i18n } = useTranslation();
  const language = normalizeLanguage(i18n.resolvedLanguage ?? i18n.language);
  return (
    <Tooltip content={t("language.switchTo")}>
      <Button
        variant="quiet"
        size="icon-sm"
        aria-label={t("language.switch")}
        onClick={() => {
          const index = appLanguages.indexOf(language);
          void i18n.changeLanguage(appLanguages[(index + 1) % appLanguages.length] as AppLanguage);
        }}
      >
        <Languages />
      </Button>
    </Tooltip>
  );
}
