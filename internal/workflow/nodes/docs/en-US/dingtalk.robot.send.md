## DingTalk robot

Sends a text message through a DingTalk custom robot webhook. This node uses the access-token webhook API:

`POST https://oapi.dingtalk.com/robot/send?access_token=...`

It does not use DingTalk signing. Store the access token as an instance secret.

### Secrets

- `access_token` (`string`): DingTalk custom robot access token.

### Inputs

- `content` (`string`): Text message content.
- `at_user_ids` (`string[]`): Optional DingTalk user ids to mention.
- `at_all` (`boolean`): Mention everyone in the group.

### Outputs

- `status_code` (`int`): HTTP response status.
- `response` (`object`): Parsed DingTalk response body.
- `errcode` (`number`): DingTalk error code when returned.
- `errmsg` (`string`): DingTalk message when returned.

### Example

```js
/**
 * @param {CodeTableWorkflowDefinitionInfo} info
 * @returns {Record<string, string | CodeTableWorkflowInstanceDeclaration>}
 */
function instances(info) {
  return { ding: "dingtalk.robot.send" };
}

/**
 * @param {CodeTableWorkflowRunInfo} info
 * @returns {Record<string, unknown>}
 */
function run(info) {
  return info.instance("ding").exec({
    content: "Codetable alert",
    at_user_ids: ["user123"]
  });
}
```
