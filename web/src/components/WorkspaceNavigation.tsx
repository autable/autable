import {
  AppItemStatic,
  Button,
  Dialog,
  DialogActions,
  DialogBody,
  DialogContent,
  DialogSurface,
  DialogTitle,
  Field as FluentField,
  Input,
  Menu,
  MenuItem,
  MenuList,
  MenuPopover,
  MenuTrigger,
  Nav,
  NavCategory,
  NavCategoryItem,
  NavDivider,
  NavDrawer,
  NavDrawerBody,
  NavDrawerHeader,
  NavItem,
  NavSectionHeader,
  NavSubItem,
  NavSubItemGroup,
  Popover,
  PopoverSurface,
  PopoverTrigger,
  Text,
  Tooltip
} from "@fluentui/react-components";
import {
  AddRegular,
  CircleFilled,
  DatabaseRegular,
  DeleteRegular,
  DocumentFlowchartRegular,
  DocumentTableRegular,
  EditRegular,
  FormRegular,
  MoreHorizontalRegular,
  PanelLeftContractRegular,
  PanelLeftExpandRegular,
  PeopleRegular,
  PersonRegular
} from "@fluentui/react-icons";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import type {
  AuthUser,
  Catalog,
  DatabaseMetadata,
  FormDefinition,
  RoleDefinition,
  TableMetadata,
  WorkflowDefinition
} from "../api";

export type WorkspaceView = "table" | "workflow" | "form" | "permission";

type WorkspaceNavigationProps = {
  catalog: Catalog;
  collapsed: boolean;
  onToggleCollapsed: () => void;
  currentUser: AuthUser | null;
  database: DatabaseMetadata;
  forms: FormDefinition[];
  newDatabaseName: string;
  newFormName: string;
  newRoleName: string;
  newTableName: string;
  newWorkflowName: string;
  onCreateDatabase: () => void;
  onCreateForm: () => void;
  onCreateRole: () => void;
  onCreateTable: () => void;
  onCreateWorkflow: () => void;
  onDeleteForm: (id?: number) => void;
  onDeleteWorkflow: (id?: number) => void;
  onLogout: () => void;
  onNewDatabaseNameChange: (value: string) => void;
  onNewFormNameChange: (value: string) => void;
  onNewRoleNameChange: (value: string) => void;
  onNewTableNameChange: (value: string) => void;
  onNewWorkflowNameChange: (value: string) => void;
  onOpenLogin: () => void;
  onCreateTableView: (tableName: string, viewName: string) => void;
  onSelectDatabaseSection: (databaseName: string, view: WorkspaceView) => void;
  onSelectFormID: (id: number) => void;
  onSelectRoleName: (name: string) => void;
  onSelectTable: (name: string) => void;
  onSelectTableView: (name: string) => void;
  onSelectWorkflowID: (id: number) => void;
  onRenameForm: (name: string, id?: number) => void;
  onRenameWorkflow: (name: string, id?: number) => void;
  roles: RoleDefinition[];
  selectedForm?: FormDefinition;
  selectedRole?: RoleDefinition;
  selectedTableView: string;
  selectedWorkflow?: WorkflowDefinition;
  table: TableMetadata;
  view: WorkspaceView;
  workflows: WorkflowDefinition[];
};

