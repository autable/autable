import {
  Button,
  Combobox,
  CounterBadge,
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
  Text
} from "@fluentui/react-components";
import { AddRegular, ChevronDownRegular, DismissRegular, PeopleRegular, SaveRegular } from "@fluentui/react-icons";
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
export { compactRoleGrants } from "../permissionState";

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
  return (
    <div className="permission-view">
      <div className="section-header">
        <div>
          <Text weight="semibold">{role?.name ?? t("permission.noRoleSelected")}</Text>
          <Text size={200}>{t("permission.roleAccessMatrix", { database: database.name })}</Text>
        </div>
        {role && (
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
            <Button icon={<SaveRegular />} appearance="primary" onClick={onSave}>
              {t("common.save")}
            </Button>
          </div>
        )}
      </div>
      {role ? (
        <PermissionMatrix
          key={role.name}
          database={database}
          forms={forms}
          grants={grants}
          onGrantChange={onGrantChange}
          workflows={workflows}
        />
      ) : (
        <div className="empty-state">
          <Text>{t("permission.empty")}</Text>
        </div>
      )}
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
  const memberItems = members.map((memberID) => ({
    member: memberID,
    key: `${memberID.type}:${memberID.id}`,
    label:
      memberID.type === "workflow"
        ? workflowByID.get(memberID.id)?.name ?? `workflow:${memberID.id}`
        : memberByID.get(memberID.id)?.email ?? memberID.id
  }));
  const selectableWorkflows = workflows.filter((workflow) => workflow.id && !members.some((member) => member.type === "workflow" && member.id === String(workflow.id)));
  return (
    <Popover positioning="below-end" trapFocus>
      <PopoverTrigger disableButtonEnhancement>
        <Button icon={<PeopleRegular />}>
          {t("permission.members")}
          <CounterBadge
            className="members-count"
            appearance="filled"
            color="brand"
            count={members.length}
            showZero
          />
        </Button>
      </PopoverTrigger>
      <PopoverSurface className="members-popover" aria-label={t("permission.members")}>
        <div className="create-rowline">
          <Combobox
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
          <Button icon={<AddRegular />} aria-label={t("permission.addRoleMember")} onClick={() => onAddMember()} />
        </div>
        <Select
          aria-label={t("permission.workflowMember")}
          value=""
          onChange={(event) => {
            const selected = workflows.find((workflow) => String(workflow.id) === event.currentTarget.value);
            if (selected) {
              onAddWorkflowMember(selected);
            }
          }}
        >
          <option value="">{t("permission.addWorkflowMember")}</option>
          {selectableWorkflows.map((workflow) => (
            <option key={workflow.id} value={workflow.id}>
              {workflow.name}
            </option>
          ))}
        </Select>
        {members.length === 0 ? (
          <Text size={200}>{t("permission.noMembers")}</Text>
        ) : (
          <List navigationMode="items" aria-label={t("permission.members")}>
            {memberItems.map((member) => (
              <ListItem key={member.key}>
                <div className="member-list-item">
                  <Text truncate>{member.label}</Text>
                  <Button
                    appearance="subtle"
                    icon={<DismissRegular />}
                    aria-label={t("permission.removeMember", { email: member.label })}
                    onClick={() => onMemberRemove(member.member)}
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
            <RecordPermissionSelect
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

type RecordPermissionMode = "none" | "create" | "delete" | "create_delete";

function RecordPermissionSelect(props: {
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
  const mode: RecordPermissionMode = canCreate && canDelete ? "create_delete" : canCreate ? "create" : canDelete ? "delete" : "none";
  const labels: Record<RecordPermissionMode, string> = {
    none: t("permission.recordModes.none"),
    create: t("permission.recordModes.create"),
    delete: t("permission.recordModes.delete"),
    create_delete: t("permission.recordModes.createDelete")
  };

  function changeMode(nextMode: RecordPermissionMode) {
    props.onGrantChange("record", props.resource, "create", nextMode === "create" || nextMode === "create_delete" ? 2 : 0);
    props.onGrantChange("record", props.resource, "delete", nextMode === "delete" || nextMode === "create_delete" ? 2 : 0);
  }

  return (
    <label className="permission-row">
      <span>{t("permission.records")}</span>
      <Select
        aria-label={t("permission.records")}
        value={mode}
        onChange={(_, data) => changeMode(data.value as RecordPermissionMode)}
      >
        {(Object.keys(labels) as RecordPermissionMode[]).map((key) => (
          <option key={key} value={key}>
            {labels[key]}
          </option>
        ))}
      </Select>
    </label>
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
