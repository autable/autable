# 钉钉审批详情

按实例 id 读取单个审批,走 `GET /v1.0/workflow/processInstances`。一个实例一次
请求,所以要由你手上已有的 id 列表来驱动——通常是那些还没拿到结果的记录上存
的 id。这样请求数只跟"还没落定的单子"成正比,而不是跟审批历史的总量成正比。

## 输入

- `instance_id`——审批实例 id。

## 输出

`status` 为 `NEW`、`RUNNING`、`TERMINATED` 或 `COMPLETED`;`result` 是 `agree`
或 `refuse`,只有在 `COMPLETED` 之后才有意义。另外还有 `title`、`business_id`、
发起人的 userId / 部门 id / 部门名、`cc_user_ids`、`attached_instance_ids`,以及:

- `values`——表单字段摊平成 `{字段名: 值}`,可直接写进表。模板里重名的字段以
  第一个为准。
- `form_values`——钉钉返回的原始数组,含 `component_type` 和 `biz_alias`,摊平
  表达不了的情况用它。
- `tasks` 和 `operation_records`——谁在什么时候审批的。

## 时间格式

`create_time`、`finish_time`、每个 task 的时间和每条记录的 `date` 都原样透传钉钉
的格式,例如 `2026-08-05T16:58Z`。**结尾的 `Z` 不代表 UTC**——钉钉是按企业所在
时区渲染的。请按你们企业的时区去解析;当成 UTC 会让所有时间整体偏移一个时差。

## 让表跟审批保持一致

定时把还没结果的实例重读一遍,把结果写回存着 id 的那行记录。因为只有结果为空
的行才会被重读,每个实例只会落定一次,也不需要回调。