export function WorkspaceNavigation({
  catalog,
  collapsed,
  onToggleCollapsed,
  currentUser,
  database,
  forms,
  newDatabaseName,
  newFormName,
  newRoleName,
  newTableName,
  newWorkflowName,
  onCreateDatabase,
  onCreateForm,
  onCreateRole,
  onCreateTable,
  onCreateWorkflow,
  onDeleteForm,
  onDeleteWorkflow,
  onLogout,
  onNewDatabaseNameChange,
  onNewFormNameChange,
  onNewRoleNameChange,
  onNewTableNameChange,
  onNewWorkflowNameChange,
  onOpenLogin,
  onCreateTableView,
  onSelectDatabaseSection,
  onSelectFormID,
  onSelectRoleName,
  onSelectTable,
  onSelectTableView,
  onSelectWorkflowID,
  onRenameForm,
  onRenameWorkflow,
  roles,
  selectedForm,
  selectedRole,
  selectedTableView,
  selectedWorkflow,
  table,
  view,
  workflows
}: WorkspaceNavigationProps) {
  const { t } = useTranslation();
  const isAuthenticated = Boolean(currentUser);
  return (
    <>
      {collapsed ? (
        <PrimaryRail
          catalog={catalog}
          currentUser={currentUser}
          database={database}
          onLogout={onLogout}
          onOpenLogin={onOpenLogin}
          onSelectDatabaseSection={onSelectDatabaseSection}
          onToggleCollapsed={onToggleCollapsed}
          view={view}
        />
      ) : (
      <NavDrawer className="primary-sidebar" type="inline" open>
        <NavDrawerHeader>
          <div className="primary-header">
            <AppItemStatic icon={<DatabaseRegular />}>{t("nav.codetable")}</AppItemStatic>
            <Tooltip content={t("nav.collapseSidebar", "Collapse")} relationship="label">
              <Button
                appearance="subtle"
                icon={<PanelLeftContractRegular />}
                aria-label={t("nav.collapseSidebar", "Collapse sidebar")}
                onClick={onToggleCollapsed}
              />
            </Tooltip>
          </div>
        </NavDrawerHeader>
        <NavDrawerBody>
          <div className="sidebar-heading">
            <Text size={200} weight="semibold">
              {t("nav.databases")}
            </Text>
            <CreateNamePopover
              ariaLabel={t("nav.createDb")}
              buttonLabel={t("nav.createDb")}
              disabled={!isAuthenticated}
              inputLabel={t("nav.newDatabaseName")}
              name={newDatabaseName}
              onNameChange={onNewDatabaseNameChange}
              onSave={onCreateDatabase}
              placeholder={t("nav.placeholderDatabase")}
            />
          </div>
          <Nav
            className="database-nav"
            aria-label={t("nav.databaseList")}
            selectedValue={`${database.name}:${view}`}
            selectedCategoryValue={database.name}
            openCategories={database.name ? [database.name] : []}
            onNavCategoryItemToggle={(_, data) => {
              const databaseName = data.categoryValue ?? data.value;
              const nextDatabase = catalog.databases.find((item) => item.name === databaseName);
              if (nextDatabase) {
                onSelectDatabaseSection(nextDatabase.name, "table");
              }
            }}
            onNavItemSelect={(_, data) => {
              const [dbName, nextView] = data.value.split(":");
              if (isWorkspaceView(nextView)) {
                onSelectDatabaseSection(dbName, nextView);
              }
            }}
          >
            {catalog.databases.map((item) => (
              <NavCategory key={item.name} value={item.name}>
                  <NavCategoryItem icon={<DatabaseRegular />}>{item.name}</NavCategoryItem>
                  <NavSubItemGroup>
                  <NavSubItem value={`${item.name}:table`}>{t("common.table")}</NavSubItem>
                  <NavSubItem value={`${item.name}:workflow`}>{t("common.workflow")}</NavSubItem>
                  <NavSubItem value={`${item.name}:form`}>{t("common.form")}</NavSubItem>
                  <NavSubItem value={`${item.name}:permission`}>{t("common.permission")}</NavSubItem>
                </NavSubItemGroup>
              </NavCategory>
            ))}
          </Nav>
          <NavDivider />
          <div className="account-slot">
            {currentUser ? (
              <Button icon={<PersonRegular />} onClick={onLogout}>
                <span className="account-email">{currentUser.email}</span>
              </Button>
            ) : (
              <Button icon={<PersonRegular />} appearance="primary" onClick={onOpenLogin}>
                {t("common.login")}
              </Button>
            )}
          </div>
        </NavDrawerBody>
      </NavDrawer>
      )}

      <NavDrawer className="secondary-sidebar" type="inline" open>
        <NavDrawerHeader>
          <div className="secondary-title">
            <Text size={200}>{t("nav.database")}</Text>
            <Text weight="semibold" className="secondary-db-name" title={database.name || undefined}>
              {database.name || t("common.noDatabase")}
            </Text>
          </div>
        </NavDrawerHeader>
        <NavDrawerBody>
          {view === "table" && (
            <>
              <TableNav
                database={database}
                onCreateTable={onCreateTable}
                onNewTableNameChange={onNewTableNameChange}
                onSelectTable={onSelectTable}
                onSelectTableView={onSelectTableView}
                onCreateTableView={onCreateTableView}
                newTableName={newTableName}
                selectedTableView={selectedTableView}
                table={table}
              />
            </>
          )}
          {view === "workflow" && (
            <ResourceNav
              ariaLabel={t("nav.workflowList")}
              canCreate={(database.workflow_permission_level ?? 2) >= 2}
              createLabel={t("nav.createWorkflow")}
              databaseName={database.name}
              icon="workflow"
              items={workflows.map((item) => ({ id: item.id ?? 0, name: item.name, enabled: item.enabled ?? true }))}
              newName={newWorkflowName}
              onCreate={onCreateWorkflow}
              onDelete={onDeleteWorkflow}
              onNewNameChange={onNewWorkflowNameChange}
              onRename={onRenameWorkflow}
              onSelect={onSelectWorkflowID}
              placeholder={t("nav.placeholderWorkflow")}
              selectedID={selectedWorkflow?.id ?? 0}
              title={t("nav.workflows")}
            />
          )}
          {view === "form" && (
            <ResourceNav
              ariaLabel={t("nav.formList")}
              canCreate={(database.form_permission_level ?? 2) >= 2}
              createLabel={t("nav.createForm")}
              databaseName={database.name}
              icon="form"
              items={forms.map((item) => ({ id: item.id ?? 0, name: item.name }))}
              newName={newFormName}
              onCreate={onCreateForm}
              onDelete={onDeleteForm}
              onNewNameChange={onNewFormNameChange}
              onRename={onRenameForm}
              onSelect={onSelectFormID}
              placeholder={t("nav.placeholderForm")}
              selectedID={selectedForm?.id ?? 0}
              title={t("nav.forms")}
            />
          )}
          {view === "permission" && (
            <>
              <Nav
                className="resource-nav"
                aria-label={t("nav.roleList")}
                selectedValue={selectedRole?.name ?? ""}
                onNavItemSelect={(_, data) => onSelectRoleName(data.value)}
              >
                <NavSectionHeader>{t("nav.roles")}</NavSectionHeader>
                {roles.map((role) => (
                  <NavItem key={role.name} value={role.name} icon={<PeopleRegular />}>
                    {role.name}
                  </NavItem>
                ))}
              </Nav>
              <div className="create-rowline">
                <Input
                  aria-label={t("nav.newRoleName")}
                  placeholder={t("nav.placeholderRole")}
                  value={newRoleName}
                  onChange={(_, data) => onNewRoleNameChange(data.value)}
                  disabled={!database.name}
                />
                <Button
                  icon={<AddRegular />}
                  aria-label={t("nav.createRole")}
                  onClick={onCreateRole}
                  disabled={!database.name || (database.permission_level ?? 2) < 2}
                />
              </div>
            </>
          )}
        </NavDrawerBody>
      </NavDrawer>
    </>
  );
}

