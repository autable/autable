import { Info, Table2, Users, Workflow, X } from "lucide-react";
import { useId, useState, type ReactNode } from "react";
import { useTranslation } from "react-i18next";
import {
  type AppDefinition,
  type AuthUser,
  type DatabaseMetadata,
  type PermissionGrant,
  type RoleDefinition,
  type RoleMember,
  type WorkflowSummary
} from "../api";
import { compactRoleGrants } from "../permissionState";
import { Page, SectionHeading } from "../shell/Page";
import { Button, Callout, Card, cn, EmptyState, Input, NativeSelect, Tabs, TabsList, TabsTrigger } from "../ui";

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
  memberWorkflows: WorkflowSummary[];
  newMemberQuery: string;
  onAddMember: (user: AuthUser) => void;
  onAddWorkflowMember: (workflow: WorkflowSummary) => void;
  onGrantChange: (scope: PermissionGrant["scope"], resource: string, field: string, level: PermissionGrant["level"]) => void;
  onMemberRemove: (member: RoleMember) => void;
  onNewMemberQueryChange: (value: string) => void;
  onSave: () => void;
  role?: RoleDefinition;
  workflows: WorkflowSummary[];
};

/**
 * One role of a database: what it grants (tables, workflows, apps) on the
 * left, who and which workflows are in it on the right. Changes are drafts
 * until Save.
 */
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
      <Page title={t("nav.permissions")} description={t("permission.pageDescription", { database: database.name })}>
        <Card>
          <EmptyState icon={Users} title={t("empty.noRoleTitle")} description={t("empty.noRoleDescription")} />
        </Card>
      </Page>
    );
  }
  const dirty =
    normalizeGrants(grants) !== normalizeGrants(role.grants ?? []) || normalizeMembers(members) !== normalizeMembers(role.members ?? []);
  return (
    <Page
      width="wide"
      title={role.name}
      description={t("permission.roleAccessMatrix", { database: database.name })}
      icon={<Users />}
      actions={
        <>
          {dirty && <span className="text-12 text-warning">{t("permission.unsaved")}</span>}
          <Button variant="cta" onClick={onSave} disabled={!dirty}>
            {t("common.save")}
          </Button>
        </>
      }
    >
      <div className="grid items-start gap-6 xl:grid-cols-[minmax(0,1fr)_20rem]">
        <section aria-labelledby="role-access" className="min-w-0">
          <SectionHeading id="role-access" title={t("permission.access")} />
          <PermissionMatrix key={role.name} apps={apps} database={database} grants={grants} onGrantChange={onGrantChange} workflows={workflows} />
        </section>
        <div className="flex min-w-0 flex-col gap-6 xl:sticky xl:top-6">
          <UserMembers
            members={members}
            memberOptions={memberOptions}
            memberUsers={memberUsers}
            newMemberQuery={newMemberQuery}
            onAddMember={onAddMember}
            onMemberRemove={onMemberRemove}
            onNewMemberQueryChange={onNewMemberQueryChange}
          />
          <WorkflowMembers
            members={members}
            memberWorkflows={memberWorkflows}
            onAddWorkflowMember={onAddWorkflowMember}
            onMemberRemove={onMemberRemove}
            workflows={workflows}
          />
        </div>
      </div>
    </Page>
  );
}

function MemberSection({ id, title, count, children }: { id: string; title: string; count: number; children: ReactNode }) {
  return (
    <section aria-labelledby={id}>
      <SectionHeading
        id={id}
        title={
          <span className="flex items-center gap-2">
            {title}
            <span className="rounded-full bg-chip px-1.5 text-11 font-medium tabular-nums text-fg">{count}</span>
          </span>
        }
      />
      <Card>{children}</Card>
    </section>
  );
}

function MemberRow({ label, detail, icon, onRemove }: { label: string; detail?: string; icon: ReactNode; onRemove: () => void }) {
  const { t } = useTranslation();
  return (
    <li className="group flex items-center gap-2.5 px-4 py-2">
      <span className="flex size-7 shrink-0 items-center justify-center rounded-full bg-chip text-12 font-medium text-description" aria-hidden>
        {icon}
      </span>
      <span className="min-w-0 flex-1">
        <span className="block truncate text-13 text-fg">{label}</span>
        {detail && <span className="block truncate text-12 text-muted">{detail}</span>}
      </span>
      <Button variant="quiet" size="icon-sm" aria-label={t("permission.removeMember", { name: label })} onClick={onRemove}>
        <X />
      </Button>
    </li>
  );
}

