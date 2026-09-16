## 读取用户

按 id 读取一个 autable 用户。触发器只会告诉你"谁做的"这个不透明的 `actor_id`，
这个节点把它换成背后的账号，工作流就能按人来处理：去别的系统查他的 id、给他发
通知、把他的邮箱记到记录上。

### 输入

- `user_id` (`string`)：用户 id，通常是 `table.record.changed` 触发时的
  `info.inputs.actor_id`。

### 输出

- `id` (`string`)：读到的用户 id。
- `email` (`string`)：账号邮箱，已经统一成小写。
- `display_name` (`string`)
- `provider` (`string`)：`password` 或 `oidc`。
- `provider_name` (`string`)：账号来源：`password`，或者配置的 OIDC provider 名。

id 不存在直接报错，不会返回空结果。

### 示例

从一张"邮箱 → 外部系统 id"的表里，查出建行的人在外部系统里的 id：

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
