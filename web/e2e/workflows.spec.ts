import { expect, type Page, test } from "@playwright/test";
import { defaultManualTrigger, workflowBundle } from "./workflowBundle";

// Workflows v2 through the real backend: bundles are uploaded the way
// `autablectl workflow push` does, then published, run, configured, rolled
// back, permission-checked and deleted from the web UI.

type User = { id: string; email: string };

let sequence = 0;
// The only global admin in the e2e config (see start-backend.mjs).
const adminEmail = "e2e-admin@example.com";
const password = "correct horse";

test.describe.configure({ mode: "serial" });

async function register(page: Page): Promise<User> {
  sequence += 1;
  const email = `workflows-${Date.now()}-${sequence}@example.com`;
  const response = await page.request.post("/api/auth/register", { data: { email, password, display_name: email } });
  expect(response.ok(), await response.text()).toBe(true);
  return (await response.json()) as User;
}

async function signInAdmin(page: Page): Promise<User> {
  const credentials = { email: adminEmail, password };
  let response = await page.request.post("/api/auth/register", { data: { ...credentials, display_name: adminEmail } });
  if (response.status() === 409) {
    response = await page.request.post("/api/auth/login", { data: credentials });
  }
  expect(response.ok(), await response.text()).toBe(true);
  return (await response.json()) as User;
}

async function login(page: Page, email: string) {
  await page.request.post("/api/auth/logout");
  const response = await page.request.post("/api/auth/login", { data: { email, password } });
  expect(response.ok(), await response.text()).toBe(true);
}

async function apiPost(page: Page, path: string, data: unknown) {
  const response = await page.request.post(path, { data });
  expect(response.ok(), `${path}: ${await response.text()}`).toBe(true);
  const text = await response.text();
  return text ? JSON.parse(text) : null;
}

async function createDatabase(page: Page) {
  sequence += 1;
  const database = `flows${Date.now()}${sequence}`;
  await apiPost(page, "/api/databases", { name: database });
  await apiPost(page, `/api/databases/${database}/tables`, {
    name: "contacts",
    display_name: "Contacts",
    fields: [
      { name: "name", type: "string", deleted: false },
      { name: "status", type: "string", deleted: false }
    ],
    views: []
  });
  return database;
}

async function push(page: Page, database: string, name: string, release: string, query: string, triggers?: Record<string, unknown>) {
  const response = await page.request.post(`/api/databases/${database}/workflows/${name}/versions${query}`, {
    headers: { "Content-Type": "application/gzip" },
    data: workflowBundle({ database, name, title: "Contact review", release, triggers })
  });
  expect(response.ok(), await response.text()).toBe(true);
  return (await response.json()) as { workflow: { id: number }; version: { version: number } };
}

const allTriggers = {
  start: defaultManualTrigger,
  changed: { kind: "recordChanged", table: "contacts", operations: ["update"], fields: ["status"] },
  nightly: { kind: "schedule", cron: "0 9 * * 1-5", timezone: "Asia/Shanghai" },
  hook: { kind: "webhook", path: "approval" }
};

function sidebar(page: Page) {
  return page.getByRole("navigation", { name: "Workspace" });
}

