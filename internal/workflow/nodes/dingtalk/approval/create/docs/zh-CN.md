## 钉钉审批

通过钉钉开放平台的「发起审批实例」API(`POST /v1.0/workflow/processInstances`)自动创建一张 OA 审批单。审批单会出现在发起人的钉钉里,并按模板配置的流程流转。

### 前置条件

1. 在钉钉管理后台的 OA 审批里建好审批模板,拿到 `processCode`(流程编辑页 URL 中可见)。
2. 企业内部应用开通 OA 审批相关权限,且该模板允许此应用调用。
3. 表单控件名称必须和 `form_values` 里的 `name` 精确一致,类型不匹配会返回「审批实例参数错误」。

### 密钥

- `app_key` (`string`):钉钉应用的 AppKey。
- `app_secret` (`string`):钉钉应用的 AppSecret。

### 变量

- `process_code` (`string`):审批模板码,例如 `PROC-xxxx`。
- `originator_user_id` (`string`):默认发起人的钉钉 userId。注意是 userId,不是 OIDC 登录返回的 unionId。
- `dept_id` (`string`):可选,发起人部门 ID;`-1` 表示主部门。

### 输入

- `form_values` (`object[]`):必填,`{name, value}` 数组,对应审批模板的表单控件。字符串原样传递,数字/布尔自动转为文本,对象和数组(明细表、多选)自动 JSON 编码。
- `originator_user_id` (`string`):可选,覆盖变量中的默认发起人,适合按记录动态指定。
- `dept_id` (`int`):可选,覆盖变量中的部门。

### 输出

- `instance_id` (`string`):创建的审批实例 ID,可写回记录字段用于后续查询状态。

### 示例

新条目触发后自动发起采购审批:

```js
function instances(info) {
  return {
    changed: "table.record.changed",
    approval: "dingtalk.approval.create",
  };
}
function trigger(info) {
  return { instance: "changed", params: { table: "purchase_requests" } };
}
function run(info) {
  const record = info.inputs.values;
  const result = info.instance("approval").exec({
    form_values: [
      { name: "采购单号", value: record.bill_no },
      { name: "金额", value: record.amount },
      { name: "说明", value: record.note },
    ],
  });
  return { instance_id: result.instance_id };
}
```
