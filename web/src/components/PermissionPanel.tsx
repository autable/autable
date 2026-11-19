import {
  Button,
  Combobox,
  CounterBadge,
  Dropdown,
  List,
  ListItem,
  Option,
  Popover,
  PopoverSurface,
  PopoverTrigger,
  Select,
  Tab,
  TabList,
  Text,
  ToggleButton
} from "@fluentui/react-components";
import {
  DismissRegular,
  DocumentFlowchartRegular,
  PeopleRegular,
  SaveRegular
} from "@fluentui/react-icons";
import { useState, type ReactElement, type ReactNode } from "react";
import { useTranslation } from "react-i18next";
import {
  type AppDefinition,
  type DatabaseMetadata,
  type PermissionGrant,
  type RoleDefinition,
  type RoleMember,
  type AuthUser,
  type WorkflowDefinition
} from "../api";
import { compactRoleGrants } from "../permissionState";
import { Card, EmptyState } from "../ui";
import { Users } from "lucide-react";

export { compactRoleGrants };

// Workflow and app grants are on/off: 0 = none, 1 = run (workflows) or
// open (apps). Building and managing them is admin-only.
const accessLevels = [0, 1] as const;

// Field value grants are bitmasks: read=1, update=2, create=4.
const fieldBits = [1, 2, 4] as const;

type PermissionPanelProps = {
  apps: AppDefinition[];
  database: DatabaseMetadata;
  grants: PermissionGrant[];
  members: RoleMember[];
  memberOptions: AuthUser[];
  memberUsers: AuthUser[];
  memberWorkflows: WorkflowDefinition[];
  newMemberQuery: string;
  onAddMember: (user: AuthUser) => void;
  onAddWorkflowMember: (workflow: WorkflowDefinition) => void;
  onGrantChange: (
    scope: PermissionGrant["scope"],
    resource: string,
    field: string,
    level: PermissionGrant["level"]
  ) => void;
  onMemberRemove: (member: RoleMember) => void;
  onNewMemberQueryChange: (value: string) => void;
  onSave: () => void;
  role?: RoleDefinition;
  workflows: WorkflowDefinition[];
};

export function PermissionPanel({
  apps,
  database,
  grants,
  members,
  memberOptions,
  memberUsers,
  memberWorkflows,
  newMemberQuery,
  onAddMember,
  onAddWorkflowMember,
  onGrantChange,
  onMemberRemove,
  onNewMemberQueryChange,
  onSave,
  role,
  workflows
}: PermissionPanelProps) {
  const { t } = useTranslation();
  if (!role) {
    return (
      <Card className="mx-auto max-w-5xl">
        <EmptyState icon={Users} title={t("empty.noRoleTitle")} description={t("empty.noRoleDescription")} />
      </Card>
    );
  }
  const dirty =
    normalizeGrants(grants) !== normalizeGrants(role.grants ?? []) ||
    normalizeMembers(members) !== normalizeMembers(role.members ?? []);
  return (
    <div className="permission-view">
      <div className="section-header">
        <div>
          <Text weight="semibold">{role.name}</Text>
          <Text size={200}>{t("permission.roleAccessMatrix", { database: database.name })}</Text>
        </div>
        <div className="permission-actions">
          <MembersControl
            members={members}
            memberOptions={memberOptions}
            memberUsers={memberUsers}
            memberWorkflows={memberWorkflows}
            newMemberQuery={newMemberQuery}
            onAddMember={onAddMember}
            onAddWorkflowMember={onAddWorkflowMember}
            onMemberRemove={onMemberRemove}
            onNewMemberQueryChange={onNewMemberQueryChange}
            workflows={workflows}
          />
          <Button icon={<SaveRegular />} appearance="primary" onClick={onSave} disabled={!dirty}>
            {t("common.save")}
          </Button>
        </div>
      </div>
      <PermissionMatrix
        key={role.name}
        apps={apps}
        database={database}
        grants={grants}
        onGrantChange={onGrantChange}
        workflows={workflows}
      />
    </div>
  );
}

