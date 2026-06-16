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
      { name: "name", type: "text", required: true, deleted: false },
      { name: "email", type: "email", required: false, deleted: false },
      { name: "status", type: "text", required: false, deleted: false }
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
      "root.append(api.input({ name: 'name', label: 'Name', required: true }), api.input({ name: 'email', label: 'Email', type: 'email' }), api.select({ name: 'status', label: 'Status', options: ['Active', 'Review'] }), api.submit('Create record'));"
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
  await page.getByRole("button", { name: "Fields" }).click();
  let dialog = page.getByRole("dialog");
  await dialog.getByLabel("New field name").fill("priority");
  await dialog.getByLabel("New field type").selectOption("text");
  await dialog.getByRole("button", { name: "Add Field" }).click();
  await expect(page.getByText("Added field priority")).toBeVisible();
  await dialog.getByRole("button", { name: "Delete field email" }).click();
  await expect(page.getByText("Deleted field email")).toBeVisible();
  await dialog.getByRole("button", { name: "Close" }).click();

  await page.getByRole("button", { name: "Row", exact: true }).click();
  await expect(page.getByText(/Created record \d+/)).toBeVisible();
  await page.getByRole("button", { name: "Edit Row" }).click();
  dialog = page.getByRole("dialog");
  await dialog.getByLabel("name value").fill("Grace Hopper");
  await dialog.getByLabel("status value").fill("Active");
  await dialog.getByRole("button", { name: "Save Row" }).click();
  await expect(page.getByText(/Updated record \d+/)).toBeVisible();
  await dialog.getByRole("button", { name: "Close" }).click();

  await page.getByRole("button", { name: "View", exact: true }).click();
  dialog = page.getByRole("dialog");
  await dialog.getByLabel("New view name").fill("active-desc");
  await dialog.getByLabel("Base view").selectOption("active");
  await dialog.getByLabel("View sort field").selectOption("name");
  await dialog.getByLabel("View sort direction").selectOption("desc");
  await dialog.getByRole("button", { name: "Create View" }).click();
  await expect(page.getByText("Created view active-desc")).toBeVisible();
  await dialog.getByRole("button", { name: "Close" }).click();

  const viewRows = (await api(
    page,
    "GET",
    `/api/tables/${workspace.databaseName}/${workspace.tableName}/rows?view=active-desc`
  )) as Array<{ values: { name?: string } }>;
  expect(viewRows.map((row) => row.values.name)).toEqual(["Grace Hopper", "Ada Lovelace"]);

  await page.getByLabel("Table view").selectOption("active");
  await expect(page.getByText(/\d+ of \d+ records/).first()).toBeVisible();
  await page.getByRole("button", { name: "History" }).click();
  await expect(page.getByText(new RegExp(`rhistory_${workspace.databaseName}_contacts_`)).first()).toBeVisible();
  await page.getByRole("button", { name: "Delete Row" }).click();
  await expect(page.getByText(/Deleted record \d+/)).toBeVisible();

  const metadata = (await api(page, "GET", "/api/metadata")) as {
    databases: Array<{ name: string; tables: Array<{ name: string; fields: Array<{ name: string; deleted: boolean }>; views: Array<{ name: string; base_view?: string }> }> }>;
  };
  const table = metadata.databases
    .find((database) => database.name === workspace.databaseName)
    ?.tables.find((item) => item.name === workspace.tableName);
  expect(table?.fields).toEqual(
    expect.arrayContaining([
      expect.objectContaining({ name: "priority", deleted: false }),
      expect.objectContaining({ name: "email", deleted: true })
    ])
  );
  expect(table?.views).toEqual(expect.arrayContaining([expect.objectContaining({ name: "active-desc", base_view: "active" })]));
});

test("covers workflow editor, node list, and run history through the real backend", async ({ page }) => {
  const workspace = await setupWorkspace(page);

  await page.getByRole("button", { name: "Workflow", exact: true }).click();
  const workflowName = `ui-workflow-${Date.now()}`;
  await page.getByRole("textbox", { name: "New workflow name" }).fill(workflowName);
  await page.getByRole("button", { name: "Create Workflow" }).click();
  await expect(page.getByText(`Created workflow ${workflowName}`)).toBeVisible();
  await expect(page.getByRole("button", { name: workflowName })).toBeVisible();
  await expect(page.getByLabel("Workflow JavaScript")).toHaveValue(/info\.node/);
  await expect(page.getByText("echo").first()).toBeVisible();
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
  await expect(page.getByLabel("Workflow run flow").getByText("table.record.changed")).toBeVisible();
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
  const formScript = "root.append(api.input({ name: 'email' }))";
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
  const editedFormScript = "root.append(api.input({ name: 'from_file' }))";
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
  await expect(page.getByText(/Form created record \d+/)).toBeVisible();
});

test("covers role members and resource permission grants through the real backend", async ({ page }) => {
  const { user, databaseName, tableName } = await setupWorkspace(page);

  await page.getByRole("button", { name: "Permission", exact: true }).click();
  await page.getByRole("textbox", { name: "New role name" }).fill("editor");
  await page.getByRole("button", { name: "Create Role" }).click();
  await expect(page.getByRole("button", { name: /editor/ })).toBeVisible();
  await page.getByRole("textbox", { name: "Role member user id" }).fill(user.id);
  await page.getByRole("button", { name: "Add role member" }).click();
  await expect(page.getByText(user.id)).toBeVisible();
  await page.getByLabel("contacts permission").selectOption("2");
  await page.getByLabel("email permission").selectOption("1");
  await page.getByRole("button", { name: "Save" }).click();
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
