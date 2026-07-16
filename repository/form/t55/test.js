/**
 * @param {AutableFormAPI} api
 * @param {AutableFormRoot} root
 * @returns {AutableFormDefinition}
 */
function render(api, root) {
  root.append(
    api.input({ field: 'name', label: 'Name' }),
    api.button('Submit', async (api) => {
      const row = await api.rows.create("", api.values());
      api.show(row);
    })
  );
  return { table: "" };
}