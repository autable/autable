import type { Catalog } from "./api";

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

export const defaultWorkflowScript = `export default async function run(info) {
  const changed = await info.nodes.trigger.recordChanged();
  const row = await info.nodes.table.getRecord(changed.record);

  if (row.status === "Review") {
    await info.nodes.notification.send({
      channel: info.variables.CHANNEL,
      text: \`Review needed for \${row.name}\`
    });
  }

  return { checked: true, record_id: changed.record.record_id };
}`;

export const defaultFormScript = `const email = api.input({
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

root.append(email, status, api.submit("Create record"));`;
