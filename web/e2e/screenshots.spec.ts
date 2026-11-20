import { type Page, test } from "@playwright/test";
import { mkdirSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";
import { appBundle } from "./appBundle";
import { workflowBundle } from "./workflowBundle";

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

// The screenshots show the full admin UI, so they are taken as the shared
// e2e admin (see start-backend.mjs): registered on first use, else logged in.
async function signInAdmin(page: Page) {
  sequence += 1;
  const email = "e2e-admin@example.com";
  await page.goto("/");
  const form = page.getByRole("form", { name: "Sign in" });
  await form.getByRole("tab", { name: "Register" }).click();
  await form.getByLabel("Email").fill(email);
  await form.getByLabel("Display name").fill(email);
  await form.getByLabel("Password").fill("correct horse");
  const registered = page.waitForResponse((response) => response.url().endsWith("/api/auth/register"));
  await form.getByRole("button", { name: "Register" }).click();
  if ((await registered).status() === 409) {
    await form.getByRole("tab", { name: "Login" }).click();
    await form.getByLabel("Email").fill(email);
    await form.getByLabel("Password").fill("correct horse");
    await form.getByRole("button", { name: "Login" }).click();
  }
  await page.getByRole("navigation", { name: "Workspace" }).or(page.getByText("No databases yet")).first().waitFor();
  return { email };
}

function sidebar(page: Page) {
  return page.getByRole("navigation", { name: "Workspace" });
}

async function uploadApp(page: Page, databaseName: string, name: string, title: string, query = "") {
  const response = await page.request.post(`/api/databases/${databaseName}/apps/${name}/versions${query}`, {
    headers: { "Content-Type": "application/gzip" },
    data: appBundle({ database: databaseName, name, title, heading: title })
  });
  if (!response.ok()) {
    throw new Error(`app upload failed: ${response.status()} ${await response.text()}`);
  }
}

async function shot(page: Page, name: string) {
  await page.screenshot({ path: join(shotsDir, `${name}.png`) });
}

async function navShot(page: Page, name: string) {
  // Clip just the sidebar so we can inspect icon/text/selection detail.
  await page.screenshot({ path: join(shotsDir, `${name}.png`), clip: { x: 0, y: 0, width: 256, height: 900 } });
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

  // Sign-in page before auth.
  await page.goto("/");
  await page.getByRole("form", { name: "Sign in" }).waitFor();
  await page.getByRole("tab", { name: "Register" }).waitFor();
  await page.waitForTimeout(600);
  await shot(page, "01-login");

  await signInAdmin(page);
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
  const workflowName = `welcome-${suffix}`;
  const pushed = await page.request.post(`/api/databases/${databaseName}/workflows/${workflowName}/versions?publish=1`, {
    headers: { "Content-Type": "application/gzip" },
    data: workflowBundle({
      database: databaseName,
      name: workflowName,
      title: "Welcome contact",
      release: "v1",
      triggers: {
        start: { kind: "manual", fields: [{ name: "note", label: "Note", type: "string" }] },
        changed: { kind: "recordChanged", table: tableName, operations: ["create", "update"] }
      }
    })
  });
  if (!pushed.ok()) {
    throw new Error(`workflow push failed: ${pushed.status()} ${await pushed.text()}`);
  }
  await uploadApp(page, databaseName, "orders", "Orders", "?message=First%20version&publish=1");
  await uploadApp(page, databaseName, "orders", "Orders", "?message=Adds%20a%20totals%20row&publish=1");
  await uploadApp(page, databaseName, "orders", "Orders", "?message=Work%20in%20progress");
  await uploadApp(page, databaseName, "stock-check", "Stock check");
  // Selection is scoped to the URL route, so land on the database directly.
  await page.goto(`/databases/${databaseName}`);
  await page.getByRole("button", { name: databaseName, exact: true }).waitFor();
  await sidebar(page).getByRole("link", { name: /Contacts/ }).waitFor();
  await shot(page, "02-home");

  // Table view.
  await sidebar(page).getByRole("link", { name: /Contacts/ }).click();
  await page.locator(".autable-grid").waitFor();
  await page.waitForTimeout(400);
  await shot(page, "02-table-view");
  await navShot(page, "nav-01-table");
  await page.screenshot({ path: join(shotsDir, "nav-zoom-primary.png"), clip: { x: 0, y: 0, width: 256, height: 300 } });

  // Filter popover (best-effort; never abort the whole capture run on one flaky step).
  await capture(page, "03-table-filter-popover", async () => {
    await page.getByRole("navigation", { name: "Views" }).getByRole("link", { name: "Active", exact: true }).click(bestEffort);
    await page.getByRole("button", { name: "Filter" }).click(bestEffort);
    await page.getByLabel("View filters").waitFor(bestEffort);
    await page.waitForTimeout(200);
  });
  await page.keyboard.press("Escape");

  // Add field popover.
  await capture(page, "04-table-add-field", async () => {
    await page.getByRole("button", { name: "Add field" }).click(bestEffort);
    await page.getByRole("dialog", { name: "Add field" }).waitFor(bestEffort);
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

  // Workflows: the list, one workflow's page, then a run.
  await sidebar(page).getByRole("link", { name: "Workflows", exact: true }).click();
  await page.getByRole("table", { name: "Workflows" }).waitFor();
  await page.waitForTimeout(400);
  await shot(page, "06-workflow-list");
  await navShot(page, "nav-02-workflow");
  await page.getByRole("table", { name: "Workflows" }).getByRole("link", { name: "Welcome contact" }).click();
  await page.getByRole("group", { name: "Trigger changed" }).waitFor();
  await page.waitForTimeout(400);
  await shot(page, "07-workflow-detail");
  await capture(page, "08-workflow-run", async () => {
    await page.getByRole("button", { name: "Run", exact: true }).first().click(bestEffort);
    const dialog = page.getByRole("dialog", { name: "Run Welcome contact" });
    await dialog.getByLabel("Note").fill("hello", bestEffort);
    await dialog.getByRole("button", { name: "Start run" }).click(bestEffort);
    await dialog.getByRole("button", { name: "Open run" }).click(bestEffort);
    await page.getByText("checked input").waitFor(bestEffort);
    await page.waitForTimeout(400);
  });

  // Apps: list, then one app's versions.
  await sidebar(page).getByRole("link", { name: "Apps", exact: true }).click();
  await page.getByRole("table", { name: "Apps" }).waitFor();
  await page.waitForTimeout(400);
  await shot(page, "09-app-list");
  await page.getByRole("table", { name: "Apps" }).getByRole("button", { name: "Orders" }).click();
  await page.getByRole("table", { name: "Versions" }).waitFor();
  await page.waitForTimeout(400);
  await shot(page, "09-app-detail");

  // Permission view — capture the empty state before creating a role.
  await sidebar(page).getByRole("link", { name: "Permissions", exact: true }).click();
  await page.waitForTimeout(300);
  await shot(page, "19-permission-empty");
  await page.getByRole("button", { name: "Create Role" }).click();
  await page.getByRole("textbox", { name: "New role name" }).fill("editor");
  await page.getByRole("button", { name: "Save" }).last().click();
  await sidebar(page).getByRole("link", { name: "editor", exact: true }).waitFor();
  await page.waitForTimeout(300);
  await shot(page, "10-permission-matrix");

  // Member search: matching people appear under the combobox.
  await capture(page, "12-members-search", async () => {
    await page.getByRole("combobox", { name: "Role member display name" }).fill("e2e", bestEffort);
    await page.getByRole("listbox").waitFor(bestEffort);
    await page.waitForTimeout(200);
  });
  await page.keyboard.press("Escape");

  // Apps tab: an all-apps row and one none/open row per app.
  await capture(page, "13-permission-apps", async () => {
    await page.getByRole("tab", { name: "Apps" }).click(bestEffort);
    await page.getByLabel("All apps permission").waitFor(bestEffort);
    await page.waitForTimeout(200);
  });

  // Workflows tab: one none/run row per workflow.
  await capture(page, "14-permission-workflows", async () => {
    await page.getByRole("tab", { name: "Workflows" }).click(bestEffort);
    await page.getByLabel(/permission$/).first().waitFor(bestEffort);
    await page.waitForTimeout(200);
  });

  // Switch to Chinese to verify translations.
  await capture(page, "15-permission-zh", async () => {
    await page.getByRole("button", { name: "Switch language" }).click(bestEffort);
    await page.getByRole("tab", { name: "表" }).click(bestEffort);
    await page.waitForTimeout(300);
  });
  await capture(page, "16-permission-workflows-zh", async () => {
    await page.getByRole("tab", { name: "工作流" }).click(bestEffort);
    await page.waitForTimeout(250);
  });
  await capture(page, "17-permission-apps-zh", async () => {
    await page.getByRole("tab", { name: "应用" }).click(bestEffort);
    await page.getByLabel("全部应用 permission").waitFor(bestEffort);
    await page.waitForTimeout(250);
  });

  // Narrow viewport (responsive) on the permission matrix.
  await page.setViewportSize({ width: 760, height: 900 });
  await page.waitForTimeout(400);
  await shot(page, "11-narrow-permission");
  await page.setViewportSize({ width: 1440, height: 900 });

  // Back to English for the remaining captures.
  await page.getByRole("button", { name: "切换语言" }).click(bestEffort).catch(() => undefined);

  // Empty database: table and app empty states.
  const emptyDb = `empty${Date.now()}-${sequence}`;
  await api(page, "POST", "/api/databases", { name: emptyDb });
  await capture(page, "20-database-empty", async () => {
    await page.goto(`/databases/${emptyDb}`);
    await page.getByText("No tables yet").first().waitFor(bestEffort);
    await page.waitForTimeout(400);
  });
  await capture(page, "21-app-empty", async () => {
    await page.goto(`/databases/${emptyDb}/apps`);
    await page.getByText(/No apps yet|还没有应用/).waitFor(bestEffort);
    await page.waitForTimeout(300);
  });
});
