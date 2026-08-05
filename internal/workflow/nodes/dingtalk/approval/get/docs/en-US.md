# DingTalk approval detail

Reads one approval instance by id through `GET /v1.0/workflow/processInstances`.
One request per instance, so drive it from a list of ids you already hold —
typically the ids stored on rows whose outcome is still unknown, which keeps
the request count proportional to what is actually outstanding rather than to
the size of the approval history.

## Inputs

- `instance_id` — the approval instance id.

## Outputs

`status` is `NEW`, `RUNNING`, `TERMINATED` or `COMPLETED`; `result` is `agree`
or `refuse` and only carries meaning once the instance is `COMPLETED`. Also
returned: `title`, `business_id`, the originator's user id, department id and
department name, `cc_user_ids`, `attached_instance_ids`, and:

- `values` — the form components flattened to a `{field name: value}` map,
  ready to write straight into a table. A name repeated by the template keeps
  its first value.
- `form_values` — the components as DingTalk returned them, including
  `component_type` and `biz_alias`, for what the flat map cannot express.
- `tasks` and `operation_records` — who approved what, and when.

## Timestamps

`create_time`, `finish_time`, each task's timestamps and each record's `date`
are passed through exactly as DingTalk formats them, for example
`2026-08-05T16:58Z`. Despite the trailing `Z` this is **not** necessarily UTC —
DingTalk renders it in the organisation's own time zone. Parse it against the
zone your organisation is in; treating it as UTC shifts every timestamp by that
offset.

## Keeping a table in step

Re-read the outstanding instances on a schedule and write the outcome back to
the row that holds the id. Because a row is only re-read while its outcome is
empty, each instance settles exactly once and no callback is required.