function PrimaryRail({
  catalog,
  currentUser,
  database,
  onLogout,
  onOpenLogin,
  onSelectDatabaseSection,
  onToggleCollapsed,
  view
}: {
  catalog: Catalog;
  currentUser: AuthUser | null;
  database: DatabaseMetadata;
  onLogout: () => void;
  onOpenLogin: () => void;
  onSelectDatabaseSection: (databaseName: string, view: WorkspaceView) => void;
  onToggleCollapsed: () => void;
  view: WorkspaceView;
}) {
  const { t } = useTranslation();
  const hasDatabase = Boolean(database.name);
  const sections = [
    { key: "table" as WorkspaceView, label: t("common.table"), icon: <DocumentTableRegular /> },
    { key: "workflow" as WorkspaceView, label: t("common.workflow"), icon: <DocumentFlowchartRegular /> },
    { key: "form" as WorkspaceView, label: t("common.form"), icon: <FormRegular /> },
    { key: "permission" as WorkspaceView, label: t("common.permission"), icon: <PeopleRegular /> }
  ];
  return (
    <aside className="primary-sidebar primary-rail" aria-label={t("nav.databaseList")}>
      <Tooltip content={t("nav.expandSidebar", "Expand")} relationship="label">
        <Button
          appearance="subtle"
          icon={<PanelLeftExpandRegular />}
          aria-label={t("nav.expandSidebar", "Expand sidebar")}
          onClick={onToggleCollapsed}
        />
      </Tooltip>
      <Menu>
        <MenuTrigger disableButtonEnhancement>
          <Tooltip content={database.name || t("common.noDatabase")} relationship="label">
            <Button appearance="subtle" icon={<DatabaseRegular />} aria-label={t("nav.databaseList")} />
          </Tooltip>
        </MenuTrigger>
        <MenuPopover>
          <MenuList>
            {catalog.databases.map((item) => (
              <MenuItem
                key={item.name}
                icon={<DatabaseRegular />}
                onClick={() => onSelectDatabaseSection(item.name, view)}
              >
                {item.name}
              </MenuItem>
            ))}
          </MenuList>
        </MenuPopover>
      </Menu>
      <div className="primary-rail-divider" />
      <div className="primary-rail-sections">
        {sections.map((section) => (
          <Tooltip key={section.key} content={section.label} relationship="label">
            <Button
              appearance={view === section.key ? "primary" : "subtle"}
              icon={section.icon}
              aria-label={section.label}
              disabled={!hasDatabase}
              onClick={() => onSelectDatabaseSection(database.name, section.key)}
            />
          </Tooltip>
        ))}
      </div>
      <div className="rail-spacer" />
      {currentUser ? (
        <Tooltip content={currentUser.email} relationship="label">
          <Button appearance="subtle" icon={<PersonRegular />} aria-label={currentUser.email} onClick={onLogout} />
        </Tooltip>
      ) : (
        <Tooltip content={t("common.login")} relationship="label">
          <Button appearance="primary" icon={<PersonRegular />} aria-label={t("common.login")} onClick={onOpenLogin} />
        </Tooltip>
      )}
    </aside>
  );
}

