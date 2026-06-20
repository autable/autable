## 钉钉 AI 表格记录

通过钉钉 OpenAPI Go SDK 获取 AI 表格中的多行记录。节点会使用配置的应用凭证先从钉钉换取 OpenAPI access token，再读取记录。

### Secret

- `app_key` (`string`): 钉钉 OpenAPI 应用 app key。
- `app_secret` (`string`): 钉钉 OpenAPI 应用 app secret。

### 变量

- `base_id` (`string`): 当前节点实例配置的 AI 表格 ID。
- `sheet_id_or_name` (`string`): 当前节点实例配置的数据表 ID 或数据表名称。
- `operator_id` (`string`): 当前节点实例配置的操作人 unionId。

### 输入

- `field_id_or_names` (`string[]`): 可选，只返回指定字段 ID 或字段名。
- `max_results` (`int`): 可选，由 workflow JS 本次调用传入的分页大小。
- `next_token` (`string`): 可选，由 workflow JS 从上一次调用结果传入的分页游标。
- `filter` (`object`): 可选筛选对象，例如 `{ combination, conditions: [{ field, operator, value }] }`。

### 输出

- `records` (`DingTalkNotableRecord[]`): 记录列表，包含 `id`、`fields`、时间戳，以及接口返回的创建/修改人 unionId。
- `has_more` (`boolean`): 是否还有下一页。
- `next_token` (`string`): 下一页分页游标。
- `status_code` (`int`): HTTP 响应状态码。

### 示例

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
