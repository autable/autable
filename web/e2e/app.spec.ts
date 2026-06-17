import { expect, type Page, test } from "@playwright/test";
import { readFileSync, writeFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

type AuthUser = {
  id: string;
  email: string;
  provider: string;
};

type WorkspaceSetup = {
  user: AuthUser;
  databaseName: string;
  tableName: string;
};

let sequence = 0;
const runtimeDir = join(dirname(fileURLToPath(import.meta.url)), ".runtime");

test.describe.configure({ mode: "serial" });

test("does not request protected workspace resources before login", async ({ page }) => {
  const apiPaths: string[] = [];
  page.on("request", (request) => {
    const url = new URL(request.url());
    if (url.pathname.startsWith("/api/")) {
      apiPaths.push(url.pathname);
    }
  });

  await page.goto("/");
  await page.waitForResponse((response) => response.url().includes("/api/auth/me"));
  await expect(page.getByRole("button", { name: "Login" })).toBeVisible();
  await expect(page.getByRole("button", { name: "Create DB" })).toBeDisabled();
  await expect(page.getByRole("button", { name: "Refresh metadata" })).toBeDisabled();

  expect(apiPaths).toContain("/api/auth/me");
  expect(apiPaths).toContain("/api/auth/oidc/providers");
  expect(apiPaths).not.toContain("/api/metadata");
  expect(apiPaths.some((path) => path.includes("/rows"))).toBe(false);
  expect(apiPaths.some((path) => path.includes("/workflows"))).toBe(false);
  expect(apiPaths.some((path) => path.includes("/forms"))).toBe(false);
  expect(apiPaths.some((path) => path.includes("/roles"))).toBe(false);
});

async function registerUser(page: Page): Promise<AuthUser> {
  sequence += 1;
  const email = `person-${Date.now()}-${sequence}@example.com`;
  await page.goto("/");
  await page.getByRole("button", { name: "Login" }).click();
  const dialog = page.getByRole("dialog");
  await dialog.getByLabel("Email").fill(email);
  await dialog.getByLabel("Password").fill("correct horse");
  await dialog.getByRole("button", { name: "Register" }).click();
  await expect(page.getByRole("button", { name: email })).toBeVisible();
  return page.evaluate(async () => {
    const response = await fetch("/api/auth/me");
    if (!response.ok) {
      throw new Error(`auth/me failed: ${response.status}`);
    }
    return (await response.json()) as AuthUser;
  });
}

async function loginUser(page: Page, email: string) {
  await api(page, "POST", "/api/auth/login", {
    email,
    password: "correct horse"
  });
}

async function api(page: Page, method: string, path: string, body?: unknown) {
  return page.evaluate(
    async ({ method: requestMethod, path: requestPath, body: requestBody }) => {
      const response = await fetch(requestPath, {
        method: requestMethod,
        headers: requestBody === undefined ? undefined : { "Content-Type": "application/json" },
        body: requestBody === undefined ? undefined : JSON.stringify(requestBody)
      });
      const text = await response.text();
      const json = text ? JSON.parse(text) : null;
      if (!response.ok) {
        throw new Error(`${requestMethod} ${requestPath} failed: ${response.status} ${text}`);
      }
      return json;
    },
    { method, path, body }
  );
}

async function setupWorkspace(page: Page): Promise<WorkspaceSetup> {
  const user = await registerUser(page);
  const suffix = `${Date.now()}-${sequence}`;
  const databaseName = `workspace${suffix}`;
  const tableName = "contacts";
  await api(page, "POST", "/api/databases", {
    name: databaseName,
    sqlite_path: `./data/${databaseName}.sqlite`
  });
  await api(page, "POST", `/api/databases/${databaseName}/tables`, {
    name: tableName,
    display_name: "Contacts",
    fields: [
      { name: "name", type: "text", deleted: false },
      { name: "email", type: "email", deleted: false },
      { name: "status", type: "text", deleted: false }
    ],
    views: [
      {
        name: "active",
        display_name: "Active",
        filters: [{ field: "status", op: "eq", value: "Active" }],
        sorts: []
      }
    ]
  });
  await api(page, "POST", `/api/tables/${databaseName}/${tableName}/rows`, {
    values: { name: "Ada Lovelace", email: "ada@example.com", status: "Active" }
  });
  await api(page, "POST", `/api/databases/${databaseName}/workflows`, {
    database_name: databaseName,
    name: `welcome-contact-${suffix}`,
    script:
      'function run(info) { const echoed = info.node("echo", { value: info.inputs.name }); return { message: echoed.value }; }',
    secrets: {},
    variables: {}
  });
  await api(page, "POST", `/api/databases/${databaseName}/forms`, {
    database_name: databaseName,
    name: `quick-status-${suffix}`,
    script:
      "function render(api, root) { root.append(api.input({ name: 'name', label: 'Name', required: true }), api.input({ name: 'email', label: 'Email', type: 'email' }), api.select({ name: 'status', label: 'Status', options: ['Active', 'Review'] }), api.submit('Create record')); return { table: 'contacts', fields: { name: 'name', email: 'email', status: 'status' } }; }"
  });
  await page.reload();
  await expect(page.getByRole("button", { name: databaseName })).toBeVisible();
  await expect(page.getByRole("button", { name: /Contacts/ })).toBeVisible();
  return { user, databaseName, tableName };
}

test("hides databases when the signed-in user has no permission", async ({ page }) => {
  await registerUser(page);

  await expect(page.getByRole("button", { name: "workspace" })).toHaveCount(0);
  await expect(page.getByText("No database").first()).toBeVisible();
});

test("covers login modal and workspace navigation through the real backend", async ({ page }) => {
  await setupWorkspace(page);

  await expect(page.getByRole("button", { name: "Table", exact: true })).toBeVisible();
  await page.getByRole("button", { name: "Workflow", exact: true }).click();
  await expect(page.getByRole("button", { name: /welcome-contact/ })).toBeVisible();
  await page.getByRole("button", { name: "Form", exact: true }).click();
  await expect(page.getByRole("button", { name: /quick-status/ })).toBeVisible();
  await page.getByRole("button", { name: "Permission", exact: true }).click();
  await expect(page.getByRole("heading", { name: "Roles" })).toBeVisible();
});

test("shows database-owned workflow and form lists across table owners", async ({ page }) => {
  const tableOwner = await registerUser(page);
  await api(page, "POST", "/api/auth/logout");
  const dbOwner = await registerUser(page);
  const suffix = `${Date.now()}-${sequence}`;
  const databaseName = `owned${suffix}`;
  const workflowName = `table-workflow-${suffix}`;
  const formName = `table-form-${suffix}`;

  await api(page, "POST", "/api/databases", {
    name: databaseName,
    sqlite_path: `./data/${databaseName}.sqlite`
  });
  await api(page, "POST", `/api/databases/${databaseName}/tables`, {
    name: "contacts",
    display_name: "Contacts",
    fields: [{ name: "name", type: "text", deleted: false }],
    views: []
  });
  await api(page, "POST", "/api/permissions/grants", {
    subject_id: tableOwner.id,
    scope: "table",
    resource: `${databaseName}.contacts`,
    field: "",
    level: 2
  });

  await api(page, "POST", "/api/auth/logout");
  await loginUser(page, tableOwner.email);
  await api(page, "POST", `/api/databases/${databaseName}/workflows`, {
    name: workflowName,
    script: "function run() { return {}; }",
    secrets: {},
    variables: {}
  });
  await api(page, "POST", `/api/databases/${databaseName}/forms`, {
    name: formName,
    script:
      "function render(api, root) { root.append(api.input({ name: 'name', label: 'Name' }), api.submit('Save')); return { table: 'contacts', fields: { name: 'name' } }; }"
  });

  await api(page, "POST", "/api/auth/logout");
  await loginUser(page, dbOwner.email);
  await page.goto("/");
  await expect(page.getByRole("button", { name: databaseName })).toBeVisible();
  await page.getByRole("button", { name: "Workflow", exact: true }).click();
  await expect(page.getByRole("button", { name: workflowName })).toBeVisible();
  await page.getByRole("button", { name: "Form", exact: true }).click();
  await expect(page.getByRole("button", { name: formName })).toBeVisible();
});

test("hides workflow and form resources without resource permission", async ({ page }) => {
  const resourceUser = await registerUser(page);
  await api(page, "POST", "/api/auth/logout");
  const dbOwner = await registerUser(page);
  const suffix = `${Date.now()}-${sequence}`;
  const databaseName = `scoped${suffix}`;
  const workflowName = `private-workflow-${suffix}`;
  const formName = `private-form-${suffix}`;

  await api(page, "POST", "/api/databases", {
    name: databaseName,
    sqlite_path: `./data/${databaseName}.sqlite`
  });
  await api(page, "POST", `/api/databases/${databaseName}/tables`, {
    name: "contacts",
    display_name: "Contacts",
    fields: [{ name: "name", type: "text", deleted: false }],
    views: []
  });
  await api(page, "POST", `/api/databases/${databaseName}/workflows`, {
    name: workflowName,
    script: "function run() { return {}; }",
    secrets: {},
    variables: {}
  });
  await api(page, "POST", `/api/databases/${databaseName}/forms`, {
    name: formName,
    script:
      "function render(api, root) { root.append(api.input({ name: 'name', label: 'Name' }), api.submit('Save')); return { table: 'contacts', fields: { name: 'name' } }; }"
  });
  await api(page, "POST", "/api/permissions/grants", {
    subject_id: resourceUser.id,
    scope: "table",
    resource: `${databaseName}.contacts`,
    field: "",
    level: 1
  });

  await api(page, "POST", "/api/auth/logout");
  await loginUser(page, resourceUser.email);
  await page.goto("/");
  await expect(page.getByRole("button", { name: databaseName })).toBeVisible();
  await expect(page.getByRole("button", { name: /Contacts/ })).toBeVisible();
  const tableCanvas = page.locator(".table-view");
  const tableActions = tableCanvas.getByRole("toolbar", { name: "Table canvas actions" });
  await expect(tableCanvas.getByRole("grid", { name: "Table records" }).getByRole("button", { name: "Add field" })).toBeDisabled();
  await expect(tableActions.getByRole("button", { name: "Edit Row" })).toBeDisabled();
  await expect(tableActions.getByRole("button", { name: "Row", exact: true })).toBeDisabled();
  await page.getByRole("button", { name: "Workflow", exact: true }).click();
  await expect(page.getByRole("button", { name: workflowName })).toHaveCount(0);
  await page.getByRole("button", { name: "Form", exact: true }).click();
  await expect(page.getByRole("button", { name: formName })).toHaveCount(0);
});

test("renders read-only workflow and form resources as non-editable", async ({ page }) => {
  const readOnlyUser = await registerUser(page);
  await api(page, "POST", "/api/auth/logout");
  await registerUser(page);
  const suffix = `${Date.now()}-${sequence}`;
  const databaseName = `readonly${suffix}`;
  const workflowName = `read-workflow-${suffix}`;
  const formName = `read-form-${suffix}`;

  await api(page, "POST", "/api/databases", {
    name: databaseName,
    sqlite_path: `./data/${databaseName}.sqlite`
  });
  await api(page, "POST", `/api/databases/${databaseName}/tables`, {
    name: "contacts",
    display_name: "Contacts",
    fields: [{ name: "name", type: "text", deleted: false }],
    views: []
  });
  const workflow = (await api(page, "POST", `/api/databases/${databaseName}/workflows`, {
    name: workflowName,
    script: "function run() { return {}; }",
    secrets: {},
    variables: {}
  })) as { id: number };
  const form = (await api(page, "POST", `/api/databases/${databaseName}/forms`, {
    name: formName,
    script:
      "function render(api, root) { root.append(api.input({ name: 'name', label: 'Name' }), api.submit('Submit record')); return { table: 'contacts', fields: { name: 'name' } }; }"
  })) as { id: number };
  await api(page, "POST", "/api/permissions/grants", {
    subject_id: readOnlyUser.id,
    scope: "table",
    resource: `${databaseName}.contacts`,
    field: "",
    level: 1
  });
  await api(page, "POST", "/api/permissions/grants", {
    subject_id: readOnlyUser.id,
    scope: "workflow",
    resource: String(workflow.id),
    field: "",
    level: 1
  });
  await api(page, "POST", "/api/permissions/grants", {
    subject_id: readOnlyUser.id,
    scope: "form",
    resource: String(form.id),
    field: "",
    level: 1
  });

  await api(page, "POST", "/api/auth/logout");
  await loginUser(page, readOnlyUser.email);
  await page.goto("/");
  await expect(page.getByRole("button", { name: databaseName })).toBeVisible();

  await page.getByRole("button", { name: "Workflow", exact: true }).click();
  await expect(page.getByRole("button", { name: workflowName })).toBeVisible();
  await expect(page.getByLabel("Workflow JavaScript")).toBeDisabled();
  await expect(page.getByLabel("Workflow Variables JSON")).toBeDisabled();
  await expect(page.getByLabel("Workflow Secrets JSON")).toBeDisabled();
  await expect(page.getByLabel("Workflow Inputs JSON")).toBeEnabled();
  await expect(page.getByRole("button", { name: "Save" })).toBeDisabled();
  await expect(page.getByRole("button", { name: "Run" })).toBeDisabled();

  await page.getByRole("button", { name: "Form", exact: true }).click();
  await expect(page.getByRole("button", { name: formName })).toBeVisible();
  await expect(page.getByLabel("Form JavaScript")).toBeDisabled();
  await expect(page.getByRole("button", { name: "Save" })).toBeDisabled();
  await expect(page.getByRole("button", { name: "Submit record" })).toBeEnabled();
});

test("covers database and table creation through the real backend", async ({ page }) => {
  const workspace = await setupWorkspace(page);

  const suffix = `${Date.now()}-${sequence}`;
  const databaseName = `sales${suffix}`;
  const tableName = `projects${suffix}`;
  await page.getByRole("textbox", { name: "New database name" }).fill(databaseName);
  await page.getByRole("button", { name: "Create DB" }).click();
  await expect(page.getByRole("button", { name: databaseName })).toHaveAttribute("aria-expanded", "true");
  await expect(page.getByText(`Created database ${databaseName}`)).toBeVisible();

  await page.getByRole("textbox", { name: "New table name" }).fill(tableName);
  await page.getByRole("button", { name: "Create Table" }).click();
  await expect(page.getByRole("button", { name: tableName })).toBeVisible();
  await expect(page.getByText(`Created table ${databaseName}.${tableName}`)).toBeVisible();

  await page.getByRole("button", { name: workspace.databaseName, exact: true }).click();
  await expect(page.getByRole("button", { name: workspace.databaseName, exact: true })).toHaveAttribute("aria-expanded", "true");
});

test("covers table views, row creation, and row history through the real backend", async ({ page }) => {
  const workspace = await setupWorkspace(page);

  await expect(page.getByText(/\d+ of \d+ records/).first()).toBeVisible();
  const tableCanvas = page.locator(".table-view");
  const tableActions = tableCanvas.getByRole("toolbar", { name: "Table canvas actions" });
  await expect(tableActions.getByRole("button", { name: "Row", exact: true })).toBeVisible();
  await expect(page.getByRole("toolbar", { name: "Workspace actions" }).getByRole("button", { name: "Create row" })).toHaveCount(0);

  const recordsGrid = tableCanvas.getByRole("grid", { name: "Table records" });
  for (let index = 0; index < 80; index += 1) {
    await api(page, "POST", `/api/tables/${workspace.databaseName}/${workspace.tableName}/rows`, {
      values: {
        name: `Bulk contact ${index}`,
        email: `bulk-${index}@example.com`,
        status: "Backlog"
      }
    });
  }
  await page.reload();
  await expect(page.getByRole("button", { name: workspace.databaseName })).toBeVisible();
  await expect(recordsGrid).toBeVisible();
  await expect
    .poll(async () =>
      tableCanvas.locator(".codetable-grid").evaluate((element) => {
        const grid = element as HTMLElement;
        return grid.scrollHeight > grid.clientHeight;
      })
    )
    .toBe(true);
  const gridLayout = await tableCanvas.locator(".grid-host").evaluate((element) => {
    const host = element as HTMLElement;
    const grid = host.querySelector(".codetable-grid") as HTMLElement | null;
    const hostStyle = window.getComputedStyle(host);
    const gridStyle = grid ? window.getComputedStyle(grid) : null;
    const documentElement = document.documentElement;
    return {
      hostOverflow: hostStyle.overflow,
      gridOverflow: gridStyle?.overflow,
      hostHeight: host.getBoundingClientRect().height,
      documentHeight: documentElement.scrollHeight,
      viewportHeight: window.innerHeight
    };
  });
  expect(gridLayout.hostOverflow).toBe("hidden");
  expect(gridLayout.gridOverflow).toBe("auto");
  expect(gridLayout.hostHeight).toBeLessThan(gridLayout.viewportHeight);
  expect(gridLayout.documentHeight).toBeLessThanOrEqual(gridLayout.viewportHeight + 1);
  await recordsGrid.getByRole("button", { name: "Add field" }).click();
  const addFieldEditor = page.getByLabel("Add field");
  await addFieldEditor.getByLabel("Field name").fill("priority");
  await addFieldEditor.getByLabel("New field type").selectOption("text");
  await addFieldEditor.getByRole("button", { name: "Add" }).click();
  await expect(page.getByText("Added field priority")).toBeVisible();

  await recordsGrid.getByRole("button", { name: "Field actions email" }).click();
  await page.getByRole("menuitem", { name: "Delete field" }).click();
  await expect(page.getByText("Deleted field email")).toBeVisible();

  await tableActions.getByRole("button", { name: "Row", exact: true }).click();
  await expect(page.getByText(/Created record \d+/)).toBeVisible();
  await recordsGrid.getByRole("gridcell", { name: /New record/ }).dblclick();
  await recordsGrid.locator(".rdg-text-editor").fill("Grace Hopper");
  await page.keyboard.press("Enter");
  await expect(page.getByText(/Updated record \d+/)).toBeVisible();

  await recordsGrid.getByRole("gridcell", { name: "Review" }).last().dblclick();
  await recordsGrid.locator(".rdg-text-editor").fill("Active");
  await page.keyboard.press("Enter");
  await expect(page.getByText(/Updated record \d+/)).toBeVisible();

  await page.getByRole("button", { name: workspace.tableName }).click();
  await page.getByRole("button", { name: "+ View" }).click();
  await expect(page.getByText("Created view View 2")).toBeVisible();
  const viewFilters = page.getByLabel("View filters");
  await viewFilters.getByLabel("View filter field").selectOption("status");
  await viewFilters.getByLabel("View filter value").fill("Active");
  await viewFilters.getByLabel("View sort field").selectOption("name");
  await viewFilters.getByLabel("View sort direction").selectOption("desc");
  await viewFilters.getByRole("button", { name: "Save View" }).click();
  await expect(page.getByText("Updated view View 2")).toBeVisible();

  const viewRows = (await api(
    page,
    "GET",
    `/api/tables/${workspace.databaseName}/${workspace.tableName}/rows?view=view_2`
  )) as Array<{ values: { name?: string } }>;
  expect(viewRows.map((row) => row.values.name)).toEqual(["Grace Hopper", "Ada Lovelace"]);

  await page.getByRole("button", { name: "Active", exact: true }).click();
  await expect(page.getByText(/\d+ of \d+ records/).first()).toBeVisible();
  await recordsGrid.getByRole("gridcell", { name: "Grace Hopper" }).click({ button: "right" });
  await page.getByRole("menuitem", { name: "View details" }).click();
  const detailsPanel = page.getByLabel("Record panel");
  await expect(detailsPanel.getByRole("tab", { name: "Details" })).toHaveAttribute("aria-selected", "true");
  await expect(detailsPanel.getByLabel("name value")).toHaveValue("Grace Hopper");
  await expect(detailsPanel.getByLabel("History record")).toHaveCount(0);
  await detailsPanel.getByRole("button", { name: "Close record panel" }).click();

  await recordsGrid.getByRole("gridcell", { name: "Grace Hopper" }).click({ button: "right" });
  await page.getByRole("menuitem", { name: "View history" }).click();
  const recordPanel = page.getByLabel("Record panel");
  await expect(recordPanel.getByRole("tab", { name: "History" })).toHaveAttribute("aria-selected", "true");
  await expect(recordPanel.getByLabel("Row history").getByText(/Created|Updated|Record change/).first()).toBeVisible();
  await expect(page.getByText(new RegExp(`rhistory_${workspace.databaseName}_contacts_`))).toHaveCount(0);
  await recordPanel.getByRole("button", { name: "Close record panel" }).click();

  await recordsGrid.getByRole("gridcell", { name: "Grace Hopper" }).click({ button: "right" });
  await page.getByRole("menuitem", { name: "Delete record" }).click();
  await expect(page.getByText(/Deleted record \d+/)).toBeVisible();

  const metadata = (await api(page, "GET", "/api/metadata")) as {
    databases: Array<{ name: string; tables: Array<{ name: string; fields: Array<{ name: string; type?: string; deleted: boolean }>; views: Array<{ name: string; filters: Array<{ field: string; value?: string }>; sorts: Array<{ field: string; direction: string }> }> }> }>;
  };
  const table = metadata.databases
    .find((database) => database.name === workspace.databaseName)
    ?.tables.find((item) => item.name === workspace.tableName);
  expect(table?.fields).toEqual(
    expect.arrayContaining([
      expect.objectContaining({ name: "priority", type: "text", deleted: false }),
      expect.objectContaining({ name: "email", deleted: true })
    ])
  );
  expect(table?.views).toEqual(
    expect.arrayContaining([
      expect.objectContaining({
        name: "view_2",
        filters: [expect.objectContaining({ field: "status", value: "Active" })],
        sorts: [expect.objectContaining({ field: "name", direction: "desc" })]
      })
    ])
  );
});

test("covers workflow editor, node list, and run history through the real backend", async ({ page }) => {
  const workspace = await setupWorkspace(page);

  await page.getByRole("button", { name: "Workflow", exact: true }).click();
  const workflowName = `ui-workflow-${Date.now()}`;
  await page.getByRole("textbox", { name: "New workflow name" }).fill(workflowName);
  await page.getByRole("button", { name: "Create Workflow" }).click();
  await expect(page.getByText(`Created workflow ${workflowName}`)).toBeVisible();
  await expect(page.getByRole("button", { name: workflowName })).toBeVisible();
  await expect(page.getByLabel("Workflow JavaScript")).toHaveValue(/function trigger\(info\)/);
  await expect(page.getByLabel("Workflow JavaScript")).toHaveValue(/table\.record\.changed/);
  await expect(page.getByText("echo").first()).toBeVisible();
  const nodeListLayout = await page.locator(".node-list").evaluate((element) => {
    const list = element as HTMLElement;
    return {
      overflowY: window.getComputedStyle(list).overflowY,
      clientHeight: list.clientHeight,
      scrollHeight: list.scrollHeight
    };
  });
  expect(nodeListLayout.overflowY).toBe("auto");
  expect(nodeListLayout.scrollHeight).toBeGreaterThan(nodeListLayout.clientHeight);
  const rowHistory = (await api(
    page,
    "GET",
    `/api/tables/${workspace.databaseName}/${workspace.tableName}/rows/1/history`
  )) as Array<{ history_key: string }>;
  await page.getByLabel("Workflow JavaScript").fill(
    "function run(info) {\n  const triggered = info.node('table.record.changed', { history_key: info.inputs.history_key });\n  return { record_id: triggered.record.record_id, name: triggered.values.name };\n}"
  );
  await page.getByRole("button", { name: "Save" }).click();
  await expect(page.getByText(/Workflow saved as #/)).toBeVisible();
  await page.getByLabel("Workflow Inputs JSON").fill(JSON.stringify({ history_key: rowHistory[0].history_key }, null, 2));
  await page.getByRole("button", { name: "Run" }).click();
  await expect(page.getByText(/Workflow run saved: whistory_/)).toBeVisible();
  await expect(page.getByRole("button", { name: /whistory_/ })).toBeVisible();
  const runFlow = page.getByLabel("Workflow run flow");
  await expect(runFlow.getByText("table.record.changed")).toBeVisible();
  await expect(runFlow.getByText("Run input")).toBeVisible();
  await expect(runFlow.getByText("Run output")).toBeVisible();
  await expect(runFlow.getByText(rowHistory[0].history_key).first()).toBeVisible();
  await expect(runFlow.getByText(/"record_id": 1/).first()).toBeVisible();
});

test("runs table row workflow nodes through the real backend", async ({ page }) => {
  const workspace = await setupWorkspace(page);

  await page.getByRole("button", { name: "Workflow", exact: true }).click();
  const workflowName = `row-node-workflow-${Date.now()}`;
  await page.getByRole("textbox", { name: "New workflow name" }).fill(workflowName);
  await page.getByRole("button", { name: "Create Workflow" }).click();
  await expect(page.getByText(`Created workflow ${workflowName}`)).toBeVisible();
  await expect(page.getByRole("button", { name: workflowName })).toBeVisible();
  await page.getByLabel("Workflow JavaScript").fill(
    "function run(info) {\n  const created = info.node('table.row.create', {\n    table: 'contacts',\n    values: { name: info.inputs.name, email: info.inputs.email, status: 'Review' }\n  });\n  const beforeUpdate = info.node('table.row.list', { table: 'contacts' });\n  const updated = info.node('table.row.update', {\n    table: 'contacts',\n    record_id: created.record.record_id,\n    values: { status: 'Active' }\n  });\n  const deleted = info.node('table.row.delete', {\n    table: 'contacts',\n    record_id: created.record.record_id\n  });\n  return {\n    created_id: created.record.record_id,\n    before_count: beforeUpdate.rows.length,\n    updated_status: updated.record.values.status,\n    deleted_name: deleted.record.values.name\n  };\n}"
  );
  await page.getByRole("button", { name: "Save" }).click();
  await expect(page.getByText(/Workflow saved as #/)).toBeVisible();
  await page.getByLabel("Workflow Inputs JSON").fill(
    JSON.stringify({ name: "Grace Hopper", email: "grace@example.com" }, null, 2)
  );
  await page.getByRole("button", { name: "Run" }).click();
  await expect(page.getByText(/Workflow run saved: whistory_/)).toBeVisible();

  const rows = (await api(
    page,
    "GET",
    `/api/tables/${workspace.databaseName}/${workspace.tableName}/rows`
  )) as Array<{ record_id: number; values: Record<string, unknown> }>;
  expect(rows.some((row) => row.values.name === "Grace Hopper")).toBe(false);
  const runFlow = page.getByLabel("Workflow run flow");
  await expect(runFlow.getByText("table.row.create")).toBeVisible();
  await expect(runFlow.getByText("table.row.list")).toBeVisible();
  await expect(runFlow.getByText("table.row.update")).toBeVisible();
  await expect(runFlow.getByText("table.row.delete")).toBeVisible();
  await expect(runFlow.getByText(/Grace Hopper/).first()).toBeVisible();
  await expect(runFlow.getByText(/"updated_status": "Active"/).first()).toBeVisible();
});

test("persists workflow and form JavaScript into the repository path", async ({ page }) => {
  await registerUser(page);
  const suffix = `${Date.now()}-${sequence}`;
  const databaseName = `repo${suffix}`;
  await api(page, "POST", "/api/databases", {
    name: databaseName,
    sqlite_path: `./data/${databaseName}.sqlite`
  });

  const workflowName = `repo-workflow-${suffix}`;
  const workflowScript = 'function run(info) { return { name: info.inputs.name }; }';
  const workflow = (await api(page, "POST", `/api/databases/${databaseName}/workflows`, {
    database_name: databaseName,
    name: workflowName,
    script: workflowScript,
    secrets: {},
    variables: {}
  })) as { id: number };
  const workflowPath = join(
    runtimeDir,
    "workspace",
    "workflows",
    databaseName,
    `${String(workflow.id).padStart(20, "0")}-${workflowName}.js`
  );
  expect(readFileSync(workflowPath, "utf8")).toBe(workflowScript);
  const editedWorkflowScript = "function run() { return { source: 'file' }; }";
  writeFileSync(workflowPath, editedWorkflowScript);
  const loadedWorkflow = (await api(page, "GET", `/api/workflows/${workflow.id}`)) as { script: string };
  expect(loadedWorkflow.script).toBe(editedWorkflowScript);
  const run = (await api(page, "POST", `/api/workflows/${workflow.id}/runs`, { inputs: {} })) as {
    run: { outputs: { source?: string } };
  };
  expect(run.run.outputs.source).toBe("file");

  const formName = `repo-form-${suffix}`;
  const formScript =
    "function render(api, root) { root.append(api.input({ name: 'email', label: 'Email' }), api.submit('Save')); return { table: 'contacts', fields: { email: 'email' } }; }";
  const form = (await api(page, "POST", `/api/databases/${databaseName}/forms`, {
    database_name: databaseName,
    name: formName,
    script: formScript
  })) as { id: number };
  const formPath = join(
    runtimeDir,
    "workspace",
    "forms",
    databaseName,
    `${String(form.id).padStart(20, "0")}-${formName}.js`
  );
  expect(readFileSync(formPath, "utf8")).toBe(formScript);
  const editedFormScript =
    "function render(api, root) { root.append(api.input({ name: 'from_file', label: 'From file' }), api.submit('Save')); return { table: 'contacts', fields: { from_file: 'name' } }; }";
  writeFileSync(formPath, editedFormScript);
  const loadedForm = (await api(page, "GET", `/api/forms/${form.id}`)) as { script: string };
  expect(loadedForm.script).toBe(editedFormScript);
});

test("covers form runtime preview and submit through the real backend", async ({ page }) => {
  await setupWorkspace(page);

  await page.getByRole("button", { name: "Form", exact: true }).click();
  const formName = `ui-form-${Date.now()}`;
  await page.getByRole("textbox", { name: "New form name" }).fill(formName);
  await page.getByRole("button", { name: "Create Form" }).click();
  await expect(page.getByText(`Created form ${formName}`)).toBeVisible();
  await expect(page.getByRole("button", { name: formName })).toBeVisible();
  await page.getByRole("textbox", { name: "Name", exact: true }).fill("Margaret Hamilton");
  await page.getByRole("button", { name: "Submit" }).click();
  await expect(page.getByText(/Form created contacts record \d+/)).toBeVisible();
});

test("publishes form links that require login and explicit form permission", async ({ page }) => {
  const workspace = await setupWorkspace(page);
  await page.getByRole("button", { name: "Form", exact: true }).click();
  await page.getByRole("button", { name: "Publish" }).click();
  await expect(page.getByText(/Published form/)).toBeVisible();
  const link = await page.getByLabel("Published form link").inputValue();
  expect(link).toContain("/forms/");
  const token = link.split("/forms/").at(-1) ?? "";
  const forms = (await api(page, "GET", `/api/databases/${workspace.databaseName}/forms`)) as Array<{ id: number; published_token?: string }>;
  const form = forms.find((item) => item.published_token === token);
  expect(form?.id).toBeTruthy();

  const readerEmail = `form-reader-${Date.now()}-${sequence}@example.com`;
  const reader = (await api(page, "POST", "/api/auth/register", {
    email: readerEmail,
    password: "correct horse"
  })) as AuthUser;
  await loginUser(page, workspace.user.email);
  await api(page, "POST", "/api/permissions/grants", {
    subject_id: reader.id,
    scope: "form",
    resource: String(form?.id),
    field: "",
    level: 1
  });

  await page.context().clearCookies();
  await page.goto(link);
  const dialog = page.getByRole("dialog");
  await expect(dialog.getByRole("button", { name: "Login" })).toBeVisible();
  await dialog.getByLabel("Email").fill(readerEmail);
  await dialog.getByLabel("Password").fill("correct horse");
  await dialog.getByRole("button", { name: "Login" }).click();
  await expect(dialog).toBeHidden();
  await expect(page.getByText(/Opened/)).toBeVisible();
  await page.getByLabel("Name").fill("Published User");
  await page.getByLabel("Email").fill("published@example.com");
  await page.getByLabel("Status").selectOption("Review");
  await page.getByRole("button", { name: "Create record" }).click();
  await expect(page.getByText(/Form submitted as record \d+/)).toBeVisible();

  await loginUser(page, workspace.user.email);
  const rows = (await api(page, "GET", `/api/tables/${workspace.databaseName}/${workspace.tableName}/rows`)) as Array<{
    values: { name?: string; email?: string; status?: string };
  }>;
  expect(rows.map((row) => row.values)).toEqual(
    expect.arrayContaining([
      expect.objectContaining({ name: "Published User", email: "published@example.com", status: "Review" })
    ])
  );
});

test("covers role members and resource permission grants through the real backend", async ({ page }) => {
  const { user, databaseName, tableName } = await setupWorkspace(page);

  await page.getByRole("button", { name: "Permission", exact: true }).click();
  await page.getByRole("textbox", { name: "New role name" }).fill("editor");
  await page.getByRole("button", { name: "Create Role" }).click();
  await expect(page.getByRole("button", { name: /editor/ })).toBeVisible();
  const permissionView = page.locator(".permission-view");
  await permissionView.getByRole("textbox", { name: "Role member user id" }).fill(user.id);
  await permissionView.getByRole("button", { name: "Add role member" }).click();
  await expect(permissionView.getByText(user.id)).toBeVisible();
  await permissionView.getByLabel("contacts permission").selectOption("2");
  await permissionView.getByLabel("email permission").selectOption("1");
  await permissionView.getByRole("button", { name: "Save" }).click();
  await expect(page.getByText("Saved role editor")).toBeVisible();

  const roles = (await api(page, "GET", `/api/databases/${databaseName}/roles`)) as Array<{
    name: string;
    grants: Array<{ scope: string; resource: string; field: string; level: number }>;
    members: string[];
  }>;
  const role = roles.find((item) => item.name === "editor");
  expect(role?.members).toContain(user.id);
  expect(role?.grants).toEqual(
    expect.arrayContaining([
      expect.objectContaining({ scope: "table", resource: `${databaseName}.${tableName}`, field: "", level: 2 }),
      expect.objectContaining({ scope: "field", resource: `${databaseName}.${tableName}`, field: "name", level: 0 }),
      expect.objectContaining({ scope: "field", resource: `${databaseName}.${tableName}`, field: "email", level: 1 })
    ])
  );
});