function ResourceNav(props: {
  ariaLabel: string;
  canCreate: boolean;
  createLabel: string;
  databaseName: string;
  icon: "workflow" | "form";
  items: Array<{ id: number; name: string; enabled?: boolean }>;
  newName: string;
  onCreate: () => void;
  onDelete: (id: number) => void;
  onNewNameChange: (value: string) => void;
  onRename: (name: string, id: number) => void;
  onSelect: (id: number) => void;
  placeholder: string;
  selectedID: number;
  title: string;
}) {
  const { t } = useTranslation();
  const Icon = props.icon === "workflow" ? DocumentFlowchartRegular : FormRegular;
  const [renameOpen, setRenameOpen] = useState(false);
  const [renameDraft, setRenameDraft] = useState("");
  const [renameID, setRenameID] = useState(0);
  return (
    <>
      <div className="list-heading">
        <Text size={200} weight="semibold">{props.title}</Text>
        <CreateNamePopover
          ariaLabel={props.createLabel}
          buttonLabel={props.createLabel}
          disabled={!props.databaseName || !props.canCreate}
          inputLabel={props.icon === "workflow" ? t("nav.newWorkflowName") : t("nav.newFormName")}
          name={props.newName}
          onNameChange={props.onNewNameChange}
          onSave={props.onCreate}
          placeholder={props.placeholder}
        />
      </div>
      <Nav
        className="resource-nav"
        aria-label={props.ariaLabel}
        selectedValue={props.selectedID ? String(props.selectedID) : ""}
        onNavItemSelect={(_, data) => props.onSelect(Number(data.value))}
      >
        {props.items.map((item) => (
          <NavItem key={item.id || item.name} value={String(item.id)} icon={<Icon />}>
            <span className="resource-nav-row">
              <span className="resource-nav-name">
                {props.icon === "workflow" && (
                  <CircleFilled className={item.enabled ? "enabled-dot" : "disabled-dot"} />
                )}
                <span>{item.name}</span>
              </span>
              <Menu>
                <MenuTrigger disableButtonEnhancement>
                  <span
                    role="button"
                    tabIndex={0}
                    className="resource-nav-menu-button"
                    aria-label={
                      props.icon === "workflow"
                        ? t("nav.workflowActions", { id: item.id })
                        : t("nav.formActions", { id: item.id })
                    }
                    onClick={(event) => event.stopPropagation()}
                  >
                    <MoreHorizontalRegular />
                  </span>
                </MenuTrigger>
                <MenuPopover>
                  <MenuList>
                    <MenuItem
                      icon={<EditRegular />}
                      onClick={() => {
                        props.onSelect(item.id);
                        setRenameDraft(item.name);
                        setRenameID(item.id);
                        setRenameOpen(true);
                      }}
                    >
                      {t("common.rename")}
                    </MenuItem>
                    <MenuItem
                      icon={<DeleteRegular />}
                      onClick={() => {
                        props.onDelete(item.id);
                      }}
                    >
                      {t("common.delete")}
                    </MenuItem>
                  </MenuList>
                </MenuPopover>
              </Menu>
            </span>
          </NavItem>
        ))}
      </Nav>
      <RenameDialog
        open={renameOpen}
        title={props.icon === "workflow" ? t("nav.renameWorkflow") : t("nav.renameForm")}
        value={renameDraft}
        onOpenChange={setRenameOpen}
        onValueChange={setRenameDraft}
        onSave={() => {
          props.onRename(renameDraft, renameID);
          setRenameOpen(false);
        }}
      />
    </>
  );
}

