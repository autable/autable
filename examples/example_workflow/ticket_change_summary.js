function instances(info) {
  return {
    ticket_changed: "table.record.changed"
  };
}

function trigger(info) {
  return {
    instance: "ticket_changed",
    params: {
      table: "tickets",
      operations: ["create", "update"],
      fields: ["status", "priority"]
    }
  };
}

function run(info) {
  return {
    record_id: info.inputs.record.record_id,
    operation: info.inputs.operation,
    changed_fields: Object.keys(info.inputs.diff || {})
  };
}
