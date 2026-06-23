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
const workflowEvaluationDelayMs = 5200;

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
  expect(apiPaths).toContain("/api/auth/config");
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
  const user = (await api(page, "POST", "/api/auth/register", {
    email,
    password: "correct horse"
  })) as AuthUser;
  await page.reload();
  await expect(page.getByRole("button", { name: email })).toBeVisible();
  return user;
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

async function fillMonacoEditor(page: Page, label: string, value: string) {
  const testID = label === "Workflow JavaScript" ? "workflow-js-editor" : "form-js-editor";
  const editor = await waitForMonacoEditor(page, testID);
  await editor.evaluate((element, nextValue) => {
    const win = element.ownerDocument.defaultView as Window & {
      monaco?: {
        editor: {
          getEditors: () => Array<{
            getContainerDomNode: () => HTMLElement;
            getValue: () => string;
            setValue: (value: string) => void;
          }>;
        };
      };
    };
    const monacoEditor = win.monaco?.editor
      .getEditors()
      .find((candidate) => element.contains(candidate.getContainerDomNode()));
    if (!monacoEditor) {
      throw new Error("Monaco editor instance not found");
    }
    monacoEditor.setValue(nextValue);
  }, value);
  await expect.poll(() => monacoEditorValueByTestID(page, testID)).toBe(value);
}

async function monacoEditorValue(page: Page, label: string) {
  const testID = label === "Workflow JavaScript" ? "workflow-js-editor" : "form-js-editor";
  return monacoEditorValueByTestID(page, testID);
}

async function waitForMonacoEditor(page: Page, testID: string) {
  const editor = page.getByTestId(testID);
  await expect(editor).toBeVisible();
  await expect(editor.locator(".monaco-editor")).toBeVisible();
  await expect
    .poll(
      () =>
        editor.evaluate((element) => {
          const win = element.ownerDocument.defaultView as Window & {
            monaco?: {
              editor: {
                getEditors: () => Array<{
                  getContainerDomNode: () => HTMLElement;
                }>;
              };
            };
          };
          return Boolean(
            win.monaco?.editor
              .getEditors()
              .some((candidate) => element.contains(candidate.getContainerDomNode()))
          );
        }),
      { timeout: 30_000 }
    )
    .toBe(true);
  return editor;
}

