## 记录变更

监听行历史事件的触发器节点。触发器输入就是 `trigger(info)` 返回的 `params`。当后端行事件匹配这些参数后，这个节点的输出会成为 `run(info).inputs`。

不要在 `run(info)` 里主动调用这个触发器节点。后端会在 workflow run 开始前执行它。

### 触发参数

- `table` (`string`): 可选，要监听的表名。为空表示监听当前 workflow 数据库里的所有表。
- `operations` (`string[]`): 可选操作过滤。支持 `create`、`update`、`delete`。
- `fields` (`string[]`): 可选字段过滤。create 或 update 事件里至少一个变更字段命中时才触发。

### Run 输入

- `history_key` (`string`): 行历史事件的 LevelDB key。
- `database` (`string`): 数据库名。
- `table` (`string`): 表名。
- `record_id` (`int64`): 记录 id。
- `operation` (`string`): `create`、`update` 或 `delete`。
- `record` (`TriggerRecord`): 简化的记录引用。
- `values` (`object`): 事件里的当前行值。
- `diff` (`object`): 按字段名组织的变更，包含 `old` 和 `new`。
- `actor_id` (`string`): 触发变更的用户 id。

### 示例

```js
function instances(info) {
  return { row_change: "table.record.changed" };
}

function trigger(info) {
  return {
    instance: "row_change",
    params: {
      table: "contacts",
      operations: ["create", "update"],
      fields: ["status"]
    }
  };
}

function run(info) {
  return {
    record_id: info.inputs.record_id,
    operation: info.inputs.operation,
    diff: info.inputs.diff
  };
}
```
