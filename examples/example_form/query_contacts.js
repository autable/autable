function render(api, root) {
  root.append(
    api.input({ field: "email", label: "Email", type: "email" }),
    api.button("Search", async (ctx) => {
      const rows = await ctx.rows.list("contacts", {
        query: { field: "email", op: "=", value: ctx.value("email") },
        limit: 20
      });
      ctx.show(rows);
    })
  );

  return { table: "contacts" };
}
