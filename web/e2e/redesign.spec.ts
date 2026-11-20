import { type Browser, type Page, test } from "@playwright/test";
import { mkdirSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";
import * as XLSX from "xlsx";
import { appBundle } from "./appBundle";
import { workflowBundle } from "./workflowBundle";

// Review captures of the redesigned screens. Not an assertion test: it seeds
// the real backend with realistic sample data, then writes PNGs to
// e2e/.shots/redesign for the product owner to approve the direction.

const shotsDir = join(dirname(fileURLToPath(import.meta.url)), ".shots", "redesign");
mkdirSync(shotsDir, { recursive: true });

// The only admin in the e2e config (see start-backend.mjs).
const adminEmail = "e2e-admin@example.com";
const memberEmail = "mia.chen@example.com";
const password = "correct horse";
const database = "operations";

async function signIn(page: Page, email: string, displayName: string) {
  let response = await page.request.post("/api/auth/register", { data: { email, password, display_name: displayName } });
  if (response.status() === 409) {
    response = await page.request.post("/api/auth/login", { data: { email, password } });
  }
  if (!response.ok()) {
    throw new Error(`sign-in failed: ${response.status()} ${await response.text()}`);
  }
  return (await response.json()) as { id: string };
}

async function post(page: Page, path: string, data: unknown) {
  const response = await page.request.post(path, { data });
  if (!response.ok()) {
    throw new Error(`POST ${path} failed: ${response.status()} ${await response.text()}`);
  }
  const text = await response.text();
  return text ? JSON.parse(text) : null;
}

async function put(page: Page, path: string, data: unknown) {
  const response = await page.request.put(path, { data });
  if (!response.ok()) {
    throw new Error(`PUT ${path} failed: ${response.status()} ${await response.text()}`);
  }
}

async function uploadApp(page: Page, name: string, title: string, query = "") {
  const response = await page.request.post(`/api/databases/${database}/apps/${name}/versions${query}`, {
    headers: { "Content-Type": "application/gzip" },
    data: appBundle({ database, name, title, heading: title })
  });
  if (!response.ok()) {
    throw new Error(`app upload failed: ${response.status()} ${await response.text()}`);
  }
  return ((await response.json()) as { app: { id: number } }).app;
}

async function pushWorkflow(page: Page, name: string, title: string, release: string, triggers: Record<string, unknown>, query = "?publish=1") {
  const response = await page.request.post(`/api/databases/${database}/workflows/${name}/versions${query}`, {
    headers: { "Content-Type": "application/gzip" },
    data: workflowBundle({ database, name, title, release, triggers })
  });
  if (!response.ok()) {
    throw new Error(`workflow push failed: ${response.status()} ${await response.text()}`);
  }
  return ((await response.json()) as { workflow: { id: number } }).workflow;
}

async function startRun(page: Page, name: string, input: Record<string, unknown>) {
  return (await post(page, `/api/databases/${database}/workflows/${name}/runs`, { input, wait: true })) as { id: number };
}

async function shot(page: Page, name: string) {
  // Let fonts, transitions and toasts settle.
  await page.evaluate(() => document.fonts.ready);
  await page.waitForTimeout(500);
  await page.screenshot({ path: join(shotsDir, `${name}.png`) });
}

const contacts: Array<[string, string, string, string, string, number]> = [
  ["Ada Lovelace", "ada@example.com", "Northwind Labs", "Active", "London", 92],
  ["Grace Hopper", "grace@example.com", "Harbor Systems", "Active", "New York", 88],
  ["陈静", "chen.jing@example.com", "青石科技", "Lead", "上海", 74],
  ["Margaret Hamilton", "margaret@example.com", "Apollo Works", "Active", "Boston", 95],
  ["王磊", "wang.lei@example.com", "远山贸易", "Active", "杭州", 81],
  ["Katherine Johnson", "katherine@example.com", "Langley Partners", "Lead", "Hampton", 79],
  ["Dorothy Vaughan", "dorothy@example.com", "Langley Partners", "Inactive", "Hampton", 63],
  ["Alan Turing", "alan@example.com", "Bletchley & Co", "Active", "Manchester", 90],
  ["李娜", "li.na@example.com", "青石科技", "Active", "深圳", 86],
  ["Hedy Lamarr", "hedy@example.com", "Frequency Studio", "Lead", "Vienna", 71],
  ["Radia Perlman", "radia@example.com", "Spanning Tree Inc", "Active", "Seattle", 84],
  ["Barbara Liskov", "barbara@example.com", "Harbor Systems", "Inactive", "Cambridge", 58],
  ["Linus Pauling", "linus@example.com", "Bond Chemicals", "Lead", "Portland", 67],
  ["Mary Jackson", "mary@example.com", "Langley Partners", "Active", "Hampton", 89]
];

const orders: Array<[string, string, number, string, string]> = [
  ["SO-1042", "Northwind Labs", 1280.5, "Paid", "2026-11-02"],
  ["SO-1043", "Harbor Systems", 342, "Shipped", "2026-11-03"],
  ["SO-1044", "青石科技", 2210, "Pending", "2026-11-05"],
  ["SO-1045", "Apollo Works", 96.4, "Paid", "2026-11-06"],
  ["SO-1046", "远山贸易", 780, "Shipped", "2026-11-09"],
  ["SO-1047", "Langley Partners", 1530, "Pending", "2026-11-11"],
  ["SO-1048", "Frequency Studio", 415.25, "Paid", "2026-11-13"],
  ["SO-1049", "Spanning Tree Inc", 3120, "Paid", "2026-11-16"]
];

// A spreadsheet as people bring it: the table's columns plus a new one, and
// one cell that does not fit its column's type.
function importWorkbook(): Buffer {
  const sheet = XLSX.utils.aoa_to_sheet([
    ["name", "email", "company", "status", "city", "score", "region"],
    ["Joan Clarke", "joan@example.com", "Bletchley & Co", "Lead", "London", 77, "EMEA"],
    ["Annie Easley", "annie@example.com", "Langley Partners", "Active", "Cleveland", 83, "AMER"],
    ["张伟", "zhang.wei@example.com", "远山贸易", "Lead", "成都", "seventy", "APAC"],
    ["Evelyn Boyd Granville", "evelyn@example.com", "Apollo Works", "Active", "Washington", 91, "AMER"]
  ]);
  const book = XLSX.utils.book_new();
  XLSX.utils.book_append_sheet(book, sheet, "Contacts");
  return XLSX.write(book, { type: "buffer", bookType: "xlsx" }) as Buffer;
}

const widgets: Array<[string, string, number, number, string]> = [
  ["Hex bolt M6", "WD-0101", 1240, 0.18, "常用件"],
  ["Hinge, brushed steel", "WD-0144", 86, 3.4, ""],
  ["Cable gland PG9", "WD-0210", 412, 0.95, "补货中"],
  ["Rubber foot 20mm", "WD-0233", 0, 0.32, "缺货"],
  ["Toggle switch", "WD-0307", 57, 2.1, ""]
];

async function seed(page: Page) {
  const admin = await signIn(page, adminEmail, "Alex Morgan");
  const member = await (async () => {
    const context = page.request;
    const response = await context.post("/api/auth/register", { data: { email: memberEmail, password, display_name: "Mia Chen" } });
    if (response.status() !== 409 && !response.ok()) {
      throw new Error(`member register failed: ${response.status()} ${await response.text()}`);
    }
    // A colleague who is not in any role yet, to find with the member search.
    await context.post("/api/auth/register", { data: { email: "noah.patel@example.com", password, display_name: "Noah Patel" } });
    // Registering signs the browser in as the new user; sign back in as the admin.
    const login = await context.post("/api/auth/login", { data: { email: memberEmail, password } });
    const user = (await login.json()) as { id: string };
    await context.post("/api/auth/login", { data: { email: adminEmail, password } });
    return user;
  })();

  await post(page, "/api/databases", { name: database });
  await post(page, `/api/databases/${database}/tables`, {
    name: "contacts",
    display_name: "Contacts",
    fields: [
      { name: "name", type: "string", deleted: false },
      { name: "email", type: "string", deleted: false },
      { name: "company", type: "string", deleted: false },
      { name: "status", type: "string", options: ["Active", "Lead", "Inactive"], deleted: false },
      { name: "city", type: "string", deleted: false },
      { name: "score", type: "int", deleted: false }
    ],
    views: [
      { name: "active", display_name: "Active", query: { combinator: "and", rules: [{ field: "status", operator: "=", value: "Active" }] }, sorts: [] },
      { name: "leads", display_name: "Leads", query: { combinator: "and", rules: [{ field: "status", operator: "=", value: "Lead" }] }, sorts: [{ field: "score", direction: "desc" }] }
    ]
  });
  await post(page, `/api/databases/${database}/tables`, {
    name: "orders",
    display_name: "Orders",
    fields: [
      { name: "order_no", type: "string", deleted: false },
      { name: "customer", type: "string", deleted: false },
      { name: "amount", type: "float", deleted: false },
      { name: "status", type: "string", options: ["Pending", "Paid", "Shipped"], deleted: false },
      { name: "placed_on", type: "string", deleted: false }
    ],
    views: [{ name: "open", display_name: "Open", query: { combinator: "and", rules: [{ field: "status", operator: "=", value: "Pending" }] }, sorts: [] }]
  });
  await post(page, `/api/databases/${database}/tables`, {
    name: "widgets",
    display_name: "Widgets",
    fields: [
      { name: "名称", type: "string", deleted: false },
      { name: "sku", type: "string", deleted: false },
      { name: "数量", type: "int", deleted: false },
      { name: "price", type: "float", deleted: false },
      { name: "备注", type: "string", deleted: false }
    ],
    views: []
  });
  for (const [name, email, company, status, city, score] of contacts) {
    await post(page, `/api/tables/${database}/contacts/rows`, { values: { name, email, company, status, city, score } });
  }
  for (const [order_no, customer, amount, status, placed_on] of orders) {
    await post(page, `/api/tables/${database}/orders/rows`, { values: { order_no, customer, amount, status, placed_on } });
  }
  for (const [名称, sku, 数量, price, 备注] of widgets) {
    await post(page, `/api/tables/${database}/widgets/rows`, { values: { 名称, sku, 数量, price, 备注 } });
  }

  // Workflows: one with every trigger kind, several versions, runs (one
  // failed), secrets and vars; a webhook receiver; a disabled report.
  const followUpTriggers = {
    start: {
      kind: "manual",
      fields: [
        { name: "orderNo", label: "Order number", type: "string" },
        { name: "quantity", label: "Quantity", type: "number" },
        { name: "notify", label: "Notify customer", type: "boolean" }
      ]
    },
    changed: { kind: "recordChanged", table: "orders", operations: ["update"], fields: ["status"] },
    nightly: { kind: "schedule", cron: "0 9 * * 1-5", timezone: "Asia/Shanghai" },
    callback: { kind: "webhook", path: "erp/callback" }
  };
  await pushWorkflow(page, "order-follow-up", "Order follow-up", "1.0", followUpTriggers, "?message=First%20version&publish=1");
  await pushWorkflow(page, "order-follow-up", "Order follow-up", "1.1", followUpTriggers, "?message=Retry%20ERP%20calls%20twice&publish=1");
  const followUp = await pushWorkflow(page, "order-follow-up", "Order follow-up", "1.2", followUpTriggers, "?message=Send%20the%20customer%20notice%20in%20their%20language&publish=1");
  await pushWorkflow(page, "order-follow-up", "Order follow-up", "1.3", followUpTriggers, "?message=Draft%3A%20batch%20notices");
  await put(page, `/api/databases/${database}/workflows/order-follow-up/secrets/ERP_TOKEN`, { value: "seed-token" });
  await put(page, `/api/databases/${database}/workflows/order-follow-up/secrets/SMTP_PASSWORD`, { value: "seed-password" });
  await put(page, `/api/databases/${database}/workflows/order-follow-up/vars/ERP_BASE_URL`, { value: "https://erp.example.com/api" });
  await put(page, `/api/databases/${database}/workflows/order-follow-up/vars/REGION`, { value: "eu-west" });
  await startRun(page, "order-follow-up", { orderNo: "SO-1042", quantity: 2, notify: true });
  await startRun(page, "order-follow-up", { orderNo: "SO-1044", fail: "ERP answered 503 for SO-1044" });
  await startRun(page, "order-follow-up", { orderNo: "SO-1047", quantity: 12, notify: false });
  await pushWorkflow(page, "stock-webhook", "Stock level webhook", "1.0", { levels: { kind: "webhook", path: "stock/levels" } }, "?message=Initial%20version&publish=1");
  await pushWorkflow(page, "weekly-report", "Weekly report", "1.0", { weekly: { kind: "schedule", cron: "0 8 * * 1", timezone: "Europe/London" }, start: { kind: "manual" } }, "?message=Initial%20version&publish=1");
  const patched = await page.request.patch(`/api/databases/${database}/workflows/weekly-report`, { data: { enabled: false } });
  if (!patched.ok()) {
    throw new Error(`workflow disable failed: ${patched.status()}`);
  }

  await uploadApp(page, "order-desk", "Order desk", "?message=First%20release&publish=1");
  await uploadApp(page, "order-desk", "Order desk", "?message=Adds%20totals%20and%20CSV%20export&publish=1");
  await uploadApp(page, "order-desk", "Order desk", "?message=Faster%20search%20on%20large%20order%20lists&publish=1");
  await uploadApp(page, "order-desk", "Order desk", "?message=Draft%3A%20new%20layout%20for%20tablets");
  const stock = await uploadApp(page, "stock-check", "Stock check", "?message=Initial%20version&publish=1");
  const visits = await uploadApp(page, "visit-log", "Visit log", "?message=Initial%20version&publish=1");
  await uploadApp(page, "price-list", "Price list", "?message=Work%20in%20progress");
  const orderDesk = (await (await page.request.get(`/api/databases/${database}/apps/order-desk`)).json()) as { app: { id: number } };

  const grants = [
    { scope: "field_set", resource: `${database}.contacts`, field: "", level: 3 },
    { scope: "view_set", resource: `${database}.contacts`, field: "", level: 1 },
    { scope: "field_set", resource: `${database}.orders`, field: "", level: 1 },
    { scope: "view_set", resource: `${database}.orders`, field: "", level: 1 },
    { scope: "workflow", resource: String(followUp.id), field: "", level: 1 },
    { scope: "app", resource: String(orderDesk.app.id), field: "", level: 1 },
    { scope: "app", resource: String(stock.id), field: "", level: 1 },
    { scope: "app", resource: String(visits.id), field: "", level: 1 }
  ];
  // The member gets access through a role, the way an admin would set it up.
  await post(page, `/api/databases/${database}/roles`, { name: "sales" });
  await put(page, `/api/databases/${database}/roles/sales/grants`, { grants });
  await put(page, `/api/databases/${database}/roles/sales/members`, { members: [{ type: "user", id: member.id }] });
  await post(page, `/api/databases/${database}/roles`, { name: "support" });
  return { admin, member };
}

async function memberPage(browser: Browser, viewport = { width: 1440, height: 900 }, email = memberEmail) {
  const context = await browser.newContext({ viewport, colorScheme: "light" });
  const page = await context.newPage();
  await page.goto("/login");
  const response = await page.request.post("/api/auth/login", { data: { email, password } });
  if (!response.ok()) {
    throw new Error(`member login failed: ${response.status()}`);
  }
  return page;
}

test("capture redesign review screenshots", async ({ page, browser }) => {
  test.setTimeout(240_000);
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.emulateMedia({ colorScheme: "light" });

  // Sign-in page, signed out.
  await page.goto("/");
  await page.getByRole("form", { name: "Sign in" }).waitFor();
  await shot(page, "01-login");

  await seed(page);

  // Admin home.
  await page.goto(`/databases/${database}`);
  await page.getByRole("heading", { name: "Apps" }).waitFor();
  await shot(page, "02-home-admin");

  // Table data view with realistic rows, then the row panel.
  await page.goto(`/databases/${database}/tables/contacts`);
  await page.getByRole("gridcell", { name: "Ada Lovelace", exact: true }).waitFor();
  await shot(page, "04-table");
  await page.getByRole("gridcell", { name: "Margaret Hamilton", exact: true }).click();
  await page.getByRole("toolbar", { name: "Table canvas actions" }).getByRole("button", { name: "Edit Row" }).click();
  await page.getByRole("complementary", { name: "Record panel" }).waitFor();
  await shot(page, "05-table-row-panel");

  // Filter editor on a view.
  await page.goto(`/databases/${database}/tables/contacts/views/leads`);
  await page.getByRole("gridcell", { name: "Katherine Johnson", exact: true }).waitFor();
  await page.getByRole("button", { name: "Filter" }).click();
  await page.getByLabel("View filters").first().waitFor();
  await shot(page, "06-table-view-filter");
  await page.keyboard.press("Escape");

  // Apps list and one app with several versions.
  await page.goto(`/databases/${database}/apps`);
  await page.getByRole("table", { name: "Apps" }).waitFor();
  await shot(page, "07-apps");
  await page.goto(`/databases/${database}/apps/order-desk`);
  await page.getByRole("table", { name: "Versions" }).waitFor();
  await shot(page, "08-app-detail");

  // Admin area landing.
  await page.goto(`/databases/${database}/admin`);
  await page.getByRole("heading", { name: "Manage" }).waitFor();
  await shot(page, "09-admin");

  // Workflows: the list, then one workflow's overview, versions, runs with
  // a run's log, secrets and vars, and the manual run form.
  await page.goto(`/databases/${database}/workflows`);
  await page.getByRole("table", { name: "Workflows" }).waitFor();
  await shot(page, "10-workflow-list");
  await page.goto(`/databases/${database}/workflows/order-follow-up`);
  await page.getByRole("group", { name: "Trigger callback" }).waitFor();
  await shot(page, "11-workflow-overview");
  await page.getByRole("tab", { name: "Versions" }).click();
  await page.getByRole("table", { name: "Versions" }).waitFor();
  await page.evaluate(() => (document.activeElement as HTMLElement | null)?.blur());
  await shot(page, "11b-workflow-versions");
  await page.getByRole("tab", { name: "Runs" }).click();
  await page.getByRole("button", { name: /^Run \d+, Failed$/ }).click();
  await page.getByText("giving up").waitFor();
  await shot(page, "12-workflow-run-detail");
  await page.getByRole("tab", { name: "Secrets & vars" }).click();
  await page.getByRole("list", { name: "Secrets" }).waitFor();
  await shot(page, "12b-workflow-secrets-vars");
  await page.getByRole("button", { name: "Run", exact: true }).click();
  const runDialog = page.getByRole("dialog", { name: "Run Order follow-up" });
  await runDialog.getByLabel("Order number").fill("SO-1049");
  await runDialog.getByLabel("Quantity").fill("4");
  await runDialog.getByRole("checkbox", { name: "Notify customer" }).check();
  await runDialog.getByRole("button", { name: "Start run" }).click();
  await runDialog.getByRole("region", { name: "Run result" }).waitFor();
  await shot(page, "12c-workflow-manual-run");
  await page.keyboard.press("Escape");

  // A role: access by table, members on the side; then the other tabs.
  await page.goto(`/databases/${database}/permissions/sales`);
  await page.getByRole("button", { name: "email Update" }).waitFor();
  await shot(page, "13-permissions-tables");
  await page.getByRole("combobox", { name: "Role member display name" }).fill("noah");
  await page.getByRole("listbox").waitFor();
  await shot(page, "14-permissions-members-search");
  await page.keyboard.press("Escape");
  await page.getByRole("tab", { name: "Apps" }).click();
  await page.getByLabel("All apps permission").waitFor();
  await shot(page, "15-permissions-apps");
  await page.getByRole("tab", { name: "Workflows" }).click();
  await page.getByLabel("Order follow-up permission").waitFor();
  await shot(page, "16-permissions-workflows");

  // Remote runners, after generating a token.
  await page.getByRole("navigation", { name: "Workspace" }).getByRole("button", { name: "Remote runners" }).click();
  const runners = page.getByRole("dialog", { name: "Remote runners" });
  await runners.getByRole("button", { name: "Generate token" }).click();
  await runners.getByText(/will not be shown again/).waitFor();
  await shot(page, "17-runners");
  await page.keyboard.press("Escape");

  // Excel import: columns, then the preview.
  await page.goto(`/databases/${database}/tables/contacts`);
  await page.getByRole("gridcell", { name: "Ada Lovelace", exact: true }).waitFor();
  const chooser = page.waitForEvent("filechooser");
  await page.getByRole("button", { name: "Import" }).click();
  await (await chooser).setFiles({
    name: "new-contacts.xlsx",
    mimeType: "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
    buffer: importWorkbook()
  });
  const importDialog = page.getByRole("dialog", { name: /Import into/ });
  await importDialog.getByRole("heading", { name: "Columns to import" }).waitFor();
  await shot(page, "18-import-columns");
  await importDialog.getByRole("button", { name: "Next: preview" }).click();
  await importDialog.getByRole("table", { name: "Import preview" }).waitFor();
  await shot(page, "19-import-preview");
  await importDialog.getByRole("button", { name: "Cancel" }).click();

  // Dark mode.
  await page.emulateMedia({ colorScheme: "dark" });
  await page.goto(`/databases/${database}`);
  await page.getByRole("heading", { name: "Apps" }).waitFor();
  await shot(page, "20-home-admin-dark");
  await page.goto(`/databases/${database}/tables/contacts`);
  await page.getByRole("gridcell", { name: "Ada Lovelace", exact: true }).waitFor();
  await page.getByRole("gridcell", { name: "Grace Hopper", exact: true }).click();
  await page.getByRole("toolbar", { name: "Table canvas actions" }).getByRole("button", { name: "Edit Row" }).click();
  await shot(page, "21-table-dark");
  await page.goto(`/databases/${database}/permissions/sales`);
  await page.getByRole("button", { name: "email Update" }).waitFor();
  await shot(page, "22-permissions-dark");
  await page.goto(`/databases/${database}/workflows/order-follow-up`);
  await page.getByRole("group", { name: "Trigger callback" }).waitFor();
  await shot(page, "23-workflow-detail-dark");
  await page.emulateMedia({ colorScheme: "light" });

  // Ordinary user: home with granted apps and tables, and their workflows.
  const member = await memberPage(browser);
  await member.goto(`/databases/${database}`);
  await member.getByRole("heading", { name: "Apps" }).waitFor();
  await shot(member, "03-home-member");
  await member.goto(`/databases/${database}/workflows`);
  await member.getByRole("list", { name: "Runnable workflows" }).waitFor();
  await member.getByRole("button", { name: "Run Order follow-up" }).click();
  const memberDialog = member.getByRole("dialog", { name: "Run Order follow-up" });
  await memberDialog.getByLabel("Order number").fill("SO-1045");
  await memberDialog.getByRole("button", { name: "Start run" }).click();
  await memberDialog.getByRole("region", { name: "Run result" }).waitFor();
  await member.keyboard.press("Escape");
  await member.getByRole("list", { name: "Your recent runs" }).getByText("Order follow-up").click();
  await shot(member, "24-workflows-member");
  await member.context().close();

  // Mobile width.
  const mobile = await memberPage(browser, { width: 390, height: 844 });
  await mobile.goto(`/databases/${database}`);
  await mobile.getByRole("heading", { name: "Apps" }).waitFor();
  await shot(mobile, "25-mobile-home");
  await mobile.goto(`/databases/${database}/tables/contacts`);
  await mobile.getByRole("gridcell", { name: "Ada Lovelace", exact: true }).waitFor();
  await shot(mobile, "26-mobile-table");
  await mobile.getByRole("button", { name: "Open navigation" }).click();
  await mobile.getByRole("dialog", { name: "Navigation" }).waitFor();
  await shot(mobile, "27-mobile-navigation");
  await mobile.context().close();

  const mobileAdmin = await memberPage(browser, { width: 390, height: 844 }, adminEmail);
  await mobileAdmin.goto(`/databases/${database}/workflows/order-follow-up`);
  await mobileAdmin.getByRole("group", { name: "Trigger callback" }).waitFor();
  await shot(mobileAdmin, "28-mobile-workflow");
  await mobileAdmin.goto(`/databases/${database}/workflows`);
  await mobileAdmin.getByRole("table", { name: "Workflows" }).waitFor();
  await shot(mobileAdmin, "28b-mobile-workflow-list");
  await mobileAdmin.context().close();
});
