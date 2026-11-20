import { act, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { App } from "./App";
import i18n from "./i18n";

const catalogFixture = {
  databases: [
    {
      name: "workspace",
      tables: [
        {
          name: "contacts",
          display_name: "Contacts",
          fields: [
            { name: "name", type: "string", deleted: false },
            { name: "email", type: "string", deleted: false },
            { name: "status", type: "string", deleted: false }
          ],
          views: [
            { name: "all", display_name: "" },
            { name: "active", display_name: "Active", sorts: [] },
            { name: "active-ops", display_name: "Active ops", base_view: "active", sorts: [] }
          ]
        }
      ]
    }
  ]
};

const rowFixture = [
  { record_id: 1, values: { name: "Ada Lovelace", email: "ada@example.com", status: "Active" } },
  { record_id: 2, values: { name: "Grace Hopper", email: "grace@example.com", status: "Review" } },
  { record_id: 3, values: { name: "Katherine Johnson", email: "katherine@example.com", status: "Active" } }
];

const workflowFixture = [
  {
    id: 1,
    database_name: "workspace",
    name: "record-review",
    title: "Record review",
    enabled: true,
    published_version: 2,
    latest_version: 2,
    timeout_seconds: 600,
    history_retention_days: 30,
    creator_id: "test-user",
    permission_level: 2,
    created_at: 1781600000000,
    updated_at: 1781603000000,
    triggers: [
      { name: "review", kind: "manual", fields: [{ name: "note", label: "Note", type: "string" }] },
      { name: "changed", kind: "recordChanged", table: "contacts", operations: ["update"], fields: ["status"] }
    ],
    last_run: { id: 7, workflow_id: 1, version: 2, trigger: "review", status: "succeeded", created_at: 1781604000000 }
  },
  {
    id: 2,
    database_name: "workspace",
    name: "welcome-contact",
    title: "",
    enabled: false,
    published_version: 1,
    latest_version: 1,
    timeout_seconds: 600,
    history_retention_days: 30,
    permission_level: 2,
    created_at: 1781600000000,
    updated_at: 1781600000000,
    triggers: [{ name: "daily", kind: "schedule", cron: "0 9 * * 1-5", timezone: "Asia/Shanghai" }]
  }
];

function workflowDetailFixture(workflow = workflowFixture[0]) {
  return {
    workflow,
    versions: [
      { workflow_id: workflow.id, version: 1, sdk: "1.0", message: "first", size: 1024, created_by: "test-user", created_at: 1781600000000, published: false, supported: true },
      { workflow_id: workflow.id, version: 2, sdk: "1.0", message: "second", size: 2048, created_by: "test-user", created_at: 1781601000000, published: true, supported: true }
    ],
    secrets: ["API_TOKEN"],
    vars: { REGION: "eu" },
    sdk: { version: "1.0", min_major: 1 }
  };
}

const workflowRunFixture = {
  id: 7,
  workflow_id: 1,
  version: 2,
  trigger: "review",
  status: "succeeded",
  event: { trigger: "review", runId: 7, input: { note: "hello" } },
  output: { ok: true, note: "hello" },
  started_by: "test-user",
  created_at: 1781604000000,
  started_at: 1781604000100,
  finished_at: 1781604000350,
  logs: [{ time: 1781604000200, level: "warn", message: "slow lookup", data: { ms: 120 } }]
};

const appFixture = [
  {
    id: 1,
    database_name: "workspace",
    name: "orders",
    title: "Orders",
    published_version: 2,
    latest_version: 3,
    creator_id: "test-user",
    permission_level: 2,
    created_at: 1781600000000,
    updated_at: 1781603000000,
    url: "/apps/workspace/orders/"
  },
  {
    id: 2,
    database_name: "workspace",
    name: "stock-check",
    title: "Stock check",
    published_version: 0,
    latest_version: 1,
    creator_id: "test-user",
    permission_level: 2,
    created_at: 1781600000000,
    updated_at: 1781600000000,
    url: "/apps/workspace/stock-check/"
  }
];

function appVersionFixture(appName: string, version: number, extra: Record<string, unknown> = {}) {
  return {
    app_id: 1,
    version,
    sdk: "1.0",
    message: `change ${version}`,
    size: 2048 * version,
    created_by: "test-user",
    created_at: 1781600000000 + version,
    supported: true,
    preview_url: `/apps/workspace/${appName}/@${version}/`,
    ...extra
  };
}

function appDetailFixture(app = appFixture[0]) {
  return {
    app,
    versions:
      app.name === "orders"
        ? [
            appVersionFixture("orders", 1),
            appVersionFixture("orders", 2),
            appVersionFixture("orders", 3, { sdk: "9.0", supported: false, sdk_error: "app SDK 9.0 is newer than this server's SDK 1.0" })
          ]
        : [appVersionFixture(app.name, 1)],
    sdk: { version: "1.0", min_major: 1 }
  };
}

const runnersFixture = {
  token: { exists: true, created_at: 1781600000000 },
  runners: [{ name: "intranet", version: "v1.0.0", connected_at: 1781603000000 }]
};

const authUserFixture = {
  id: "test-user",
  email: "user@example.com",
  display_name: "Test User",
  provider: "password",
  is_admin: true
};

const memberUserFixture = { ...authUserFixture, is_admin: false };

// What the server returns to a non-admin: data access only, structure is
// read-only and workflows come without their definitions.
const memberCatalogFixture = {
  databases: [
    {
      name: "workspace",
      tables: catalogFixture.databases[0].tables.map((table) => ({
        ...table,
        permission_level: 1,
        fields: table.fields.map((field) => ({ ...field, permission_level: 7 })),
        views: table.views.map((view) => ({ ...view, permission_level: 1 }))
      }))
    }
  ]
};

const memberWorkflowFixture = [
  {
    id: 1,
    database_name: "workspace",
    name: "record-review",
    title: "Record review",
    enabled: true,
    published_version: 2,
    permission_level: 1,
    triggers: [{ name: "review", kind: "manual", fields: [{ name: "note", label: "Note", type: "string" }] }]
  }
];

const adminOnlyEndpoints = ["/api/databases/workspace/roles", "/api/databases/workspace/runners", "/api/databases/workspace/workflows/record-review"];

// memberFetch serves the non-admin view of the workspace and records every
// request; admin-only endpoints answer 403 like the server does.
function memberFetch(requests: Array<{ url: string; init?: RequestInit }> = []) {
  return async (input: RequestInfo | URL, init?: RequestInit): Promise<Response> => {
    const url = String(input);
    requests.push({ url, init });
    if (url === "/api/auth/me") {
      return jsonResponse(memberUserFixture);
    }
    if (url === "/api/metadata") {
      return jsonResponse(memberCatalogFixture);
    }
    if (url === "/api/databases/workspace/workflows" && !init?.method) {
      return jsonResponse(memberWorkflowFixture);
    }
    if (adminOnlyEndpoints.includes(url) || url.includes("/workflows/record-review/runs?")) {
      return jsonResponse({ error: "admin only" }, 403);
    }
    if (url === "/api/databases/workspace/apps/orders") {
      return jsonResponse({ ...appDetailFixture({ ...appFixture[0], permission_level: 1 }), versions: [] });
    }
    if (url === "/api/databases/workspace/apps") {
      return jsonResponse(appFixture.map((app) => ({ ...app, permission_level: 1 })));
    }
    return defaultFetch(input, init);
  };
}

function jsonResponse(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), { status });
}

