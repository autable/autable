/**
 * @param {AutableWorkflowDefinitionInfo} info
 * @returns {Record<string, string | AutableWorkflowInstanceDeclaration>}
 */
function instances(info) {
  return {
    row_change: 'table.record.changed',
    ding: "dingtalk.robot.send"
  };
}

/**
 * @param {AutableWorkflowDefinitionInfo} info
 * @returns {AutableWorkflowTriggerDeclaration}
 */
function trigger(info) {
  return {
    instance: 'row_change',
    params: {
      table: "",
      operations: ['create', 'update', 'delete']
    }
  };
}

/**
 * @param {AutableWorkflowRunInfo} info
 * @returns {Record<string, unknown>}
 */
function run(info) {
  return {
    database: info.inputs.database,
    table: info.inputs.table,
    record_id: info.inputs.record_id,
    operation: info.inputs.operation,
    diff: info.inputs.diff
  };
}