async function monacoEditorValueByTestID(page: Page, testID: string) {
  const editor = await waitForMonacoEditor(page, testID);
  return editor.evaluate((element) => {
    const win = element.ownerDocument.defaultView as Window & {
      monaco?: {
        editor: {
          getEditors: () => Array<{
            getContainerDomNode: () => HTMLElement;
            getValue: () => string;
          }>;
        };
      };
    };
    const monacoEditor = win.monaco?.editor
      .getEditors()
      .find((candidate) => element.contains(candidate.getContainerDomNode()));
    if (!monacoEditor) {
      throw new Error("Monaco editor instance not found");
    }
    return monacoEditor.getValue();
  });
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

async function waitForWorkspaceReady(page: Page, databaseName: string, tableName = /Contacts/) {
  await expect(page.getByRole("button", { name: databaseName })).toBeVisible();
  await expect(page.getByRole("button", { name: tableName })).toBeVisible();
}

async function waitForTableReady(page: Page, databaseName: string, tableName = /Contacts/) {
  await waitForWorkspaceReady(page, databaseName, tableName);
  const tableCanvas = page.locator(".table-view");
  await expect(tableCanvas.getByRole("grid", { name: "Table records" })).toBeVisible();
  await expect(tableCanvas.getByText(/\d+ of \d+ records/).first()).toBeVisible();
}

async function setupWorkspace(page: Page): Promise<WorkspaceSetup> {
  const user = await registerUser(page);
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
  await api(page, "POST", `/api/databases/${databaseName}/workflows`, {
    database_name: databaseName,
    name: `welcome-contact-${suffix}`,
    script:
      'function instances(info) { return { welcome_echo: "echo" }; }\nfunction run(info) { const echoed = info.instance("welcome_echo").exec({ value: info.inputs.name }); return { message: echoed.value }; }',
    secrets: {},
    variables: {}
  });
  await api(page, "POST", `/api/databases/${databaseName}/forms`, {
    database_name: databaseName,
    name: `quick-status-${suffix}`,
    script:
      "function render(api, root) { root.append(api.input({ field: 'name', label: 'Name' }), api.input({ field: 'email', label: 'Email', type: 'email' }), api.select({ field: 'status', label: 'Status', options: ['Active', 'Review'] }), api.submit('Create record')); return { table: 'contacts' }; }"
  });
  await page.reload();
  await waitForTableReady(page, databaseName);
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
  await expect(page.getByRole("button", { name: "Create Role" })).toBeVisible();
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
  });
  await api(page, "POST", `/api/databases/${databaseName}/tables`, {
    name: "contacts",
    display_name: "Contacts",
    fields: [{ name: "name", type: "string", deleted: false }],
    views: []
  });
  await api(page, "POST", "/api/permissions/grants", {
    subject_id: tableOwner.id,
    scope: "workflow_set",
    resource: databaseName,
    field: "",
    level: 2
  });
  await api(page, "POST", "/api/permissions/grants", {
    subject_id: tableOwner.id,
    scope: "form_set",
    resource: databaseName,
    field: "",
    level: 2
  });

  await api(page, "POST", "/api/auth/logout");
  await loginUser(page, tableOwner.email);
  await api(page, "POST", `/api/databases/${databaseName}/workflows`, {
    name: workflowName,
    script: "function instances(info) { return { noop: 'echo' }; }\nfunction run(info) { return {}; }",
    secrets: {},
    variables: {}
  });
  await api(page, "POST", `/api/databases/${databaseName}/forms`, {
    name: formName,
    script:
      "function render(api, root) { root.append(api.input({ field: 'name', label: 'Name' }), api.submit('Save')); return { table: 'contacts' }; }"
  });

  await api(page, "POST", "/api/auth/logout");
  await loginUser(page, dbOwner.email);
  await page.goto("/");
  await waitForWorkspaceReady(page, databaseName);
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
  });
  await api(page, "POST", `/api/databases/${databaseName}/tables`, {
    name: "contacts",
    display_name: "Contacts",
    fields: [{ name: "name", type: "string", deleted: false }],
    views: []
  });
  await api(page, "POST", `/api/databases/${databaseName}/workflows`, {
    name: workflowName,
    script: "function instances(info) { return { noop: 'echo' }; }\nfunction run(info) { return {}; }",
    secrets: {},
    variables: {}
  });
  await api(page, "POST", `/api/databases/${databaseName}/forms`, {
    name: formName,
    script:
      "function render(api, root) { root.append(api.input({ field: 'name', label: 'Name' }), api.submit('Save')); return { table: 'contacts' }; }"
  });
  await api(page, "POST", "/api/permissions/grants", {
    subject_id: resourceUser.id,
    scope: "field_set",
    resource: `${databaseName}.contacts`,
    field: "",
    level: 1
  });

  await api(page, "POST", "/api/auth/logout");
  await loginUser(page, resourceUser.email);
  await page.goto("/");
  await waitForTableReady(page, databaseName);
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

