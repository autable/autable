## Get user

Reads one Autable user by id. Triggers report who acted as an opaque
`actor_id`; this node turns that id into the account behind it, so a workflow
can route on the person — look up their id in another system, address a
notification, stamp a record with their email.

### Inputs

- `user_id` (`string`): the user id, typically `info.inputs.actor_id` from a
  `table.record.changed` run.

### Outputs

- `id` (`string`): the id that was read.
- `email` (`string`): the account email, already normalized to lower case.
- `display_name` (`string`)
- `provider` (`string`): `password` or `oidc`.
- `provider_name` (`string`): where the account came from: `password`, or
  the configured OIDC provider name.

An unknown id is an error, not an empty result.

### Example

Look up the DingTalk user id of whoever created a row, from a table that maps
email to user id:

```js
function instances(info) {
  return {
    changed: "table.record.changed",
    who: "user.get",
    directory: "table.row.query"
  };
}
function trigger(info) {
  return { instance: "changed", params: { table: "requests", operations: ["create"] } };
}
function run(info) {
  const user = info.instance("who").exec({ user_id: info.inputs.actor_id });
  const rows = info.instance("directory").exec({
    table: "directory",
    query: { field: "email", operator: "=", value: user.email },
    limit: 1
  }).rows;
  return { email: user.email, external_id: rows[0]?.values.external_id };
}
```
