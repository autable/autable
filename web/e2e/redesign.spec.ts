import { type Browser, type Page, test } from "@playwright/test";
import { mkdirSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";
import { appBundle } from "./appBundle";

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

  const welcome = (await post(page, `/api/databases/${database}/workflows`, {
    name: "welcome-new-contact",
    script:
      "function instances(info) {\n  return { greet: 'echo' };\n}\n\nfunction run(info) {\n  return info.instance('greet').exec({ value: info.inputs.name });\n}",
    secrets: {},
    variables: {}
  })) as { id: number };
  await post(page, `/api/databases/${database}/workflows`, {
    name: "nightly-stock-report",
    script: "function instances(info) {\n  return { report: 'echo' };\n}\n\nfunction run(info) {\n  return {};\n}",
    secrets: {},
    variables: {}
  });

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
    { scope: "workflow", resource: String(welcome.id), field: "", level: 1 },
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

async function memberPage(browser: Browser, viewport = { width: 1440, height: 900 }) {
  const context = await browser.newContext({ viewport, colorScheme: "light" });
  const page = await context.newPage();
  await page.goto("/login");
  const response = await page.request.post("/api/auth/login", { data: { email: memberEmail, password } });
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

  // Screens still on Fluent, inside the new shell.
  await page.goto(`/databases/${database}/workflows`);
  await page.getByText("Instances", { exact: true }).waitFor();
  await shot(page, "10-workflow-editor-fluent");
  await page.goto(`/databases/${database}/permissions/sales`);
  await page.waitForTimeout(800);
  await shot(page, "11-permissions-fluent");

  // Dark mode.
  await page.emulateMedia({ colorScheme: "dark" });
  await page.goto(`/databases/${database}`);
  await page.getByRole("heading", { name: "Apps" }).waitFor();
  await shot(page, "12-home-admin-dark");
  await page.goto(`/databases/${database}/tables/contacts`);
  await page.getByRole("gridcell", { name: "Ada Lovelace", exact: true }).waitFor();
  await page.getByRole("gridcell", { name: "Grace Hopper", exact: true }).click();
  await page.getByRole("toolbar", { name: "Table canvas actions" }).getByRole("button", { name: "Edit Row" }).click();
  await shot(page, "13-table-dark");
  await page.emulateMedia({ colorScheme: "light" });

  // Ordinary user: home with granted apps and tables, and their workflows.
  const member = await memberPage(browser);
  await member.goto(`/databases/${database}`);
  await member.getByRole("heading", { name: "Apps" }).waitFor();
  await shot(member, "03-home-member");
  await member.goto(`/databases/${database}/workflows`);
  await member.getByRole("table", { name: "Runnable workflows" }).waitFor();
  await shot(member, "14-workflows-member");
  await member.context().close();

  // Mobile width.
  const mobile = await memberPage(browser, { width: 390, height: 844 });
  await mobile.goto(`/databases/${database}`);
  await mobile.getByRole("heading", { name: "Apps" }).waitFor();
  await shot(mobile, "15-mobile-home");
  await mobile.goto(`/databases/${database}/tables/contacts`);
  await mobile.getByRole("gridcell", { name: "Ada Lovelace", exact: true }).waitFor();
  await shot(mobile, "16-mobile-table");
  await mobile.getByRole("button", { name: "Open navigation" }).click();
  await mobile.getByRole("dialog", { name: "Navigation" }).waitFor();
  await shot(mobile, "17-mobile-navigation");
  await mobile.context().close();
});
