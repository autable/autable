import { expect, type Page, test } from "@playwright/test";
import { appBundle } from "./appBundle";
import { workflowBundle } from "./workflowBundle";

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
// The only global admin in the e2e config (see start-backend.mjs); everyone
// else the specs register is an ordinary user.
const adminEmail = "e2e-admin@example.com";

test.describe.configure({ mode: "serial" });

test("does not request protected workspace resources before login", async ({ page }) => {
  const apiPaths: string[] = [];
  page.on("request", (request) => {
    const url = new URL(request.url());
    if (url.pathname.startsWith("/api/")) {
      apiPaths.push(url.pathname);
    }
  });

  // The waiter has to exist before the navigation that triggers the request:
  // page.goto resolves on load, by which time the response may already have
  // arrived, and a waiter registered after it would wait for a second one
  // that never comes.
  const authResponse = page.waitForResponse((response) => response.url().includes("/api/auth/me"));
  await page.goto("/");
  await authResponse;
  await expect(page.getByRole("form", { name: "Sign in" })).toBeVisible();
  await expect(page.getByRole("button", { name: "Login" })).toBeVisible();
  await expect(page.getByRole("navigation", { name: "Workspace" })).toHaveCount(0);
  await expect(page.getByRole("button", { name: "New database" })).toHaveCount(0);

  expect(apiPaths).toContain("/api/auth/me");
  expect(apiPaths).toContain("/api/auth/config");
  expect(apiPaths).not.toContain("/api/metadata");
  expect(apiPaths.some((path) => path.includes("/rows"))).toBe(false);
  expect(apiPaths.some((path) => path.includes("/workflows"))).toBe(false);
  expect(apiPaths.some((path) => path.includes("/apps"))).toBe(false);
  expect(apiPaths.some((path) => path.includes("/roles"))).toBe(false);
});

async function registerUser(page: Page): Promise<AuthUser> {
  sequence += 1;
  const email = `person-${Date.now()}-${sequence}@example.com`;
  await page.goto("/");
  const user = (await api(page, "POST", "/api/auth/register", {
    email,
    password: "correct horse",
    // The nav account button shows display_name; keep it equal to the email
    // so existing name-based assertions keep working.
    display_name: email
  })) as AuthUser;
  await page.reload();
  await expect(page.getByRole("button", { name: email })).toBeVisible();
  return user;
}

