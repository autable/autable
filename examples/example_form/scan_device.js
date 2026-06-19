function render(api, root) {
  root.append(
    api.input({
      field: "device_code",
      label: "Device code",
      scanner: true,
      onChange: async (ctx) => {
        const rows = await ctx.rows.list("devices", {
          query: { field: "device_code", op: "=", value: ctx.value("device_code") },
          limit: 10
        });
        ctx.show(rows);
      }
    })
  );

  return { table: "devices" };
}
