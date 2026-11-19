import { expect, type Page, test } from "@playwright/test";
import { appBundle } from "./appBundle";

// Hosted apps through the real backend: bundles are uploaded the way
// `autablectl app push` does, then listed, published, opened, rolled back,
// permission-checked and deleted from the web UI.

type User = { id: string; email: string };

let sequence = 0;

test.describe.configure({ mode: "serial" });

async function register(page: Page): Promise<User> {
  sequence += 1;
  const email = `apps-${Date.now()}-${sequence}@example.com`;
  const response = await page.request.post("/api/auth/register", {
    data: { email, password: "correct horse", display_name: email }
  });
  expect(response.ok(), await response.text()).toBe(true);
  return (await response.json()) as User;
}

async function login(page: Page, email: string) {
  const response = await page.request.post("/api/auth/login", { data: { email, password: "correct horse" } });
  expect(response.ok(), await response.text()).toBe(true);
}

async function logout(page: Page) {
  await page.request.post("/api/auth/logout");
}

async function apiPost(page: Page, path: string, data: unknown) {
  const response = await page.request.post(path, { data });
  expect(response.ok(), `${path}: ${await response.text()}`).toBe(true);
  const text = await response.text();
  return text ? JSON.parse(text) : null;
}

async function upload(page: Page, database: string, name: string, title: string, heading: string, query = "") {
  const response = await page.request.post(`/api/databases/${database}/apps/${name}/versions${query}`, {
    headers: { "Content-Type": "application/gzip" },
    data: appBundle({ database, name, title, heading })
  });
  expect(response.ok(), await response.text()).toBe(true);
  return (await response.json()) as { app: { id: number; url: string }; version: { version: number; preview_url: string } };
}

async function createDatabase(page: Page) {
  const database = `apps${Date.now()}${sequence}`;
  await apiPost(page, "/api/databases", { name: database });
  await apiPost(page, `/api/databases/${database}/tables`, {
    name: "contacts",
    display_name: "Contacts",
    fields: [{ name: "name", type: "string", deleted: false }],
    views: []
  });
  return database;
}

async function expectAppPage(page: Page, url: string, heading: string) {
  const appPage = await page.context().newPage();
  await appPage.goto(url);
  await expect(appPage.getByRole("heading", { name: heading })).toBeVisible();
  // The relative script resolves through the injected <base>, whatever the URL.
  await expect(appPage.locator("#status")).toHaveText(`script ran: ${heading}`);
  await appPage.close();
}

