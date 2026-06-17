## Echo

Returns the node input unchanged. Use it to test workflow wiring or pass a value through a named instance.

### Inputs

- `value` (`any`): Any JSON-compatible value.

### Outputs

- `value` (`any`): The same value from input.

### Example

```js
function instances(info) {
  return { echo_1: "echo" };
}

function run(info) {
  return info.instance("echo_1").exec({ value: info.inputs.name });
}
```
