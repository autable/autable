## 钉钉机器人

通过钉钉自定义机器人 webhook 发送文本消息。这个节点使用 access token webhook API：

`POST https://oapi.dingtalk.com/robot/send?access_token=...`

当前不做钉钉加签。access token 需要保存为 instance secret。

### Secret

- `access_token` (`string`): 钉钉自定义机器人的 access token。

### 输入

- `content` (`string`): 文本消息内容。
- `at_user_ids` (`string[]`): 可选，要 @ 的钉钉用户 id。
- `at_all` (`boolean`): 是否 @ 所有人。

### 输出

- `status_code` (`int`): HTTP 响应状态码。
- `response` (`object`): 钉钉响应体。
- `errcode` (`number`): 钉钉返回的错误码。
- `errmsg` (`string`): 钉钉返回的消息。

### 示例

```js
function instances(info) {
  return { ding: "dingtalk.robot.send" };
}

function run(info) {
  return info.instance("ding").exec({
    content: "Codetable alert",
    at_user_ids: ["user123"]
  });
}
```
