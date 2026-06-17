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
  Text
} from "@fluentui/react-components";
import {
  AddRegular,
  CircleRegular,
  DatabaseRegular,
  DeleteRegular,
  DocumentFlowchartRegular,
  DocumentTableRegular,
  EditRegular,
  FormRegular,
  MoreHorizontalRegular,
  PeopleRegular,
  PersonRegular
} from "@fluentui/react-icons";
import { useState } from "react";
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
  onOpenTableViewPanel: (tableName: string) => void;
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
  onOpenTableViewPanel,
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
  const isAuthenticated = Boolean(currentUser);
  return (
    <>
      <NavDrawer className="primary-sidebar" type="inline" open>
        <NavDrawerHeader>
          <AppItemStatic icon={<DatabaseRegular />}>codetable</AppItemStatic>
        </NavDrawerHeader>
        <NavDrawerBody>
          <div className="sidebar-heading">
            <Text size={200} weight="semibold">
              Databases
            </Text>
            <CreateNamePopover
              ariaLabel="Create DB"
              buttonLabel="Create DB"
              disabled={!isAuthenticated}
              inputLabel="New database name"
              name={newDatabaseName}
              onNameChange={onNewDatabaseNameChange}
              onSave={onCreateDatabase}
              placeholder="database name"
            />
          </div>
          <Nav
            className="database-nav"
            aria-label="Database list"
            density="small"
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
                  <NavSubItem value={`${item.name}:table`}>Table</NavSubItem>
                  <NavSubItem value={`${item.name}:workflow`}>Workflow</NavSubItem>
                  <NavSubItem value={`${item.name}:form`}>Form</NavSubItem>
                  <NavSubItem value={`${item.name}:permission`}>Permission</NavSubItem>
                </NavSubItemGroup>
              </NavCategory>
            ))}
          </Nav>
          <NavDivider />
          <div className="account-slot">
            {currentUser ? (
              <Button icon={<PersonRegular />} onClick={onLogout}>
                {currentUser.email}
              </Button>
            ) : (
              <Button icon={<PersonRegular />} appearance="primary" onClick={onOpenLogin}>
                Login
              </Button>
            )}
          </div>
        </NavDrawerBody>
      </NavDrawer>

      <NavDrawer className="secondary-sidebar" type="inline" open>
        <NavDrawerHeader>
          <div className="secondary-title">
            <Text weight="semibold">
              {view === "table" && "Tables"}
              {view === "workflow" && "Workflows"}
              {view === "form" && "Forms"}
              {view === "permission" && "Roles"}
            </Text>
            <Text size={200}>{database.name || "No database"}</Text>
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
              onOpenTableViewPanel={onOpenTableViewPanel}
                newTableName={newTableName}
                selectedTableView={selectedTableView}
                table={table}
              />
            </>
          )}
          {view === "workflow" && (
            <ResourceNav
              ariaLabel="Workflow list"
              createLabel="Create Workflow"
              databaseName={database.name}
              icon="workflow"
              items={workflows.map((item) => ({ id: item.id ?? 0, name: item.name, enabled: item.enabled ?? true }))}
              newName={newWorkflowName}
              onCreate={onCreateWorkflow}
              onDelete={onDeleteWorkflow}
              onNewNameChange={onNewWorkflowNameChange}
              onRename={onRenameWorkflow}
              onSelect={onSelectWorkflowID}
              placeholder="new workflow"
              selectedID={selectedWorkflow?.id ?? 0}
              title="Workflows"
            />
          )}
          {view === "form" && (
            <ResourceNav
              ariaLabel="Form list"
              createLabel="Create Form"
              databaseName={database.name}
              icon="form"
              items={forms.map((item) => ({ id: item.id ?? 0, name: item.name }))}
              newName={newFormName}
              onCreate={onCreateForm}
              onDelete={onDeleteForm}
              onNewNameChange={onNewFormNameChange}
              onRename={onRenameForm}
              onSelect={onSelectFormID}
              placeholder="new form"
              selectedID={selectedForm?.id ?? 0}
              title="Forms"
            />
          )}
          {view === "permission" && (
            <>
              <Nav
                className="resource-nav"
                aria-label="Role list"
                density="small"
                selectedValue={selectedRole?.name ?? ""}
                onNavItemSelect={(_, data) => onSelectRoleName(data.value)}
              >
                <NavSectionHeader>Roles</NavSectionHeader>
                {roles.map((role) => (
                  <NavItem key={role.name} value={role.name} icon={<PeopleRegular />}>
                    {role.name}
                  </NavItem>
                ))}
              </Nav>
              <div className="create-rowline">
                <Input
                  aria-label="New role name"
                  placeholder="new role"
                  value={newRoleName}
                  onChange={(_, data) => onNewRoleNameChange(data.value)}
                  disabled={!database.name}
                />
                <Button icon={<AddRegular />} aria-label="Create Role" onClick={onCreateRole} disabled={!database.name} />
              </div>
            </>
          )}
        </NavDrawerBody>
      </NavDrawer>
    </>
  );
}