function TableNav(props: {
  database: DatabaseMetadata;
  newTableName: string;
  onCreateTable: () => void;
  onNewTableNameChange: (value: string) => void;
  onCreateTableView: (tableName: string, viewName: string) => void;
  onSelectTable: (name: string) => void;
  onSelectTableView: (name: string) => void;
  selectedTableView: string;
  table: TableMetadata;
}) {
  const { t } = useTranslation();
  const [newViewNames, setNewViewNames] = useState<Record<string, string>>({});
  function newViewName(tableName: string) {
    return newViewNames[tableName] ?? "";
  }
  function setNewViewName(tableName: string, value: string) {
    setNewViewNames((current) => ({ ...current, [tableName]: value }));
  }
  return (
    <>
      <div className="list-heading">
        <Text size={200} weight="semibold">{t("nav.tables")}</Text>
        <CreateNamePopover
          ariaLabel={t("nav.createTable")}
          buttonLabel={t("nav.createTable")}
          disabled={!props.database.name || (props.database.permission_level ?? 2) < 2}
          inputLabel={t("nav.newTableName")}
          name={props.newTableName}
          onNameChange={props.onNewTableNameChange}
          onSave={props.onCreateTable}
          placeholder={t("nav.placeholderTable")}
        />
      </div>
      <Nav
        className="resource-nav"
        aria-label={t("nav.tableList")}
        selectedValue={props.table.name ? `${props.table.name}:view:${props.selectedTableView}` : ""}
        selectedCategoryValue={props.table.name}
        openCategories={props.table.name ? [props.table.name] : []}
        onNavCategoryItemToggle={(_, data) => {
          const tableName = data.categoryValue ?? data.value;
          if (tableName) {
            props.onSelectTable(tableName);
            props.onSelectTableView("all");
          }
        }}
        onNavItemSelect={(_, data) => {
          const [tableName, action, viewName] = data.value.split(":");
          if (!tableName) {
            return;
          }
          props.onSelectTable(tableName);
          if (action === "view") {
            props.onSelectTableView(viewName || "all");
          }
        }}
      >
        {props.database.tables.map((item) => (
          <NavCategory key={item.name} value={item.name}>
            <NavCategoryItem icon={<DocumentTableRegular />}>{item.display_name || item.name}</NavCategoryItem>
            <NavSubItemGroup>
              <NavSubItem value={`${item.name}:view:all`}>{t("common.allRecords")}</NavSubItem>
              {(item.views ?? []).map((viewDef) => (
                <NavSubItem key={viewDef.name} value={`${item.name}:view:${viewDef.name}`}>
                  {viewDef.display_name || viewDef.name}
                </NavSubItem>
              ))}
              {(item.view_permission_level ?? item.permission_level ?? 2) >= 2 && (
                <div className="nav-create-subitem">
                  <CreateNamePopover
                    ariaLabel={t("nav.createView")}
                    buttonLabel={t("nav.createView")}
                    buttonText={t("nav.view")}
                    hideIcon
                    inputLabel={t("nav.newViewName")}
                    name={newViewName(item.name)}
                    onNameChange={(value) => setNewViewName(item.name, value)}
                    onSave={() => {
                      props.onSelectTable(item.name);
                      props.onCreateTableView(item.name, newViewName(item.name));
                      setNewViewName(item.name, "");
                    }}
                    placeholder={t("nav.placeholderView")}
                  />
                </div>
              )}
            </NavSubItemGroup>
          </NavCategory>
        ))}
      </Nav>
    </>
  );
}