// signInAdmin signs in as the shared admin. The backend keeps its data for the
// whole run, so the first call registers the account and later ones log in.
async function signInAdmin(page: Page): Promise<AuthUser> {
  await page.goto("/");
  const result = await page.evaluate(async (email) => {
    const post = (path: string, body: unknown) =>
      fetch(path, { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify(body) });
    let response = await post("/api/auth/register", { email, password: "correct horse", display_name: email });
    if (response.status === 409) {
      response = await post("/api/auth/login", { email, password: "correct horse" });
    }
    return { ok: response.ok, status: response.status, text: await response.text() };
  }, adminEmail);
  if (!result.ok) {
    throw new Error(`admin sign-in failed: ${result.status} ${result.text}`);
  }
  await page.reload();
  await expect(page.getByRole("button", { name: adminEmail })).toBeVisible();
  return JSON.parse(result.text) as AuthUser;
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

async function createRows(page: Page, databaseName: string, tableName: string, rows: Array<Record<string, unknown>>) {
  await page.evaluate(
    async ({ databaseName: dbName, tableName: targetTable, rows: nextRows }) => {
      await Promise.all(
        nextRows.map(async (values) => {
          const response = await fetch(`/api/tables/${dbName}/${targetTable}/rows`, {
            method: "POST",
            headers: { "Content-Type": "application/json" },
            body: JSON.stringify({ values })
          });
          if (!response.ok) {
            throw new Error(`row create failed: ${response.status} ${await response.text()}`);
          }
        })
      );
    },
    { databaseName, tableName, rows }
  );
}

// uploadApp pushes a hosted app version the way `autablectl app push` does.
async function uploadApp(page: Page, databaseName: string, name: string, title: string, query = "") {
  const response = await page.request.post(`/api/databases/${databaseName}/apps/${name}/versions${query}`, {
    headers: { "Content-Type": "application/gzip" },
    data: appBundle({ database: databaseName, name, title, heading: title })
  });
  expect(response.ok(), await response.text()).toBe(true);
  return ((await response.json()) as { app: { id: number; name: string; title: string } }).app;
}

// pushWorkflow uploads a workflow version the way `autablectl workflow push` does.
async function pushWorkflow(page: Page, databaseName: string, name: string, title: string, query = "?publish=1") {
  const response = await page.request.post(`/api/databases/${databaseName}/workflows/${name}/versions${query}`, {
    headers: { "Content-Type": "application/gzip" },
    data: workflowBundle({ database: databaseName, name, title, release: "v1" })
  });
  expect(response.ok(), await response.text()).toBe(true);
  return ((await response.json()) as { workflow: { id: number; name: string; title: string } }).workflow;
}

async function createNamedResource(page: Page, buttonName: string, inputName: string, name: string) {
  const button = page.getByRole("button", { name: buttonName });
  await expect(button).toBeEnabled();
  await button.click();
  const input = page.getByRole("textbox", { name: inputName });
  await expect(input).toBeVisible();
  await input.fill(name);
  const saveButton = page.getByRole("button", { name: "Save" }).last();
  await expect(saveButton).toBeEnabled();
  await saveButton.click();
}

// The sidebar: database switcher, Home/Apps, tables and (admins) Manage.
function sidebar(page: Page) {
  return page.getByRole("navigation", { name: "Workspace" });
}

async function waitForWorkspaceReady(page: Page, databaseName: string, tableName = /Contacts/) {
  await expect(page.getByRole("button", { name: databaseName, exact: true })).toBeVisible();
  await expect(sidebar(page).getByRole("link", { name: tableName })).toBeVisible();
}

// Opens the database switcher; admins find "New database" at its end.
async function openDatabaseSwitcher(page: Page, databaseName: string) {
  await page.getByRole("button", { name: databaseName, exact: true }).click();
  return page.getByRole("menu");
}

async function waitForTableReady(page: Page, databaseName: string, tableName = /Contacts/) {
  await waitForWorkspaceReady(page, databaseName, tableName);
  const tableCanvas = page.locator(".table-view");
  await expect(tableCanvas.getByRole("grid", { name: "Table records" })).toBeVisible();
  await expect(tableCanvas.getByText(/\d+ of \d+ records/).first()).toBeVisible();
}

async function setupWorkspace(page: Page): Promise<WorkspaceSetup> {
  const user = await signInAdmin(page);
  const suffix = `${Date.now()}-${sequence}`;
  const databaseName = `workspace${suffix}`;
  const tableName = "contacts";
  await api(page, "POST", "/api/databases", {
    name: databaseName,
  });
  await api(page, "POST", `/api/databases/${databaseName}/tables`, {
    name: tableName,
    display_name: "Contacts",
    fields: [
      { name: "name", type: "string", deleted: false },
      { name: "email", type: "string", deleted: false },
      { name: "status", type: "string", deleted: false }
    ],
    views: [
      {
        name: "active",
        display_name: "Active",
        query: { combinator: "and", rules: [{ field: "status", operator: "=", value: "Active" }] },
        sorts: []
      }
    ]
  });
  await api(page, "POST", `/api/tables/${databaseName}/${tableName}/rows`, {
    values: { name: "Ada Lovelace", email: "ada@example.com", status: "Active" }
  });
  await pushWorkflow(page, databaseName, `welcome-contact-${suffix}`, "Welcome contact");
  // Selection is scoped to the URL route, so land on the table directly.
  await page.goto(`/databases/${databaseName}/tables/${tableName}`);
  await waitForTableReady(page, databaseName);
  return { user, databaseName, tableName };
}

test("hides databases when the signed-in user has no permission", async ({ page }) => {
  await registerUser(page);

  await expect(page.getByRole("button", { name: "workspace" })).toHaveCount(0);
  await expect(page.getByText("No database").first()).toBeVisible();
});

test("covers workspace navigation through the real backend", async ({ page }) => {
  const { databaseName } = await setupWorkspace(page);

  await expect(sidebar(page).getByRole("link", { name: "Contacts" })).toHaveAttribute("aria-current", "page");
  await sidebar(page).getByRole("link", { name: "Home", exact: true }).click();
  await expect(page).toHaveURL(`/databases/${databaseName}`);
  await expect(page.getByRole("heading", { name: databaseName, level: 1 })).toBeVisible();
  await expect(page.getByRole("region", { name: "Tables" }).getByRole("link", { name: /Contacts/ })).toBeVisible();
  await sidebar(page).getByRole("link", { name: "Workflows", exact: true }).click();
  await expect(sidebar(page).getByRole("link", { name: "Welcome contact" })).toBeVisible();
  await expect(page.getByRole("table", { name: "Workflows" }).getByText("Manual", { exact: true })).toBeVisible();
  await sidebar(page).getByRole("link", { name: "Apps", exact: true }).click();
  await expect(page.getByText("No apps yet")).toBeVisible();
  await sidebar(page).getByRole("link", { name: "Permissions", exact: true }).click();
  await expect(page.getByRole("button", { name: "Create Role" })).toBeVisible();
  await sidebar(page).getByRole("link", { name: "Overview", exact: true }).click();
  await expect(page.getByRole("heading", { name: "Manage", level: 1 })).toBeVisible();
  await expect(page.getByRole("table", { name: "Tables & schema" }).getByRole("link", { name: /Contacts/ })).toBeVisible();
});

test("gives granted non-admins data, apps and workflows but no structure actions", async ({ page }) => {
  const member = await registerUser(page);
  await api(page, "POST", "/api/auth/logout");
  await signInAdmin(page);
  const suffix = `${Date.now()}-${sequence}`;
  const databaseName = `granted${suffix}`;
  const workflowName = `granted-workflow-${suffix}`;
  const appTitle = `Granted app ${suffix}`;

  await api(page, "POST", "/api/databases", {
    name: databaseName,
  });
  await api(page, "POST", `/api/databases/${databaseName}/tables`, {
    name: "contacts",
    display_name: "Contacts",
    fields: [{ name: "name", type: "string", deleted: false }],
    views: []
  });
  await api(page, "POST", `/api/tables/${databaseName}/contacts/rows`, { values: { name: "Ada Lovelace" } });
  const workflow = await pushWorkflow(page, databaseName, workflowName, "Granted workflow");
  await uploadApp(page, databaseName, "granted-app", appTitle, "?publish=1");
  const grants = [
    { scope: "field_set", resource: `${databaseName}.contacts`, field: "", level: 3 },
    { scope: "view_set", resource: `${databaseName}.contacts`, field: "", level: 1 },
    { scope: "workflow", resource: String(workflow.id), field: "", level: 1 },
    { scope: "app_set", resource: databaseName, field: "", level: 1 }
  ];
  for (const grant of grants) {
    await api(page, "POST", "/api/permissions/grants", { subject_id: member.id, ...grant });
  }

  await api(page, "POST", "/api/auth/logout");
  await loginUser(page, member.email);
  // The home page offers the granted app and table.
  await page.goto(`/databases/${databaseName}`);
  await waitForWorkspaceReady(page, databaseName);
  await expect(page.getByRole("region", { name: "Apps" }).getByRole("link", { name: `Open ${appTitle}` })).toHaveAttribute(
    "href",
    `/apps/${databaseName}/granted-app/`
  );
  await page.getByRole("region", { name: "Tables" }).getByRole("link", { name: /Contacts/ }).click();
  await waitForTableReady(page, databaseName);
  const tableCanvas = page.locator(".table-view");
  const recordsGrid = tableCanvas.getByRole("grid", { name: "Table records" });
  await expect(recordsGrid.getByRole("gridcell", { name: "Ada Lovelace", exact: true })).toBeVisible();
  const switcher = await openDatabaseSwitcher(page, databaseName);
  await expect(switcher.getByRole("menuitemradio", { name: databaseName })).toBeVisible();
  await expect(switcher.getByRole("menuitem", { name: "New database" })).toHaveCount(0);
  await page.keyboard.press("Escape");
  await expect(sidebar(page).getByRole("link", { name: "Permissions" })).toHaveCount(0);
  await expect(sidebar(page).getByRole("link", { name: "Overview" })).toHaveCount(0);
  await expect(page.getByRole("button", { name: "Create Table" })).toHaveCount(0);
  await expect(page.getByRole("button", { name: "Create View" })).toHaveCount(0);
  await expect(page.getByRole("button", { name: "Remote runners" })).toHaveCount(0);
  await expect(recordsGrid.getByRole("button", { name: "Add field" })).toHaveCount(0);
  await expect(recordsGrid.getByRole("button", { name: "Field actions name" })).toHaveCount(0);
  await expect(tableCanvas.getByRole("toolbar", { name: "Table canvas actions" }).getByRole("button", { name: "Edit Row" })).toBeVisible();

  await sidebar(page).getByRole("link", { name: "Workflows", exact: true }).click();
  await expect(page.getByRole("list", { name: "Runnable workflows" }).getByText(workflowName, { exact: true })).toBeVisible();
  await expect(page.getByRole("tab", { name: "Versions" })).toHaveCount(0);
  await page.getByRole("button", { name: "Run Granted workflow" }).click();
  const runDialog = page.getByRole("dialog", { name: "Run Granted workflow" });
  await runDialog.getByRole("button", { name: "Start run" }).click();
  await expect(runDialog.getByRole("region", { name: "Run result" }).getByText("Succeeded")).toBeVisible();
  await page.keyboard.press("Escape");

  await sidebar(page).getByRole("link", { name: "Apps", exact: true }).click();
  const openLink = page.getByRole("table", { name: "Apps" }).getByRole("link", { name: `Open ${appTitle}` });
  await expect(openLink).toHaveAttribute("href", `/apps/${databaseName}/granted-app/`);
  await expect(page.getByText(/autablectl/)).toHaveCount(0);
  const appPage = await page.context().newPage();
  await appPage.goto(`/apps/${databaseName}/granted-app/`);
  await expect(appPage.getByRole("heading", { name: appTitle })).toBeVisible();
  await appPage.close();
});

test("hides workflow and app resources without resource permission", async ({ page }) => {
  const resourceUser = await registerUser(page);
  await api(page, "POST", "/api/auth/logout");
  await signInAdmin(page);
  const suffix = `${Date.now()}-${sequence}`;
  const databaseName = `scoped${suffix}`;
  const workflowName = `private-workflow-${suffix}`;
  const appTitle = `Private app ${suffix}`;

  await api(page, "POST", "/api/databases", {
    name: databaseName,
  });
  await api(page, "POST", `/api/databases/${databaseName}/tables`, {
    name: "contacts",
    display_name: "Contacts",
    fields: [{ name: "name", type: "string", deleted: false }],
    views: []
  });
  await pushWorkflow(page, databaseName, workflowName, "Private workflow");
  await uploadApp(page, databaseName, "private-app", appTitle);
  await api(page, "POST", "/api/permissions/grants", {
    subject_id: resourceUser.id,
    scope: "field_set",
    resource: `${databaseName}.contacts`,
    field: "",
    level: 1
  });

  await api(page, "POST", "/api/auth/logout");
  await loginUser(page, resourceUser.email);
  await page.goto(`/databases/${databaseName}/tables/contacts`);
  await waitForTableReady(page, databaseName);
  const tableCanvas = page.locator(".table-view");
  const tableActions = tableCanvas.getByRole("toolbar", { name: "Table canvas actions" });
  await expect(tableCanvas.getByRole("grid", { name: "Table records" }).getByRole("button", { name: "Add field" })).toHaveCount(0);
  await expect(tableActions.getByRole("button", { name: "Edit Row" })).toBeDisabled();
  await expect(tableActions.getByRole("button", { name: "Row", exact: true })).toBeDisabled();
  await sidebar(page).getByRole("link", { name: "Workflows", exact: true }).click();
  await expect(page.getByText("No workflows yet")).toBeVisible();
  await expect(page.getByText("Private workflow")).toHaveCount(0);
  await sidebar(page).getByRole("link", { name: "Apps", exact: true }).click();
  await expect(page.getByText("No apps yet")).toBeVisible();
  await expect(page.getByText(appTitle)).toHaveCount(0);
});

test("prevents partial field readers from mutating table metadata", async ({ page }) => {
  const reader = await registerUser(page);
  await api(page, "POST", "/api/auth/logout");
  await signInAdmin(page);
  const suffix = `${Date.now()}-${sequence}`;
  const databaseName = `partial${suffix}`;
  const tableName = "contacts";

  await api(page, "POST", "/api/databases", {
    name: databaseName,
  });
  await api(page, "POST", `/api/databases/${databaseName}/tables`, {
    name: tableName,
    display_name: "Contacts",
    fields: [
      { name: "name", type: "string", deleted: false },
      { name: "email", type: "string", deleted: false }
    ],
    views: []
  });
  await api(page, "POST", "/api/permissions/grants", {
    subject_id: reader.id,
    scope: "field",
    resource: `${databaseName}.${tableName}`,
    field: "email",
    level: 1
  });

  await api(page, "POST", "/api/auth/logout");
  await loginUser(page, reader.email);
  await page.goto(`/databases/${databaseName}/tables/${tableName}`);
  await waitForTableReady(page, databaseName);
  await expect(page.getByRole("button", { name: "Add field" })).toHaveCount(0);
  await expect(page.getByRole("button", { name: "Create View" })).toHaveCount(0);

  const updateStatus = await page.evaluate(
    async ({ databaseName: dbName, tableName: targetTable }) => {
      const response = await fetch(`/api/databases/${dbName}/tables/${targetTable}`, {
        method: "PUT",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          name: targetTable,
          fields: [
            { name: "email", type: "string", deleted: false },
            { name: "phone", type: "string", deleted: false }
          ]
        })
      });
      return response.status;
    },
    { databaseName, tableName }
  );
  expect(updateStatus).toBe(403);

  await loginUser(page, adminEmail);
  const catalog = (await api(page, "GET", "/api/metadata")) as {
    databases: Array<{ name: string; tables: Array<{ name: string; fields: Array<{ name: string }> }> }>;
  };
  const contacts = catalog.databases.find((item) => item.name === databaseName)?.tables.find((item) => item.name === tableName);
  const fieldNames = contacts?.fields.map((field) => field.name) ?? [];
  expect(fieldNames).toEqual(expect.arrayContaining(["name", "email"]));
  expect(fieldNames).not.toContain("phone");
});

