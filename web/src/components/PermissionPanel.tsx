import {
  Button,
  Dialog,
  DialogActions,
  DialogBody,
  DialogContent,
  DialogSurface,
  DialogTitle,
  DialogTrigger,
  Input,
  List,
  ListItem,
  Select,
  Text
} from "@fluentui/react-components";
import { AddRegular, DismissRegular, EditRegular, SaveRegular } from "@fluentui/react-icons";
import {
  type DatabaseMetadata,
  type FormDefinition,
  type PermissionGrant,
  type RoleDefinition,
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
  newMemberID: string;
  onAddMember: () => void;
  onGrantChange: (
    scope: PermissionGrant["scope"],
    resource: string,
    field: string,
    level: PermissionGrant["level"]
  ) => void;
  onMemberRemove: (memberID: string) => void;
  onNewMemberIDChange: (value: string) => void;
  onSave: () => void;
  role?: RoleDefinition;
  workflows: WorkflowDefinition[];
};

export function PermissionPanel({
  database,
  forms,
  grants,
  members,
  newMemberID,
  onAddMember,
  onGrantChange,
  onMemberRemove,
  onNewMemberIDChange,
  onSave,
  role,
  workflows
}: PermissionPanelProps) {
  return (
    <div className="permission-view">
      <Dialog>
        <div className="section-header">
          <div>
            <Text weight="semibold">{role?.name ?? "No role selected"}</Text>
            <Text size={200}>{database.name} role access matrix</Text>
          </div>
          <DialogTrigger disableButtonEnhancement>
            <Button icon={<EditRegular />} appearance="primary" disabled={!role}>
              Configure
            </Button>
          </DialogTrigger>
        </div>
        {role ? (
          <DialogSurface className="permission-dialog">
            <DialogBody>
              <DialogTitle>{role.name} permissions</DialogTitle>
              <DialogContent className="permission-dialog-content">
                <PermissionMatrix
                  database={database}
                  forms={forms}
                  grants={grants}
                  members={members}
                  newMemberID={newMemberID}
                  onAddMember={onAddMember}
                  onGrantChange={onGrantChange}
                  onMemberRemove={onMemberRemove}
                  onNewMemberIDChange={onNewMemberIDChange}
                  workflows={workflows}
                />
              </DialogContent>
              <DialogActions>
                <Button icon={<SaveRegular />} appearance="primary" onClick={onSave}>
                  Save
                </Button>
                <DialogTrigger disableButtonEnhancement>
                  <Button>Close</Button>
                </DialogTrigger>
              </DialogActions>
            </DialogBody>
          </DialogSurface>
        ) : (
          <></>
        )}
      </Dialog>
      {!role && (
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
  newMemberID,
  onAddMember,
  onGrantChange,
  onMemberRemove,
  onNewMemberIDChange,
  workflows
}: Omit<PermissionPanelProps, "onSave" | "role">) {
  return (
    <div className="permission-grid">
      <div className="permission-card">
        <Text weight="semibold">Members</Text>
        <div className="create-rowline">
          <Input
            aria-label="Role member user id"
            placeholder="user id"
            value={newMemberID}
            onChange={(_, data) => onNewMemberIDChange(data.value)}
          />
          <Button icon={<AddRegular />} aria-label="Add role member" onClick={onAddMember} />
        </div>
        {members.length === 0 ? (
          <Text size={200}>No members</Text>
        ) : (
          <List navigationMode="items" aria-label="Role members">
            {members.map((member) => (
              <ListItem key={member}>
                <div className="member-list-item">
                  <Text truncate>{member}</Text>
                  <Button
                    appearance="subtle"
                    icon={<DismissRegular />}
                    aria-label={`Remove ${member}`}
                    onClick={() => onMemberRemove(member)}
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
