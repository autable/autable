import {
  AppItemStatic,
  Button,
  Input,
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
  Text
} from "@fluentui/react-components";
import {
  AddRegular,
  AppsListRegular,
  DatabaseRegular,
  DocumentFlowchartRegular,
  DocumentTableRegular,
  FormRegular,
  PeopleRegular,
  PersonRegular
} from "@fluentui/react-icons";
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
  newRoleName: string;
  newTableName: string;
  onCreateDatabase: () => void;
  onCreateRole: () => void;
  onCreateTable: () => void;
  onLogout: () => void;
  onNewDatabaseNameChange: (value: string) => void;
  onNewRoleNameChange: (value: string) => void;
  onNewTableNameChange: (value: string) => void;
  onOpenLogin: () => void;
  onSelectDatabaseSection: (databaseName: string, view: WorkspaceView) => void;
  onSelectFormID: (id: number) => void;
  onSelectRoleName: (name: string) => void;
  onSelectTable: (name: string) => void;
  onSelectTableView: (name: string) => void;
  onSelectWorkflowID: (id: number) => void;
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
  newRoleName,
  newTableName,
  onCreateDatabase,
  onCreateRole,
  onCreateTable,
  onLogout,
  onNewDatabaseNameChange,
  onNewRoleNameChange,
  onNewTableNameChange,
  onOpenLogin,
  onSelectDatabaseSection,
  onSelectFormID,
  onSelectRoleName,
  onSelectTable,
  onSelectTableView,
  onSelectWorkflowID,
  roles,
  selectedForm,
  selectedRole,
  selectedTableView,
  selectedWorkflow,
  table,
  view,
  workflows
}: WorkspaceNavigationProps) {
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
            <Button size="small" icon={<AddRegular />} aria-label="Create database" onClick={onCreateDatabase} />
          </div>
          <Nav
            className="database-nav"
            aria-label="Database list"
            density="small"
            selectedValue={`${database.name}:${view}`}
            selectedCategoryValue={database.name}
            openCategories={database.name ? [database.name] : []}
            onNavCategoryItemToggle={(_, data) => {
              const nextDatabase = catalog.databases.find((item) => item.name === data.value);
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
          <div className="primary-actions">
            <Input
              aria-label="New database name"
              placeholder="new database"
              value={newDatabaseName}
              onChange={(_, data) => onNewDatabaseNameChange(data.value)}
            />
            <Button onClick={onCreateDatabase}>Create DB</Button>
          </div>
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
                newTableName={newTableName}
                selectedTableView={selectedTableView}
                table={table}
              />
            </>
          )}
          {view === "workflow" && (
            <Nav
              className="resource-nav"
              aria-label="Workflow list"
              density="small"
              selectedValue={selectedWorkflow?.id ? String(selectedWorkflow.id) : ""}
              onNavItemSelect={(_, data) => onSelectWorkflowID(Number(data.value))}
            >
              <NavSectionHeader>Workflows</NavSectionHeader>
              {workflows.map((item) => (
                <NavItem key={item.id ?? item.name} value={String(item.id ?? 0)} icon={<DocumentFlowchartRegular />}>
                  {item.name}
                </NavItem>
              ))}
            </Nav>
          )}
          {view === "form" && (
            <Nav
              className="resource-nav"
              aria-label="Form list"
              density="small"
              selectedValue={selectedForm?.id ? String(selectedForm.id) : ""}
              onNavItemSelect={(_, data) => onSelectFormID(Number(data.value))}
            >
              <NavSectionHeader>Forms</NavSectionHeader>
              {forms.map((item) => (
                <NavItem key={item.id ?? item.name} value={String(item.id ?? 0)} icon={<FormRegular />}>
                  {item.name}
                </NavItem>
              ))}
            </Nav>
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

function TableNav(props: {
  database: DatabaseMetadata;
  newTableName: string;
  onCreateTable: () => void;
  onNewTableNameChange: (value: string) => void;
  onSelectTable: (name: string) => void;
  onSelectTableView: (name: string) => void;
  selectedTableView: string;
  table: TableMetadata;
}) {
  return (
    <>
      <Nav
        className="resource-nav"
        aria-label="Table list"
        density="small"
        selectedValue={props.table.name}
        onNavItemSelect={(_, data) => {
          props.onSelectTable(data.value);
          props.onSelectTableView("all");
        }}
      >
        <NavSectionHeader>Tables</NavSectionHeader>
        {props.database.tables.map((item) => (
          <NavItem key={item.name} value={item.name} icon={<DocumentTableRegular />}>
            {item.display_name || item.name}
          </NavItem>
        ))}
      </Nav>
      <div className="create-rowline">
        <Input
          aria-label="New table name"
          placeholder="new table"
          value={props.newTableName}
          onChange={(_, data) => props.onNewTableNameChange(data.value)}
          disabled={!props.database.name}
        />
        <Button
          icon={<AddRegular />}
          aria-label="Create Table"
          onClick={props.onCreateTable}
          disabled={!props.database.name}
        />
      </div>
      <div className="side-section">
        <Text size={200} weight="semibold">
          Views
        </Text>
        <Nav
          className="resource-nav"
          aria-label="View list"
          density="small"
          selectedValue={props.selectedTableView}
          onNavItemSelect={(_, data) => props.onSelectTableView(data.value)}
        >
          <NavItem value="all" icon={<AppsListRegular />}>
            All records
          </NavItem>
          {(props.table.views ?? []).map((item) => (
            <NavItem key={item.name} value={item.name} icon={<AppsListRegular />}>
              {item.display_name || item.name}
            </NavItem>
          ))}
        </Nav>
      </div>
    </>
  );
}

function isWorkspaceView(value: string): value is WorkspaceView {
  return value === "table" || value === "workflow" || value === "form" || value === "permission";
}
