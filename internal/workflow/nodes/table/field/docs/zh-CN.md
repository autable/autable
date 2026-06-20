## 创建表字段

通过后端 metadata API 给表补齐缺失字段。已存在字段会返回到 `existing`，因此可以在每批同步前安全调用。

### 输入

- `database` (`string`): 可选数据库名，默认使用 workflow 所属数据库。
- `table` (`string`): 目标表名。
- `fields` (`string[] | object[] | object`): 要确保存在的字段。字符串会创建 `string` 字段；对象支持 `{ name, type }`；也支持 `{ title: "string", score: "float" }` 这种映射。

支持的字段类型是 `string`、`int`、`float`。

### 输出

- `created` (`Field[]`): 本次新增的字段。
- `restored` (`Field[]`): 本次恢复的软删除字段。
- `existing` (`Field[]`): 调用前已经存在的字段。
- `fields` (`Field[]`): 更新后的 active 字段列表。

### 示例

```js
/**
 * @param {AutableWorkflowDefinitionInfo} info
 * @returns {Record<string, string | AutableWorkflowInstanceDeclaration>}
 */
function instances(info) {
  return { ensure_fields: "table.field.create" };
}

/**
 * @param {AutableWorkflowRunInfo} info
 * @returns {Record<string, unknown>}
 */
function run(info) {
  return info.instance("ensure_fields").exec({
    table: "contacts",
    fields: [
      "name",
      { name: "score", type: "float" }
    ]
  });
}
```
