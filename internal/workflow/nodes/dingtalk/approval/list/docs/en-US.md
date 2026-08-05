# DingTalk approvals

Pages approval instances of one template out of DingTalk in a single batch
call per page, so a sync costs one request per 20 instances rather than one
request per instance.

## Inputs

- `start_time` — millisecond timestamp; instances created at or after it are
  returned.
- `end_time` — optional millisecond timestamp closing the window.
- `next_token` — cursor returned by the previous page; omit for the first.
- `limit` — page size, 1-20 (DingTalk's maximum, also the default).
- `process_code` — overrides the `process_code` variable.
- `edition` — overrides the `edition` variable.

## Editions

DingTalk serves the same query from two endpoints, and an org may only call
the one matching its approval plan. Set the `edition` variable to pick:

- `standard` (default) — `QueryAllProcessInstances`.
- `premium` — `PremiumGetProcessInstances`, for orgs on the premium approval
  plan.

Calling the wrong one is not a clean permission error: DingTalk answers with
an HTTP 500 `system.error`. If a correctly-scoped app gets that, switch the
edition before looking anywhere else.

## Outputs

Each instance in `instances` carries `instance_id`, `business_id`, `title`,
`status`, `result`, `originator_user_id`, `originator_dept_id`, `create_time`,
`finish_time`, plus:

- `values` — the form components flattened to a `{field name: value}` map,
  ready to write straight into a table. A name repeated by the template keeps
  its first value.
- `form_values` — the components as DingTalk returned them, for the cases the
  flat map cannot express.
- `tasks` and `operation_records` — who approved what, and when.

Page with `next_token` while `has_more` is true.

## Keeping a table in step

Re-scan a window on a schedule and `table.row.upsert` on `instance_id`.
Because every scan re-reads the whole window, an instance that changed after
the last scan is corrected on the next one — no callback required.