test("prevents partial field readers from mutating table metadata", async ({ page }) => {
  const reader = await registerUser(page);
  await api(page, "POST", "/api/auth/logout");
  const owner = await registerUser(page);
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
  await page.goto("/");
  await waitForTableReady(page, databaseName);
  await expect(page.getByRole("button", { name: "Add field" })).toBeDisabled();
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

  await loginUser(page, owner.email);
  const catalog = (await api(page, "GET", "/api/metadata")) as {
    databases: Array<{ name: string; tables: Array<{ name: string; fields: Array<{ name: string }> }> }>;
  };
  const contacts = catalog.databases.find((item) => item.name === databaseName)?.tables.find((item) => item.name === tableName);
  const fieldNames = contacts?.fields.map((field) => field.name) ?? [];
  expect(fieldNames).toEqual(expect.arrayContaining(["name", "email"]));
  expect(fieldNames).not.toContain("phone");
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
  });
  await api(page, "POST", `/api/databases/${databaseName}/tables`, {
    name: "contacts",
    display_name: "Contacts",
    fields: [{ name: "name", type: "string", deleted: false }],
    views: []
  });
  const workflow = (await api(page, "POST", `/api/databases/${databaseName}/workflows`, {
    name: workflowName,
    script:
      "function instances(info) { return { notifier: { node: 'echo', variables: [{ name: 'channel', type: 'string' }], secrets: [{ name: 'token', type: 'string' }] } }; }\nfunction run(info) { return {}; }",
    secrets: { "notifier.token": "" },
    variables: { "notifier.channel": "ops" }
  })) as { id: number };
  const form = (await api(page, "POST", `/api/databases/${databaseName}/forms`, {
    name: formName,
    script:
      "function render(api, root) { root.append(api.input({ field: 'name', label: 'Name' }), api.submit('Submit record')); return { table: 'contacts' }; }"
  })) as { id: number };
  await api(page, "POST", "/api/permissions/grants", {
    subject_id: readOnlyUser.id,
    scope: "field_set",
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
  await waitForWorkspaceReady(page, databaseName);

  await page.getByRole("button", { name: "Workflow", exact: true }).click();
  await expect(page.getByRole("button", { name: workflowName })).toBeVisible();
  await expect(page.getByTestId("workflow-js-editor")).toHaveAttribute("aria-disabled", "true");
  await expect(page.getByRole("button", { name: "Edit config notifier" })).toBeDisabled();
  await expect(page.getByRole("button", { name: "Save" })).toBeDisabled();
  await expect(page.getByRole("button", { name: "Run" })).toBeDisabled();

  await page.getByRole("button", { name: "Form", exact: true }).click();
  await expect(page.getByRole("button", { name: formName })).toBeVisible();
  await expect(page.getByTestId("form-js-editor")).toHaveAttribute("aria-disabled", "true");
  await expect(page.getByRole("button", { name: "Save" })).toBeDisabled();
  await expect(page.getByRole("button", { name: "Submit record" })).toBeEnabled();
});

test("covers database and table creation through the real backend", async ({ page }) => {
  const workspace = await setupWorkspace(page);

  const suffix = `${Date.now()}-${sequence}`;
  const databaseName = `sales${suffix}`;
  const tableName = `projects${suffix}`;
  await createNamedResource(page, "Create DB", "New database name", databaseName);
  await expect(page.getByRole("button", { name: databaseName })).toHaveAttribute("aria-expanded", "true");
  await expect(page.getByText(`Created database ${databaseName}`).first()).toBeVisible();

  await createNamedResource(page, "Create Table", "New table name", tableName);
  await expect(page.getByRole("button", { name: tableName })).toBeVisible();
  await expect(page.getByText(`Created table ${databaseName}.${tableName}`).first()).toBeVisible();

  await page.getByRole("button", { name: workspace.databaseName, exact: true }).click();
  await waitForTableReady(page, workspace.databaseName);
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
  await relationFieldEditor.getByLabel("Field name").fill("owner");
  await relationFieldEditor.getByLabel("Field type").selectOption("relation");
  await relationFieldEditor.getByLabel("Target table").selectOption("contacts");
  await relationFieldEditor.getByRole("button", { name: "Add" }).click();
  await expect(page.getByText("Added field owner").first()).toBeVisible();

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
    values: { owner: adaRelationRow?.record_id }
  });
  await page.reload();
  await waitForTableReady(page, workspace.databaseName);
  await expect(recordsGrid.getByRole("gridcell", { name: "Bulk contact 0", exact: true })).toBeVisible();
  const bulkContactRow = recordsGrid.locator('[role="row"]', { hasText: "Bulk contact 0" });
  await bulkContactRow.getByText("Ada Lovelace", { exact: true }).dblclick();
  const relationPanel = page.getByLabel("Relation record detail");
  await expect(relationPanel.getByText("owner -> record 1")).toBeVisible();
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

  await page.getByRole("button", { name: "Active", exact: true }).click();
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
      expect.objectContaining({ name: "owner", type: "relation", relation_table: "contacts", deleted: false }),
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

