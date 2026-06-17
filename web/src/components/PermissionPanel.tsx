import {
  Button,
  Combobox,
  List,
  ListItem,
  Option,
  Select,
  Text
} from "@fluentui/react-components";
import { AddRegular, DismissRegular, SaveRegular } from "@fluentui/react-icons";
import {
  type DatabaseMetadata,
  type FormDefinition,
  type PermissionGrant,
  type RoleDefinition,
  type AuthUser,
  type WorkflowDefinition
} from "../api";
export { compactRoleGrants } from "../permissionState";

const permissionLevels = [
  { value: 0, label: "None" },
  { value: 1, label: "Read" },
  { value: 2, label: "Write" }
] as const;

type PermissionPanelProps = {
  database: DatabaseMetadata;
  forms: FormDefinition[];
  grants: PermissionGrant[];
  members: string[];
  memberOptions: AuthUser[];
  memberUsers: AuthUser[];
  newMemberEmail: string;
  onAddMember: (user?: AuthUser) => void;
  onGrantChange: (
    scope: PermissionGrant["scope"],
    resource: string,
    field: string,
    level: PermissionGrant["level"]
  ) => void;
  onMemberRemove: (memberID: string) => void;
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
  newMemberEmail,
  onAddMember,
  onGrantChange,
  onMemberRemove,
  onNewMemberEmailChange,
  onSave,
  role,
  workflows
}: PermissionPanelProps) {
  return (
    <div className="permission-view">
      <div className="section-header">
        <div>
          <Text weight="semibold">{role?.name ?? "No role selected"}</Text>
          <Text size={200}>{database.name} role access matrix</Text>
        </div>
        {role && (
          <Button icon={<SaveRegular />} appearance="primary" onClick={onSave}>
            Save
          </Button>
        )}
      </div>
      {role ? (
        <PermissionMatrix
          database={database}
          forms={forms}
          grants={grants}
          members={members}
          memberOptions={memberOptions}
          memberUsers={memberUsers}
          newMemberEmail={newMemberEmail}
          onAddMember={onAddMember}
          onGrantChange={onGrantChange}
          onMemberRemove={onMemberRemove}
          onNewMemberEmailChange={onNewMemberEmailChange}
          workflows={workflows}
        />
      ) : (
        <div className="empty-state">
          <Text>Create a role to configure table, field, workflow, and form permissions.</Text>
        </div>
      )}
    </div>
  );
}

function PermissionMatrix({
  database,
  forms,
  grants,
  members,
  memberOptions,
  memberUsers,
  newMemberEmail,
  onAddMember,
  onGrantChange,
  onMemberRemove,
  onNewMemberEmailChange,
  workflows
}: Omit<PermissionPanelProps, "onSave" | "role">) {
  const memberByID = new Map(memberUsers.map((member) => [member.id, member]));
  const memberItems = members.map((memberID) => ({
    id: memberID,
    email: memberByID.get(memberID)?.email ?? memberID
  }));
  return (
    <div className="permission-grid">
      <div className="permission-card">
        <Text weight="semibold">Members</Text>
        <div className="create-rowline">
          <Combobox
            aria-label="Role member email"
            placeholder="Search email"
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
          <Button icon={<AddRegular />} aria-label="Add role member" onClick={() => onAddMember()} />
        </div>
        {members.length === 0 ? (
          <Text size={200}>No members</Text>
        ) : (
          <List navigationMode="items" aria-label="Role members">
            {memberItems.map((member) => (
              <ListItem key={member.id}>
                <div className="member-list-item">
                  <Text truncate>{member.email}</Text>
                  <Button
                    appearance="subtle"
                    icon={<DismissRegular />}
                    aria-label={`Remove ${member.email}`}
                    onClick={() => onMemberRemove(member.id)}
                  />
                </div>
              </ListItem>
            ))}
          </List>
        )}
      </div>
      <div className="permission-card">
        <Text weight="semibold">Tables</Text>
        {database.tables.map((table) => (
          <div key={table.name} className="permission-resource">
            <PermissionLevelSelect
              label={table.name}
              value={grantLevel(grants, "table", `${database.name}.${table.name}`, "")}
              onChange={(level) => onGrantChange("table", `${database.name}.${table.name}`, "", level)}
            />
            <div className="permission-fields">
              {table.fields
                .filter((field) => !field.deleted)
                .map((field) => (
                  <PermissionLevelSelect
                    key={field.name}
                    label={field.name}
                    value={grantLevel(grants, "field", `${database.name}.${table.name}`, field.name)}
                    onChange={(level) => onGrantChange("field", `${database.name}.${table.name}`, field.name, level)}
                  />
                ))}
            </div>
          </div>
        ))}
      </div>
      <div className="permission-card">
        <Text weight="semibold">Workflows</Text>
        {workflows.map((workflow) => (
          <PermissionLevelSelect
            key={workflow.id ?? workflow.name}
            label={workflow.name}
            value={grantLevel(grants, "workflow", String(workflow.id ?? 0), "")}
            onChange={(level) => onGrantChange("workflow", String(workflow.id ?? 0), "", level)}
          />
        ))}
      </div>
      <div className="permission-card">
        <Text weight="semibold">Forms</Text>
        {forms.map((form) => (
          <PermissionLevelSelect
            key={form.id ?? form.name}
            label={form.name}
            value={grantLevel(grants, "form", String(form.id ?? 0), "")}
            onChange={(level) => onGrantChange("form", String(form.id ?? 0), "", level)}
          />
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

function PermissionLevelSelect(props: {
  label: string;
  value: PermissionGrant["level"];
  onChange: (level: PermissionGrant["level"]) => void;
}) {
  return (
    <label className="permission-row">
      <span>{props.label}</span>
      <Select
        aria-label={`${props.label} permission`}
        value={String(props.value)}
        onChange={(_, data) => props.onChange(Number(data.value) as PermissionGrant["level"])}
      >
        {permissionLevels.map((level) => (
          <option key={level.value} value={level.value}>
            {level.label}
          </option>
        ))}
      </Select>
    </label>
  );
}
