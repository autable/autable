## Echo

原样返回输入。适合用来测试 workflow 串联，或者把一个值通过命名 instance 传下去。

### 输入

- `value` (`any`): 任意 JSON 兼容值。

### 输出

- `value` (`any`): 和输入相同的值。

### 示例

```js
/**
 * @param {AutableWorkflowDefinitionInfo} info
 * @returns {Record<string, string | AutableWorkflowInstanceDeclaration>}
 */
function instances(info) {
  return { echo_1: "echo" };
}

/**
 * @param {AutableWorkflowRunInfo} info
 * @returns {Record<string, unknown>}
 */
function run(info) {
  return info.instance("echo_1").exec({ value: info.inputs.name });
}
```