test("covers workflow editor, node list, and run history through the real backend", async ({ page }) => {
  const workspace = await setupWorkspace(page);

  await page.getByRole("button", { name: "Workflow", exact: true }).click();
  const workflowName = `ui-workflow-${Date.now()}`;
  await createNamedResource(page, "Create Workflow", "New workflow name", workflowName);
  await expect(page.getByText(`Created workflow ${workflowName}`).first()).toBeVisible();
  await expect(page.getByRole("button", { name: workflowName })).toBeVisible();
  const createdWorkflows = (await api(page, "GET", `/api/databases/${workspace.databaseName}/workflows`)) as Array<{
    name: string;
    script: string;
  }>;
  const defaultWorkflowScript = createdWorkflows.find((workflow) => workflow.name === workflowName)?.script ?? "";
  expect(defaultWorkflowScript).toContain("@param {AutableWorkflowRunInfo} info");
  expect(defaultWorkflowScript).toContain("function trigger(info)");
  expect(defaultWorkflowScript).toContain('table: "contacts"');
  expect(defaultWorkflowScript).toContain("table.record.changed");
  const workflowNodes = (await api(page, "GET", "/api/workflow/nodes")) as Array<{
    type: string;
    documentation?: Record<string, string>;
    inputs: Array<{ name: string; type: string }>;
    secrets?: Array<{ name: string; type: string }>;
  }>;
  const dingTalkNode = workflowNodes.find((node) => node.type === "dingtalk.robot.send");
  expect(dingTalkNode?.inputs).toEqual(
    expect.arrayContaining([expect.objectContaining({ name: "content", type: "string" })])
  );
  expect(dingTalkNode?.secrets).toEqual(
    expect.arrayContaining([expect.objectContaining({ name: "access_token", type: "string" })])
  );
  expect(dingTalkNode?.secrets).toHaveLength(1);
  expect(dingTalkNode?.documentation?.["en-US"]).toContain("DingTalk robot");
  expect(dingTalkNode?.documentation?.["zh-CN"]).toContain("钉钉机器人");
  const headerActionLayout = await page.getByRole("button", { name: "Workflow nodes" }).evaluate((button) => {
    const nodesButton = button as HTMLElement;
    const saveButton = nodesButton.parentElement?.querySelector("button:last-child") as HTMLElement | null;
    return {
      nodesTop: Math.round(nodesButton.getBoundingClientRect().top),
      saveTop: saveButton ? Math.round(saveButton.getBoundingClientRect().top) : -1
    };
  });
  expect(headerActionLayout.nodesTop).toBe(headerActionLayout.saveTop);
  await page.getByRole("button", { name: "Workflow nodes" }).click();
  await expect(page.getByRole("dialog", { name: "Workflow node catalog" })).toBeVisible();
  await page.waitForTimeout(300);
  const dialogLayout = await page.getByRole("dialog", { name: "Workflow node catalog" }).evaluate((dialog) => {
    return {
      dialogWidth: Math.round((dialog as HTMLElement).getBoundingClientRect().width),
      viewportWidth: window.innerWidth
    };
  });
  expect(dialogLayout.dialogWidth).toBeGreaterThanOrEqual(dialogLayout.viewportWidth - 40);
  await page.getByRole("button", { name: "dingtalk.robot.send" }).click();
  await expect(page.getByText("DingTalk robot").first()).toBeVisible();
  await page.getByRole("button", { name: /table\.record\.changed/ }).click();
  await expect(page.getByText("Record changed").first()).toBeVisible();
  await expect(page.getByText(/run\(info\)\.inputs/).first()).toBeVisible();
  const nodeDialogLayout = await page.getByLabel("Workflow node documentation").evaluate((element) => {
    const list = element as HTMLElement;
    return {
      overflowY: window.getComputedStyle(list).overflowY,
      clientHeight: list.clientHeight,
      scrollHeight: list.scrollHeight
    };
  });
  expect(nodeDialogLayout.overflowY).toBe("auto");
  expect(nodeDialogLayout.scrollHeight).toBeGreaterThanOrEqual(nodeDialogLayout.clientHeight);
  await page.keyboard.press("Escape");
  await page.getByRole("button", { name: "Switch language" }).click();
  await page.getByRole("button", { name: "工作流节点" }).click();
  await page.getByRole("button", { name: "dingtalk.robot.send" }).click();
  await expect(page.getByText("钉钉机器人").first()).toBeVisible();
  await page.keyboard.press("Escape");
  await page.getByRole("button", { name: "切换语言" }).click();
  await expect(page.getByLabel("Instances").getByText("row_change")).toBeVisible();
  await fillMonacoEditor(
    page,
    "Workflow JavaScript",
    "function instances(info) {\n  return { ding: { node: 'dingtalk.robot.send' } };\n}\n\nfunction run(info) {\n  return info.instance('ding').exec({ content: 'hello' });\n}"
  );
  await page.waitForTimeout(workflowEvaluationDelayMs);
  await page.getByRole("button", { name: "Edit config ding" }).click();
  await expect(page.getByLabel("Secret ding.access_token")).toBeVisible();
  await page.keyboard.press("Escape");
  await fillMonacoEditor(
    page,
    "Workflow JavaScript",
    "function instances(info) {\n  return { row_change: { node: 'table.record.changed', variables: [{ name: 'label', type: 'string' }], secrets: [{ name: 'token', type: 'string' }] } };\n}\n\nfunction trigger(info) {\n  return { instance: 'row_change', params: { table: 'contacts', operations: ['create', 'update', 'delete'] } };\n}\n\nfunction run(info) {\n  return { record_id: 1, name: 'Ada Lovelace' };\n}"
  );
  await page.waitForTimeout(workflowEvaluationDelayMs);
  await page.getByRole("button", { name: "Edit config row_change" }).click();
  await page.getByLabel("Variable row_change.label").fill("review");
  await page.getByLabel("Secret row_change.token").fill("hidden-token");
  await page.getByRole("button", { name: "Save config" }).click();
  await expect(page.getByText("Saved instance config row_change").first()).toBeVisible();
  const savedWorkflows = (await api(
    page,
    "GET",
    `/api/databases/${workspace.databaseName}/workflows`
  )) as Array<{
    id: number;
    name: string;
    enabled: boolean;
    variables: Record<string, string>;
    secrets: Record<string, number>;
    created_at: number;
    updated_at: number;
  }>;
  const savedWorkflow = savedWorkflows.find((item) => item.name === workflowName);
  expect(savedWorkflow?.variables["row_change.label"]).toBe("review");
  expect(savedWorkflow?.secrets["row_change.token"]).toBe("hidden-token".length);
  expect(savedWorkflow?.enabled).toBe(true);
  expect(typeof savedWorkflow?.created_at).toBe("number");
  expect(typeof savedWorkflow?.updated_at).toBe("number");
  await page.getByRole("switch", { name: "Enabled" }).click();
  await expect(page.getByText(`${workflowName} disabled`).first()).toBeVisible();
  const disabledWorkflows = (await api(
    page,
    "GET",
    `/api/databases/${workspace.databaseName}/workflows`
  )) as Array<{ id: number; enabled: boolean }>;
  expect(disabledWorkflows.find((item) => item.id === savedWorkflow?.id)?.enabled).toBe(false);
  await page.getByRole("switch", { name: "Enabled" }).click();
  await expect(page.getByText(`${workflowName} enabled`).first()).toBeVisible();
  await page.getByRole("button", { name: "Edit config row_change" }).click();
  await expect(page.getByLabel("Secret row_change.token")).toHaveValue("x".repeat("hidden-token".length));
  await expect(page.getByText(/Saved secret length/)).toHaveCount(0);
  await page.keyboard.press("Escape");
  await page.getByRole("button", { name: "Run" }).click();
  await expect(page.getByText(/Workflow run saved: whistory_/).first()).toBeVisible();
  await page.getByRole("tab", { name: "History" }).click();
  await expect(page.getByRole("button", { name: "Workflow run history" })).toBeVisible();
  const runList = page.getByLabel("Workflow run nodes");
  await expect(runList.getByRole("button", { name: /Run input/ })).toBeVisible();
  await expect(runList.getByRole("button", { name: /Run output/ })).toBeVisible();
  await runList.getByRole("button", { name: /Run output/ }).click();
  await expect
    .poll(() => monacoEditorValueByTestID(page, "workflow-run-output-editor"))
    .toContain('"record_id": 1');
  const workflowRuns = (await api(page, "GET", `/api/workflows/${savedWorkflow?.id}/runs`)) as Array<{
    run: { timestamp: number };
  }>;
  expect(typeof workflowRuns[0]?.run.timestamp).toBe("number");
});

