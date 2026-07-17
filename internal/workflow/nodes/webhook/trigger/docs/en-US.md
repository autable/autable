## Webhook trigger

Lets external systems push events into a workflow instead of being polled — for example a DingTalk approval callback can deliver its result the moment the approval finishes.

### Calling the webhook

```
POST https://<your-host>/api/databases/<database>/workflows/<workflow-id>/webhook
Content-Type: application/json

{
  "token": "<configured secret>",
  "payload": { "any": "JSON data" }
}
```

- On success the server answers `200 {"accepted":true}` and runs the workflow asynchronously;
- A missing or mismatched token answers 401; while no token is configured the webhook is considered disabled and rejects every call;
- `payload` is limited to 1MB and may be omitted (defaults to an empty object).

### Secrets

- `token` (`string`): must be configured on the trigger instance first; callers send the same value in the request body. Use a long random string.

### Outputs (available as `info.inputs`)

- `payload` (`object`): the payload object from the request body.
- `received_at` (`int64`): receive time as a millisecond timestamp.
- `event` (`string`): always `webhook`.

### Example

```js
function instances(info) {
  return {
    hook: "webhook.trigger",
    upsert: "table.row.upsert",
  };
}
function trigger(info) {
  return { instance: "hook" };
}
function run(info) {
  const payload = info.inputs.payload;
  return info.instance("upsert").exec({
    table: "approval_results",
    match_field: "approval_id",
    values: { approval_id: payload.instance_id, result: payload.result },
  });
}
```