test("covers database and table creation through the real backend", async ({ page }) => {
  const workspace = await setupWorkspace(page);

  const suffix = `${Date.now()}-${sequence}`;
  const databaseName = `sales${suffix}`;
  const tableName = `projects${suffix}`;
  const switcher = await openDatabaseSwitcher(page, workspace.databaseName);
  await switcher.getByRole("menuitem", { name: "New database" }).click();
  const createDialog = page.getByRole("dialog", { name: "New database" });
  await createDialog.getByRole("textbox", { name: "New database name" }).fill(databaseName);
  await createDialog.getByRole("button", { name: "Save" }).click();
  await expect(page).toHaveURL(`/databases/${databaseName}`);
  await expect(page.getByRole("button", { name: databaseName, exact: true })).toBeVisible();
  await expect(page.getByText(`Created database ${databaseName}`).first()).toBeVisible();

  await createNamedResource(page, "Create Table", "New table name", tableName);
  await expect(sidebar(page).getByRole("link", { name: tableName })).toHaveAttribute("aria-current", "page");
  await expect(page).toHaveURL(`/databases/${databaseName}/tables/${tableName}`);
  await expect(page.getByText(`Created table ${databaseName}.${tableName}`).first()).toBeVisible();

  const back = await openDatabaseSwitcher(page, databaseName);
  await back.getByRole("menuitemradio", { name: workspace.databaseName }).click();
  await expect(page).toHaveURL(`/databases/${workspace.databaseName}`);
  await sidebar(page).getByRole("link", { name: /Contacts/ }).click();
  await waitForTableReady(page, workspace.databaseName);
  await expect(sidebar(page).getByRole("link", { name: /Contacts/ })).toHaveAttribute("aria-current", "page");
});

