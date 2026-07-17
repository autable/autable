## Webhook 触发

由外部系统主动推送触发工作流,替代轮询。例如钉钉审批结束后的回调可以立即把结果推给工作流处理。

### 调用方式

```
POST https://<你的域名>/api/databases/<数据库>/workflows/<工作流ID>/webhook
Content-Type: application/json

{
  "token": "<配置的密钥>",
  "payload": { "任意": "JSON 数据" }
}
```

- 校验通过返回 `202 {"accepted":true}`,工作流异步执行;
- token 缺失或不匹配返回 401;未配置 token 时 webhook 视为关闭,拒绝所有调用;
- `payload` 上限 1MB,可省略(默认为空对象)。

### 密钥

- `token` (`string`):必须先在触发器实例配置里设置,调用方在请求 body 里携带同样的值。请使用足够长的随机串。

### 输出(即 `info.inputs`)

- `payload` (`object`):请求 body 里的 payload 对象。
- `received_at` (`int64`):接收时间(毫秒时间戳)。
- `event` (`string`):固定为 `webhook`。

### 示例

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
    table: "审批结果",
    match_field: "审批id",
    values: { 审批id: payload.instance_id, 结果: payload.result },
  });
}
```
