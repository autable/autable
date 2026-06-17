## 删除记录

通过后端 table API 删除一行记录。权限使用 workflow 创建者的权限。

### 输入

- `database` (`string`): 可选数据库名，默认使用 workflow 所属数据库。
- `table` (`string`): 目标表名。
- `record_id` (`int64`): 要删除的记录 id。

### 输出

- `record` (`RowRecord`): 被删除记录的快照。

### 示例

```js
function instances(info) {
  return { delete_contact: "table.row.delete" };
}

function run(info) {
  return info.instance("delete_contact").exec({
    table: "contacts",
    record_id: info.inputs.record_id
  });
}
```