function MembersControl({
  members,
  memberOptions,
  memberUsers,
  memberWorkflows,
  newMemberQuery,
  onAddMember,
  onAddWorkflowMember,
  onMemberRemove,
  onNewMemberQueryChange,
  workflows
}: Pick<
  PermissionPanelProps,
  | "members"
  | "memberOptions"
  | "memberUsers"
  | "memberWorkflows"
  | "newMemberQuery"
  | "onAddMember"
  | "onAddWorkflowMember"
  | "onMemberRemove"
  | "onNewMemberQueryChange"
  | "workflows"
>) {
  const { t } = useTranslation();
  const memberByID = new Map(memberUsers.map((member) => [member.id, member]));
  const workflowByID = new Map(memberWorkflows.map((workflow) => [String(workflow.id), workflow]));
  const userItems = members
    .filter((member) => member.type === "user")
    .map((member) => ({
      member,
      key: `user:${member.id}`,
      label: memberByID.get(member.id)?.display_name ?? member.id
    }));
  const workflowItems = members
    .filter((member) => member.type === "workflow")
    .map((member) => ({
      member,
      key: `workflow:${member.id}`,
      label: workflowByID.get(member.id)?.name ?? `#${member.id}`
    }));
  const selectableWorkflows = workflows.filter(
    (workflow) => workflow.id && !members.some((member) => member.type === "workflow" && member.id === String(workflow.id))
  );

  return (
    <>
      <MemberPopover
        icon={<PeopleRegular />}
        label={t("permission.members")}
        ariaLabel={t("permission.members")}
        count={userItems.length}
        items={userItems}
        emptyText={t("permission.noMembers")}
        onRemove={onMemberRemove}
      >
        <Combobox
          className="member-add-control"
          aria-label={t("permission.roleMemberDisplayName")}
          placeholder={t("permission.searchMember")}
          open={newMemberQuery.trim().length >= 2 && memberOptions.length > 0}
          value={newMemberQuery}
          onChange={(event) => onNewMemberQueryChange(event.currentTarget.value)}
          onOptionSelect={(_, data) => {
            const selected = memberOptions.find((member) => member.id === data.optionValue);
            if (selected) {
              onAddMember(selected);
            }
          }}
        >
          {memberOptions.map((member) => (
            <Option key={member.id} value={member.id} text={member.display_name}>
              {member.display_name}
            </Option>
          ))}
        </Combobox>
      </MemberPopover>
      <MemberPopover
        icon={<DocumentFlowchartRegular />}
        label={t("permission.workflows")}
        ariaLabel={t("permission.workflows")}
        count={workflowItems.length}
        items={workflowItems}
        emptyText={t("permission.noWorkflowMembers")}
        onRemove={onMemberRemove}
      >
        <Dropdown
          className="member-add-control"
          aria-label={t("permission.addWorkflowMember")}
          placeholder={t("permission.addWorkflowMember")}
          selectedOptions={[]}
          value=""
          disabled={selectableWorkflows.length === 0}
          onOptionSelect={(_, data) => {
            const selected = selectableWorkflows.find((workflow) => String(workflow.id) === data.optionValue);
            if (selected) {
              onAddWorkflowMember(selected);
            }
          }}
        >
          {selectableWorkflows.map((workflow) => (
            <Option key={workflow.id} value={String(workflow.id)}>
              {workflow.name}
            </Option>
          ))}
        </Dropdown>
      </MemberPopover>
    </>
  );
}

function MemberPopover({
  icon,
  label,
  ariaLabel,
  count,
  items,
  emptyText,
  onRemove,
  children
}: {
  icon: ReactElement;
  label: string;
  ariaLabel: string;
  count: number;
  items: Array<{ key: string; label: string; member: RoleMember }>;
  emptyText: string;
  onRemove: (member: RoleMember) => void;
  children: ReactNode;
}) {
  const { t } = useTranslation();
  return (
    <Popover positioning="below-end" trapFocus>
      <PopoverTrigger disableButtonEnhancement>
        <Button icon={icon}>
          {label}
          <CounterBadge className="members-count" appearance="filled" color="brand" count={count} showZero />
        </Button>
      </PopoverTrigger>
      <PopoverSurface className="members-popover" aria-label={ariaLabel}>
        {children}
        {items.length === 0 ? (
          <Text size={200}>{emptyText}</Text>
        ) : (
          <List navigationMode="items" aria-label={ariaLabel}>
            {items.map((item) => (
              <ListItem key={item.key}>
                <div className="member-list-item">
                  <Text truncate>{item.label}</Text>
                  <Button
                    appearance="subtle"
                    icon={<DismissRegular />}
                    aria-label={t("permission.removeMember", { name: item.label })}
                    onClick={() => onRemove(item.member)}
                  />
                </div>
              </ListItem>
            ))}
          </List>
        )}
      </PopoverSurface>
    </Popover>
  );
}