test("admins publish, run, configure, roll back and delete a pushed workflow", async ({ page }) => {
  await page.goto("/");
  await signInAdmin(page);
  const database = await createDatabase(page);

  await page.goto(`/databases/${database}/workflows`);
  await expect(page.getByText("No workflows yet")).toBeVisible();
  await expect(page.getByText(`autablectl workflow init my-workflow --database ${database} --name my-workflow`)).toBeVisible();

  await push(page, database, "contact-review", "first", "?message=First%20cut&publish=1", allTriggers);
  await push(page, database, "contact-review", "second", "?message=Second%20cut&publish=1", allTriggers);
  await page.reload();
  const row = page.getByRole("table", { name: "Workflows" }).getByRole("row", { name: "contact-review" });
  await expect(row.getByText("Enabled")).toBeVisible();
  await expect(row.getByText("v2", { exact: true })).toBeVisible();
  await expect(row.getByText("Every weekday 09:00 (Asia/Shanghai)")).toBeVisible();
  await expect(row.getByText("On change: contacts · status")).toBeVisible();
  await expect(row.getByText("Webhook /approval")).toBeVisible();
  await expect(row.getByText("Manual", { exact: true })).toBeVisible();
  await expect(row.getByText("Never run")).toBeVisible();

  await row.getByRole("link", { name: "Contact review" }).click();
  await expect(page).toHaveURL(`/databases/${database}/workflows/contact-review`);
  await expect(page.getByRole("heading", { name: "Contact review", level: 1 })).toBeVisible();
  await expect(sidebar(page).getByRole("link", { name: "Contact review" })).toHaveAttribute("aria-current", "page");
  const hook = page.getByRole("group", { name: "Trigger hook" });
  await expect(hook.getByText(`http://127.0.0.1:5175/api/hooks/${database}/contact-review/approval`)).toBeVisible();
  await expect(page.getByRole("group", { name: "Trigger changed" }).getByText("Ignored: changes made by this workflow's runs do not fire it")).toBeVisible();

  // Run the manual trigger with its generated form.
  await page.getByRole("button", { name: "Run", exact: true }).first().click();
  let dialog = page.getByRole("dialog", { name: "Run Contact review" });
  await dialog.getByLabel("Note").fill("hello");
  await dialog.getByLabel("Count").fill("3");
  await dialog.getByRole("checkbox", { name: "Urgent" }).check();
  await dialog.getByRole("button", { name: "Start run" }).click();
  let result = dialog.getByRole("region", { name: "Run result" });
  await expect(result.getByText("Succeeded")).toBeVisible();
  await expect(result.getByLabel("Output")).toContainText('"release": "second"');
  await expect(result.getByLabel("Output")).toContainText('"note": "hello"');
  await expect(result.getByLabel("Output")).toContainText('"count": 3');
  await expect(result.getByLabel("Output")).toContainText('"hasToken": false');
  await dialog.getByRole("button", { name: "Open run" }).click();
  await expect(page).toHaveURL(new RegExp(`/databases/${database}/workflows/contact-review/runs/\\d+$`));
  const runID = Number(page.url().split("/").pop());
  const run = page.getByRole("region", { name: `Run #${runID}` });
  await expect(run.getByText("Succeeded")).toBeVisible();
  await expect(run.getByText("starting run")).toBeVisible();
  await expect(run.getByText("checked input")).toBeVisible();
  await expect(run.getByText("warn", { exact: true })).toBeVisible();
  await run.getByText("Trigger event").click();
  await expect(run.getByLabel("Trigger event")).toContainText('"urgent": true');
  await expect(page.getByRole("list", { name: "Runs" }).getByRole("button", { name: `Run ${runID}, Succeeded` })).toHaveAttribute("aria-current", "true");

  // Secrets are write-only; vars are plain.
  await page.getByRole("tab", { name: "Secrets & vars" }).click();
  await expect(page).toHaveURL(`/databases/${database}/workflows/contact-review/config`);
  const secretForm = page.getByRole("form", { name: "Set secret" });
  await secretForm.getByLabel("Secret name").fill("API_TOKEN");
  await secretForm.getByLabel("Secret value").fill("very-secret-value");
  await secretForm.getByRole("button", { name: "Set secret" }).click();
  await expect(page.getByText("Saved secret API_TOKEN").first()).toBeVisible();
  await expect(page.getByRole("list", { name: "Secrets" }).getByText("API_TOKEN")).toBeVisible();
  await expect(page.getByText("very-secret-value")).toHaveCount(0);
  const detail = await (await page.request.get(`/api/databases/${database}/workflows/contact-review`)).text();
  expect(detail).toContain("API_TOKEN");
  expect(detail).not.toContain("very-secret-value");
  const varForm = page.getByRole("form", { name: "Add var" });
  await varForm.getByLabel("Var name").fill("REGION");
  await varForm.getByLabel("Value").fill("eu");
  await varForm.getByRole("button", { name: "Add var" }).click();
  await expect(page.getByLabel("Value of REGION")).toHaveValue("eu");

  await page.getByRole("button", { name: "Run", exact: true }).click();
  dialog = page.getByRole("dialog", { name: "Run Contact review" });
  await dialog.getByRole("button", { name: "Start run" }).click();
  result = dialog.getByRole("region", { name: "Run result" });
  await expect(result.getByLabel("Output")).toContainText('"hasToken": true');
  await expect(result.getByLabel("Output")).toContainText('"region": "eu"');
  await page.keyboard.press("Escape");

  // Rolling back is publishing an older version, after a confirmation.
  await page.getByRole("tab", { name: "Versions" }).click();
  const versions = page.getByRole("table", { name: "Versions" });
  await expect(versions.getByRole("row", { name: "v2" }).getByText("Published", { exact: true })).toBeVisible();
  await expect(versions.getByRole("row", { name: "v1" }).getByText("First cut")).toBeVisible();
  const sourceHref = await versions.getByRole("row", { name: "v1" }).getByRole("link", { name: "Download source of version 1" }).getAttribute("href");
  expect(sourceHref).toBe(`/api/databases/${database}/workflows/contact-review/versions/1/source`);
  expect((await page.request.get(sourceHref ?? "")).headers()["content-type"]).toBe("application/gzip");
  await versions.getByRole("row", { name: "v1" }).getByRole("button", { name: "Roll back to version 1" }).click();
  const rollback = page.getByRole("dialog", { name: "Roll back workflow" });
  await expect(rollback.getByText(/New runs currently use version 2\. Roll back to version 1\?/)).toBeVisible();
  await rollback.getByRole("button", { name: "Roll back" }).click();
  await expect(page.getByText("Rolled back Contact review to version 1").first()).toBeVisible();
  await expect(versions.getByRole("row", { name: "v1" }).getByText("Published", { exact: true })).toBeVisible();
  await expect(versions.getByRole("row", { name: "v2" }).getByRole("button", { name: "Publish version 2" })).toBeVisible();

  await page.getByRole("button", { name: "Run", exact: true }).click();
  dialog = page.getByRole("dialog", { name: "Run Contact review" });
  await dialog.getByRole("button", { name: "Start run" }).click();
  await expect(dialog.getByRole("region", { name: "Run result" }).getByLabel("Output")).toContainText('"release": "first"');
  await page.keyboard.press("Escape");

  // The runs list filters by status; every run so far succeeded.
  await page.getByRole("tab", { name: "Runs" }).click();
  await expect(page.getByRole("list", { name: "Runs" }).getByRole("button")).toHaveCount(3);
  await page.getByLabel("Filter runs by status").selectOption("failed");
  await expect(page.getByText("No runs with this status")).toBeVisible();

  // Disabling stops triggers; deleting removes everything.
  await page.getByRole("tab", { name: "Overview" }).click();
  await page.getByRole("switch", { name: "Enabled" }).click();
  await expect(page.getByText("Disabled Contact review").first()).toBeVisible();
  await expect(page.getByRole("switch", { name: "Enabled" })).not.toBeChecked();
  await page.getByRole("button", { name: "Delete workflow" }).click();
  await page.getByRole("dialog", { name: "Delete workflow" }).getByRole("button", { name: "Delete" }).click();
  await expect(page).toHaveURL(`/databases/${database}/workflows`);
  await expect(page.getByText("No workflows yet")).toBeVisible();
});