function UserMembers({
  members,
  memberOptions,
  memberUsers,
  newMemberQuery,
  onAddMember,
  onMemberRemove,
  onNewMemberQueryChange
}: Pick<
  PermissionPanelProps,
  "members" | "memberOptions" | "memberUsers" | "newMemberQuery" | "onAddMember" | "onMemberRemove" | "onNewMemberQueryChange"
>) {
  const { t } = useTranslation();
  const memberByID = new Map(memberUsers.map((member) => [member.id, member]));
  const users = members.filter((member) => member.type === "user");
  return (
    <MemberSection id="role-members" title={t("permission.members")} count={users.length}>
      <div className="p-3">
        <MemberSearch
          // People already in the role are not offered again.
          options={memberOptions.filter((option) => !users.some((member) => member.id === option.id))}
          query={newMemberQuery}
          onQueryChange={onNewMemberQueryChange}
          onSelect={onAddMember}
        />
      </div>
      {users.length === 0 ? (
        <p className="border-t border-border-soft px-4 py-3 text-13 text-muted">{t("permission.noMembers")}</p>
      ) : (
        <ul aria-label={t("permission.members")} className="divide-y divide-border-soft border-t border-border-soft">
          {users.map((member) => {
            const user = memberByID.get(member.id);
            const label = user?.display_name ?? member.id;
            return (
              <MemberRow
                key={member.id}
                label={label}
                detail={user?.email && user.email !== label ? user.email : undefined}
                icon={Array.from(label.trim())[0]?.toUpperCase() ?? "?"}
                onRemove={() => onMemberRemove(member)}
              />
            );
          })}
        </ul>
      )}
    </MemberSection>
  );
}

