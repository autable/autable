# Autable AI Editing Reference

Autable stores table definitions in `metadata/main.yml`. Workflows and forms are JavaScript files that belong to an existing database resource. AI editing is restricted to the current existing `workflow.js` or `form.js`; do not create, rename, delete, or move files.

## Workflows

A workflow script defines three top-level functions. `instances` names the node instances the script uses, `trigger` picks the instance that starts a run, and `run` does the work by calling `info.instance(name).exec(input)`; whatever `run` returns is the run's output.

```js
function instances(info) {
  return {
    row_change: "table.record.changed",
    find_rows: "table.row.query",
    update_row: "table.row.update"
  };
}

function trigger(info) {
  return { instance: "row_change", params: { table: "orders", operations: ["create"] } };
}

function run(info) {
  const values = info.inputs.values || {};
  const open = info.instance("find_rows").exec({
    table: "orders",
    query: { combinator: "and", rules: [{ field: "status", operator: "=", value: "open" }] }
  }).rows || [];
  info.instance("update_row").exec({
    table: "orders",
    record_id: info.inputs.record.record_id,
    values: { open_count: open.length }
  });
  return { open: open.length, name: values.name };
}
```

Keep node function type comments from existing workflow scripts intact. They drive editor type hints and document the expected inputs and outputs.

## Forms

A form script defines `render(api, root)`. It appends controls bound to table fields (`api.input`, `api.select`, `api.relation`, `api.file`) and buttons whose actions receive an action API (`value`, `values`, `setValue`, `rows.create/update/upsert/list`, `show`, `pdf`), and returns `{ table }`, the table the form writes to. Field names must match the table's metadata.

```js
function render(api, root) {
  root.append(
    api.input({ field: "name", label: "Name" }),
    api.button("Save", async (api) => {
      const row = await api.rows.create("orders", api.values());
      api.show(row);
    })
  );
  return { table: "orders" };
}
```

## Table Query Rules

Structured queries use `{ combinator, rules, not }`. A list under `rules` is combined by the parent `combinator`; use `"and"` when every rule must match and `"or"` when any rule may match. Rules can be nested.

```js
{
  combinator: "and",
  rules: [
    { field: "userid", operator: "contains", value: "abc" },
    { field: "userid", operator: "beginsWith", value: "01" }
  ]
}
```

Prefer existing node APIs over direct database access. Keep secrets in node instance secrets or workflow variables, never hard-coded in generated JavaScript.
