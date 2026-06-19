function render(api, root) {
  root.append(
    api.input({ field: "title", label: "Title" }),
    api.input({ field: "requester_email", label: "Requester email", type: "email" }),
    api.select({ field: "priority", label: "Priority", options: ["low", "normal", "high"] }),
    api.button("Create ticket", async (ctx) => {
      const row = await ctx.rows.create("tickets", ctx.values());
      ctx.show(row);
    })
  );

  return { table: "tickets" };
}
