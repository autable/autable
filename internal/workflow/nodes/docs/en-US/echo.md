## Echo

Returns the node input unchanged. Use it to test workflow wiring or pass a value through a named instance.

### Inputs

- `value` (`any`): Any JSON-compatible value.

### Outputs

- `value` (`any`): The same value from input.

### Example

```js
/**
 * @param {CodeTableWorkflowDefinitionInfo} info
 * @returns {Record<string, string | CodeTableWorkflowInstanceDeclaration>}
 */
function instances(info) {
  return { echo_1: "echo" };
}

/**
 * @param {CodeTableWorkflowRunInfo} info
 * @returns {Record<string, unknown>}
 */
function run(info) {
  return info.instance("echo_1").exec({ value: info.inputs.name });
}
```
