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
  type DatabaseMetadata,
  type FormDefinition,
  type PermissionGrant,
  type RoleDefinition,
  type RoleMember,
  type AuthUser,
  type WorkflowDefinition
} from "../api";
import { compactRoleGrants } from "../permissionState";
import { WorkspaceEmptyState } from "./WorkspaceEmptyState";

export { compactRoleGrants };

const permissionLevels = [0, 1, 2] as const;

// Field value grants are bitmasks: read=1, update=2, create=4.
const fieldBits = [1, 2, 4] as const;

type PermissionPanelProps = {
  database: DatabaseMetadata;
  forms: FormDefinition[];
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
  database,
  forms,
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
      <WorkspaceEmptyState
        icon={<PeopleRegular />}
        title={t("empty.noRoleTitle")}
        description={t("empty.noRoleDescription")}
      />
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
        database={database}
        forms={forms}
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

function PermissionMatrix({
  database,
  forms,
  grants,
  onGrantChange,
  workflows
}: Pick<PermissionPanelProps, "database" | "forms" | "grants" | "onGrantChange" | "workflows">) {
  const { t } = useTranslation();
  const [tab, setTab] = useState<"tables" | "workflows" | "forms">("tables");
  const [selectedTableName, setSelectedTableName] = useState("");
  const selectedTable = database.tables.find((table) => table.name === selectedTableName) ?? database.tables[0];

  return (
    <div className="permission-card permission-matrix">
      <TabList selectedValue={tab} onTabSelect={(_, data) => setTab(data.value as "tables" | "workflows" | "forms")}>
        <Tab value="tables">{t("permission.tables")}</Tab>
        <Tab value="workflows">{t("permission.workflows")}</Tab>
        <Tab value="forms">{t("permission.forms")}</Tab>
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
          allLabel={t("permission.workflowSet")}
          setScope="workflow_set"
          setResource={database.name}
          itemScope="workflow"
          grants={grants}
          items={workflows.map((workflow) => ({
            key: String(workflow.id ?? workflow.name),
            label: workflow.name,
            resource: String(workflow.id ?? 0),
            field: ""
          }))}
          onGrantChange={onGrantChange}
        />
      )}
      {tab === "forms" && (
        <ResourceLevelSection
          allLabel={t("permission.formSet")}
          setScope="form_set"
          setResource={database.name}
          itemScope="form"
          grants={grants}
          items={forms.map((form) => ({
            key: String(form.id ?? form.name),
            label: form.name,
            resource: String(form.id ?? 0),
            field: ""
          }))}
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

// TablePermissionEditor is the per-table permission sheet, split into the
// two permission layers: data (records, field values, views, files) and
// schema (adding fields, changing field definitions).
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
  const definableFields = activeFields.filter((field) => field.type !== "formula");
  const canCreate = grantLevel(grants, "record", resource, "create") >= 2;
  const canDelete = grantLevel(grants, "record", resource, "delete") >= 2;
  const canViewFiles = grantLevel(grants, "file", resource, "") >= 1;
  const canAddFields = grantLevel(grants, "field_add", resource, "") >= 2;
  const fieldSetLevel = grantLevel(grants, "field_set", resource, "");
  const viewSetLevel = grantLevel(grants, "view_set", resource, "");
  const viewItems = [
    { name: "all", label: t("permission.allRecordsView") },
    ...table.views.map((view) => ({ name: view.name, label: view.display_name || view.name }))
  ];

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
      <div className="permission-block">
        <Text weight="semibold">{t("permission.schemaSection")}</Text>
        <div className="permission-row">
          <span>{t("permission.fieldAdd")}</span>
          <ToggleButton
            size="small"
            checked={canAddFields}
            appearance={canAddFields ? "primary" : "secondary"}
            aria-label={t("permission.fieldAdd")}
            onClick={() => onGrantChange("field_add", resource, "", canAddFields ? 0 : 2)}
          >
            {t("permission.allowed")}
          </ToggleButton>
        </div>
        {definableFields.length > 0 && (
          <>
            <div className="permission-subheader">{t("permission.fieldDefine")}</div>
            {definableFields.map((field) => {
              const level = grantLevel(grants, "field_modify", resource, field.name);
              return (
                <div key={field.name} className="permission-row">
                  <span>{field.name}</span>
                  <ToggleButton
                    size="small"
                    checked={level >= 2}
                    appearance={level >= 2 ? "primary" : "secondary"}
                    aria-label={`${field.name} ${t("permission.fieldDefine")}`}
                    onClick={() => onGrantChange("field_modify", resource, field.name, level >= 2 ? 0 : 2)}
                  >
                    {t("permission.allowed")}
                  </ToggleButton>
                </div>
              );
            })}
          </>
        )}
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

// ResourceLevelSection is the flat none/read/write editor for workflows and
// forms: an "all" preset row plus one row per resource, mutually exclusive.
function ResourceLevelSection(props: {
  allLabel: string;
  setScope: PermissionGrant["scope"];
  setResource: string;
  itemScope: PermissionGrant["scope"];
  grants: PermissionGrant[];
  items: ScopeItem[];
  onGrantChange: GrantChangeFn;
}) {
  const { t } = useTranslation();
  const setLevel = grantLevel(props.grants, props.setScope, props.setResource, "");

  function chooseAll(level: number) {
    props.items.forEach((item) => props.onGrantChange(props.itemScope, item.resource, item.field, 0));
    props.onGrantChange(props.setScope, props.setResource, "", level);
  }

  function changeItem(item: ScopeItem, level: number) {
    if (setLevel > 0) {
      props.onGrantChange(props.setScope, props.setResource, "", 0);
    }
    props.onGrantChange(props.itemScope, item.resource, item.field, level);
  }

  return (
    <div className="permission-table-detail">
      <div className="permission-block">
        <PermissionLevelSelect label={props.allLabel} value={setLevel} onChange={chooseAll} />
        {props.items.length === 0 ? (
          <Text size={200}>{t("permission.noPartialItems")}</Text>
        ) : (
          props.items.map((item) => (
            <PermissionLevelSelect
              key={item.key}
              label={item.label}
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
  value: PermissionGrant["level"];
  onChange: (level: PermissionGrant["level"]) => void;
}) {
  const { t } = useTranslation();
  const permissionLevelLabels: Record<(typeof permissionLevels)[number], string> = {
    0: t("permission.levels.none"),
    1: t("permission.levels.read"),
    2: t("permission.levels.write")
  };
  return (
    <div className="permission-row">
      <span>{props.label}</span>
      <Select
        aria-label={props.ariaLabel ?? `${props.label} permission`}
        value={String(props.value)}
        onChange={(_, data) => props.onChange(Number(data.value) as PermissionGrant["level"])}
      >
        {permissionLevels.map((level) => (
          <option key={level} value={level}>
            {permissionLevelLabels[level]}
          </option>
        ))}
      </Select>
    </div>
  );
}