type MatrixTab = "tables" | "workflows" | "apps";

function PermissionMatrix({
  apps,
  database,
  grants,
  onGrantChange,
  workflows
}: Pick<PermissionPanelProps, "apps" | "database" | "grants" | "onGrantChange" | "workflows">) {
  const { t } = useTranslation();
  const [tab, setTab] = useState<MatrixTab>("tables");
  const [selectedTableName, setSelectedTableName] = useState("");
  const selectedTable = database.tables.find((table) => table.name === selectedTableName) ?? database.tables[0];

  return (
    <div className="permission-card permission-matrix">
      <TabList selectedValue={tab} onTabSelect={(_, data) => setTab(data.value as MatrixTab)}>
        <Tab value="tables">{t("permission.tables")}</Tab>
        <Tab value="workflows">{t("permission.workflows")}</Tab>
        <Tab value="apps">{t("permission.apps")}</Tab>
      </TabList>
      {tab === "tables" &&
        (selectedTable ? (
          <div className="permission-tables-layout">
            <div className="permission-table-list" aria-label={t("permission.tables")}>
              {database.tables.map((table) => (
                <Button
                  key={table.name}
                  appearance={table.name === selectedTable.name ? "secondary" : "subtle"}
                  onClick={() => setSelectedTableName(table.name)}
                >
                  {table.display_name || table.name}
                </Button>
              ))}
            </div>
            <TablePermissionEditor
              key={selectedTable.name}
              database={database}
              table={selectedTable}
              grants={grants}
              onGrantChange={onGrantChange}
            />
          </div>
        ) : (
          <Text size={200}>{t("permission.noPartialItems")}</Text>
        ))}
      {tab === "workflows" && (
        <ResourceLevelSection
          itemScope="workflow"
          grants={grants}
          items={workflows.map((workflow) => ({
            key: String(workflow.id ?? workflow.name),
            label: workflow.name,
            resource: String(workflow.id ?? 0),
            field: ""
          }))}
          levelLabels={{
            0: t("permission.workflowLevels.none"),
            1: t("permission.workflowLevels.run")
          }}
          onGrantChange={onGrantChange}
        />
      )}
      {tab === "apps" && (
        <ResourceLevelSection
          all={{ label: t("permission.appSet"), scope: "app_set", resource: database.name }}
          itemScope="app"
          grants={grants}
          items={apps.map((app) => ({
            key: String(app.id),
            label: app.title || app.name,
            resource: String(app.id),
            field: ""
          }))}
          levelLabels={{
            0: t("permission.appLevels.none"),
            1: t("permission.appLevels.open")
          }}
          onGrantChange={onGrantChange}
        />
      )}
    </div>
  );
}

type ScopeItem = { key: string; label: string; resource: string; field: string };

type GrantChangeFn = (
  scope: PermissionGrant["scope"],
  resource: string,
  field: string,
  level: PermissionGrant["level"]
) => void;

