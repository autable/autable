import { type Page, test } from "@playwright/test";
import { mkdirSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

// Visual capture spec. Not an assertion test: it drives the real backend + UI
// and writes screenshots to e2e/.shots so we can eyeball styling.

const shotsDir = join(dirname(fileURLToPath(import.meta.url)), ".shots");
mkdirSync(shotsDir, { recursive: true });

let sequence = 0;

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

async function registerUser(page: Page) {
  sequence += 1;
  const email = `shot-${Date.now()}-${sequence}@example.com`;
  await page.goto("/");
  await page.getByRole("button", { name: "Login" }).click();
  const dialog = page.getByRole("dialog");
  await dialog.getByLabel("Email").fill(email);
  await dialog.getByLabel("Password").fill("correct horse");
  await dialog.getByRole("button", { name: "Register" }).click();
  await page.getByRole("button", { name: email }).waitFor();
  return { email };
}

async function shot(page: Page, name: string) {
  await page.screenshot({ path: join(shotsDir, `${name}.png`) });
}

async function navShot(page: Page, name: string) {
  // Clip just the two nav columns so we can inspect icon/text/selection detail.
  await page.screenshot({ path: join(shotsDir, `${name}.png`), clip: { x: 0, y: 0, width: 522, height: 900 } });
}

// Run an interaction, then screenshot; swallow failures so one flaky step
// does not abort the rest of the capture run.
// Short timeout for best-effort interactions so a flaky step fails fast
// instead of burning the whole test budget.
const bestEffort = { timeout: 6000 } as const;

async function capture(page: Page, name: string, interact: () => Promise<void>) {
  try {
    await interact();
  } catch (error) {
    console.warn(`capture ${name} interaction failed:`, error instanceof Error ? error.message : error);
  }
  await shot(page, name);
}

test("capture workspace screenshots", async ({ page }) => {
  test.setTimeout(180_000);
  await page.setViewportSize({ width: 1440, height: 900 });

  // Login dialog before auth.
  await page.goto("/");
  await page.getByRole("button", { name: "Login" }).click();
  await page.getByRole("dialog").waitFor();
  await page.getByRole("button", { name: "Register" }).waitFor();
  await page.waitForTimeout(600);
  await shot(page, "01-login-dialog");
  await page.keyboard.press("Escape");

  const { email } = await registerUser(page);
  const suffix = `${Date.now()}-${sequence}`;
  const databaseName = `shots${suffix}`;
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
      { name: "status", type: "string", deleted: false },
      { name: "score", type: "int", deleted: false }
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
  const people = [
    ["Ada Lovelace", "ada@example.com", "Active", 91],
    ["Grace Hopper", "grace@example.com", "Review", 88],
    ["Margaret Hamilton", "margaret@example.com", "Active", 95],
    ["Katherine Johnson", "katherine@example.com", "Backlog", 99],
    ["Dorothy Vaughan", "dorothy@example.com", "Active", 84]
  ] as const;
  for (const [name, mail, status, score] of people) {
    await api(page, "POST", `/api/tables/${databaseName}/${tableName}/rows`, {
      values: { name, email: mail, status, score }
    });
  }
  await api(page, "POST", `/api/databases/${databaseName}/workflows`, {
    database_name: databaseName,
    name: `welcome-${suffix}`,
    script:
      "function instances(info) {\n  return { row_change: { node: 'table.record.changed', variables: [{ name: 'label', type: 'string' }], secrets: [{ name: 'token', type: 'string' }] } };\n}\n\nfunction trigger(info) {\n  return { instance: 'row_change', params: { table: 'contacts', operations: ['create', 'update'] } };\n}\n\nfunction run(info) {\n  return { record_id: 1, name: 'Ada Lovelace' };\n}",
    secrets: {},
    variables: {}
  });
  await api(page, "POST", `/api/databases/${databaseName}/forms`, {
    database_name: databaseName,
    name: `signup-${suffix}`,
    script:
      "function render(api, root) {\n  root.append(\n    api.input({ field: 'name', label: 'Name' }),\n    api.input({ field: 'email', label: 'Email', type: 'email' }),\n    api.select({ field: 'status', label: 'Status', options: ['Active', 'Review', 'Backlog'] }),\n    api.submit('Create record')\n  );\n  return { table: 'contacts' };\n}"
  });
  await page.reload();
  await page.getByRole("button", { name: databaseName }).waitFor();
  await page.getByRole("button", { name: /Contacts/ }).waitFor();

  // Table view.
  await page.getByRole("button", { name: /Contacts/ }).click();
  await page.locator(".autable-grid").waitFor();
  await page.waitForTimeout(400);
  await shot(page, "02-table-view");
  await navShot(page, "nav-01-table");
  await page.screenshot({ path: join(shotsDir, "nav-zoom-primary.png"), clip: { x: 0, y: 0, width: 260, height: 300 } });

  // Collapsed primary nav rail.
  await capture(page, "nav-03-rail", async () => {
    await page.getByRole("button", { name: /Collapse/ }).click(bestEffort);
    await page.waitForTimeout(300);
  });
  await page.screenshot({ path: join(shotsDir, "nav-zoom-rail.png"), clip: { x: 0, y: 0, width: 320, height: 420 } });
  await page.getByRole("button", { name: /Expand/ }).click(bestEffort).catch(() => undefined);
  await page.waitForTimeout(200);

  // Filter popover (best-effort; never abort the whole capture run on one flaky step).
  await capture(page, "03-table-filter-popover", async () => {
    await page.getByRole("button", { name: "Active", exact: true }).click(bestEffort);
    await page.getByRole("button", { name: "Filter" }).click(bestEffort);
    await page.getByLabel("View filters").waitFor(bestEffort);
    await page.waitForTimeout(200);
  });
  await page.keyboard.press("Escape");

  // Add field popover.
  await capture(page, "04-table-add-field", async () => {
    await page.getByRole("button", { name: "Add field" }).click(bestEffort);
    await page.getByRole("group", { name: "Add field" }).waitFor(bestEffort);
    await page.getByLabel("Field type").selectOption("relation", bestEffort);
    await page.waitForTimeout(200);
  });
  await page.keyboard.press("Escape");

  // Record drawer.
  await capture(page, "05-record-drawer", async () => {
    await page.getByRole("gridcell", { name: "Ada Lovelace", exact: true }).click({ button: "right", ...bestEffort });
    await page.getByRole("menuitem", { name: "View details" }).click(bestEffort);
    await page.getByRole("complementary", { name: "Record panel" }).waitFor(bestEffort);
    await page.waitForTimeout(200);
  });
  await page.getByRole("button", { name: "Close" }).click({ ...bestEffort }).catch(() => undefined);

  // Workflow view.
  await page.getByRole("button", { name: "Workflow", exact: true }).click();
  await page.getByText("Instances").waitFor();
  await page.waitForTimeout(500);
  await shot(page, "06-workflow-editor");
  await navShot(page, "nav-02-workflow");

  // Node catalog dialog.
  await page.getByRole("button", { name: "Workflow nodes" }).click();
  await page.getByRole("dialog", { name: "Workflow node catalog" }).waitFor();
  await page.waitForTimeout(400);
  await shot(page, "07-workflow-node-catalog");
  await page.keyboard.press("Escape");

  // Workflow run + history.
  await capture(page, "08-workflow-history", async () => {
    await page.getByRole("button", { name: "Run" }).click(bestEffort);
    await page.getByText(/Workflow run saved/).waitFor(bestEffort);
    await page.getByRole("tab", { name: "History" }).click(bestEffort);
    await page.locator(".workflow-run-node-list").waitFor(bestEffort);
    await page.waitForTimeout(800);
  });

  // Form view.
  await page.getByRole("button", { name: "Form", exact: true }).click();
  await page.getByText("Preview").waitFor();
  await page.waitForTimeout(400);
  await shot(page, "09-form-editor");

  // Permission view — capture the empty state before creating a role.
  await page.getByRole("button", { name: "Permission", exact: true }).click();
  await page.waitForTimeout(300);
  await shot(page, "19-permission-empty");
  await page.getByRole("button", { name: "Create Role" }).click();
  await page.getByRole("textbox", { name: "New role name" }).fill("editor");
  await page.getByRole("button", { name: "Save" }).last().click();
  await page.getByRole("button", { name: /editor/ }).waitFor();
  await page.waitForTimeout(300);
  await shot(page, "10-permission-matrix");

  // Partial popover (per-item levels) from the connected split control.
  await capture(page, "13-permission-partial", async () => {
    await page.getByRole("button", { name: /Partial/ }).first().click(bestEffort);
    await page.waitForTimeout(250);
  });
  await page.keyboard.press("Escape");

  // Members popover (users): count badge + add/list.
  await capture(page, "12-members-popover", async () => {
    await page.getByRole("button", { name: /Members/ }).click(bestEffort);
    await page.getByRole("combobox", { name: "Role member email" }).waitFor(bestEffort);
    await page.waitForTimeout(200);
  });
  await page.keyboard.press("Escape");

  // Workflow members popover (separate from users).
  await capture(page, "14-workflow-members", async () => {
    await page.getByRole("button", { name: /Workflows/ }).click(bestEffort);
    await page.waitForTimeout(250);
  });
  await page.keyboard.press("Escape");

  // Switch to Chinese to verify translations + disabled Save.
  await capture(page, "15-permission-zh", async () => {
    await page.getByRole("button", { name: "Switch language" }).click(bestEffort);
    await page.waitForTimeout(300);
  });
  const permissionView = page.locator(".permission-view");
  await capture(page, "16-workflow-members-zh", async () => {
    await permissionView.getByRole("button", { name: /工作流|Workflows/ }).first().click(bestEffort);
    await page.waitForTimeout(250);
  });
  await page.keyboard.press("Escape");
  await capture(page, "17-permission-zh-partial", async () => {
    await permissionView.getByRole("button", { name: /部分|Partial/ }).first().click(bestEffort);
    await page.waitForTimeout(250);
  });
  await page.keyboard.press("Escape");

  // Narrow viewport (responsive) on the permission matrix.
  await page.setViewportSize({ width: 760, height: 900 });
  await page.waitForTimeout(400);
  await shot(page, "11-narrow-permission");
  await page.setViewportSize({ width: 1440, height: 900 });

  // Empty database: table/workflow/form empty states.
  const emptyDb = `empty${Date.now()}-${sequence}`;
  await api(page, "POST", "/api/databases", { name: emptyDb });
  await page.reload();
  await capture(page, "20-table-empty", async () => {
    await page.getByRole("button", { name: emptyDb }).click(bestEffort);
    await page.waitForTimeout(400);
  });
});
