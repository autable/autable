import type { Catalog, FormDefinition, WorkflowDefinition, WorkflowNodeInfo } from "./api";

export const demoCatalog: Catalog = {
  databases: [
    {
      name: "workspace",
      sqlite_path: "./data/workspace.sqlite",
      tables: [
        {
          name: "contacts",
          display_name: "Contacts",
          fields: [
            { name: "name", type: "text", required: true, deleted: false },
            { name: "email", type: "email", required: false, deleted: false },
            { name: "status", type: "select", required: false, deleted: false },
            { name: "owner", type: "text", required: false, deleted: false }
          ],
          views: [
            {
              name: "active",
              display_name: "Active",
              filters: [{ field: "status", op: "eq", value: "Active" }],
              sorts: [{ field: "name", direction: "asc" }]
            },
            {
              name: "active-ops",
              display_name: "Active ops",
              base_view: "active",
              filters: [{ field: "owner", op: "eq", value: "ops" }],
              sorts: [{ field: "record_id", direction: "desc" }]
            }
          ]
        }
      ]
    }
  ]
};

export const initialRows: Array<Record<string, unknown>> = [
  { record_id: 1, name: "Ada Lovelace", email: "ada@example.com", status: "Active", owner: "ops" },
  { record_id: 2, name: "Grace Hopper", email: "grace@example.com", status: "Review", owner: "platform" },
  { record_id: 3, name: "Katherine Johnson", email: "katherine@example.com", status: "Active", owner: "research" }
];

export const defaultWorkflowScript = `export default function run(info) {
  const changed = info.node("echo", {
    record_id: info.inputs.record_id,
    name: info.inputs.name,
    status: info.inputs.status
  });

  if (changed.status === "Review") {
    const notification = info.node("echo", {
      channel: info.variables.CHANNEL,
      text: \`Review needed for \${changed.name}\`
    });
    return { checked: true, record_id: changed.record_id, notification: notification.text };
  }

  return { checked: true, record_id: changed.record_id };
}`;

export const initialWorkflows: WorkflowDefinition[] = [
  {
    id: 1,
    database_name: "workspace",
    name: "record-review",
    script: defaultWorkflowScript,
    secrets: { TOKEN: "" },
    variables: { CHANNEL: "ops" }
  },
  {
    id: 2,
    database_name: "workspace",
    name: "welcome-contact",
    script: `export default function run(info) {
  const changed = info.node("echo", {
    record_id: info.inputs.record_id,
    name: info.inputs.name
  });
  const notification = info.node("echo", {
    channel: info.variables.CHANNEL,
    text: \`New contact: \${changed.name}\`
  });
  return { record_id: changed.record_id, notification: notification.text };
}`,
    secrets: {},
    variables: { CHANNEL: "sales" }
  }
];

export const initialWorkflowNodes: WorkflowNodeInfo[] = [
  {
    type: "echo",
    display_name: "Echo",
    description: "Returns its input unchanged.",
    inputs: [{ name: "value", type: "any", required: false }],
    outputs: [{ name: "value", type: "any", required: false }],
    stateless: true,
    trigger: false
  },
  {
    type: "table.record.changed",
    display_name: "Record changed",
    description: "Loads a row history entry by rhistory key and exposes it as a trigger record.",
    inputs: [{ name: "history_key", type: "string", required: true }],
    outputs: [
      { name: "record", type: "TriggerRecord", required: true },
      { name: "values", type: "object", required: true },
      { name: "actor_id", type: "string", required: false }
    ],
    stateless: true,
    trigger: true
  }
];

export const defaultFormScript = `const name = api.input({
  name: "name",
  label: "Name",
  required: true
});

const email = api.input({
  name: "email",
  label: "Email",
  type: "email",
  required: true
});

const status = api.select({
  name: "status",
  label: "Status",
  options: ["Active", "Review", "Archived"]
});

root.append(name, email, status, api.submit("Create record"));`;

export const initialForms: FormDefinition[] = [
  {
    id: 1,
    database_name: "workspace",
    name: "contact-intake",
    script: defaultFormScript
  },
  {
    id: 2,
    database_name: "workspace",
    name: "quick-status",
    script: `const status = api.select({
  name: "status",
  label: "Status",
  options: ["Active", "Review", "Archived"]
});

root.append(status, api.submit("Update status"));`
  }
];