// TablePermissionEditor is the per-table data permission sheet: records,
// field values, views and files. Schema changes are admin-only and have no
// grants.
function TablePermissionEditor({
  database,
  table,
  grants,
  onGrantChange
}: {
  database: DatabaseMetadata;
  table: DatabaseMetadata["tables"][number];
  grants: PermissionGrant[];
  onGrantChange: GrantChangeFn;
}) {
  const { t } = useTranslation();
  const resource = `${database.name}.${table.name}`;
  const activeFields = table.fields.filter((field) => !field.deleted);
  const canCreate = grantLevel(grants, "record", resource, "create") >= 2;
  const canDelete = grantLevel(grants, "record", resource, "delete") >= 2;
  const canViewFiles = grantLevel(grants, "file", resource, "") >= 1;
  const fieldSetLevel = grantLevel(grants, "field_set", resource, "");
  const viewSetLevel = grantLevel(grants, "view_set", resource, "");
  // The catalog already carries the built-in "all" view as an ordinary
  // entry; only its display label is localized here.
  const viewItems = table.views.map((view) => ({
    name: view.name,
    label: view.name === "all" ? t("permission.allRecordsView") : view.display_name || view.name
  }));

  function changeFieldSet(level: number) {
    activeFields.forEach((field) => onGrantChange("field", resource, field.name, 0));
    onGrantChange("field_set", resource, "", level);
  }

  function changeField(fieldName: string, level: number) {
    if (fieldSetLevel > 0) {
      onGrantChange("field_set", resource, "", 0);
    }
    onGrantChange("field", resource, fieldName, level);
  }

  function changeViewSet(level: number) {
    viewItems.forEach((view) => onGrantChange("view", resource, view.name, 0));
    onGrantChange("view_set", resource, "", level);
  }

  function changeView(viewName: string, level: number) {
    if (viewSetLevel > 0) {
      onGrantChange("view_set", resource, "", 0);
    }
    onGrantChange("view", resource, viewName, level);
  }

  return (
    <div className="permission-table-detail">
      <div className="permission-block">
        <Text weight="semibold">{t("permission.dataSection")}</Text>
        <div className="permission-row">
          <span>{t("permission.records")}</span>
          <div className="field-bits" role="group" aria-label={t("permission.records")}>
            <ToggleButton
              size="small"
              checked={canCreate}
              appearance={canCreate ? "primary" : "secondary"}
              aria-label={t("permission.recordCreate")}
              onClick={() => onGrantChange("record", resource, "create", canCreate ? 0 : 2)}
            >
              {t("permission.recordCreate")}
            </ToggleButton>
            <ToggleButton
              size="small"
              checked={canDelete}
              appearance={canDelete ? "primary" : "secondary"}
              aria-label={t("permission.recordDelete")}
              onClick={() => onGrantChange("record", resource, "delete", canDelete ? 0 : 2)}
            >
              {t("permission.recordDelete")}
            </ToggleButton>
          </div>
        </div>
        <ReadToggleRow
          label={t("permission.files")}
          checked={canViewFiles}
          onChange={(next) => onGrantChange("file", resource, "", next ? 1 : 0)}
        />
        <div className="permission-subheader">{t("permission.fieldValues")}</div>
        <FieldBitsSelect label={t("permission.allFields")} value={fieldSetLevel} onChange={changeFieldSet} />
        {activeFields.map((field) => (
          <FieldBitsSelect
            key={field.name}
            label={field.name}
            value={grantLevel(grants, "field", resource, field.name)}
            onChange={(level) => changeField(field.name, level)}
          />
        ))}
        <div className="permission-subheader">{t("permission.views")}</div>
        <ReadToggleRow
          label={t("permission.allViews")}
          checked={viewSetLevel >= 1}
          onChange={(next) => changeViewSet(next ? 1 : 0)}
        />
        {viewItems.map((view) => (
          <ReadToggleRow
            key={view.name}
            label={view.label}
            checked={grantLevel(grants, "view", resource, view.name) >= 1}
            onChange={(next) => changeView(view.name, next ? 1 : 0)}
          />
        ))}
      </div>
    </div>
  );
}

function ReadToggleRow(props: { label: string; checked: boolean; onChange: (next: boolean) => void }) {
  const { t } = useTranslation();
  return (
    <div className="permission-row">
      <span>{props.label}</span>
      <ToggleButton
        size="small"
        checked={props.checked}
        appearance={props.checked ? "primary" : "secondary"}
        aria-label={`${props.label} ${t("permission.bits.read")}`}
        onClick={() => props.onChange(!props.checked)}
      >
        {t("permission.bits.read")}
      </ToggleButton>
    </div>
  );
}

type LevelLabels = Record<(typeof accessLevels)[number], string>;

