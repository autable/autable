import { expect, type Page, test } from "@playwright/test";
import { mkdirSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";
import { tarGzip } from "./appBundle";

// Libraries through the real backend: a library and a workflow importing it
// are uploaded the way `autablectl lib push` and `workflow push` do. Publishing
// a new library version changes the workflow's next run without pushing the
// workflow again, and rolling back from the Libraries page restores it.
// Review screenshots go to e2e/.shots/redesign.

const shotsDir = join(dirname(fileURLToPath(import.meta.url)), ".shots", "redesign");
mkdirSync(shotsDir, { recursive: true });

// The only global admin in the e2e config (see start-backend.mjs).
const adminEmail = "e2e-admin@example.com";
const password = "correct horse";

test.describe.configure({ mode: "serial" });

async function signInAdmin(page: Page) {
  const credentials = { email: adminEmail, password };
  let response = await page.request.post("/api/auth/register", { data: { ...credentials, display_name: "Avery Admin" } });
  if (response.status() === 409) {
    response = await page.request.post("/api/auth/login", { data: credentials });
  }
  expect(response.ok(), await response.text()).toBe(true);
}

async function apiPost(page: Page, path: string, data: unknown) {
  const response = await page.request.post(path, { data });
  expect(response.ok(), `${path}: ${await response.text()}`).toBe(true);
  const text = await response.text();
  return text ? JSON.parse(text) : null;
}

// What the library template's build produces: an IIFE assigning __lib, and
// the declarations as declare module "@lib/<name>".
function libraryBundle(name: string, title: string, prefix: string): Buffer {
  const manifest = JSON.stringify({ name, title, sdk: "1.0" });
  const script = [
    "var __lib = (() => {",
    "  return {",
    `    label: (value) => ${JSON.stringify(prefix)} + value,`,
    '    region: () => globalThis.__autable_host.variable("REGION") || null',
    "  };",
    "})();",
    ""
  ].join("\n");
  return tarGzip({
    "autable.lib.json": manifest,
    "dist/lib.js": script,
    "dist/index.d.ts": `declare module "@lib/${name}" {\nexport function label(value: string): string;\nexport function region(): string | null;\n}\n`,
    "source/autable.lib.json": manifest,
    "source/src/index.ts": `export const label = (value: string) => ${JSON.stringify(prefix)} + value;\n`
  });
}

// A workflow importing the library as the workflow template's build emits it.
function importingWorkflowBundle(database: string, name: string, library: string): Buffer {
  const manifest = JSON.stringify({ database, name, title: "Order labels", sdk: "1.0" });
  const script = [
    "var __workflow = (() => {",
    `  var text = __autable_lib(${JSON.stringify(`@lib/${library}`)});`,
    "  return {",
    '    triggers: { start: { kind: "manual" } },',
    '    run: async () => ({ label: text.label("SO-1042"), region: text.region() })',
    "  };",
    "})();",
    ""
  ].join("\n");
  return tarGzip({
    "autable.workflow.json": manifest,
    "dist/workflow.js": script,
    "source/autable.workflow.json": manifest,
    "source/src/index.ts": `import { label } from "@lib/${library}";\n`
  });
}

async function upload(page: Page, path: string, bundle: Buffer) {
  const response = await page.request.post(path, { headers: { "Content-Type": "application/gzip" }, data: bundle });
  return response;
}

type Run = { id: number; status: string; output?: { label: string; region: string | null }; libraries?: { name: string; version: number }[]; error?: string };

async function run(page: Page, database: string, workflow: string): Promise<Run> {
  const result = (await apiPost(page, `/api/databases/${database}/workflows/${workflow}/runs`, { wait: true })) as Run;
  expect(result.status, result.error).toBe("succeeded");
  return result;
}

async function shot(page: Page, name: string) {
  await page.evaluate(() => document.fonts.ready);
  await page.waitForTimeout(500);
  await page.screenshot({ path: join(shotsDir, `${name}.png`) });
}

test("a published library change reaches workflows without pushing them, and rolls back", async ({ page }) => {
  await page.setViewportSize({ width: 1440, height: 900 });
  await signInAdmin(page);
  const stamp = Date.now();
  const database = `libs${stamp}`;
  const library = `text-${stamp}`;
  const workflow = "order-labels";
  await apiPost(page, "/api/databases", { name: database });

  // A workflow importing a library that does not exist cannot be published.
  const missing = await upload(page, `/api/databases/${database}/workflows/${workflow}/versions?publish=1`, importingWorkflowBundle(database, workflow, library));
  expect(missing.status()).toBe(422);
  expect(await missing.text()).toContain("does not exist");

  let response = await upload(page, `/api/libs/${library}/versions?publish=1&message=${encodeURIComponent("first labels")}`, libraryBundle(library, "Text helpers", "v1:"));
  expect(response.status(), await response.text()).toBe(201);
  response = await upload(page, `/api/databases/${database}/workflows/${workflow}/versions?publish=1`, importingWorkflowBundle(database, workflow, library));
  expect(response.ok(), await response.text()).toBe(true);
  const response2 = await page.request.put(`/api/databases/${database}/workflows/${workflow}/vars/REGION`, { data: { value: "eu" } });
  expect(response2.ok()).toBe(true);

  let result = await run(page, database, workflow);
  expect(result.output).toEqual({ label: "v1:SO-1042", region: "eu" });
  expect(result.libraries).toEqual([{ name: library, version: 1 }]);

  // Publish v2: the next run uses it, though the workflow was not pushed.
  response = await upload(page, `/api/libs/${library}/versions?publish=1&message=${encodeURIComponent("upper-case prefix")}`, libraryBundle(library, "Text helpers", "V2:"));
  expect(response.ok(), await response.text()).toBe(true);
  result = await run(page, database, workflow);
  expect(result.output?.label).toBe("V2:SO-1042");
  expect(result.libraries).toEqual([{ name: library, version: 2 }]);
  const detail = await (await page.request.get(`/api/databases/${database}/workflows/${workflow}`)).json();
  expect(detail.workflow.latest_version).toBe(1);

  // The Libraries page in Manage lists it with its importer.
  await page.goto(`/databases/${database}/admin`);
  await page.getByRole("navigation", { name: "Workspace" }).getByRole("link", { name: "Libraries" }).click();
  await expect(page).toHaveURL(new RegExp(`/databases/${database}/libraries$`));
  const row = page.getByRole("table", { name: "Libraries" }).getByRole("row").filter({ hasText: `@lib/${library}` });
  await expect(row).toContainText("v2");
  await expect(row).toContainText("1 workflow");
  await shot(page, "29-libraries");

  await row.getByRole("link").click();
  await expect(page.getByRole("heading", { name: "Text helpers", level: 1 })).toBeVisible();
  const importers = page.getByRole("table", { name: "Imported by" });
  await expect(importers.getByRole("link", { name: "Order labels" })).toHaveAttribute("href", `/databases/${database}/workflows/${workflow}`);
  await expect(page.getByRole("button", { name: "Delete library" })).toBeDisabled();
  await shot(page, "30-library-detail");

  // Roll back to v1 from the page.
  await page.getByRole("button", { name: "Roll back to version 1" }).click();
  const dialog = page.getByRole("dialog", { name: "Roll back library" });
  await expect(dialog).toContainText("1 importing workflow uses it from its next run");
  await shot(page, "31-library-rollback");
  await dialog.getByRole("button", { name: "Roll back" }).click();
  await expect(page.getByText("Rolled Text helpers back to version 1.", { exact: true })).toBeVisible();
  await expect(page.getByRole("table", { name: "Library versions" }).getByRole("row", { name: "v1" })).toContainText("Published");

  result = await run(page, database, workflow);
  expect(result.output?.label).toBe("v1:SO-1042");
  expect(result.libraries).toEqual([{ name: library, version: 1 }]);

  // The run's detail shows the library version it loaded.
  await page.goto(`/databases/${database}/workflows/${workflow}/runs/${result.id}`);
  const libraries = page.getByLabel("Libraries", { exact: true });
  await expect(libraries).toContainText(`@lib/${library} v1`);
  await shot(page, "32-run-libraries");

  // Deleting is refused while the published workflow imports it.
  const refused = await page.request.delete(`/api/libs/${library}`);
  expect(refused.status()).toBe(409);
  expect(await refused.text()).toContain(`workflow ${database}/${workflow}`);
});

test("libraries are admin-only", async ({ page }) => {
  const email = `libraries-${Date.now()}@example.com`;
  const response = await page.request.post("/api/auth/register", { data: { email, password, display_name: email } });
  expect(response.ok(), await response.text()).toBe(true);
  expect((await page.request.get("/api/libs")).status()).toBe(403);
  expect((await page.request.post("/api/libs/anything/versions", { headers: { "Content-Type": "application/gzip" }, data: libraryBundle("anything", "x", "x") })).status()).toBe(403);
});