function ResourceNav(props: {
  ariaLabel: string;
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
          disabled={!props.databaseName}
          inputLabel={`New ${props.icon} name`}
          name={props.newName}
          onNameChange={props.onNewNameChange}
          onSave={props.onCreate}
          placeholder={props.placeholder}
        />
      </div>
      <Nav
        className="resource-nav"
        aria-label={props.ariaLabel}
        density="small"
        selectedValue={props.selectedID ? String(props.selectedID) : ""}
        onNavItemSelect={(_, data) => props.onSelect(Number(data.value))}
      >
        {props.items.map((item) => (
          <NavItem key={item.id || item.name} value={String(item.id)} icon={<Icon />}>
            <span className="resource-nav-row">
              <span className="resource-nav-name">
                {props.icon === "workflow" && (
                  <CircleRegular className={item.enabled ? "enabled-dot" : "disabled-dot"} />
                )}
                <span>{item.name}</span>
              </span>
              <Menu>
                <MenuTrigger disableButtonEnhancement>
                  <span
                    role="button"
                    tabIndex={0}
                    className="resource-nav-menu-button"
                    aria-label={`${props.icon} actions ${item.id}`}
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
                      Rename
                    </MenuItem>
                    <MenuItem
                      icon={<DeleteRegular />}
                      onClick={() => {
                        props.onDelete(item.id);
                      }}
                    >
                      Delete
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
        title={`Rename ${props.icon}`}
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
  onOpenTableViewPanel: (tableName: string) => void;
  onSelectTable: (name: string) => void;
  onSelectTableView: (name: string) => void;
  selectedTableView: string;
  table: TableMetadata;
}) {
  return (
    <>
      <div className="list-heading">
        <Text size={200} weight="semibold">Tables</Text>
        <CreateNamePopover
          ariaLabel="Create Table"
          buttonLabel="Create Table"
          disabled={!props.database.name}
          inputLabel="New table name"
          name={props.newTableName}
          onNameChange={props.onNewTableNameChange}
          onSave={props.onCreateTable}
          placeholder="table name"
        />
      </div>
      <Nav
        className="resource-nav"
        aria-label="Table list"
        density="small"
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
          if (action === "add-view") {
            props.onSelectTableView("all");
            props.onOpenTableViewPanel(tableName);
          }
        }}
      >
        {props.database.tables.map((item) => (
          <NavCategory key={item.name} value={item.name}>
            <NavCategoryItem icon={<DocumentTableRegular />}>{item.display_name || item.name}</NavCategoryItem>
            <NavSubItemGroup>
              <NavSubItem value={`${item.name}:view:all`}>All records</NavSubItem>
              {(item.views ?? []).map((viewDef) => (
                <NavSubItem key={viewDef.name} value={`${item.name}:view:${viewDef.name}`}>
                  {viewDef.display_name || viewDef.name}
                </NavSubItem>
              ))}
              <NavSubItem value={`${item.name}:add-view`}>+ View</NavSubItem>
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
  disabled,
  inputLabel,
  name,
  onNameChange,
  onSave,
  placeholder
}: {
  ariaLabel: string;
  buttonLabel: string;
  disabled?: boolean;
  inputLabel: string;
  name: string;
  onNameChange: (value: string) => void;
  onSave: () => void;
  placeholder: string;
}) {
  const [open, setOpen] = useState(false);
  return (
    <Popover open={open} onOpenChange={(_, data) => setOpen(data.open)} positioning="below-end" withArrow>
      <PopoverTrigger disableButtonEnhancement>
        <Button size="small" icon={<AddRegular />} aria-label={ariaLabel} disabled={disabled} />
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
          <Button onClick={() => setOpen(false)}>Cancel</Button>
          <Button
            appearance="primary"
            icon={<AddRegular />}
            onClick={() => {
              onSave();
              setOpen(false);
            }}
          >
            Save
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
  return (
    <Dialog open={open} onOpenChange={(_, data) => onOpenChange(data.open)}>
      <DialogSurface aria-label={title}>
        <DialogBody>
          <DialogTitle>{title}</DialogTitle>
          <DialogContent>
            <FluentField label="Name">
              <Input aria-label="Rename resource" value={value} onChange={(_, data) => onValueChange(data.value)} />
            </FluentField>
          </DialogContent>
          <DialogActions>
            <Button onClick={() => onOpenChange(false)}>Cancel</Button>
            <Button appearance="primary" onClick={onSave}>Save</Button>
          </DialogActions>
        </DialogBody>
      </DialogSurface>
    </Dialog>
  );
}

function isWorkspaceView(value: string): value is WorkspaceView {
  return value === "table" || value === "workflow" || value === "form" || value === "permission";
}