// ResourceLevelSection is the flat none/run or none/open editor for
// workflows and apps: one row per resource, plus an optional "all" preset
// row that is mutually exclusive with the per-resource rows.
function ResourceLevelSection(props: {
  all?: { label: string; scope: PermissionGrant["scope"]; resource: string };
  levelLabels: LevelLabels;
  itemScope: PermissionGrant["scope"];
  grants: PermissionGrant[];
  items: ScopeItem[];
  onGrantChange: GrantChangeFn;
}) {
  const { t } = useTranslation();
  const all = props.all;
  const setLevel = all ? grantLevel(props.grants, all.scope, all.resource, "") : 0;

  function chooseAll(level: number) {
    if (!all) {
      return;
    }
    props.items.forEach((item) => props.onGrantChange(props.itemScope, item.resource, item.field, 0));
    props.onGrantChange(all.scope, all.resource, "", level);
  }

  function changeItem(item: ScopeItem, level: number) {
    if (all && setLevel > 0) {
      props.onGrantChange(all.scope, all.resource, "", 0);
    }
    props.onGrantChange(props.itemScope, item.resource, item.field, level);
  }

  return (
    <div className="permission-table-detail">
      <div className="permission-block">
        {all && (
          <PermissionLevelSelect label={all.label} levelLabels={props.levelLabels} value={setLevel} onChange={chooseAll} />
        )}
        {props.items.length === 0 ? (
          <Text size={200}>{t("permission.noPartialItems")}</Text>
        ) : (
          props.items.map((item) => (
            <PermissionLevelSelect
              key={item.key}
              label={item.label}
              levelLabels={props.levelLabels}
              value={grantLevel(props.grants, props.itemScope, item.resource, item.field)}
              onChange={(level) => changeItem(item, level)}
            />
          ))
        )}
      </div>
    </div>
  );
}

function FieldBitsSelect(props: { label: string; value: number; onChange: (level: number) => void }) {
  const { t } = useTranslation();
  const bitLabels: Record<number, string> = {
    1: t("permission.bits.read"),
    2: t("permission.bits.update"),
    4: t("permission.bits.create")
  };
  return (
    <div className="permission-row">
      <span>{props.label}</span>
      <div className="field-bits" role="group" aria-label={`${props.label} permission`}>
        {fieldBits.map((bit) => (
          <ToggleButton
            key={bit}
            size="small"
            checked={(props.value & bit) !== 0}
            appearance={(props.value & bit) !== 0 ? "primary" : "secondary"}
            aria-label={`${props.label} ${bitLabels[bit]}`}
            onClick={() => props.onChange(props.value ^ bit)}
          >
            {bitLabels[bit]}
          </ToggleButton>
        ))}
      </div>
    </div>
  );
}

function grantLevel(
  grants: PermissionGrant[],
  scope: PermissionGrant["scope"],
  resource: string,
  field: string
): PermissionGrant["level"] {
  return grants.find((grant) => grant.scope === scope && grant.resource === resource && grant.field === field)?.level ?? 0;
}

// Stable signatures so the Save button can tell whether the draft differs
// from the persisted role.
function normalizeGrants(grants: PermissionGrant[]): string {
  return compactRoleGrants(grants)
    .map((grant) => `${grant.scope}|${grant.resource}|${grant.field}|${grant.level}`)
    .sort()
    .join("\n");
}

function normalizeMembers(members: RoleMember[]): string {
  return members.map((member) => `${member.type}:${member.id}`).sort().join(",");
}

function PermissionLevelSelect(props: {
  ariaLabel?: string;
  label: string;
  levelLabels: LevelLabels;
  value: PermissionGrant["level"];
  onChange: (level: PermissionGrant["level"]) => void;
}) {
  return (
    <div className="permission-row">
      <span>{props.label}</span>
      <Select
        aria-label={props.ariaLabel ?? `${props.label} permission`}
        value={String(Math.min(props.value, 1))}
        onChange={(_, data) => props.onChange(Number(data.value) as PermissionGrant["level"])}
      >
        {accessLevels.map((level) => (
          <option key={level} value={level}>
            {props.levelLabels[level]}
          </option>
        ))}
      </Select>
    </div>
  );
}