test("covers table views, row creation, and row history through the real backend", async ({ page }) => {
  const workspace = await setupWorkspace(page);

  await expect(page.getByText(/\d+ of \d+ records/).first()).toBeVisible();
  const tableCanvas = page.locator(".table-view");
  const tableActions = tableCanvas.getByRole("toolbar", { name: "Table canvas actions" });
  await expect(tableActions.getByRole("button", { name: "Row", exact: true })).toBeVisible();
  await expect(page.getByRole("toolbar", { name: "Workspace actions" }).getByRole("button", { name: "Create row" })).toHaveCount(0);

  const recordsGrid = tableCanvas.getByRole("grid", { name: "Table records" });
  await createRows(
    page,
    workspace.databaseName,
    workspace.tableName,
    Array.from({ length: 30 }, (_, index) => ({
        name: `Bulk contact ${index}`,
        email: `bulk-${index}@example.com`,
        status: "Backlog"
      }))
  );
  await page.reload();
  await waitForTableReady(page, workspace.databaseName);
  await expect
    .poll(async () =>
      tableCanvas.locator(".autable-grid").evaluate((element) => {
        const grid = element as HTMLElement;
        return grid.scrollHeight > grid.clientHeight;
      })
    )
    .toBe(true);
  const gridLayout = await tableCanvas.locator(".grid-host").evaluate((element) => {
    const host = element as HTMLElement;
    const grid = host.querySelector(".autable-grid") as HTMLElement | null;
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
  await addFieldEditor.getByLabel("Field type").selectOption("string");
  await addFieldEditor.getByRole("button", { name: "Add" }).click();
  await expect(page.getByText("Added field priority").first()).toBeVisible();
  await expect(recordsGrid.getByRole("gridcell", { name: "Ada Lovelace", exact: true })).toBeVisible();
  await expect(page.getByText(/\d+ of \d+ records/).first()).not.toHaveText("0 of 0 records");

  await recordsGrid.getByRole("button", { name: "Add field" }).click();
  const formulaFieldEditor = page.getByLabel("Add field");
  await formulaFieldEditor.getByLabel("Field name").fill("summary");
  await formulaFieldEditor.getByLabel("Field type").selectOption("formula");
  await formulaFieldEditor.getByLabel("Formula value type").selectOption("string");
  await formulaFieldEditor.getByRole("textbox", { name: "Formula" }).fill(`fields["name"] + ' ' + fields["status"]`);
  await formulaFieldEditor.getByRole("button", { name: "Add" }).click();
  await expect(page.getByText("Added field summary").first()).toBeVisible();

  await recordsGrid.getByRole("button", { name: "Add field" }).click();
  const relationFieldEditor = page.getByLabel("Add field");
  await relationFieldEditor.getByLabel("Field name").fill("referrer");
  await relationFieldEditor.getByLabel("Field type").selectOption("relation");
  await relationFieldEditor.getByLabel("Target table").selectOption("contacts");
  await relationFieldEditor.getByRole("button", { name: "Add" }).click();
  await expect(page.getByText("Added field referrer").first()).toBeVisible();

  const relationRows = (await api(
    page,
    "GET",
    `/api/tables/${workspace.databaseName}/${workspace.tableName}/rows`
  )) as Array<{ record_id: number; values: { name?: string } }>;
  const adaRelationRow = relationRows.find((row) => row.values.name === "Ada Lovelace");
  const bulkRelationRow = relationRows.find((row) => row.values.name === "Bulk contact 0");
  expect(adaRelationRow?.record_id).toBeTruthy();
  expect(bulkRelationRow?.record_id).toBeTruthy();
  await api(page, "PATCH", `/api/tables/${workspace.databaseName}/${workspace.tableName}/rows/${bulkRelationRow?.record_id}`, {
    values: { referrer: adaRelationRow?.record_id }
  });
  await page.reload();
  await waitForTableReady(page, workspace.databaseName);
  await expect(recordsGrid.getByRole("gridcell", { name: "Bulk contact 0", exact: true })).toBeVisible();
  const bulkContactRow = recordsGrid.locator('[role="row"]', { hasText: "Bulk contact 0" });
  await bulkContactRow.getByText("Ada Lovelace", { exact: true }).dblclick();
  const relationPanel = page.getByLabel("Relation record detail");
  await expect(relationPanel.getByText("referrer -> record 1")).toBeVisible();
  await expect(relationPanel.getByLabel("name value")).toHaveValue("Ada Lovelace");
  await relationPanel.getByRole("button", { name: "Close" }).click();

  let formulaRows = (await api(
    page,
    "GET",
    `/api/tables/${workspace.databaseName}/${workspace.tableName}/rows`
  )) as Array<{ values: { name?: string; summary?: string } }>;
  expect(formulaRows.find((row) => row.values.name === "Ada Lovelace")?.values.summary).toBe("Ada Lovelace Active");

  await recordsGrid.getByRole("button", { name: "Field actions summary" }).click();
  await page.getByRole("menuitem", { name: "Edit formula" }).click();
  const formulaEditor = page.getByLabel("Edit formula");
  await formulaEditor.getByRole("textbox", { name: "Formula" }).fill(`fields["name"] + ' / ' + fields["status"]`);
  await formulaEditor.getByRole("button", { name: "Save" }).click();
  await expect(page.getByText("Updated formula summary").first()).toBeVisible();
  formulaRows = (await api(
    page,
    "GET",
    `/api/tables/${workspace.databaseName}/${workspace.tableName}/rows`
  )) as Array<{ values: { name?: string; summary?: string } }>;
  expect(formulaRows.find((row) => row.values.name === "Ada Lovelace")?.values.summary).toBe("Ada Lovelace / Active");

  await recordsGrid.getByRole("button", { name: "Field actions email" }).click();
  await page.getByRole("menuitem", { name: "Delete field" }).click();
  await expect(page.getByText("Deleted field email").first()).toBeVisible();

  await tableActions.getByRole("button", { name: "Row", exact: true }).click();
  await expect(page.getByText(/Created record \d+/).first()).toBeVisible();
  await recordsGrid.evaluate((grid) => {
    grid.scrollTop = grid.scrollHeight;
    grid.dispatchEvent(new Event("scroll", { bubbles: true }));
  });
  await recordsGrid.getByRole("gridcell", { name: /^New record \d+$/ }).dblclick();
  await recordsGrid.locator(".rdg-text-editor").fill("Grace Hopper");
  await page.keyboard.press("Enter");
  await expect(page.getByText(/Updated record \d+/).first()).toBeVisible();

  await recordsGrid.getByRole("gridcell", { name: "Review", exact: true }).last().dblclick();
  await recordsGrid.locator(".rdg-text-editor").fill("Active");
  await page.keyboard.press("Enter");
  await expect(page.getByText(/Updated record \d+/).first()).toBeVisible();

  const createViewButton = page.getByRole("button", { name: "Create View" });
  await expect(createViewButton).toBeVisible();
  await createViewButton.click();
  await page.getByRole("textbox", { name: "New view name" }).fill("View 2");
  await page.getByRole("button", { name: "Save" }).last().click();
  await expect(page.getByText("Created view View 2").first()).toBeVisible();
  const viewFilters = page.getByLabel("View filters");
  await viewFilters.getByRole("button", { name: "Add rule" }).click();
  await viewFilters.locator(".rule-fields").selectOption("status");
  await viewFilters.locator(".rule-operators").selectOption("=");
  await viewFilters.locator(".rule-value").fill("Active");
  await viewFilters.getByLabel("View sort field").selectOption("name");
  await viewFilters.getByLabel("View sort direction").selectOption("desc");
  await viewFilters.getByRole("button", { name: "Save View" }).click();
  await expect(page.getByText("Updated view View 2").first()).toBeVisible();

  const createdViewName = encodeURIComponent("View 2");
  const viewRows = (await api(
    page,
    "GET",
    `/api/tables/${workspace.databaseName}/${workspace.tableName}/rows?view=${createdViewName}`
  )) as Array<{ values: { name?: string } }>;
  expect(viewRows.map((row) => row.values.name)).toEqual(["Grace Hopper", "Ada Lovelace"]);

  await page.getByRole("navigation", { name: "Views" }).getByRole("link", { name: "Active", exact: true }).click();
  await expect(page.getByText(/\d+ of \d+ records/).first()).toBeVisible();
  const graceRows = (await api(
    page,
    "GET",
    `/api/tables/${workspace.databaseName}/${workspace.tableName}/rows`
  )) as Array<{ record_id: number; values: { name?: string } }>;
  const graceRow = graceRows.find((row) => row.values.name === "Grace Hopper");
  expect(graceRow?.record_id).toBeTruthy();
  await recordsGrid.getByRole("gridcell", { name: "Grace Hopper", exact: true }).click({ button: "right" });
  await page.getByRole("menuitem", { name: "View details" }).click();
  const detailsPanel = page.getByLabel("Record panel");
  await expect(detailsPanel.getByRole("tab", { name: "Details" })).toHaveAttribute("aria-selected", "true");
  await expect(detailsPanel.getByLabel("name value")).toHaveValue("Grace Hopper");
  await expect(detailsPanel.getByLabel("History record")).toHaveCount(0);
  await detailsPanel.getByRole("button", { name: "Close" }).click();

  await recordsGrid.getByRole("gridcell", { name: "Grace Hopper", exact: true }).click({ button: "right" });
  await page.getByRole("menuitem", { name: "View history" }).click();
  const recordPanel = page.getByLabel("Record panel");
  await expect(recordPanel.getByRole("tab", { name: "History" })).toHaveAttribute("aria-selected", "true");
  await expect(recordPanel.getByLabel("Row history").getByText(/Created|Updated|Record change/).first()).toBeVisible();
  const rowHistory = (await api(
    page,
    "GET",
    `/api/tables/${workspace.databaseName}/${workspace.tableName}/rows/${graceRow?.record_id}/history`
  )) as Array<{ timestamp: number }>;
  expect(typeof rowHistory[0]?.timestamp).toBe("number");
  await expect(page.getByText(new RegExp(`rhistory_${workspace.databaseName}_contacts_`))).toHaveCount(0);
  await recordPanel.getByRole("button", { name: "Close" }).click();

  await recordsGrid.getByRole("gridcell", { name: "Grace Hopper", exact: true }).click({ button: "right" });
  await page.getByRole("menuitem", { name: "Delete record" }).click();
  await expect(page.getByText(/Deleted record \d+/).first()).toBeVisible();

  const metadata = (await api(page, "GET", "/api/metadata")) as {
    databases: Array<{ name: string; tables: Array<{ name: string; fields: Array<{ name: string; type?: string; formula?: string; deleted: boolean }>; views: Array<{ name: string; query?: { rules: Array<{ field?: string; value?: string }> }; sorts: Array<{ field: string; direction: string }> }> }> }>;
  };
  const table = metadata.databases
    .find((database) => database.name === workspace.databaseName)
    ?.tables.find((item) => item.name === workspace.tableName);
  expect(table?.fields).toEqual(
    expect.arrayContaining([
      expect.objectContaining({ name: "priority", type: "string", deleted: false }),
      expect.objectContaining({ name: "summary", type: "formula", value_type: "string", formula: `fields["name"] + ' / ' + fields["status"]`, deleted: false }),
      expect.objectContaining({ name: "referrer", type: "relation", relation_table: "contacts", deleted: false }),
      expect.objectContaining({ name: "email", deleted: true })
    ])
  );
  expect(table?.views).toEqual(
    expect.arrayContaining([
      expect.objectContaining({
        name: "View 2",
        query: expect.objectContaining({ rules: [expect.objectContaining({ field: "status", value: "Active" })] }),
        sorts: [expect.objectContaining({ field: "name", direction: "desc" })]
      })
    ])
  );
});

test("covers role members and resource permission grants through the real backend", async ({ page }) => {
  const user = await registerUser(page);
  await api(page, "POST", "/api/auth/logout");
  const { databaseName, tableName } = await setupWorkspace(page);
  const workflows = (await api(page, "GET", `/api/databases/${databaseName}/workflows`)) as Array<{
    id: number;
    name: string;
    title: string;
  }>;
  const app = await uploadApp(page, databaseName, "role-app", "Role app");
  const workflow = workflows[0];
  expect(workflow).toBeTruthy();
  // The workspace listed apps before the upload; reload to pick it up.
  await page.reload();
  await waitForWorkspaceReady(page, databaseName);

  await sidebar(page).getByRole("link", { name: "Permissions", exact: true }).click();
  await createNamedResource(page, "Create Role", "New role name", "editor");
  await expect(sidebar(page).getByRole("link", { name: "editor", exact: true })).toHaveAttribute("aria-current", "page");
  const membersDialog = page.getByRole("region", { name: /^Members/ });
  await membersDialog.getByRole("combobox", { name: "Role member display name" }).fill(user.email);
  await page.getByRole("option", { name: user.email }).click();
  await expect(membersDialog.getByText(user.email)).toBeVisible();
  await page.keyboard.press("Escape");

  // Tables tab: field bits are toggle buttons; views have a read toggle.
  await page.getByRole("tab", { name: "Tables" }).click();
  await page.getByRole("button", { name: "email Update" }).click();
  await page.getByRole("button", { name: "All views Read" }).click();
  await page.getByRole("tab", { name: "Workflows" }).click();
  await page.getByLabel(`${workflow.title || workflow.name} permission`).selectOption({ label: "Run" });
  await page.getByRole("tab", { name: "Apps" }).click();
  await page.getByLabel(`${app.title} permission`).selectOption({ label: "Open" });
  await page.getByRole("button", { name: "Save", exact: true }).click();
  await expect(page.getByText("Saved role editor").first()).toBeVisible();

  const roles = (await api(page, "GET", `/api/databases/${databaseName}/roles`)) as Array<{
    name: string;
    grants: Array<{ scope: string; resource: string; field: string; level: number }>;
    members: Array<{ type: string; id: string }>;
    member_users: AuthUser[];
    created_at: number;
    updated_at: number;
  }>;
  const role = roles.find((item) => item.name === "editor");
  expect(role?.members).toEqual(expect.arrayContaining([expect.objectContaining({ type: "user", id: user.id })]));
  expect(role?.member_users.map((member) => member.email)).toContain(user.email);
  expect(typeof role?.created_at).toBe("number");
  expect(typeof role?.updated_at).toBe("number");
  expect(role?.grants).toEqual(
    expect.arrayContaining([
      expect.objectContaining({ scope: "view_set", resource: `${databaseName}.${tableName}`, field: "", level: 1 }),
      expect.objectContaining({ scope: "field", resource: `${databaseName}.${tableName}`, field: "email", level: 2 }),
      expect.objectContaining({ scope: "workflow", resource: String(workflow.id), field: "", level: 1 }),
      expect.objectContaining({ scope: "app", resource: String(app.id), field: "", level: 1 })
    ])
  );
});
