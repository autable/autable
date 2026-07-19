## 查询记录

通过后端 table API 查询记录。权限使用 workflow 创建者的权限。

这个节点使用和公开 `POST /api/tables/{database}/{table}/rows/query` API 一致的查询结构。

### 输入

- `database` (`string`): 可选数据库名，默认使用 workflow 所属数据库。
- `table` (`string`): 目标表名。
- `view` (`string`): 可选视图名。视图的 filter 和 sort 会先应用。
- `query` (`object`): 可选查询对象。可以传完整 `ViewQuery`，也可以传简写 `{ field, op/operator, value }`。
- `sorts` (`object[]`): 可选排序定义，例如 `{ field: "name", direction: "asc" }`。
- `limit` (`int`): 可选最大返回记录数。
- `offset` (`int`): 可选跳过的记录数，用于分页。必须和正的 `limit` 一起使用。
- `search` (`string`): 可选全文搜索词，在所有可读的文本/数字/公式字段上做包含匹配（关联和文件字段除外），与 `query` 是 AND 关系。

### 输出

- `rows` (`RowRecord[]`): 匹配的记录列表。

### 示例

```js
/**
 * @param {AutableWorkflowDefinitionInfo} info
 * @returns {Record<string, string | AutableWorkflowInstanceDeclaration>}
 */
function instances(info) {
  return { query_contacts: "table.row.query" };
}

/**
 * @param {AutableWorkflowRunInfo} info
 * @returns {Record<string, unknown>}
 */
function run(info) {
  const result = info.instance("query_contacts").exec({
    table: "contacts",
    query: { field: "email", operator: "=", value: "ada@example.com" },
    limit: 1
  });
  return { count: result.rows.length, rows: result.rows };
}
```