async function defaultFetch(input: RequestInfo | URL, init?: RequestInit): Promise<Response> {
  const url = String(input);
  if (url === "/api/auth/me") {
    return jsonResponse(authUserFixture);
  }
  if (url === "/api/auth/config") {
    return jsonResponse({ password_enabled: true, oidc_enabled: false, oidc_providers: [] });
  }
  if (url === "/api/metadata") {
    return jsonResponse(catalogFixture);
  }
  if (url === "/api/tables/workspace/contacts/rows" && init?.method === "POST") {
    return jsonResponse({ record_id: 4, values: JSON.parse(String(init.body)).values }, 201);
  }
  if (url === "/api/tables/workspace/contacts/rows/page" && init?.method === "POST") {
    const body = JSON.parse(String(init.body ?? "{}")) as {
      search?: string;
      offset?: number;
      limit?: number;
      sorts?: Array<{ field: string; direction: string }>;
    };
    let rows = [...rowFixture];
    if (body.search) {
      const term = body.search.toLowerCase();
      rows = rows.filter((row) => Object.values(row.values).some((value) => String(value).toLowerCase().includes(term)));
    }
    const sort = body.sorts?.[0];
    if (sort) {
      rows.sort((a, b) => {
        const left = String((a.values as Record<string, unknown>)[sort.field] ?? "");
        const right = String((b.values as Record<string, unknown>)[sort.field] ?? "");
        return sort.direction === "desc" ? right.localeCompare(left) : left.localeCompare(right);
      });
    }
    const offset = body.offset ?? 0;
    const limit = body.limit ?? rows.length;
    return jsonResponse({ rows: rows.slice(offset, offset + limit), total: rows.length });
  }
  if (url.startsWith("/api/tables/workspace/contacts/rows")) {
    return jsonResponse(rowFixture);
  }
  if (url === "/api/databases/workspace/workflows") {
    return jsonResponse(workflowFixture);
  }
  if (url === "/api/databases/workspace/workflows/record-review") {
    return jsonResponse(workflowDetailFixture(workflowFixture[0]));
  }
  if (url === "/api/databases/workspace/workflows/welcome-contact") {
    return jsonResponse(workflowDetailFixture(workflowFixture[1]));
  }
  if (url.startsWith("/api/databases/workspace/workflows/record-review/runs?")) {
    const { event: _event, logs: _logs, ...listed } = workflowRunFixture;
    return jsonResponse([listed]);
  }
  if (url === "/api/databases/workspace/workflows/record-review/runs/7") {
    return jsonResponse(workflowRunFixture);
  }
  if (url === "/api/databases/workspace/apps") {
    return jsonResponse(appFixture);
  }
  if (url === "/api/databases/workspace/apps/orders") {
    return jsonResponse(appDetailFixture(appFixture[0]));
  }
  if (url === "/api/databases/workspace/apps/stock-check") {
    return jsonResponse(appDetailFixture(appFixture[1]));
  }
  if (url === "/api/databases/workspace/roles") {
    return jsonResponse([]);
  }
  if (url === "/api/databases/workspace/runners") {
    if (init?.method === "POST") {
      return jsonResponse({ token: "atr_fresh-runner-token", created_at: 1781604000000 });
    }
    return jsonResponse(runnersFixture);
  }
  return jsonResponse({ error: `unhandled ${url}` }, 404);
}

beforeEach(async () => {
  vi.useRealTimers();
  vi.restoreAllMocks();
  window.history.replaceState(null, "", "/");
  window.localStorage.clear();
  await i18n.changeLanguage("en-US");
  vi.spyOn(globalThis, "fetch").mockImplementation(defaultFetch);
});

function renderApp(path = "/databases/workspace/tables/contacts") {
  window.history.replaceState(null, "", path);
  return render(<App />);
}

// The sidebar: database switcher, Home/Apps, tables and (admins) Manage.
async function sidebar() {
  return screen.findByRole("navigation", { name: "Workspace" });
}

async function openDatabaseSwitcher(user = userEvent.setup()) {
  await user.click(await screen.findByRole("button", { name: "workspace" }));
  return screen.findByRole("menu");
}

async function waitForSignedIn() {
  expect(await screen.findByRole("button", { name: authUserFixture.display_name })).toBeInTheDocument();
}

async function waitForDefaultTableReady(recordCountText = "3 of 3 records") {
  await waitForDefaultNavigationReady();
  await waitFor(() => expect(screen.getAllByText(recordCountText).length).toBeGreaterThan(0));
}

async function waitForDefaultNavigationReady() {
  await waitForSignedIn();
  expect(await screen.findByRole("button", { name: "workspace" })).toBeInTheDocument();
  expect(await within(await sidebar()).findByRole("link", { name: /Contacts/ })).toBeInTheDocument();
}

async function findEnabledButton(name: string | RegExp) {
  const button = await screen.findByRole("button", { name });
  await waitFor(() => expect(button).toBeEnabled());
  return button;
}

async function findDialog(name: string | RegExp) {
  return screen.findByRole("dialog", { name }, { timeout: 5000 });
}

function getFieldVisibilityStorageValue() {
  for (let index = 0; index < window.localStorage.length; index += 1) {
    const key = window.localStorage.key(index);
    if (key?.startsWith("autable.fieldVisibility:")) {
      return window.localStorage.getItem(key);
    }
  }
  return null;
}