test("users with a run grant start a workflow and see only their own runs", async ({ page }) => {
  await page.goto("/");
  const runner = await register(page);
  const other = await register(page);
  await page.request.post("/api/auth/logout");
  await signInAdmin(page);
  const database = await createDatabase(page);
  const pushed = await push(page, database, "contact-review", "granted", "?publish=1", { start: defaultManualTrigger, hook: { kind: "webhook", path: "in" } });
  for (const user of [runner, other]) {
    await apiPost(page, "/api/permissions/grants", { subject_id: user.id, scope: "workflow", resource: String(pushed.workflow.id), field: "", level: 1 });
  }
  const adminRun = (await apiPost(page, `/api/databases/${database}/workflows/contact-review/runs`, { input: {}, wait: true })) as { id: number };

  await login(page, runner.email);
  await page.goto(`/databases/${database}/workflows/contact-review`);
  // Only admins open a workflow's page.
  await expect(page).toHaveURL(`/databases/${database}/workflows`);
  const list = page.getByRole("list", { name: "Runnable workflows" });
  await expect(list.getByText("Contact review")).toBeVisible();
  await expect(list.getByText("Asks for Note, Count, Urgent")).toBeVisible();
  await expect(page.getByRole("tab", { name: "Versions" })).toHaveCount(0);
  await expect(page.getByText("Runs you start appear here with their status and output.")).toBeVisible();
  await list.getByRole("button", { name: "Run Contact review" }).click();
  const dialog = page.getByRole("dialog", { name: "Run Contact review" });
  await dialog.getByLabel("Note").fill("from a member");
  await dialog.getByRole("button", { name: "Start run" }).click();
  const output = dialog.getByRole("region", { name: "Run result" }).getByLabel("Output");
  await expect(output).toContainText('"note": "from a member"');
  await expect(dialog.getByRole("button", { name: "Open run" })).toHaveCount(0);
  await page.keyboard.press("Escape");
  const ownRuns = page.getByRole("list", { name: "Your recent runs" });
  await expect(ownRuns.getByRole("listitem")).toHaveCount(1);
  const ownRun = ownRuns.getByRole("listitem", { name: /Contact review run \d+/ });
  await expect(ownRun.getByText("Succeeded")).toBeVisible();
  await ownRun.getByText("Contact review").click();
  await expect(ownRun.getByLabel("Output")).toContainText('"release": "granted"');
  const ownRunID = Number(/run (\d+)/.exec((await ownRun.getAttribute("aria-label")) ?? "")?.[1]);
  expect(ownRunID).toBeGreaterThan(adminRun.id);
  // The runs persist across reloads.
  await page.reload();
  await expect(page.getByRole("list", { name: "Your recent runs" }).getByRole("listitem")).toHaveCount(1);

  // Run lists, other people's runs and the detail stay closed.
  expect((await page.request.get(`/api/databases/${database}/workflows/contact-review/runs`)).status()).toBe(403);
  expect((await page.request.get(`/api/databases/${database}/workflows/contact-review/runs/${adminRun.id}`)).status()).toBe(404);
  expect((await page.request.get(`/api/databases/${database}/workflows/contact-review`)).status()).toBe(403);
  const own = (await (await page.request.get(`/api/databases/${database}/workflows/contact-review/runs/${ownRunID}`)).json()) as Record<string, unknown>;
  expect(own.status).toBe("succeeded");
  expect(own).not.toHaveProperty("logs");
  expect(own).not.toHaveProperty("event");

  // A second granted user sees none of the first user's runs.
  await login(page, other.email);
  await page.goto(`/databases/${database}/workflows`);
  await expect(page.getByRole("list", { name: "Runnable workflows" }).getByText("Contact review")).toBeVisible();
  await expect(page.getByText("Runs you start appear here with their status and output.")).toBeVisible();
  expect((await page.request.get(`/api/databases/${database}/workflows/contact-review/runs/${ownRunID}`)).status()).toBe(404);
});
