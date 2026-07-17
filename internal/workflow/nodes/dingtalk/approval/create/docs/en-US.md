## DingTalk approval

Starts a DingTalk OA approval instance through the open platform "start process instance" API (`POST /v1.0/workflow/processInstances`). The approval appears in the initiator's DingTalk and follows the flow configured on the template.

### Prerequisites

1. Create the approval template in the DingTalk admin console and note its `processCode` (visible in the flow editor URL).
2. Grant the internal app the OA approval permissions and allow it to invoke the template.
3. Form component names must exactly match the `name` values in `form_values`; mismatches return a "process instance parameter error".

### Secrets

- `app_key` (`string`): DingTalk app AppKey.
- `app_secret` (`string`): DingTalk app AppSecret.

### Variables

- `process_code` (`string`): Approval template code, e.g. `PROC-xxxx`.
- `originator_user_id` (`string`): Default initiator's DingTalk userId. Note this is the userId, not the unionId returned by OIDC login.
- `dept_id` (`string`): Optional initiator department ID; `-1` means the main department.

### Inputs

- `form_values` (`object[]`): Required `{name, value}` pairs matching the template's form components. Strings pass through, numbers and booleans are printed, objects and arrays (detail tables, multi-selects) are JSON-encoded.
- `originator_user_id` (`string`): Optional override of the default initiator, useful per record.
- `dept_id` (`int`): Optional override of the department.

### Outputs

- `instance_id` (`string`): The created approval instance ID; store it on the record to query status later.

### Example

Start a purchase approval whenever a record is created:

```js
function instances(info) {
  return {
    changed: "table.record.changed",
    approval: "dingtalk.approval.create",
  };
}
function trigger(info) {
  return { instance: "changed", params: { table: "purchase_requests" } };
}
function run(info) {
  const record = info.inputs.values;
  const result = info.instance("approval").exec({
    form_values: [
      { name: "Order No", value: record.bill_no },
      { name: "Amount", value: record.amount },
      { name: "Note", value: record.note },
    ],
  });
  return { instance_id: result.instance_id };
}
```