test("lists, publishes, opens and rolls back an uploaded app", async ({ page }) => {
  await page.goto("/");
  await register(page);
  const database = await createDatabase(page);

  await page.goto(`/databases/${database}/apps`);
  await expect(page.getByText("No apps yet")).toBeVisible();
  await expect(page.getByText(`autablectl app init my-app --database ${database} --name my-app`)).toBeVisible();

  const first = await upload(page, database, "orders", "Orders", "Orders v1", "?message=First%20cut");
  expect(first.version.version).toBe(1);
  await page.reload();
  const appTable = page.getByRole("table", { name: "Apps" });
  await expect(appTable).toBeVisible();
  const appRow = appTable.getByRole("row").filter({ hasText: "orders" });
  await expect(appRow.getByText("Not published")).toBeVisible();

  await appRow.getByRole("button", { name: "Orders" }).click();
  await expect(page).toHaveURL(`/databases/${database}/apps/orders`);
  const versions = page.getByRole("table", { name: "Versions" });
  const v1 = versions.getByRole("row", { name: "v1" });
  await expect(v1.getByText("First cut")).toBeVisible();
  await expect(v1.getByText("1.0", { exact: true })).toBeVisible();
  await expect(page.getByRole("link", { name: "Open Orders" })).toHaveCount(0);

  // Preview works before anything is published.
  await expect(v1.getByRole("link", { name: "Preview version 1" })).toHaveAttribute("href", `/apps/${database}/orders/@1/`);
  await expectAppPage(page, `/apps/${database}/orders/@1/`, "Orders v1");

  await v1.getByRole("button", { name: "Publish version 1" }).click();
  await expect(page.getByText("Published Orders version 1").first()).toBeVisible();
  await expect(v1.getByText("Published", { exact: true })).toBeVisible();
  const openLink = page.getByRole("link", { name: "Open Orders" });
  await expect(openLink).toHaveAttribute("href", `/apps/${database}/orders/`);
  await expect(openLink).toHaveAttribute("target", "_blank");
  await expectAppPage(page, `/apps/${database}/orders/`, "Orders v1");
  // Client-side routes fall back to index.html.
  await expectAppPage(page, `/apps/${database}/orders/some/deep/route`, "Orders v1");

  // A second version pushed with --publish replaces it.
  await upload(page, database, "orders", "Orders", "Orders v2", "?message=Second%20cut&publish=1");
  await page.reload();
  const v2 = versions.getByRole("row", { name: "v2" });
  await expect(versions.getByRole("row")).toHaveCount(3);
  await expect(versions.getByRole("row").nth(1)).toHaveAttribute("aria-label", "v2");
  await expect(v2.getByText("Published", { exact: true })).toBeVisible();
  await expectAppPage(page, `/apps/${database}/orders/`, "Orders v2");

  // The source of any version downloads as a tar.gz.
  const sourceHref = await versions.getByRole("row", { name: "v1" }).getByRole("link", { name: "Download source of version 1" }).getAttribute("href");
  expect(sourceHref).toBe(`/api/databases/${database}/apps/orders/versions/1/source`);
  const source = await page.request.get(sourceHref ?? "");
  expect(source.status()).toBe(200);
  expect(source.headers()["content-type"]).toBe("application/gzip");

  // Publishing an older version is a rollback and asks first.
  await versions.getByRole("row", { name: "v1" }).getByRole("button", { name: "Roll back to version 1" }).click();
  const dialog = page.getByRole("dialog", { name: "Roll back app" });
  await expect(dialog.getByText("Users currently get version 2. Roll back to version 1?")).toBeVisible();
  await dialog.getByRole("button", { name: "Roll back" }).click();
  await expect(page.getByText("Rolled back Orders to version 1").first()).toBeVisible();
  await expect(versions.getByRole("row", { name: "v1" }).getByText("Published", { exact: true })).toBeVisible();
  await expect(versions.getByRole("row", { name: "v2" }).getByRole("button", { name: "Publish version 2" })).toBeVisible();
  await expectAppPage(page, `/apps/${database}/orders/`, "Orders v1");

  // The database owner can delete the app.
  await page.getByRole("button", { name: "Delete app" }).click();
  await page.getByRole("dialog", { name: "Delete app" }).getByRole("button", { name: "Delete" }).click();
  await expect(page).toHaveURL(`/databases/${database}/apps`);
  await expect(page.getByText("No apps yet")).toBeVisible();
});

test("app readers can open an app but not manage it", async ({ page }) => {
  await page.goto("/");
  const reader = await register(page);
  await logout(page);
  await register(page);
  const database = await createDatabase(page);
  const uploaded = await upload(page, database, "stock-check", "Stock check", "Stock check v1", "?publish=1");
  await upload(page, database, "stock-check", "Stock check", "Stock check v2");
  await apiPost(page, "/api/permissions/grants", {
    subject_id: reader.id,
    scope: "app",
    resource: String(uploaded.app.id),
    field: "",
    level: 1
  });

  await logout(page);
  await login(page, reader.email);
  await page.goto(`/databases/${database}/apps/stock-check`);
  const versions = page.getByRole("table", { name: "Versions" });
  await expect(versions.getByRole("row", { name: "v1" }).getByText("Published", { exact: true })).toBeVisible();
  await expect(page.getByRole("link", { name: "Open Stock check" })).toBeVisible();
  await expect(versions.getByRole("button")).toHaveCount(0);
  await expect(versions.getByRole("link")).toHaveCount(0);
  await expect(page.getByRole("button", { name: "Delete app" })).toHaveCount(0);
  await expect(page.getByRole("button", { name: "Permission", exact: true })).toHaveCount(0);

  await expectAppPage(page, `/apps/${database}/stock-check/`, "Stock check v1");
  // Previews of unpublished versions are for writers only.
  const preview = await page.request.get(`/apps/${database}/stock-check/@2/`);
  expect(preview.status()).toBe(403);
  const publish = await page.request.post(`/api/databases/${database}/apps/stock-check/publish`, { data: { version: 2 } });
  expect(publish.status()).toBe(403);
});

test("signed-out visitors are sent to login before an app opens", async ({ page }) => {
  await page.goto("/");
  const owner = await register(page);
  const database = await createDatabase(page);
  await upload(page, database, "orders", "Orders", "Orders after login", "?publish=1");
  await logout(page);

  await page.goto(`/apps/${database}/orders/`);
  await expect(page).toHaveURL(new RegExp(`/login\\?redirect=${encodeURIComponent(`/apps/${database}/orders/`)}`));
  const dialog = page.getByRole("dialog");
  await dialog.getByLabel("Email").fill(owner.email);
  await dialog.getByLabel("Password").fill("correct horse");
  await dialog.getByRole("button", { name: "Login" }).click();
  await expect(page.getByRole("heading", { name: "Orders after login" })).toBeVisible();
});