function CreateNamePopover({
  ariaLabel,
  buttonLabel,
  buttonText,
  hideIcon,
  disabled,
  inputLabel,
  name,
  onNameChange,
  onSave,
  placeholder
}: {
  ariaLabel: string;
  buttonLabel: string;
  buttonText?: string;
  hideIcon?: boolean;
  disabled?: boolean;
  inputLabel: string;
  name: string;
  onNameChange: (value: string) => void;
  onSave: () => void;
  placeholder: string;
}) {
  const { t } = useTranslation();
  const [open, setOpen] = useState(false);
  return (
    <Popover open={open} onOpenChange={(_, data) => setOpen(data.open)} positioning="below-end" withArrow>
      <PopoverTrigger disableButtonEnhancement>
        <Button appearance="subtle" icon={hideIcon ? undefined : <AddRegular />} aria-label={ariaLabel} disabled={disabled}>
          {buttonText}
        </Button>
      </PopoverTrigger>
      <PopoverSurface className="create-name-popover" aria-label={buttonLabel}>
        <FluentField label={inputLabel}>
          <Input
            aria-label={inputLabel}
            placeholder={placeholder}
            value={name}
            onChange={(_, data) => onNameChange(data.value)}
          />
        </FluentField>
        <div className="popover-actions">
          <Button onClick={() => setOpen(false)}>{t("common.cancel")}</Button>
          <Button
            appearance="primary"
            icon={<AddRegular />}
            onClick={() => {
              onSave();
              setOpen(false);
            }}
          >
            {t("common.save")}
          </Button>
        </div>
      </PopoverSurface>
    </Popover>
  );
}

function RenameDialog({
  open,
  title,
  value,
  onOpenChange,
  onValueChange,
  onSave
}: {
  open: boolean;
  title: string;
  value: string;
  onOpenChange: (open: boolean) => void;
  onValueChange: (value: string) => void;
  onSave: () => void;
}) {
  const { t } = useTranslation();
  return (
    <Dialog open={open} onOpenChange={(_, data) => onOpenChange(data.open)}>
      <DialogSurface aria-label={title}>
        <DialogBody>
          <DialogTitle>{title}</DialogTitle>
          <DialogContent>
            <FluentField label={t("nav.name")}>
              <Input aria-label={t("nav.renameResource")} value={value} onChange={(_, data) => onValueChange(data.value)} />
            </FluentField>
          </DialogContent>
          <DialogActions>
            <Button onClick={() => onOpenChange(false)}>{t("common.cancel")}</Button>
            <Button appearance="primary" onClick={onSave}>{t("common.save")}</Button>
          </DialogActions>
        </DialogBody>
      </DialogSurface>
    </Dialog>
  );
}

function isWorkspaceView(value: string): value is WorkspaceView {
  return value === "table" || value === "workflow" || value === "form" || value === "permission";
}
