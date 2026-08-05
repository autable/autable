# 钉钉审批单

按模板分页拉取钉钉审批实例。每页一次批量请求，同步 20 条审批只花一次调用，
而不是每条一次，省接口额度。

## 输入

- `start_time`——毫秒时间戳，返回该时刻之后创建的实例。
- `end_time`——可选，毫秒时间戳，窗口右边界。
- `next_token`——上一页返回的游标，第一页不填。
- `limit`——每页条数，1-20（钉钉上限，也是默认值）。
- `process_code`——覆盖 `process_code` 变量。
- `edition`——覆盖 `edition` 变量。

## 版本

同一个查询钉钉开了两个接口，企业只能调对应自己审批版本的那个。用 `edition`
变量选：

- `standard`（默认）——`QueryAllProcessInstances`。
- `premium`——`PremiumGetProcessInstances`，审批专业版/高级版的企业用这个。

调错了钉钉不会明确报权限错误，而是返回 HTTP 500 `system.error`。权限都配好了
还报这个，先换 edition 再查别的。

## 输出

`instances` 每条包含 `instance_id`、`business_id`、`title`、`status`、
`result`、`originator_user_id`、`originator_dept_id`、`create_time`、
`finish_time`，以及：

- `values`——表单字段摊平成 `{字段名: 值}`，可直接写进表。模板里重名的字段
  以第一个为准。
- `form_values`——钉钉返回的原始数组，摊平表达不了的情况用它。
- `tasks` 和 `operation_records`——谁在什么时候审批的。

`has_more` 为真时带上 `next_token` 取下一页。

## 让表跟审批保持一致

定时重扫一个时间窗，按 `instance_id` 做 `table.row.upsert`。每轮都重读整个
窗口，所以上一轮之后被改动的实例下一轮就会被纠正——不需要回调。
