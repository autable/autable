import {
  Button,
  Combobox,
  CounterBadge,
  Dropdown,
  List,
  ListItem,
  Menu,
  MenuButton,
  MenuItem,
  MenuList,
  MenuPopover,
  MenuTrigger,
  Option,
  Popover,
  PopoverSurface,
  PopoverTrigger,
  Select,
  Text,
  ToggleButton
} from "@fluentui/react-components";
import {
  ChevronDownRegular,
  DismissRegular,
  DocumentFlowchartRegular,
  PeopleRegular,
  SaveRegular
} from "@fluentui/react-icons";
import { type ReactElement, type ReactNode } from "react";
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

type PermissionPanelProps = {
  database: DatabaseMetadata;
  forms: FormDefinition[];
  grants: PermissionGrant[];
  members: RoleMember[];
  memberOptions: AuthUser[];
  memberUsers: AuthUser[];
  memberWorkflows: WorkflowDefinition[];
  newMemberEmail: string;
  onAddMember: (user?: AuthUser) => void;
  onAddWorkflowMember: (workflow: WorkflowDefinition) => void;
  onGrantChange: (
    scope: PermissionGrant["scope"],
    resource: string,
    field: string,
    level: PermissionGrant["level"]
  ) => void;
  onMemberRemove: (member: RoleMember) => void;
  onNewMemberEmailChange: (value: string) => void;
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
  newMemberEmail,
  onAddMember,
  onAddWorkflowMember,
  onGrantChange,
  onMemberRemove,
  onNewMemberEmailChange,
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
            newMemberEmail={newMemberEmail}
            onAddMember={onAddMember}
            onAddWorkflowMember={onAddWorkflowMember}
            onMemberRemove={onMemberRemove}
            onNewMemberEmailChange={onNewMemberEmailChange}
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
  newMemberEmail,
  onAddMember,
  onAddWorkflowMember,
  onMemberRemove,
  onNewMemberEmailChange,
  workflows
}: Pick<
  PermissionPanelProps,
  | "members"
  | "memberOptions"
  | "memberUsers"
  | "memberWorkflows"
  | "newMemberEmail"
  | "onAddMember"
  | "onAddWorkflowMember"
  | "onMemberRemove"
  | "onNewMemberEmailChange"
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
      label: memberByID.get(member.id)?.email ?? member.id
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
          aria-label={t("permission.roleMemberEmail")}
          placeholder={t("permission.searchEmail")}
          open={newMemberEmail.trim().length >= 2 && memberOptions.length > 0}
          value={newMemberEmail}
          onChange={(event) => onNewMemberEmailChange(event.currentTarget.value)}
          onOptionSelect={(_, data) => {
            const selected = memberOptions.find((member) => member.id === data.optionValue);
            if (selected) {
              onAddMember(selected);
            }
          }}
        >
          {memberOptions.map((member) => (
            <Option key={member.id} value={member.id} text={member.email}>
              {member.email}
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
                    aria-label={t("permission.removeMember", { email: item.label })}
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
  return (
    <div className="permission-grid">
      <div className="permission-card">
        <Text weight="semibold">{t("permission.tables")}</Text>
        {database.tables.map((table) => (
          <div key={table.name} className="permission-resource">
            <Text size={200} weight="semibold">
              {table.display_name || table.name}
            </Text>
            <RecordPermissionToggle
              resource={`${database.name}.${table.name}`}
              grants={grants}
              onGrantChange={onGrantChange}
            />
            <PermissionScopeGroup
              label={t("permission.fields")}
              setScope="field_set"
              setResource={`${database.name}.${table.name}`}
              itemScope="field"
              grants={grants}
              items={table.fields
                .filter((field) => !field.deleted)
                .map((field) => ({
                  key: field.name,
                  label: field.name,
                  resource: `${database.name}.${table.name}`,
                  field: field.name
                }))}
              onGrantChange={onGrantChange}
            />
            <PermissionScopeGroup
              label={t("permission.views")}
              setScope="view_set"
              setResource={`${database.name}.${table.name}`}
              itemScope="view"
              grants={grants}
              items={table.views.map((view) => ({
                key: view.name,
                label: view.display_name || view.name,
                resource: `${database.name}.${table.name}`,
                field: view.name
              }))}
              onGrantChange={onGrantChange}
            />
          </div>
        ))}
      </div>
      <div className="permission-card">
        <Text weight="semibold">{t("permission.workflows")}</Text>
        <PermissionScopeGroup
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
      </div>
      <div className="permission-card">
        <Text weight="semibold">{t("permission.forms")}</Text>
        <PermissionScopeGroup
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
      </div>
    </div>
  );
}

type ScopeItem = { key: string; label: string; resource: string; field: string };

function RecordPermissionToggle(props: {
  grants: PermissionGrant[];
  resource: string;
  onGrantChange: (
    scope: PermissionGrant["scope"],
    resource: string,
    field: string,
    level: PermissionGrant["level"]
  ) => void;
}) {
  const { t } = useTranslation();
  const canCreate = grantLevel(props.grants, "record", props.resource, "create") >= 2;
  const canDelete = grantLevel(props.grants, "record", props.resource, "delete") >= 2;
  const createLabel = t("permission.recordCreate", "Create");
  const deleteLabel = t("permission.recordDelete", "Delete");

  return (
    <div className="permission-scope">
      <span className="permission-scope-label">{t("permission.records")}</span>
      <div className="perm-split" role="group" aria-label={t("permission.records")}>
        <ToggleButton
          className="perm-split-all"
          appearance={canCreate ? "primary" : "secondary"}
          checked={canCreate}
          aria-label={createLabel}
          onClick={() => props.onGrantChange("record", props.resource, "create", canCreate ? 0 : 2)}
        >
          {createLabel}
        </ToggleButton>
        <ToggleButton
          className="perm-split-partial"
          appearance={canDelete ? "primary" : "secondary"}
          checked={canDelete}
          aria-label={deleteLabel}
          onClick={() => props.onGrantChange("record", props.resource, "delete", canDelete ? 0 : 2)}
        >
          {deleteLabel}
        </ToggleButton>
      </div>
    </div>
  );
}

function PermissionScopeGroup(props: {
  label?: string;
  setScope: PermissionGrant["scope"];
  setResource: string;
  itemScope: PermissionGrant["scope"];
  grants: PermissionGrant[];
  items: ScopeItem[];
  onGrantChange: (
    scope: PermissionGrant["scope"],
    resource: string,
    field: string,
    level: PermissionGrant["level"]
  ) => void;
}) {
  const { t } = useTranslation();
  const levelLabels = useLevelLabels();
  const setLevel = grantLevel(props.grants, props.setScope, props.setResource, "");
  const partialCount = props.items.filter(
    (item) => grantLevel(props.grants, props.itemScope, item.resource, item.field) > 0
  ).length;
  const noItems = props.items.length === 0;
  // The two halves are mutually exclusive; the active side is derived from the grants.
  const mode: "all" | "partial" = setLevel === 0 && partialCount > 0 ? "partial" : "all";
  const allLabel = t("permission.allItems", "All");
  const partialLabel = t("permission.partial");

  function chooseAll(level: PermissionGrant["level"]) {
    props.items.forEach((item) => props.onGrantChange(props.itemScope, item.resource, item.field, 0));
    props.onGrantChange(props.setScope, props.setResource, "", level);
  }

  function changeItem(item: ScopeItem, level: PermissionGrant["level"]) {
    if (setLevel > 0) {
      props.onGrantChange(props.setScope, props.setResource, "", 0);
    }
    props.onGrantChange(props.itemScope, item.resource, item.field, level);
  }

  return (
    <div className="permission-scope">
      {props.label && <span className="permission-scope-label">{props.label}</span>}
      <div className="perm-split" role="group" aria-label={props.label ?? props.setScope}>
        <Menu positioning="below-start">
          <MenuTrigger disableButtonEnhancement>
            <MenuButton
              className={mode === "all" ? "perm-split-all active" : "perm-split-all"}
              appearance={mode === "all" ? "primary" : "secondary"}
              aria-label={`${props.label ?? props.setScope} ${allLabel}`}
            >
              {allLabel} · {levelLabels[setLevel]}
            </MenuButton>
          </MenuTrigger>
          <MenuPopover>
            <MenuList>
              {permissionLevels.map((level) => (
                <MenuItem key={level} onClick={() => chooseAll(level)}>
                  {levelLabels[level]}
                </MenuItem>
              ))}
            </MenuList>
          </MenuPopover>
        </Menu>
        <Popover positioning="below-end" trapFocus>
          <PopoverTrigger disableButtonEnhancement>
            <Button
              className={mode === "partial" ? "perm-split-partial active" : "perm-split-partial"}
              appearance={mode === "partial" ? "primary" : "secondary"}
              icon={<ChevronDownRegular />}
              iconPosition="after"
              disabled={noItems}
              aria-label={`${props.label ?? props.setScope} ${partialLabel}`}
            >
              {partialLabel}
              {mode === "partial" ? ` · ${partialCount}` : ""}
            </Button>
          </PopoverTrigger>
          <PopoverSurface className="permission-partial-popover" aria-label={`${props.label ?? props.setScope} ${partialLabel}`}>
            <Text weight="semibold">{props.label ?? partialLabel}</Text>
            {noItems ? (
              <Text size={200}>{t("permission.noPartialItems")}</Text>
            ) : (
              <div className="permission-partial-list">
                {props.items.map((item) => (
                  <PermissionLevelSelect
                    key={item.key}
                    label={item.label}
                    value={grantLevel(props.grants, props.itemScope, item.resource, item.field)}
                    onChange={(level) => changeItem(item, level)}
                  />
                ))}
              </div>
            )}
          </PopoverSurface>
        </Popover>
      </div>
    </div>
  );
}

function useLevelLabels(): Record<(typeof permissionLevels)[number], string> {
  const { t } = useTranslation();
  return {
    0: t("permission.levels.none"),
    1: t("permission.levels.read"),
    2: t("permission.levels.write")
  };
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
    <label className="permission-row">
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
    </label>
  );
}
