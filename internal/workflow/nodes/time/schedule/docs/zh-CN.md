## 定时触发

监听后端定时 tick 的触发器节点。触发器输入就是 `trigger(info)` 返回的 `params`。匹配成功后，这个节点的输出会成为 `run(info).inputs`。

### 触发参数

- `interval_ms` (`int64`): 当前 workflow 两次运行之间的最小间隔，单位毫秒。
- `daily_at` (`string`): 可选 UTC 时间，格式为 `HH:mm`。匹配的 UTC 日期内最多运行一次。

### Run 输入

- `scheduled_at` (`int64`): 匹配到的定时事件毫秒时间戳。
- `event` (`string`): 固定为 `schedule`。

### 示例

```js
/**
 * @param {AutableWorkflowDefinitionInfo} info
 * @returns {Record<string, string | AutableWorkflowInstanceDeclaration>}
 */
function instances(info) {
  return { every_minute: "time.schedule" };
}

/**
 * @param {AutableWorkflowDefinitionInfo} info
 * @returns {AutableWorkflowTriggerDeclaration}
 */
function trigger(info) {
  return { instance: "every_minute", params: { interval_ms: 60000 } };
}

/**
 * @param {AutableWorkflowRunInfo} info
 * @returns {Record<string, unknown>}
 */
function run(info) {
  return { scheduled_at: info.inputs.scheduled_at };
}
```