test("runs table row workflow nodes through the real backend", async ({ page }) => {
  const workspace = await setupWorkspace(page);

  await page.getByRole("button", { name: "Workflow", exact: true }).click();
  const workflowName = `row-node-workflow-${Date.now()}`;
  await createNamedResource(page, "Create Workflow", "New workflow name", workflowName);
  await expect(page.getByText(`Created workflow ${workflowName}`).first()).toBeVisible();
  await expect(page.getByRole("button", { name: workflowName })).toBeVisible();
  const rowNodeWorkflows = (await api(page, "GET", `/api/databases/${workspace.databaseName}/workflows`)) as Array<{
    id: number;
    name: string;
  }>;
  const rowNodeWorkflow = rowNodeWorkflows.find((workflow) => workflow.name === workflowName);
  expect(rowNodeWorkflow?.id).toBeTruthy();
  const workflowSubject = `workflow:${rowNodeWorkflow?.id}`;
  const tableResource = `${workspace.databaseName}.${workspace.tableName}`;
  await api(page, "POST", "/api/permissions/grants", {
    subject_id: workflowSubject,
    scope: "field_set",
    resource: tableResource,
    level: 2
  });
  await api(page, "POST", "/api/permissions/grants", {
    subject_id: workflowSubject,
    scope: "record",
    resource: tableResource,
    field: "create",
    level: 2
  });
  await api(page, "POST", "/api/permissions/grants", {
    subject_id: workflowSubject,
    scope: "record",
    resource: tableResource,
    field: "delete",
    level: 2
  });
  await fillMonacoEditor(
    page,
    "Workflow JavaScript",
    "function instances(info) {\n  return {\n    create_contact: 'table.row.create',\n    list_contacts: 'table.row.list',\n    update_contact: 'table.row.update',\n    delete_contact: 'table.row.delete'\n  };\n}\n\nfunction run(info) {\n  const created = info.instance('create_contact').exec({\n    table: 'contacts',\n    values: { name: 'Grace Hopper', email: 'grace@example.com', status: 'Review' }\n  });\n  const beforeUpdate = info.instance('list_contacts').exec({ table: 'contacts' });\n  const updated = info.instance('update_contact').exec({\n    table: 'contacts',\n    record_id: created.record.record_id,\n    values: { status: 'Active' }\n  });\n  const deleted = info.instance('delete_contact').exec({\n    table: 'contacts',\n    record_id: created.record.record_id\n  });\n  return {\n    created_id: created.record.record_id,\n    before_count: beforeUpdate.rows.length,\n    updated_status: updated.record.values.status,\n    deleted_name: deleted.record.values.name\n  };\n}"
  );
  await page.waitForTimeout(workflowEvaluationDelayMs);
  await expect(page.getByLabel("Instances").getByText("create_contact")).toBeVisible();
  await page.getByRole("button", { name: "Save" }).click();
  await expect(page.getByText(/Workflow saved as #/).first()).toBeVisible();
  await page.getByRole("button", { name: "Run" }).click();
  await expect(page.getByText(/Workflow run saved: whistory_/).first()).toBeVisible();
  await page.getByRole("tab", { name: "History" }).click();

  const rows = (await api(
    page,
    "GET",
    `/api/tables/${workspace.databaseName}/${workspace.tableName}/rows`
  )) as Array<{ record_id: number; values: Record<string, unknown> }>;
  expect(rows.some((row) => row.values.name === "Grace Hopper")).toBe(false);
  const runList = page.getByLabel("Workflow run nodes");
  await expect(runList.getByText("table.row.create")).toBeVisible();
  await expect(runList.getByText("table.row.list")).toBeVisible();
  await expect(runList.getByText("table.row.update")).toBeVisible();
  await expect(runList.getByText("table.row.delete")).toBeVisible();
  await runList.getByRole("button", { name: /create_contact/ }).click();
  await expect
    .poll(() => monacoEditorValueByTestID(page, "workflow-run-input-editor"))
    .toContain("Grace Hopper");
  await runList.getByRole("button", { name: /Run output/ }).click();
  await expect
    .poll(() => monacoEditorValueByTestID(page, "workflow-run-output-editor"))
    .toContain('"updated_status": "Active"');
});

test("persists workflow and form JavaScript into the repository path", async ({ page }) => {
  await registerUser(page);
  const suffix = `${Date.now()}-${sequence}`;
  const databaseName = `repo${suffix}`;
  await api(page, "POST", "/api/databases", {
    name: databaseName,
  });

  const workflowName = `repo-workflow-${suffix}`;
  const workflowScript = 'function instances(info) { return { noop: "echo" }; }\nfunction run(info) { return { name: info.inputs.name }; }';
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
    "workflow",
    databaseName,
    `${workflowName}.js`
  );
  expect(readFileSync(workflowPath, "utf8")).toBe(workflowScript);
  const editedWorkflowScript = "function instances(info) { return { noop: 'echo' }; }\nfunction run() { return { source: 'file' }; }";
  writeFileSync(workflowPath, editedWorkflowScript);
  const loadedWorkflow = (await api(page, "GET", `/api/workflows/${workflow.id}`)) as { script: string };
  expect(loadedWorkflow.script).toBe(editedWorkflowScript);
  const run = (await api(page, "POST", `/api/workflows/${workflow.id}/runs`, { inputs: {} })) as {
    run: { outputs: { source?: string } };
  };
  expect(run.run.outputs.source).toBe("file");

  const formName = `repo-form-${suffix}`;
  const formScript =
    "function render(api, root) { root.append(api.input({ field: 'email', label: 'Email' }), api.submit('Save')); return { table: 'contacts' }; }";
  const form = (await api(page, "POST", `/api/databases/${databaseName}/forms`, {
    database_name: databaseName,
    name: formName,
    script: formScript
  })) as { id: number };
  const formPath = join(
    runtimeDir,
    "workspace",
    "form",
    databaseName,
    `${formName}.js`
  );
  expect(readFileSync(formPath, "utf8")).toBe(formScript);
  const editedFormScript =
    "function render(api, root) { root.append(api.input({ field: 'name', label: 'From file' }), api.submit('Save')); return { table: 'contacts' }; }";
  writeFileSync(formPath, editedFormScript);
  const loadedForm = (await api(page, "GET", `/api/forms/${form.id}`)) as { script: string };
  expect(loadedForm.script).toBe(editedFormScript);
});

test("covers form runtime preview and submit through the real backend", async ({ page }) => {
  await setupWorkspace(page);

  await page.getByRole("button", { name: "Form", exact: true }).click();
  const formName = `ui-form-${Date.now()}`;
  await createNamedResource(page, "Create Form", "New form name", formName);
  await expect(page.getByText(`Created form ${formName}`).first()).toBeVisible();
  await expect(page.getByRole("button", { name: formName })).toBeVisible();
  const defaultFormScript = await monacoEditorValue(page, "Form JavaScript");
  expect(defaultFormScript).toContain("@param {AutableFormAPI} api");
  expect(defaultFormScript).toContain("@param {AutableFormRoot} root");
  await fillMonacoEditor(
    page,
    "Form JavaScript",
    "function render(api, root) {\n  root.append(\n    api.input({ field: 'name', label: 'Name' }),\n    api.input({ field: 'email', label: 'Email', type: 'email' }),\n    api.submit('Submit')\n  );\n  return { table: 'contacts' };\n}"
  );
  await expect(page.getByRole("textbox", { name: "Email", exact: true })).toBeVisible();
  await page.getByRole("button", { name: "Save" }).click();
  await expect(page.getByText(/Form saved as #/).first()).toBeVisible();
  await page.getByRole("textbox", { name: "Name", exact: true }).fill("Margaret Hamilton");
  await page.getByRole("textbox", { name: "Email", exact: true }).fill("margaret@example.com");
  await page.getByRole("button", { name: "Submit" }).click();
  await expect(page.getByText(/Form created contacts record \d+/).first()).toBeVisible();
});

test("publishes form links that require login and explicit form permission", async ({ page }) => {
  const workspace = await setupWorkspace(page);
  await page.getByRole("button", { name: "Form", exact: true }).click();
  await page.getByRole("button", { name: "Publish" }).click();
  await expect(page.getByText(/Published form/).first()).toBeVisible();
  const link = await page.getByLabel("Published form link").inputValue();
  expect(link).toContain("/forms/");
  const token = link.split("/forms/").at(-1) ?? "";
  const forms = (await api(page, "GET", `/api/databases/${workspace.databaseName}/forms`)) as Array<{
    id: number;
    published_token?: string;
    created_at: number;
    updated_at: number;
  }>;
  const form = forms.find((item) => item.published_token === token);
  expect(form?.id).toBeTruthy();
  expect(typeof form?.created_at).toBe("number");
  expect(typeof form?.updated_at).toBe("number");

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
  await api(page, "POST", "/api/permissions/grants", {
    subject_id: reader.id,
    scope: "field_set",
    resource: `${workspace.databaseName}.${workspace.tableName}`,
    field: "",
    level: 2
  });
  await api(page, "POST", "/api/permissions/grants", {
    subject_id: reader.id,
    scope: "record",
    resource: `${workspace.databaseName}.${workspace.tableName}`,
    field: "create",
    level: 2
  });

  await page.context().clearCookies();
  await page.goto(link);
  const dialog = page.getByRole("dialog");
  await expect(dialog.getByRole("button", { name: "Login" })).toBeVisible();
  await dialog.getByLabel("Email").fill(readerEmail);
  await dialog.getByLabel("Password").fill("correct horse");
  await dialog.getByRole("button", { name: "Login" }).click();
  await expect(dialog).toBeHidden();
  await expect(page.getByLabel("Name")).toBeVisible();
  await page.getByLabel("Name").fill("Published User");
  await page.getByLabel("Email").fill("published@example.com");
  await page.getByLabel("Status").selectOption("Review");
  await page.getByRole("button", { name: "Create record" }).click();
  await expect(page.getByText(/Form submitted as record \d+/).first()).toBeVisible();

  await loginUser(page, workspace.user.email);
  const rows = (await api(page, "GET", `/api/tables/${workspace.databaseName}/${workspace.tableName}/rows`)) as Array<{
    values: { name?: string; email?: string; status?: string };
  }>;
  expect(rows.map((row) => row.values)).toEqual(
    expect.arrayContaining([
      expect.objectContaining({ name: "Published User", email: "published@example.com", status: "Review" })
    ])
  );
  await page.goto("/");
  await waitForWorkspaceReady(page, workspace.databaseName);
  await page.getByRole("button", { name: "Form", exact: true }).click();
  await page.getByRole("button", { name: "Unpublish" }).click();
  await expect(page.getByText(/Unpublished form/).first()).toBeVisible();
  const unpublishedForms = (await api(page, "GET", `/api/databases/${workspace.databaseName}/forms`)) as Array<{
    id: number;
    published_token?: string;
  }>;
  expect(unpublishedForms.find((item) => item.id === form?.id)?.published_token).toBeFalsy();
});

test("covers role members and resource permission grants through the real backend", async ({ page }) => {
  const { user, databaseName, tableName } = await setupWorkspace(page);
  const workflows = (await api(page, "GET", `/api/databases/${databaseName}/workflows`)) as Array<{
    id: number;
    name: string;
  }>;
  const forms = (await api(page, "GET", `/api/databases/${databaseName}/forms`)) as Array<{
    id: number;
    name: string;
  }>;
  const workflow = workflows[0];
  const form = forms[0];
  expect(workflow).toBeTruthy();
  expect(form).toBeTruthy();

  await page.getByRole("button", { name: "Permission", exact: true }).click();
  await createNamedResource(page, "Create Role", "New role name", "editor");
  await expect(page.getByRole("button", { name: /editor/ })).toBeVisible();
  const permissionView = page.locator(".permission-view");
  await permissionView.getByRole("button", { name: /Members/ }).click();
  const membersPopover = page.getByRole("complementary", { name: "Members" }).or(page.locator(".members-popover"));
  await membersPopover.getByRole("combobox", { name: "Role member email" }).fill(user.email);
  await page.getByRole("option", { name: user.email }).click();
  await expect(membersPopover.getByText(user.email)).toBeVisible();
  await page.keyboard.press("Escape");

  await permissionView.getByRole("button", { name: "Fields Partial" }).click();
  await page.getByLabel("email permission").selectOption("2");
  await page.keyboard.press("Escape");
  await permissionView.getByRole("button", { name: "Views All" }).click();
  await page.getByRole("menuitem", { name: "Read" }).click();
  await permissionView.getByRole("button", { name: "workflow_set Partial" }).click();
  await page.getByLabel(`${workflow.name} permission`).selectOption("1");
  await page.keyboard.press("Escape");
  await permissionView.getByRole("button", { name: "form_set Partial" }).click();
  await page.getByLabel(`${form.name} permission`).selectOption("2");
  await page.keyboard.press("Escape");
  await permissionView.getByRole("button", { name: "Save" }).click();
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
      expect.objectContaining({ scope: "form", resource: String(form.id), field: "", level: 2 })
    ])
  );
});