describe("App", () => {
  it("opens the first database's home from the root", async () => {
    renderApp("/");
    await waitForSignedIn();
    await waitFor(() => expect(window.location.pathname).toBe("/databases/workspace"));
    expect(await screen.findByRole("heading", { name: "workspace", level: 1 })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "workspace" })).toBeInTheDocument();
    expect(within(await sidebar()).getByRole("link", { name: "Home" })).toHaveAttribute("aria-current", "page");
  });

  it("opens the database used last from the root", async () => {
    window.localStorage.setItem("autable.database", "sales");
    vi.mocked(fetch).mockImplementation(async (input, init) => {
      if (String(input) === "/api/metadata") {
        return jsonResponse({ databases: [...catalogFixture.databases, { name: "sales", tables: [] }] });
      }
      return defaultFetch(input, init);
    });
    renderApp("/");
    await waitFor(() => expect(window.location.pathname).toBe("/databases/sales"));
    expect(await screen.findByRole("button", { name: "sales" })).toBeInTheDocument();
  });

  it("shows an empty state when there is no database", async () => {
    vi.mocked(fetch).mockImplementation(async (input, init) => {
      if (String(input) === "/api/metadata") {
        return jsonResponse({ databases: [] });
      }
      return defaultFetch(input, init);
    });
    renderApp("/");
    expect(await screen.findByText("No databases yet")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "New database" })).toBeInTheDocument();
    expect(window.location.pathname).toBe("/");
  });

  it("renders a table view from a workspace route", async () => {
    renderApp();
    await waitForDefaultTableReady();
  });

  it("returns unmatched URLs to the default database home", async () => {
    renderApp("/not-a-real-route");
    await waitForSignedIn();
    await waitFor(() => expect(window.location.pathname).toBe("/databases/workspace"));
    expect(await screen.findByRole("heading", { name: "workspace", level: 1 })).toBeInTheDocument();
  });

  it("returns unknown database URLs to the default database home", async () => {
    renderApp("/databases/missing/tables/contacts");
    await waitForSignedIn();
    await waitFor(() => expect(window.location.pathname).toBe("/databases/workspace"));
    expect(await screen.findByRole("heading", { name: "workspace", level: 1 })).toBeInTheDocument();
  });

  it("updates the URL when navigating workspace resources", async () => {
    renderApp();
    await waitForDefaultTableReady();

    await userEvent.click(within(screen.getByRole("navigation", { name: "Views" })).getByRole("link", { name: "Active" }));
    expect(window.location.pathname).toBe("/databases/workspace/tables/contacts/views/active");

    const nav = await sidebar();
    await userEvent.click(within(nav).getByRole("link", { name: "Workflows" }));
    expect(window.location.pathname).toBe("/databases/workspace/workflows");
    await userEvent.click(await within(nav).findByRole("link", { name: /welcome-contact/ }));
    expect(window.location.pathname).toBe("/databases/workspace/workflows/welcome-contact");

    await userEvent.click(within(nav).getByRole("link", { name: "Apps" }));
    expect(window.location.pathname).toBe("/databases/workspace/apps");
    await userEvent.click(await within(await screen.findByRole("table", { name: "Apps" })).findByRole("button", { name: /Stock check/ }));
    expect(window.location.pathname).toBe("/databases/workspace/apps/stock-check");
    expect(await screen.findByRole("table", { name: "Versions" })).toBeInTheDocument();

    await userEvent.click(within(nav).getByRole("link", { name: "Overview" }));
    expect(window.location.pathname).toBe("/databases/workspace/admin");
    await userEvent.click(within(nav).getByRole("link", { name: "Home" }));
    expect(window.location.pathname).toBe("/databases/workspace");
    await userEvent.click(within(nav).getByRole("link", { name: "Contacts" }));
    expect(window.location.pathname).toBe("/databases/workspace/tables/contacts");
  });

  it("restores a workspace route on initial render", async () => {
    renderApp("/databases/workspace/tables/contacts/views/active-ops");

    await waitForDefaultNavigationReady();
    await waitFor(() => expect(screen.getByRole("link", { name: "Active ops" })).toHaveAttribute("aria-current", "page"));
    expect(within(await sidebar()).getByRole("link", { name: "Contacts" })).toHaveAttribute("aria-current", "page");
    expect(window.location.pathname).toBe("/databases/workspace/tables/contacts/views/active-ops");
  });

  it("requests temporary table sorting from the rows API", async () => {
    const pageBodies: Array<{ sorts?: Array<{ field: string; direction: string }> }> = [];
    vi.mocked(fetch).mockImplementation(async (input, init) => {
      if (String(input) === "/api/tables/workspace/contacts/rows/page" && init?.method === "POST") {
        pageBodies.push(JSON.parse(String(init.body)));
      }
      return defaultFetch(input, init);
    });

    const user = userEvent.setup();
    renderApp("/databases/workspace/tables/contacts");
    await waitForDefaultTableReady();
    const sortButton = await findEnabledButton("Toggle name sort");

    await user.click(sortButton);
    await waitFor(() => expect(pageBodies.at(-1)?.sorts).toEqual([{ field: "name", direction: "desc" }]));

    await user.click(sortButton);
    await waitFor(() => expect(pageBodies.at(-1)?.sorts).toEqual([{ field: "name", direction: "asc" }]));

    await user.click(sortButton);
    await waitFor(() => expect(pageBodies.at(-1)?.sorts).toBeUndefined());
  });

  it("stores hidden table fields locally", async () => {
    renderApp();
    await waitForDefaultTableReady();
    expect(screen.getByRole("grid", { name: "Table records" })).toHaveAttribute("aria-colcount", "4");

    fireEvent.click(await findEnabledButton("Fields"));
    const dialog = await findDialog("Fields");
    // The grid updates live behind the open dialog. It stays visible, but the
    // dialog is modal, so the page behind it leaves the accessibility tree a
    // moment after the dialog takes focus; query it with hidden included.
    const gridBehindDialog = () => screen.getByRole("grid", { name: "Table records", hidden: true });
    fireEvent.click(within(dialog).getByRole("button", { name: "Hide email", hidden: true }));

    await waitFor(() => expect(gridBehindDialog()).toHaveAttribute("aria-colcount", "3"));
    expect(getFieldVisibilityStorageValue()).toBe(JSON.stringify(["email"]));

    fireEvent.click(within(dialog).getByRole("button", { name: "Show email", hidden: true }));

    await waitFor(() => expect(gridBehindDialog()).toHaveAttribute("aria-colcount", "4"));
    expect(getFieldVisibilityStorageValue()).toBeNull();
  }, 15_000);

  it("moves table fields from the fields dialog", async () => {
    const requests: Array<{ url: string; body: unknown }> = [];
    vi.mocked(fetch).mockImplementation(async (input, init) => {
      const url = String(input);
      if (url === "/api/databases/workspace/tables/contacts/fields/status/position" && init?.method === "PATCH") {
        requests.push({ url, body: JSON.parse(String(init.body)) });
        return jsonResponse(catalogFixture.databases[0].tables[0]);
      }
      return defaultFetch(input, init);
    });

    renderApp();
    await waitForDefaultTableReady();
    fireEvent.click(await findEnabledButton("Fields"));
    const dialog = await findDialog("Fields");
    const dragData = new Map<string, string>();
    const dataTransfer = {
      effectAllowed: "",
      getData: (format: string) => dragData.get(format) ?? "",
      setData: (format: string, value: string) => dragData.set(format, value)
    };

    fireEvent.dragStart(within(dialog).getByRole("button", { name: "Drag status", hidden: true }), { dataTransfer });
    fireEvent.dragOver(within(dialog).getByRole("listitem", { name: "Field email", hidden: true }), { dataTransfer });
    fireEvent.drop(within(dialog).getByRole("listitem", { name: "Field email", hidden: true }), { dataTransfer });

    await waitFor(() =>
      expect(requests).toContainEqual({
        url: "/api/databases/workspace/tables/contacts/fields/status/position",
        body: { before: "email" }
      })
    );
  }, 15_000);

  it("does not load protected workspace resources before authentication", async () => {
    const requests: string[] = [];
    vi.mocked(fetch).mockImplementation(async (input) => {
      const url = String(input);
      requests.push(url);
      if (url === "/api/auth/me") {
        return jsonResponse({ error: "not authenticated" }, 401);
      }
      if (url === "/api/auth/config") {
        return jsonResponse({ password_enabled: true, oidc_enabled: false, oidc_providers: [] });
      }
      return jsonResponse({ error: `unexpected ${url}` }, 500);
    });

    renderApp();
    await waitFor(() => expect(requests).toContain("/api/auth/me"));
    expect(await screen.findByRole("form", { name: "Sign in" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Login" })).toBeInTheDocument();
    expect(screen.queryByRole("navigation", { name: "Workspace" })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "New database" })).not.toBeInTheDocument();
    expect(requests).not.toContain("/api/metadata");
    expect(requests.some((url) => url.includes("/rows"))).toBe(false);
    expect(requests.some((url) => url.includes("/workflows"))).toBe(false);
    expect(requests.some((url) => url.includes("/apps"))).toBe(false);
    expect(requests.some((url) => url.includes("/roles"))).toBe(false);
  });

  it("shows configured OIDC providers as login actions", async () => {
    vi.mocked(fetch).mockImplementation(async (input) => {
      const url = String(input);
      if (url === "/api/auth/me") {
        return jsonResponse({ error: "not authenticated" }, 401);
      }
      if (url === "/api/auth/config") {
        return jsonResponse({
          password_enabled: true,
          oidc_enabled: true,
          oidc_providers: [{ name: "example", issuer_url: "https://accounts.example.com", scopes: ["openid"] }]
        });
      }
      return defaultFetch(input);
    });

    renderApp();
    await screen.findByRole("form", { name: "Sign in" });
    expect(await screen.findByRole("button", { name: "Continue with example" })).toBeInTheDocument();
  });

  it("loads table rows from the backend when available", async () => {
    vi.mocked(fetch).mockImplementation(async (input, init) => {
      const url = String(input);
      if (url === "/api/auth/me") {
        return jsonResponse(authUserFixture);
      }
      if (url === "/api/auth/config") {
        return jsonResponse({ password_enabled: true, oidc_enabled: false, oidc_providers: [] });
      }
      if (url === "/api/tables/workspace/contacts/rows/page") {
        return jsonResponse({
          rows: [{ record_id: 42, values: { name: "Backend Row", email: "backend@example.com" } }],
          total: 1
        });
      }
      return defaultFetch(input, init);
    });

    renderApp();
    await waitForDefaultTableReady("1 of 1 records");
  });

  it("hides admin-only actions from non-admins", async () => {
    const requests: Array<{ url: string; init?: RequestInit }> = [];
    vi.mocked(fetch).mockImplementation(memberFetch(requests));

    renderApp();
    await waitForDefaultTableReady();
    const user = userEvent.setup();
    const switcher = await openDatabaseSwitcher(user);
    expect(within(switcher).getByRole("menuitemradio", { name: "workspace" })).toBeInTheDocument();
    expect(within(switcher).queryByRole("menuitem", { name: "New database" })).not.toBeInTheDocument();
    await user.keyboard("{Escape}");
    const nav = await sidebar();
    expect(within(nav).queryByRole("link", { name: "Permissions" })).not.toBeInTheDocument();
    expect(within(nav).queryByRole("link", { name: "Overview" })).not.toBeInTheDocument();
    // Ordinary users run their granted workflows from the main group instead.
    expect(within(nav).getByRole("link", { name: "Workflows" })).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Create Table" })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Create View" })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Remote runners" })).not.toBeInTheDocument();
    // No trailing add-field column: just the three data columns.
    expect(screen.getByRole("grid", { name: "Table records" })).toHaveAttribute("aria-colcount", "3");
    expect(screen.queryByRole("button", { name: "Field actions name" })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Filter" })).not.toBeInTheDocument();
    // Data access stays: the member can still add rows with their field grants.
    expect(screen.getByRole("button", { name: "Row" })).toBeEnabled();
    expect(requests.some((request) => adminOnlyEndpoints.includes(request.url))).toBe(false);
  });

  it("keeps a non-admin out of the permission view", async () => {
    vi.mocked(fetch).mockImplementation(memberFetch());

    renderApp("/databases/workspace/permissions");
    await waitForSignedIn();
    await waitFor(() => expect(window.location.pathname).toBe("/databases/workspace"));
    await within(await sidebar()).findByRole("link", { name: /Contacts/ });
    expect(screen.queryByRole("button", { name: "Members" })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Create Role" })).not.toBeInTheDocument();
  });

  it("keeps a non-admin out of the admin overview", async () => {
    const requests: Array<{ url: string; init?: RequestInit }> = [];
    vi.mocked(fetch).mockImplementation(memberFetch(requests));

    renderApp("/databases/workspace/admin");
    await waitForSignedIn();
    await waitFor(() => expect(window.location.pathname).toBe("/databases/workspace"));
    await within(await sidebar()).findByRole("link", { name: /Contacts/ });
    expect(screen.queryByRole("heading", { name: "Manage" })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Remote runners" })).not.toBeInTheDocument();
    expect(requests.some((request) => adminOnlyEndpoints.includes(request.url))).toBe(false);
  });

  it("shows admin-only actions to admins", async () => {
    renderApp();
    await waitForDefaultTableReady();
    const user = userEvent.setup();
    const switcher = await openDatabaseSwitcher(user);
    expect(within(switcher).getByRole("menuitem", { name: "New database" })).toBeInTheDocument();
    await user.keyboard("{Escape}");
    const nav = await sidebar();
    expect(within(nav).getByRole("link", { name: "Permissions" })).toBeInTheDocument();
    expect(within(nav).getByRole("link", { name: "Overview" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Create Table" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Create View" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Remote runners" })).toBeInTheDocument();
    // The trailing add-field column.
    expect(screen.getByRole("grid", { name: "Table records" })).toHaveAttribute("aria-colcount", "4");
    expect(screen.getByRole("button", { name: "Field actions name" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Filter" })).toBeInTheDocument();
  });

  it("disables row creation outside all records", async () => {
    renderApp();
    await waitForDefaultTableReady();
    expect(screen.getByRole("button", { name: "Row" })).toBeEnabled();
    await userEvent.click(within(screen.getByRole("navigation", { name: "Views" })).getByRole("link", { name: "Active" }));

    await waitFor(() => expect(screen.getByRole("button", { name: "Row" })).toBeDisabled());
  });

  it("creates a database from the sidebar and selects it", async () => {
    let created = false;
    vi.mocked(fetch).mockImplementation(async (input, init) => {
      const url = String(input);
      if (url === "/api/auth/me") {
        return jsonResponse(authUserFixture);
      }
      if (url === "/api/auth/config") {
        return jsonResponse({ password_enabled: true, oidc_enabled: false, oidc_providers: [] });
      }
      if (url.startsWith("/api/tables/workspace/contacts/rows")) {
        return new Response(JSON.stringify({ error: "permission denied" }), { status: 403 });
      }
      if (url === "/api/databases" && init?.method === "POST") {
        created = true;
        return new Response(JSON.stringify({ name: "sales", tables: [] }), {
          status: 201
        });
      }
      if (url === "/api/metadata") {
        return jsonResponse({
          databases: created ? [...catalogFixture.databases, { name: "sales", tables: [] }] : catalogFixture.databases
        });
      }
      return defaultFetch(input, init);
    });

    renderApp();
    await waitForSignedIn();
    const user = userEvent.setup();
    const switcher = await openDatabaseSwitcher(user);
    expect(within(switcher).queryByRole("menuitemradio", { name: "sales" })).not.toBeInTheDocument();
    await user.click(within(switcher).getByRole("menuitem", { name: "New database" }));
    const dialog = await findDialog("New database");
    await user.type(within(dialog).getByRole("textbox", { name: "New database name" }), "sales");
    await user.click(within(dialog).getByRole("button", { name: "Save" }));
    await waitFor(() => expect(window.location.pathname).toBe("/databases/sales"));
    expect(await screen.findByRole("button", { name: "sales" })).toBeInTheDocument();
    expect(screen.getAllByText("Created database sales").length).toBeGreaterThan(0);
  });

  it("creates a table in the selected database", async () => {
    let created = false;
    vi.mocked(fetch).mockImplementation(async (input, init) => {
      const url = String(input);
      if (url === "/api/auth/me") {
        return jsonResponse(authUserFixture);
      }
      if (url === "/api/auth/config") {
        return jsonResponse({ password_enabled: true, oidc_enabled: false, oidc_providers: [] });
      }
      if (url.startsWith("/api/tables/workspace/contacts/rows")) {
        return new Response(JSON.stringify({ error: "permission denied" }), { status: 403 });
      }
      if (url === "/api/tables/workspace/projects/rows") {
        return jsonResponse([]);
      }
      if (url === "/api/databases/workspace/tables" && init?.method === "POST") {
        created = true;
        return new Response(
          JSON.stringify({
            name: "projects",
            display_name: "projects",
            fields: [],
            views: []
          }),
          { status: 201 }
        );
      }
      if (url === "/api/metadata") {
        return new Response(
          JSON.stringify({
            databases: [
              {
                name: "workspace",
                tables: created
                  ? [
                      {
                        name: "projects",
                        display_name: "projects",
                        fields: [],
                        views: []
                      }
                    ]
                  : []
              }
            ]
          }),
          { status: 200 }
        );
      }
      return defaultFetch(input, init);
    });

    renderApp("/databases/workspace");
    await waitForSignedIn();
    expect(await within(await sidebar()).findByText("No tables yet")).toBeInTheDocument();
    await userEvent.click(await findEnabledButton("Create Table"));
    await userEvent.type(screen.getByRole("textbox", { name: "New table name" }), "projects");
    await userEvent.click(await findEnabledButton("Save"));
    await waitFor(() => expect(within(screen.getByRole("navigation", { name: "Workspace" })).getByRole("link", { name: /projects/ })).toBeInTheDocument());
    await waitFor(() => expect(window.location.pathname).toBe("/databases/workspace/tables/projects"));
  });

  it("creates a named table view from the view strip", async () => {
    let metadata = catalogFixture;
    let savedTable: unknown;
    vi.mocked(fetch).mockImplementation(async (input, init) => {
      const url = String(input);
      if (url === "/api/metadata") {
        return jsonResponse(metadata);
      }
      if (url === "/api/databases/workspace/tables/contacts" && init?.method === "PUT") {
        savedTable = JSON.parse(String(init.body));
        metadata = {
          databases: [
            {
              ...catalogFixture.databases[0],
              tables: [savedTable as typeof catalogFixture.databases[number]["tables"][number]]
            }
          ]
        };
        return jsonResponse(savedTable);
      }
      return defaultFetch(input, init);
    });

    renderApp();
    await waitForDefaultTableReady();
    await userEvent.click(await findEnabledButton("Create View"));
    await userEvent.type(screen.getByRole("textbox", { name: "New view name" }), "Needs Review");
    await userEvent.click(await findEnabledButton("Save"));

    await waitFor(() => expect(screen.getByText("Created view Needs Review")).toBeInTheDocument());
    expect(savedTable).toMatchObject({
      views: expect.arrayContaining([{ name: "Needs Review", display_name: "Needs Review", sorts: [] }])
    });
    expect(screen.getAllByText("Needs Review").length).toBeGreaterThan(0);
    await waitFor(() => expect(window.location.pathname).toBe("/databases/workspace/tables/contacts/views/Needs%20Review"));
    // The new view opens with its filter editor, ready to define it.
    expect(await screen.findByRole("dialog", { name: "View filters" })).toBeInTheDocument();
  });

  it("loads row history for the selected table record", async () => {
    vi.mocked(fetch).mockImplementation(async (input, init) => {
      const url = String(input);
      if (url === "/api/auth/me") {
        return jsonResponse(authUserFixture);
      }
      if (url === "/api/auth/config") {
        return jsonResponse({ password_enabled: true, oidc_enabled: false, oidc_providers: [] });
      }
      if (url === "/api/tables/workspace/contacts/rows/page") {
        return jsonResponse({
          rows: [{ record_id: 42, values: { name: "Backend Row", email: "backend@example.com" } }],
          total: 1
        });
      }
      if (url === "/api/tables/workspace/contacts/rows/42/history") {
        return new Response(
          JSON.stringify([
            {
              history_key: "rhistory_workspace_contacts_00000000000000000042_00000000000000000100",
              database: "workspace",
              table: "contacts",
              record_id: 42,
              timestamp: 1781604000000,
              values: { name: "Backend Row" },
              actor_id: "test-user"
            }
          ]),
          { status: 200 }
        );
      }
      return defaultFetch(input, init);
    });

    renderApp();
    await waitForDefaultTableReady("1 of 1 records");
    await userEvent.click(await findEnabledButton("History"));
    expect(await screen.findByRole("tab", { name: "History", selected: true })).toBeInTheDocument();
    expect(screen.getByText("Record change")).toBeInTheDocument();
    expect(screen.queryByText("rhistory_workspace_contacts_00000000000000000042_00000000000000000100")).not.toBeInTheDocument();
    expect(screen.getAllByText(/Backend Row/).length).toBeGreaterThan(0);
  });

  it("lists workflows with their state, triggers and last run", async () => {
    renderApp("/databases/workspace/workflows");
    const table = await screen.findByRole("table", { name: "Workflows" });
    const review = within(table).getByRole("row", { name: "record-review" });
    expect(within(review).getByRole("link", { name: "Record review" })).toHaveAttribute("href", "/databases/workspace/workflows/record-review");
    expect(within(review).getByText("Enabled")).toBeInTheDocument();
    expect(within(review).getByText("Manual")).toBeInTheDocument();
    expect(within(review).getByText("On change: contacts · status")).toBeInTheDocument();
    expect(within(review).getByText("Succeeded")).toBeInTheDocument();
    const welcome = within(table).getByRole("row", { name: "welcome-contact" });
    expect(within(welcome).getByText("Disabled")).toBeInTheDocument();
    expect(within(welcome).getByText("Every weekday 09:00 (Asia/Shanghai)")).toBeInTheDocument();
    expect(within(welcome).getByText("Never run")).toBeInTheDocument();
    expect(screen.getByText(/autablectl workflow init my-workflow --database workspace/)).toBeInTheDocument();
  });

  it("shows a workflow's triggers, versions and runs as tabs of its page", async () => {
    renderApp("/databases/workspace/workflows/record-review");
    expect(await screen.findByRole("heading", { name: "Record review", level: 1 })).toBeInTheDocument();
    expect(within(await sidebar()).getByRole("link", { name: "Record review" })).toHaveAttribute("aria-current", "page");
    const changed = screen.getByRole("group", { name: "Trigger changed" });
    expect(within(changed).getByText("contacts")).toBeInTheDocument();
    expect(within(changed).getByText("Update")).toBeInTheDocument();
    expect(screen.getByRole("switch", { name: "Enabled" })).toBeChecked();

    await userEvent.click(screen.getByRole("tab", { name: "Versions" }));
    expect(window.location.pathname).toBe("/databases/workspace/workflows/record-review/versions");
    const versions = await screen.findByRole("table", { name: "Versions" });
    expect(within(versions).getByRole("button", { name: "Roll back to version 1" })).toBeInTheDocument();

    await userEvent.click(screen.getByRole("tab", { name: "Runs" }));
    expect(window.location.pathname).toBe("/databases/workspace/workflows/record-review/runs");
    await userEvent.click(await screen.findByRole("button", { name: "Run 7, Succeeded" }));
    expect(window.location.pathname).toBe("/databases/workspace/workflows/record-review/runs/7");
    const run = await screen.findByRole("region", { name: "Run #7" });
    expect(within(run).getByLabelText("Output")).toHaveTextContent('"note": "hello"');
    expect(within(run).getByText("slow lookup")).toBeInTheDocument();
    expect(within(run).getByText("Test User")).toBeInTheDocument();
  });

  it("returns unknown workflow routes to the workflow list", async () => {
    renderApp("/databases/workspace/workflows/missing");
    await waitFor(() => expect(window.location.pathname).toBe("/databases/workspace/workflows"));
    expect(await screen.findByRole("table", { name: "Workflows" })).toBeInTheDocument();
  });

  it("manages the database's remote runners from the sidebar", async () => {
    renderApp();
    await waitForDefaultTableReady();
    await userEvent.click(await screen.findByRole("button", { name: "Remote runners" }));

    const dialog = await findDialog("Remote runners");
    // The dialog is scoped to the current database.
    expect(within(dialog).getByText(/for workspace from machines in other networks/)).toBeInTheDocument();
    expect(await within(dialog).findByText("intranet")).toBeInTheDocument();
    expect(within(dialog).getByText(/v1\.0\.0/)).toBeInTheDocument();
    expect(within(dialog).queryByText(/atr_/)).not.toBeInTheDocument();

    await userEvent.click(within(dialog).getByRole("button", { name: "Reset token" }));
    expect(await within(dialog).findByText("atr_fresh-runner-token")).toBeInTheDocument();
    expect(within(dialog).getByText(/will not be shown again/)).toBeInTheDocument();
  }, 15_000);

  it("lets run-granted users start a workflow and follow their own runs", async () => {
    const requests: Array<{ url: string; init?: RequestInit }> = [];
    const member = memberFetch(requests);
    vi.mocked(fetch).mockImplementation(async (input, init) => {
      const url = String(input);
      if (url === "/api/databases/workspace/workflows/record-review/runs" && init?.method === "POST") {
        requests.push({ url, init });
        return jsonResponse({ id: 12, workflow_id: 1, trigger: "review", status: "succeeded", output: { ok: true }, created_at: 1781604000000 }, 200);
      }
      if (url === "/api/databases/workspace/workflows/record-review/runs/12") {
        return jsonResponse({ id: 12, workflow_id: 1, trigger: "review", status: "succeeded", output: { ok: true }, created_at: 1781604000000 });
      }
      return member(input, init);
    });

    renderApp("/databases/workspace/workflows");
    await waitForSignedIn();
    expect(await screen.findByText("Runs you start appear here with their status and output.")).toBeInTheDocument();
    await userEvent.click(await findEnabledButton("Run Record review"));
    const dialog = await findDialog("Run Record review");
    await userEvent.type(within(dialog).getByLabelText("Note"), "please check");
    await userEvent.click(within(dialog).getByRole("button", { name: "Start run" }));
    await waitFor(() =>
      expect(requests.find((request) => request.url.endsWith("/record-review/runs") && request.init?.method === "POST")?.init?.body).toBe(
        JSON.stringify({ input: { note: "please check" }, wait: true })
      )
    );
    expect(await within(dialog).findByLabelText("Output")).toHaveTextContent('"ok": true');
    // Users cannot open the run's admin page.
    expect(within(dialog).queryByRole("button", { name: "Open run" })).not.toBeInTheDocument();
    await userEvent.keyboard("{Escape}");

    const ownRuns = await screen.findByRole("list", { name: "Your recent runs" });
    expect(within(ownRuns).getByRole("listitem", { name: "Record review run 12" })).toHaveTextContent("Succeeded");
    expect(JSON.parse(window.localStorage.getItem("autable.workflowRuns:test-user:workspace") ?? "[]")).toEqual([{ workflow: "record-review", id: 12 }]);
    expect(requests.some((request) => adminOnlyEndpoints.includes(request.url))).toBe(false);
    expect(requests.some((request) => request.url.includes("/runs?"))).toBe(false);
  });

  it("keeps run-granted users on the workflow list", async () => {
    vi.mocked(fetch).mockImplementation(memberFetch());
    renderApp("/databases/workspace/workflows/record-review/versions");
    await waitForSignedIn();
    await waitFor(() => expect(window.location.pathname).toBe("/databases/workspace/workflows"));
    expect(await screen.findByRole("list", { name: "Runnable workflows" })).toBeInTheDocument();
    expect(screen.queryByRole("tab", { name: "Versions" })).not.toBeInTheDocument();
  });

  it("lists apps with their published versions", async () => {
    renderApp("/databases/workspace/apps");
    await waitForSignedIn();
    const appTable = await screen.findByRole("table", { name: "Apps" });
    const rows = within(appTable).getAllByRole("row");
    expect(rows).toHaveLength(3);
    expect(within(rows[1]).getByText("orders")).toBeInTheDocument();
    expect(within(rows[1]).getByText("v2")).toBeInTheDocument();
    expect(within(rows[1]).getByRole("link", { name: "Open Orders" })).toHaveAttribute("href", "/apps/workspace/orders/");
    expect(within(rows[2]).getByText("Not published")).toBeInTheDocument();
    expect(within(rows[2]).queryByRole("link")).not.toBeInTheDocument();
    expect(screen.getByText("autablectl app init my-app --database workspace --name my-app")).toBeInTheDocument();

    await userEvent.click(within(rows[1]).getByRole("button", { name: "Orders" }));
    expect(window.location.pathname).toBe("/databases/workspace/apps/orders");
  });

  it("explains how to create apps from the command line when there are none", async () => {
    vi.mocked(fetch).mockImplementation(async (input, init) => {
      if (String(input) === "/api/databases/workspace/apps") {
        return jsonResponse([]);
      }
      return defaultFetch(input, init);
    });

    renderApp("/databases/workspace/apps");
    expect(await screen.findByText("No apps yet")).toBeInTheDocument();
    expect(screen.getByText("autablectl app init my-app --database workspace --name my-app")).toBeInTheDocument();
    expect(screen.getByText(/autablectl app push --publish/)).toBeInTheDocument();
    expect(screen.getByText("autablectl skill install")).toBeInTheDocument();
  });

  it("shows app versions newest first with preview, publish, rollback and source actions", async () => {
    const publishBodies: unknown[] = [];
    vi.mocked(fetch).mockImplementation(async (input, init) => {
      const url = String(input);
      if (url === "/api/databases/workspace/apps/orders/publish" && init?.method === "POST") {
        publishBodies.push(JSON.parse(String(init.body)));
        return jsonResponse({ ...appFixture[0], published_version: 1 });
      }
      return defaultFetch(input, init);
    });

    renderApp("/databases/workspace/apps/orders");
    const versionTable = await screen.findByRole("table", { name: "Versions" });
    const rows = within(versionTable).getAllByRole("row").slice(1);
    expect(rows.map((row) => row.getAttribute("aria-label"))).toEqual(["v3", "v2", "v1"]);
    expect(screen.getByRole("link", { name: "Open Orders" })).toHaveAttribute("href", "/apps/workspace/orders/");

    // v3 was built against an SDK this server does not support.
    expect(within(rows[0]).getByText("Unsupported")).toBeInTheDocument();
    expect(within(rows[0]).getByRole("button", { name: "Publish version 3" })).toBeDisabled();
    // v2 is the published one and carries the marker instead of a publish action.
    expect(within(rows[1]).getByText("Published")).toBeInTheDocument();
    expect(within(rows[1]).queryByRole("button", { name: /Publish version 2|Roll back to version 2/ })).not.toBeInTheDocument();
    expect(within(rows[2]).getByRole("link", { name: "Preview version 1" })).toHaveAttribute("href", "/apps/workspace/orders/@1/");
    expect(within(rows[2]).getByRole("link", { name: "Download source of version 1" })).toHaveAttribute(
      "href",
      "/api/databases/workspace/apps/orders/versions/1/source"
    );

    await userEvent.click(within(rows[2]).getByRole("button", { name: "Roll back to version 1" }));
    const dialog = await findDialog("Roll back app");
    expect(within(dialog).getByText(/Roll back to version 1\?/)).toBeInTheDocument();
    expect(publishBodies).toEqual([]);
    await userEvent.click(within(dialog).getByRole("button", { name: "Roll back" }));
    await waitFor(() => expect(publishBodies).toEqual([{ version: 1 }]));
    expect((await screen.findAllByText("Rolled back Orders to version 1")).length).toBeGreaterThan(0);
  });

  it("shows non-admins the open link but no app management", async () => {
    vi.mocked(fetch).mockImplementation(memberFetch());

    renderApp("/databases/workspace/apps");
    const appTable = await screen.findByRole("table", { name: "Apps" });
    expect(within(appTable).getByRole("link", { name: "Open Orders" })).toHaveAttribute("href", "/apps/workspace/orders/");
    expect(screen.queryByText(/autablectl/)).not.toBeInTheDocument();

    await userEvent.click(within(appTable).getByRole("button", { name: "Orders" }));
    expect(await screen.findByRole("link", { name: "Open Orders" })).toBeInTheDocument();
    expect(await screen.findByText("orders in workspace")).toBeInTheDocument();
    expect(screen.queryByRole("table", { name: "Versions" })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Delete app" })).not.toBeInTheDocument();
  });

  it("deletes an app as an admin after confirmation", async () => {
    let deleted = false;
    vi.mocked(fetch).mockImplementation(async (input, init) => {
      const url = String(input);
      if (url === "/api/databases/workspace/apps/orders" && init?.method === "DELETE") {
        deleted = true;
        return new Response(null, { status: 204 });
      }
      if (url === "/api/databases/workspace/apps" && deleted) {
        return jsonResponse([appFixture[1]]);
      }
      return defaultFetch(input, init);
    });

    renderApp("/databases/workspace/apps/orders");
    await screen.findByRole("table", { name: "Versions" });
    await userEvent.click(screen.getByRole("button", { name: "Delete app" }));
    const dialog = await findDialog("Delete app");
    await userEvent.click(within(dialog).getByRole("button", { name: "Delete" }));
    await waitFor(() => expect(deleted).toBe(true));
    await waitFor(() => expect(window.location.pathname).toBe("/databases/workspace/apps"));
    expect(await screen.findByRole("table", { name: "Apps" })).toBeInTheDocument();
  });

  it("returns unknown app routes to the app list", async () => {
    renderApp("/databases/workspace/apps/missing");
    await waitFor(() => expect(window.location.pathname).toBe("/databases/workspace/apps"));
    expect(await screen.findByRole("table", { name: "Apps" })).toBeInTheDocument();
  });

  it("hides the built-in all view when the server omits it and falls back to the first view", async () => {
    const pageBodies: Array<{ view?: string }> = [];
    vi.mocked(fetch).mockImplementation(async (input, init) => {
      const url = String(input);
      if (url === "/api/metadata") {
        return jsonResponse({
          databases: [
            {
              name: "workspace",
              tables: [
                {
                  name: "contacts",
                  display_name: "Contacts",
                  fields: [{ name: "name", type: "string", deleted: false }],
                  views: [{ name: "mine", display_name: "Mine", sorts: [] }]
                }
              ]
            }
          ]
        });
      }
      if (url === "/api/tables/workspace/contacts/rows/page" && init?.method === "POST") {
        pageBodies.push(JSON.parse(String(init.body)));
        return jsonResponse({ rows: rowFixture, total: rowFixture.length });
      }
      return defaultFetch(input, init);
    });

    renderApp();
    await waitForSignedIn();
    const views = await screen.findByRole("navigation", { name: "Views" });
    expect(await within(views).findByRole("link", { name: "Mine" })).toBeInTheDocument();
    expect(within(views).queryByRole("link", { name: "All records" })).not.toBeInTheDocument();
    await waitFor(() => expect(within(views).getByRole("link", { name: "Mine" })).toHaveAttribute("aria-current", "page"));
    await waitFor(() => expect(pageBodies.some((body) => body.view === "mine")).toBe(true));
  });

  it("searches records server-side from the table toolbar", async () => {
    const pageBodies: Array<{ search?: string }> = [];
    vi.mocked(fetch).mockImplementation(async (input, init) => {
      if (String(input) === "/api/tables/workspace/contacts/rows/page" && init?.method === "POST") {
        pageBodies.push(JSON.parse(String(init.body)));
      }
      return defaultFetch(input, init);
    });

    renderApp();
    await waitForDefaultTableReady();

    await userEvent.type(screen.getByRole("searchbox", { name: "Search records" }), "grace");
    await waitFor(() => expect(pageBodies.at(-1)?.search).toBe("grace"));
    await waitFor(() => expect(screen.getAllByText("1 of 1 records").length).toBeGreaterThan(0));

    await userEvent.clear(screen.getByRole("searchbox", { name: "Search records" }));
    await waitFor(() => expect(screen.getAllByText("3 of 3 records").length).toBeGreaterThan(0));
  }, 15_000);

  it("loads the next page when the grid scrolls near the bottom", async () => {
    const manyRows = Array.from({ length: 250 }, (_, index) => ({
      record_id: index + 1,
      values: { name: `Person ${index + 1}`, email: `person${index + 1}@example.com`, status: "Active" }
    }));
    const pageBodies: Array<{ offset?: number; limit?: number }> = [];
    vi.mocked(fetch).mockImplementation(async (input, init) => {
      const url = String(input);
      if (url === "/api/tables/workspace/contacts/rows/page" && init?.method === "POST") {
        const body = JSON.parse(String(init.body)) as { offset?: number; limit?: number };
        pageBodies.push(body);
        const offset = body.offset ?? 0;
        const limit = body.limit ?? manyRows.length;
        return jsonResponse({ rows: manyRows.slice(offset, offset + limit), total: manyRows.length });
      }
      return defaultFetch(input, init);
    });

    renderApp();
    await waitForDefaultTableReady("200 of 250 records");

    const grid = screen.getByRole("grid", { name: "Table records" });
    Object.defineProperty(grid, "scrollHeight", { value: 8000, configurable: true });
    Object.defineProperty(grid, "clientHeight", { value: 600, configurable: true });
    grid.scrollTop = 7500;
    fireEvent.scroll(grid);

    await waitFor(() => expect(pageBodies.some((body) => body.offset === 200)).toBe(true));
    await waitFor(() => expect(screen.getAllByText("250 of 250 records").length).toBeGreaterThan(0));
  }, 15_000);
  it("shows admins every app as a launcher card and the tables on the home page", async () => {
    renderApp("/databases/workspace");
    const apps = await screen.findByRole("region", { name: "Apps" });
    expect(await within(apps).findByRole("link", { name: "Orders" })).toHaveAttribute("href", "/databases/workspace/apps/orders");
    expect(within(apps).getByRole("link", { name: "Open Orders" })).toHaveAttribute("href", "/apps/workspace/orders/");
    // Unpublished apps are listed for admins, without an open link.
    expect(within(apps).getByRole("link", { name: "Stock check" })).toBeInTheDocument();
    expect(within(apps).getByText("Not published")).toBeInTheDocument();
    expect(within(apps).queryByRole("link", { name: "Open Stock check" })).not.toBeInTheDocument();
    const tables = screen.getByRole("region", { name: "Tables" });
    expect(within(tables).getByRole("link", { name: /Contacts/ })).toHaveAttribute("href", "/databases/workspace/tables/contacts");
    expect(within(tables).getByText("3 fields · 3 views")).toBeInTheDocument();

    await userEvent.click(within(apps).getByRole("link", { name: "Orders" }));
    expect(window.location.pathname).toBe("/databases/workspace/apps/orders");
  });

  it("shows ordinary users only the apps they can open on the home page", async () => {
    vi.mocked(fetch).mockImplementation(memberFetch());
    renderApp("/databases/workspace");
    const apps = await screen.findByRole("region", { name: "Apps" });
    expect(await within(apps).findByRole("link", { name: "Open Orders" })).toHaveAttribute("href", "/apps/workspace/orders/");
    expect(within(apps).queryByText("Stock check")).not.toBeInTheDocument();
    expect(within(apps).queryByText("Not published")).not.toBeInTheDocument();
    expect(within(screen.getByRole("region", { name: "Tables" })).getByRole("link", { name: /Contacts/ })).toBeInTheDocument();
  });

  it("tells ordinary users when nothing is shared with them", async () => {
    const member = memberFetch();
    vi.mocked(fetch).mockImplementation(async (input, init) => {
      const url = String(input);
      if (url === "/api/metadata") {
        return jsonResponse({ databases: [{ name: "workspace", tables: [] }] });
      }
      if (url === "/api/databases/workspace/apps") {
        return jsonResponse([]);
      }
      return member(input, init);
    });
    renderApp("/databases/workspace");
    expect(await screen.findByText("Nothing shared with you yet")).toBeInTheDocument();
    expect(screen.queryByRole("region", { name: "Apps" })).not.toBeInTheDocument();
  });

  it("summarizes the management areas on the admin overview", async () => {
    renderApp("/databases/workspace/admin");
    expect(await screen.findByRole("heading", { name: "Manage", level: 1 })).toBeInTheDocument();
    await waitFor(() => expect(screen.getByRole("link", { name: /Workflows.*2 workflows · 1 active/ })).toHaveAttribute("href", "/databases/workspace/workflows"));
    expect(screen.getByRole("link", { name: /Apps & versions.*2 apps · 1 published/ })).toHaveAttribute("href", "/databases/workspace/apps");
    expect(screen.getByRole("link", { name: /Permissions & roles/ })).toHaveAttribute("href", "/databases/workspace/permissions");
    const tables = screen.getByRole("table", { name: "Tables & schema" });
    expect(within(tables).getByRole("link", { name: /Contacts/ })).toHaveAttribute("href", "/databases/workspace/tables/contacts");

    await userEvent.click(screen.getByRole("button", { name: /Remote runners.*other networks/ }));
    const dialog = await findDialog("Remote runners");
    expect(await within(dialog).findByText("intranet")).toBeInTheDocument();
  });

  it("signs out from the account menu", async () => {
    const user = userEvent.setup();
    let signedIn = true;
    vi.mocked(fetch).mockImplementation(async (input, init) => {
      const url = String(input);
      if (url === "/api/auth/logout") {
        signedIn = false;
        return new Response(null, { status: 204 });
      }
      if (url === "/api/auth/me" && !signedIn) {
        return jsonResponse({ error: "not authenticated" }, 401);
      }
      return defaultFetch(input, init);
    });
    renderApp();
    await waitForDefaultTableReady();
    await user.click(screen.getByRole("button", { name: authUserFixture.display_name }));
    expect(within(await screen.findByRole("menu")).getByText(authUserFixture.email)).toBeInTheDocument();
    await user.click(screen.getByRole("menuitem", { name: "Logout" }));
    expect(await screen.findByRole("form", { name: "Sign in" })).toBeInTheDocument();
    expect(screen.queryByRole("navigation", { name: "Workspace" })).not.toBeInTheDocument();
  });

  it("switches the theme and the language from the sidebar", async () => {
    const user = userEvent.setup();
    renderApp("/databases/workspace");
    await waitForSignedIn();
    await user.click(screen.getByRole("button", { name: "Appearance: System" }));
    await user.click(await screen.findByRole("menuitemradio", { name: "Dark" }));
    await waitFor(() => expect(document.documentElement).toHaveClass("dark"));
    expect(window.localStorage.getItem("autable.theme")).toBe("dark");

    await user.click(screen.getByRole("button", { name: "Switch language" }));
    expect(await screen.findByRole("navigation", { name: "工作区" })).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "首页" })).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "切换语言" }));
    expect(await within(await sidebar()).findByRole("link", { name: "Home" })).toBeInTheDocument();
    document.documentElement.classList.remove("dark");
  });

  it("puts the navigation in a drawer on narrow screens", async () => {
    const user = userEvent.setup();
    vi.stubGlobal("matchMedia", (query: string) => ({
      matches: false,
      media: query,
      addEventListener: () => undefined,
      removeEventListener: () => undefined
    }));
    try {
      renderApp();
      await waitFor(() => expect(screen.getAllByText("3 of 3 records").length).toBeGreaterThan(0));
      expect(screen.queryByRole("navigation", { name: "Workspace" })).not.toBeInTheDocument();
      await user.click(screen.getByRole("button", { name: "Open navigation" }));
      const drawer = await screen.findByRole("dialog", { name: "Navigation" });
      await user.click(within(drawer).getByRole("link", { name: "Apps" }));
      expect(window.location.pathname).toBe("/databases/workspace/apps");
      await waitFor(() => expect(screen.queryByRole("dialog", { name: "Navigation" })).not.toBeInTheDocument());
    } finally {
      vi.unstubAllGlobals();
    }
  });
});
