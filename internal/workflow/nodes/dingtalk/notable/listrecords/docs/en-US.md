## DingTalk AI table records

Lists records from a DingTalk AI table through the DingTalk OpenAPI Go SDK. The node fetches the OpenAPI access token from DingTalk with the configured app credentials before listing records.

### Secrets

- `app_key` (`string`): DingTalk OpenAPI app key.
- `app_secret` (`string`): DingTalk OpenAPI app secret.

### Variables

- `base_id` (`string`): AI table base ID configured for this node instance.
- `sheet_id_or_name` (`string`): Sheet ID or sheet name configured for this node instance.
- `operator_id` (`string`): Operator union ID configured for this node instance.

### Inputs

- `field_id_or_names` (`string[]`): Optional field IDs or names to return.
- `max_results` (`int`): Optional page size supplied by the workflow script for this call.
- `next_token` (`string`): Optional pagination token supplied by the workflow script from a previous call.
- `filter` (`object`): Optional filter object, for example `{ combination, conditions: [{ field, operator, value }] }`.

### Outputs

- `records` (`DingTalkNotableRecord[]`): Records with `id`, `fields`, timestamps, and creator/modifier union IDs when returned.
- `has_more` (`boolean`): Whether more records are available.
- `next_token` (`string`): Pagination token for the next page.
- `status_code` (`int`): HTTP response status.

### Example

```js
/**
 * @param {AutableWorkflowDefinitionInfo} info
 * @returns {Record<string, string | AutableWorkflowInstanceDeclaration>}
 */
function instances(info) {
  return { source: "dingtalk.notable.records.list" };
}

/**
 * @param {AutableWorkflowRunInfo} info
 * @returns {Record<string, unknown>}
 */
function run(info) {
  return info.instance("source").exec({
    max_results: 50
  });
}
```
