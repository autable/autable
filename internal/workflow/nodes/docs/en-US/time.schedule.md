## Schedule

Trigger node for backend schedule ticks. The trigger input is the `params` object returned by `trigger(info)`. When a tick matches, this node output becomes `run(info).inputs`.

### Trigger params

- `interval_ms` (`int64`): Minimum milliseconds between runs for this workflow.
- `daily_at` (`string`): Optional UTC time in `HH:mm` format. The workflow can run once per matching UTC day.

### Run inputs

- `scheduled_at` (`int64`): Millisecond timestamp for the matched schedule event.
- `event` (`string`): Always `schedule`.

### Example

```js
function instances(info) {
  return { every_minute: "time.schedule" };
}

function trigger(info) {
  return { instance: "every_minute", params: { interval_ms: 60000 } };
}

function run(info) {
  return { scheduled_at: info.inputs.scheduled_at };
}
```
