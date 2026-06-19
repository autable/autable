function instances(info) {
  return {
    schedule: "time.schedule",
    ensure_fields: "table.field.create",
    upsert_ticket: "table.row.upsert"
  };
}

function trigger(info) {
  return {
    instance: "schedule",
    params: { every: "1h" }
  };
}

function run(info) {
  info.instance("ensure_fields").exec({
    table: "tickets",
    fields: {
      external_id: "string",
      title: "string",
      status: "string",
      updated_at: "string"
    }
  });

  const incoming = info.inputs.records || [];
  let changed = 0;
  for (const item of incoming) {
    const result = info.instance("upsert_ticket").exec({
      table: "tickets",
      match_field: "external_id",
      values: {
        external_id: item.external_id,
        title: item.title,
        status: item.status,
        updated_at: item.updated_at
      }
    });
    if (result.operation !== "noop") {
      changed += 1;
    }
  }

  return { checked: incoming.length, changed };
}