/** Search people by name or email; matches appear below once two characters are typed. */
function MemberSearch({
  options,
  query,
  onQueryChange,
  onSelect
}: {
  options: AuthUser[];
  query: string;
  onQueryChange: (value: string) => void;
  onSelect: (user: AuthUser) => void;
}) {
  const { t } = useTranslation();
  const listID = useId();
  const [active, setActive] = useState(0);
  const [dismissed, setDismissed] = useState(false);
  const open = !dismissed && query.trim().length >= 2 && options.length > 0;
  function choose(user: AuthUser) {
    onSelect(user);
    setDismissed(true);
  }
  return (
    <div className="relative">
      <Input
        inputSize="sm"
        role="combobox"
        aria-label={t("permission.roleMemberDisplayName")}
        aria-expanded={open}
        aria-controls={listID}
        aria-autocomplete="list"
        aria-activedescendant={open ? `${listID}-${active}` : undefined}
        placeholder={t("permission.searchMember")}
        value={query}
        onChange={(event) => {
          setDismissed(false);
          setActive(0);
          onQueryChange(event.target.value);
        }}
        onKeyDown={(event) => {
          if (!open) {
            return;
          }
          if (event.key === "ArrowDown") {
            event.preventDefault();
            setActive((current) => (current + 1) % options.length);
          } else if (event.key === "ArrowUp") {
            event.preventDefault();
            setActive((current) => (current - 1 + options.length) % options.length);
          } else if (event.key === "Enter") {
            event.preventDefault();
            choose(options[Math.min(active, options.length - 1)]);
          } else if (event.key === "Escape") {
            setDismissed(true);
          }
        }}
      />
      {open && (
        <ul
          id={listID}
          role="listbox"
          aria-label={t("permission.addRoleMember")}
          className="absolute inset-x-0 top-full z-20 mt-1 max-h-60 overflow-y-auto rounded-container border border-border bg-elevated p-1 shadow-float animate-pop-in"
        >
          {options.map((user, index) => (
            <li
              key={user.id}
              id={`${listID}-${index}`}
              role="option"
              aria-selected={index === active}
              onMouseDown={(event) => event.preventDefault()}
              onMouseEnter={() => setActive(index)}
              onClick={() => choose(user)}
              className={cn("flex cursor-default flex-col rounded-inner px-2.5 py-1.5", index === active && "bg-hover")}
            >
              <span className="truncate text-13 text-fg">{user.display_name}</span>
              {user.email !== user.display_name && <span className="truncate text-12 text-muted">{user.email}</span>}
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}

function WorkflowMembers({
  members,
  memberWorkflows,
  onAddWorkflowMember,
  onMemberRemove,
  workflows
}: Pick<PermissionPanelProps, "members" | "memberWorkflows" | "onAddWorkflowMember" | "onMemberRemove" | "workflows">) {
  const { t } = useTranslation();
  const workflowByID = new Map(memberWorkflows.map((workflow) => [String(workflow.id), workflow]));
  const workflowMembers = members.filter((member) => member.type === "workflow");
  const selectable = workflows.filter(
    (workflow) => workflow.id && !members.some((member) => member.type === "workflow" && member.id === String(workflow.id))
  );
  return (
    <MemberSection id="role-workflow-members" title={t("permission.workflowMembers")} count={workflowMembers.length}>
      <p className="px-4 pt-3 text-12 text-muted">{t("permission.workflowMembersHint")}</p>
      <div className="p-3">
        <NativeSelect
          selectSize="sm"
          aria-label={t("permission.addWorkflowMember")}
          value=""
          disabled={selectable.length === 0}
          onChange={(event) => {
            const selected = selectable.find((workflow) => String(workflow.id) === event.target.value);
            if (selected) {
              onAddWorkflowMember(selected);
            }
          }}
        >
          <option value="">{t("permission.addWorkflowMember")}</option>
          {selectable.map((workflow) => (
            <option key={workflow.id} value={String(workflow.id)}>
              {workflowLabel(workflow)}
            </option>
          ))}
        </NativeSelect>
      </div>
      {workflowMembers.length === 0 ? (
        <p className="border-t border-border-soft px-4 py-3 text-13 text-muted">{t("permission.noWorkflowMembers")}</p>
      ) : (
        <ul aria-label={t("permission.workflowMembers")} className="divide-y divide-border-soft border-t border-border-soft">
          {workflowMembers.map((member) => (
            <MemberRow
              key={member.id}
              label={workflowLabel(workflowByID.get(member.id)) ?? `#${member.id}`}
              icon={<Workflow className="size-3.5" />}
              onRemove={() => onMemberRemove(member)}
            />
          ))}
        </ul>
      )}
    </MemberSection>
  );
}

function workflowLabel(workflow: WorkflowSummary | undefined): string | undefined {
  return workflow ? workflow.title || workflow.name : undefined;
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
    <>
      <Tabs value={tab} onValueChange={(value) => setTab(value as MatrixTab)} className="mb-3">
        <TabsList>
          <TabsTrigger value="tables">{t("permission.tables")}</TabsTrigger>
          <TabsTrigger value="workflows">{t("permission.workflows")}</TabsTrigger>
          <TabsTrigger value="apps">{t("permission.apps")}</TabsTrigger>
        </TabsList>
      </Tabs>
      {tab === "tables" &&
        (selectedTable ? (
          <Card className="overflow-hidden md:grid md:grid-cols-[13rem_minmax(0,1fr)]">
            <nav aria-label={t("permission.tables")} className="flex gap-1 overflow-x-auto border-b border-border-soft p-2 md:flex-col md:border-b-0 md:border-r">
              {database.tables.map((table) => {
                const active = table.name === selectedTable.name;
                return (
                  <button
                    key={table.name}
                    type="button"
                    aria-current={active ? "true" : undefined}
                    onClick={() => setSelectedTableName(table.name)}
                    className={cn(
                      "flex h-8 shrink-0 items-center gap-2 rounded-inner px-2.5 text-left text-13 transition-colors duration-150",
                      active ? "bg-chip font-medium text-fg" : "text-description hover:bg-hover hover:text-fg"
                    )}
                  >
                    <Table2 className="size-4 shrink-0" strokeWidth={1.75} aria-hidden />
                    <span className="truncate">{table.display_name || table.name}</span>
                  </button>
                );
              })}
            </nav>
            <TablePermissionEditor key={selectedTable.name} database={database} table={selectedTable} grants={grants} onGrantChange={onGrantChange} />
          </Card>
        ) : (
          <Card className="px-5 py-6 text-center text-13 text-muted">{t("permission.noPartialItems")}</Card>
        ))}
      {tab === "workflows" && (
        <ResourceLevelSection
          description={t("permission.workflowsHint")}
          itemScope="workflow"
          grants={grants}
          items={workflows.map((workflow) => ({
            key: String(workflow.id),
            label: workflowLabel(workflow) ?? workflow.name,
            resource: String(workflow.id),
            field: ""
          }))}
          levelLabels={{ 0: t("permission.workflowLevels.none"), 1: t("permission.workflowLevels.run") }}
          onGrantChange={onGrantChange}
        />
      )}
      {tab === "apps" && (
        <ResourceLevelSection
          description={t("permission.appsHint")}
          all={{ label: t("permission.appSet"), scope: "app_set", resource: database.name }}
          itemScope="app"
          grants={grants}
          items={apps.map((app) => ({ key: String(app.id), label: app.title || app.name, resource: String(app.id), field: "" }))}
          levelLabels={{ 0: t("permission.appLevels.none"), 1: t("permission.appLevels.open") }}
          onGrantChange={onGrantChange}
        />
      )}
    </>
  );
}

type ScopeItem = { key: string; label: string; resource: string; field: string };

type GrantChangeFn = (scope: PermissionGrant["scope"], resource: string, field: string, level: PermissionGrant["level"]) => void;

function GroupTitle({ children }: { children: ReactNode }) {
  return <h3 className="px-5 pb-1 pt-4 text-12 font-medium text-subtle">{children}</h3>;
}

function Row({ label, children, strong }: { label: string; children: ReactNode; strong?: boolean }) {
  return (
    <div className="flex flex-wrap items-center justify-between gap-x-4 gap-y-2 px-5 py-2">
      <span className={cn("min-w-0 truncate text-13", strong ? "font-medium text-fg" : "text-description")}>{label}</span>
      <div className="flex shrink-0 items-center gap-1">{children}</div>
    </div>
  );
}

/** A pill that is on or off; the pressed state is its value. */
function BitToggle({ label, ariaLabel, pressed, onClick }: { label: string; ariaLabel: string; pressed: boolean; onClick: () => void }) {
  return (
    <button
      type="button"
      aria-pressed={pressed}
      aria-label={ariaLabel}
      onClick={onClick}
      className={cn(
        "inline-flex h-7 min-w-16 items-center justify-center rounded-full border px-3 text-12 font-medium transition-colors duration-150",
        pressed ? "border-transparent bg-fg text-surface hover:bg-[color-mix(in_oklab,var(--text-primary),var(--surface)_12%)]" : "border-border text-muted hover:bg-hover hover:text-fg"
      )}
    >
      {label}
    </button>
  );
}

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
    <div className="min-w-0 pb-3">
      <GroupTitle>{t("permission.dataSection")}</GroupTitle>
      <Row label={t("permission.records")}>
        <div className="flex gap-1" role="group" aria-label={t("permission.records")}>
          <BitToggle
            label={t("permission.recordCreate")}
            ariaLabel={t("permission.recordCreate")}
            pressed={canCreate}
            onClick={() => onGrantChange("record", resource, "create", canCreate ? 0 : 2)}
          />
          <BitToggle
            label={t("permission.recordDelete")}
            ariaLabel={t("permission.recordDelete")}
            pressed={canDelete}
            onClick={() => onGrantChange("record", resource, "delete", canDelete ? 0 : 2)}
          />
        </div>
      </Row>
      <ReadToggleRow label={t("permission.files")} checked={canViewFiles} onChange={(next) => onGrantChange("file", resource, "", next ? 1 : 0)} />

      <GroupTitle>{t("permission.fieldValues")}</GroupTitle>
      <FieldBitsRow label={t("permission.allFields")} strong value={fieldSetLevel} onChange={changeFieldSet} />
      {activeFields.map((field) => (
        <FieldBitsRow
          key={field.name}
          label={field.name}
          value={grantLevel(grants, "field", resource, field.name)}
          onChange={(level) => changeField(field.name, level)}
        />
      ))}

      <GroupTitle>{t("permission.views")}</GroupTitle>
      <ReadToggleRow label={t("permission.allViews")} strong checked={viewSetLevel >= 1} onChange={(next) => changeViewSet(next ? 1 : 0)} />
      {viewItems.map((view) => (
        <ReadToggleRow
          key={view.name}
          label={view.label}
          checked={grantLevel(grants, "view", resource, view.name) >= 1}
          onChange={(next) => changeView(view.name, next ? 1 : 0)}
        />
      ))}

      <GroupTitle>{t("permission.structureSection")}</GroupTitle>
      <div className="px-5 pt-1">
        <Callout icon={Info}>{t("permission.structureNote")}</Callout>
      </div>
    </div>
  );
}

function ReadToggleRow(props: { label: string; checked: boolean; strong?: boolean; onChange: (next: boolean) => void }) {
  const { t } = useTranslation();
  return (
    <Row label={props.label} strong={props.strong}>
      <BitToggle
        label={t("permission.bits.read")}
        ariaLabel={`${props.label} ${t("permission.bits.read")}`}
        pressed={props.checked}
        onClick={() => props.onChange(!props.checked)}
      />
    </Row>
  );
}

function FieldBitsRow(props: { label: string; value: number; strong?: boolean; onChange: (level: number) => void }) {
  const { t } = useTranslation();
  const bitLabels: Record<number, string> = {
    1: t("permission.bits.read"),
    2: t("permission.bits.update"),
    4: t("permission.bits.create")
  };
  return (
    <Row label={props.label} strong={props.strong}>
      <div className="flex gap-1" role="group" aria-label={`${props.label} permission`}>
        {fieldBits.map((bit) => (
          <BitToggle
            key={bit}
            label={bitLabels[bit]}
            ariaLabel={`${props.label} ${bitLabels[bit]}`}
            pressed={(props.value & bit) !== 0}
            onClick={() => props.onChange(props.value ^ bit)}
          />
        ))}
      </div>
    </Row>
  );
}

type LevelLabels = Record<(typeof accessLevels)[number], string>;

// ResourceLevelSection is the flat none/run or none/open editor for
// workflows and apps: one row per resource, plus an optional "all" preset
// row that is mutually exclusive with the per-resource rows.
function ResourceLevelSection(props: {
  all?: { label: string; scope: PermissionGrant["scope"]; resource: string };
  description: string;
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
    <Card className="pb-2">
      <p className="px-5 pb-2 pt-4 text-12 text-muted">{props.description}</p>
      {all && (
        <div className="border-b border-border-soft pb-2">
          <PermissionLevelRow label={all.label} strong levelLabels={props.levelLabels} value={setLevel} onChange={chooseAll} />
        </div>
      )}
      {props.items.length === 0 ? (
        <p className="px-5 py-3 text-13 text-muted">{t("permission.noPartialItems")}</p>
      ) : (
        props.items.map((item) => (
          <PermissionLevelRow
            key={item.key}
            label={item.label}
            levelLabels={props.levelLabels}
            value={grantLevel(props.grants, props.itemScope, item.resource, item.field)}
            onChange={(level) => changeItem(item, level)}
          />
        ))
      )}
    </Card>
  );
}

function grantLevel(grants: PermissionGrant[], scope: PermissionGrant["scope"], resource: string, field: string): PermissionGrant["level"] {
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
  return members
    .map((member) => `${member.type}:${member.id}`)
    .sort()
    .join(",");
}

function PermissionLevelRow(props: {
  label: string;
  strong?: boolean;
  levelLabels: LevelLabels;
  value: PermissionGrant["level"];
  onChange: (level: PermissionGrant["level"]) => void;
}) {
  return (
    <Row label={props.label} strong={props.strong}>
      <NativeSelect
        selectSize="sm"
        className="w-32"
        aria-label={`${props.label} permission`}
        value={String(Math.min(props.value, 1))}
        onChange={(event) => props.onChange(Number(event.target.value) as PermissionGrant["level"])}
      >
        {accessLevels.map((level) => (
          <option key={level} value={level}>
            {props.levelLabels[level]}
          </option>
        ))}
      </NativeSelect>
    </Row>
  );
}
