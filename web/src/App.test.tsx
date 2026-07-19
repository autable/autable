import { act, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { FluentProvider, webLightTheme } from "@fluentui/react-components";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { App } from "./App";
import i18n from "./i18n";

const catalogFixture = {
  databases: [
    {
      name: "workspace",
      permission_level: 2,
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
            { name: "all", display_name: "", sorts: [] },
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
    script:
      'function instances(info) { return { review_echo: { node: "echo", variables: [{ name: "CHANNEL", type: "string" }], secrets: [{ name: "TOKEN", type: "string" }] } }; }\nfunction run(info) { return info.instance("review_echo").exec({ value: info.inputs.name }); }',
    secrets: { "review_echo.TOKEN": 12 },
    variables: { "review_echo.CHANNEL": "ops" },
    permission_level: 2
  },
  {
    id: 2,
    database_name: "workspace",
    name: "welcome-contact",
    script:
      'function instances(info) { return { welcome_echo: "echo" }; }\nfunction run(info) { return info.instance("welcome_echo").exec({ value: info.inputs.name }); }',
    secrets: {},
    variables: { "welcome_echo.CHANNEL": "sales" },
    permission_level: 2
  }
];

const formFixture = [
  {
    id: 1,
    database_name: "workspace",
    name: "contact-intake",
    script:
      'function render(api, root) { root.append(api.input({ field: "name", label: "Name" }), api.submit("Create record")); return { table: "contacts" }; }',
    permission_level: 2
  },
  {
    id: 2,
    database_name: "workspace",
    name: "quick-status",
    script:
      'function render(api, root) { root.append(api.select({ field: "status", label: "Status", options: ["Active", "Review"] }), api.submit("Update status")); return { table: "contacts" }; }',
    permission_level: 2
  }
];

const runnersFixture = {
  token: { exists: true, created_at: 1781600000000 },
  can_manage: true,
  runners: [{ name: "intranet", version: "v1.0.0", node_types: ["echo", "dingtalk.robot.send"], connected_at: 1781603000000 }],
  remote_node_types: ["dingtalk.robot.send", "echo"]
};

const workflowNodeFixture = [
  {
    type: "dingtalk.robot.send",
    display_name: "DingTalk robot",
    documentation: {
      "en-US": "## DingTalk robot\n\n- `access_token` (`string`): DingTalk custom robot access token.",
      "zh-CN": "## 钉钉机器人\n\n- `access_token` (`string`): 钉钉自定义机器人的 access token。"
    },
    inputs: [{ name: "content", type: "string" }],
    outputs: [{ name: "status_code", type: "int" }],
    secrets: [{ name: "access_token", type: "string" }],
    stateless: true,
    trigger: false
  },
  {
    type: "echo",
    display_name: "Echo",
    documentation: {
      "en-US": "## Echo\n\nReturns the node input unchanged.",
      "zh-CN": "## Echo\n\n原样返回输入。"
    },
    inputs: [{ name: "value", type: "any" }],
    outputs: [{ name: "value", type: "any" }],
    stateless: true,
    trigger: false
  },
  {
    type: "table.record.changed",
    display_name: "Record changed",
    documentation: {
      "en-US": "## Record changed\n\nThe node output becomes `run(info).inputs`.\n\n- `table` (`string`): Optional table name.",
      "zh-CN": "## 记录变更\n\n节点输出会成为 `run(info).inputs`。\n\n- `table` (`string`): 可选表名。"
    },
    inputs: [{ name: "table", type: "string" }],
    outputs: [{ name: "history_key", type: "string" }],
    stateless: true,
    trigger: true
  }
];

const authUserFixture = {
  id: "test-user",
  email: "user@example.com",
  display_name: "Test User",
  provider: "password"
};

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
    if (init?.method === "POST") {
      const body = JSON.parse(String(init.body)) as typeof workflowFixture[number] & { secrets: Record<string, string> };
      return jsonResponse(
        {
          ...workflowFixture[0],
          ...body,
          id: body.id ?? workflowFixture[0].id,
          secrets: Object.fromEntries(
            Object.entries(body.secrets ?? {}).map(([key, value]) => [key, String(value).length])
          )
        },
        201
      );
    }
    return jsonResponse(workflowFixture);
  }
  if (url === "/api/databases/workspace/forms") {
    return jsonResponse(formFixture);
  }
  if (url === "/api/databases/workspace/roles") {
    return jsonResponse([]);
  }
  if (url === "/api/workflow/nodes") {
    return jsonResponse(workflowNodeFixture);
  }
  if (url === "/api/databases/workspace/runners") {
    if (init?.method === "POST") {
      return jsonResponse({ token: "atr_fresh-runner-token", created_at: 1781604000000 });
    }
    return jsonResponse(runnersFixture);
  }
  if (url === "/api/workflows/1/runs") {
    return jsonResponse([]);
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
  return render(
    <FluentProvider theme={webLightTheme}>
      <App />
    </FluentProvider>
  );
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
  expect(await screen.findByRole("button", { name: /^Table$/ })).toBeInTheDocument();
  expect(await screen.findByRole("button", { name: /Contacts/ })).toBeInTheDocument();
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
  it("renders the unselected default page at root", async () => {
    renderApp("/");
    await waitForSignedIn();
    expect(await screen.findByText("No database selected")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "workspace" })).toBeInTheDocument();
    expect(window.location.pathname).toBe("/");
  });

  it("renders a table view from a workspace route", async () => {
    renderApp();
    await waitForDefaultTableReady();
  });

  it("returns unmatched URLs to the unselected default page", async () => {
    renderApp("/not-a-real-route");
    await waitForSignedIn();
    expect(await screen.findByText("No database selected")).toBeInTheDocument();
    expect(window.location.pathname).toBe("/");
  });

  it("returns unknown database URLs to the unselected default page", async () => {
    renderApp("/databases/missing/tables/contacts");
    await waitForSignedIn();
    expect(await screen.findByText("No database selected")).toBeInTheDocument();
    await waitFor(() => expect(window.location.pathname).toBe("/"));
  });

  it("updates the URL when navigating workspace resources", async () => {
    renderApp();
    await waitForDefaultTableReady();

    await userEvent.click(await screen.findByRole("button", { name: "Active" }));
    expect(window.location.pathname).toBe("/databases/workspace/tables/contacts/views/active");

    await userEvent.click(await screen.findByRole("button", { name: /^Workflow$/ }));
    expect(window.location.pathname).toBe("/databases/workspace/workflows");
    await userEvent.click(await screen.findByRole("button", { name: /welcome-contact/ }));
    expect(window.location.pathname).toBe("/databases/workspace/workflows/2");

    await userEvent.click(await screen.findByRole("button", { name: /^Form$/ }));
    expect(window.location.pathname).toBe("/databases/workspace/forms");
    await userEvent.click(await screen.findByRole("button", { name: /quick-status/ }));
    expect(window.location.pathname).toBe("/databases/workspace/forms/2");
  });

  it("restores a workspace route on initial render", async () => {
    renderApp("/databases/workspace/tables/contacts/views/active-ops");

    await waitForDefaultNavigationReady();
    await waitFor(() => expect(screen.getByRole("button", { name: "Active ops" })).toHaveAttribute("aria-current", "page"));
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
    renderApp("/databases/workspace");
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
    fireEvent.click(within(dialog).getByRole("button", { name: "Hide email", hidden: true }));

    await waitFor(() => expect(screen.getByRole("grid", { name: "Table records" })).toHaveAttribute("aria-colcount", "3"));
    expect(getFieldVisibilityStorageValue()).toBe(JSON.stringify(["email"]));

    fireEvent.click(within(dialog).getByRole("button", { name: "Show email", hidden: true }));

    await waitFor(() => expect(screen.getByRole("grid", { name: "Table records" })).toHaveAttribute("aria-colcount", "4"));
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
    expect(screen.getByRole("button", { name: "Login" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Create DB" })).toBeDisabled();
    expect(screen.getByRole("button", { name: "Refresh metadata" })).toBeDisabled();
    expect(requests).not.toContain("/api/metadata");
    expect(requests.some((url) => url.includes("/rows"))).toBe(false);
    expect(requests.some((url) => url.includes("/workflows"))).toBe(false);
    expect(requests.some((url) => url.includes("/forms"))).toBe(false);
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
    await userEvent.click(await screen.findByRole("button", { name: "Login" }));
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

  it("hides database permissions for non-owners", async () => {
    vi.mocked(fetch).mockImplementation(async (input, init) => {
      const url = String(input);
      if (url === "/api/metadata") {
        return jsonResponse({
          databases: [{ ...catalogFixture.databases[0], permission_level: 0 }]
        });
      }
      return defaultFetch(input, init);
    });

    renderApp();
    await waitForSignedIn();
    await screen.findByRole("button", { name: /Contacts/ });
    expect(screen.queryByRole("button", { name: "Permission" })).not.toBeInTheDocument();
  });

  it("hides collapsed database permissions for non-owners", async () => {
    vi.mocked(fetch).mockImplementation(async (input, init) => {
      const url = String(input);
      if (url === "/api/metadata") {
        return jsonResponse({
          databases: [{ ...catalogFixture.databases[0], permission_level: 0 }]
        });
      }
      return defaultFetch(input, init);
    });

    renderApp();
    await waitForSignedIn();
    await screen.findByRole("button", { name: /Contacts/ });
    await userEvent.click(await findEnabledButton("Collapse sidebar"));

    expect(screen.queryByRole("button", { name: "Permission" })).not.toBeInTheDocument();
  });

  it("disables row creation outside all records", async () => {
    renderApp();
    await waitForDefaultTableReady();
    await userEvent.click(await screen.findByRole("button", { name: "Active" }));

    expect(screen.getByRole("button", { name: "Row" })).toBeDisabled();
  });

  it("creates a database from the sidebar and selects it", async () => {
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
        return new Response(JSON.stringify({ name: "sales", tables: [] }), {
          status: 201
        });
      }
      if (url === "/api/metadata") {
        return new Response(
          JSON.stringify({
            databases: [
              {
                name: "sales",
                tables: []
              }
            ]
          }),
          { status: 200 }
        );
      }
      return defaultFetch(input, init);
    });

    renderApp();
    await waitForSignedIn();
    await userEvent.click(await findEnabledButton("Create DB"));
    await userEvent.type(screen.getByRole("textbox", { name: "New database name" }), "sales");
    await userEvent.click(await findEnabledButton("Save"));
    await waitFor(() => expect(screen.getByRole("button", { name: "sales" })).toHaveAttribute("aria-expanded", "true"));
    expect(screen.getAllByText("Created database sales").length).toBeGreaterThan(0);
  });

  it("creates a table in the selected database", async () => {
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
                tables: [
                  {
                    name: "projects",
                    display_name: "projects",
                    fields: [],
                    views: []
                  }
                ]
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
    await userEvent.click(await findEnabledButton("Create Table"));
    await userEvent.type(screen.getByRole("textbox", { name: "New table name" }), "projects");
    await userEvent.click(await findEnabledButton("Save"));
    await waitFor(() => expect(screen.getByRole("button", { name: /projects/ })).toBeInTheDocument());
  });

  it("creates a named table view from the sidebar", async () => {
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

  it("shows workflow JavaScript as the workflow view", async () => {
    renderApp();
    await waitForDefaultTableReady();
    await userEvent.click(await screen.findByRole("button", { name: /^Workflow$/ }));
    expect(await screen.findByRole("button", { name: /welcome-contact/ })).toBeInTheDocument();
    await waitFor(() => expect((screen.getByLabelText("Workflow JavaScript") as HTMLTextAreaElement).value).toContain(
      'info.instance("review_echo").exec'
    ));
    await userEvent.click(await findEnabledButton("Workflow nodes"));
    expect(await findDialog("Workflow node catalog")).toBeInTheDocument();
    expect(screen.getAllByText("dingtalk.robot.send").length).toBeGreaterThan(0);
    expect(screen.getAllByText("DingTalk robot").length).toBeGreaterThan(0);
    expect(screen.getByText(/DingTalk custom robot access token/)).toBeInTheDocument();
    await userEvent.click(await screen.findByRole("button", { name: /table\.record\.changed/ }));
    expect(screen.getByText("Record changed")).toBeInTheDocument();
    expect(screen.getByText(/run\(info\)\.inputs/)).toBeInTheDocument();
    await userEvent.keyboard("{Escape}");
    await waitFor(() => expect(screen.queryByRole("dialog", { name: "Workflow node catalog" })).not.toBeInTheDocument());
    await act(async () => {
      await i18n.changeLanguage("zh-CN");
    });
    await userEvent.click(await findEnabledButton("工作流节点"));
    expect(await findDialog("工作流节点目录")).toBeInTheDocument();
    await userEvent.click(await screen.findByRole("button", { name: "dingtalk.robot.send" }));
    expect(screen.getByText("钉钉机器人")).toBeInTheDocument();
    expect(screen.getByText(/钉钉自定义机器人的 access token/)).toBeInTheDocument();
    await userEvent.keyboard("{Escape}");
    await waitFor(() => expect(screen.queryByRole("dialog", { name: "工作流节点目录" })).not.toBeInTheDocument());
    await act(async () => {
      await i18n.changeLanguage("en-US");
    });
    await userEvent.click(await findEnabledButton("Edit config review_echo"));
    expect(screen.getByLabelText("Variable review_echo.CHANNEL")).toHaveValue("ops");
    expect(screen.getByLabelText("Secret review_echo.TOKEN")).toHaveValue("x".repeat(12));
    expect(screen.queryByText(/Saved secret length/)).not.toBeInTheDocument();
    await userEvent.clear(screen.getByLabelText("Variable review_echo.CHANNEL"));
    fireEvent.change(screen.getByLabelText("Variable review_echo.CHANNEL"), { target: { value: "support" } });
    expect(screen.getByLabelText("Variable review_echo.CHANNEL")).toHaveValue("support");
    await userEvent.click(await findEnabledButton("Save config"));
    expect(screen.getAllByText("echo").length).toBeGreaterThan(0);
    expect(screen.getAllByText("review_echo").length).toBeGreaterThan(0);
    await userEvent.click(await screen.findByRole("tab", { name: "History" }));
    expect(screen.getAllByText("No runs yet").length).toBeGreaterThan(0);
    await userEvent.click(await screen.findByRole("tab", { name: "Editor" }));
    vi.useFakeTimers();
    fireEvent.change(screen.getByLabelText("Workflow JavaScript"), {
      target: {
        value:
          "function instances(info) { return { ding: { node: 'dingtalk.robot.send' } }; }\nfunction run(info) { return info.instance('ding').exec({ content: 'hello' }); }"
      }
    });
    await act(async () => {
      vi.advanceTimersByTime(5000);
    });
    vi.useRealTimers();
    await userEvent.click(await findEnabledButton("Edit config ding"));
    expect(screen.getByLabelText("Secret ding.access_token")).toBeInTheDocument();
  }, 15_000);

  it("binds workflow instances to remote runners from the config popover", async () => {
    const saveBodies: Array<Record<string, unknown>> = [];
    vi.mocked(fetch).mockImplementation(async (input, init) => {
      const url = String(input);
      if (url === "/api/databases/workspace/workflows" && init?.method === "POST") {
        saveBodies.push(JSON.parse(String(init.body)) as Record<string, unknown>);
      }
      return defaultFetch(input, init);
    });

    renderApp();
    await waitForDefaultTableReady();
    await userEvent.click(await screen.findByRole("button", { name: /^Workflow$/ }));
    await userEvent.click(await findEnabledButton("Edit config review_echo"));

    const runnerSelect = await screen.findByLabelText("Runner for review_echo");
    expect(runnerSelect).toHaveValue("");
    expect(within(runnerSelect as HTMLElement).getByRole("option", { name: "Server (default)" })).toBeInTheDocument();
    await userEvent.selectOptions(runnerSelect, "intranet");
    await userEvent.click(await findEnabledButton("Save config"));

    await waitFor(() => expect(saveBodies.length).toBeGreaterThan(0));
    expect(saveBodies.at(-1)?.runners).toEqual({ review_echo: "intranet" });
    expect(await screen.findByText("runner intranet")).toBeInTheDocument();
  }, 15_000);

  it("manages the database's remote runners from the workflow sidebar", async () => {
    renderApp();
    await waitForDefaultTableReady();
    await userEvent.click(await screen.findByRole("button", { name: /^Workflow$/ }));
    await userEvent.click(await screen.findByRole("button", { name: "Remote runners" }));

    const dialog = await findDialog("Remote runners");
    expect(within(dialog).getByText(/Remote runners · workspace/)).toBeInTheDocument();
    expect(within(dialog).getByText("intranet")).toBeInTheDocument();
    expect(within(dialog).getByText(/v1\.0\.0/)).toBeInTheDocument();
    expect(within(dialog).queryByText(/atr_/)).not.toBeInTheDocument();

    await userEvent.click(within(dialog).getByRole("button", { name: "Reset token" }));
    expect(await within(dialog).findByText("atr_fresh-runner-token")).toBeInTheDocument();
    expect(within(dialog).getByText(/will not be shown again/)).toBeInTheDocument();
  }, 15_000);

  it("loads persisted workflow runs and renders their flow", async () => {
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
      if (url === "/api/workflows/1/runs") {
        return new Response(
          JSON.stringify([
            {
              history_key: "whistory_00000000000000000001_00000000000000000100",
              run: {
                workflow_id: 1,
                timestamp: 1781604000000,
                inputs: { name: "Ada" },
                outputs: { message: "Ada" },
                steps: [
                  { node_id: "echo", node_type: "echo", runner: "intranet", input: { value: "Ada" }, output: { value: "Ada" } }
                ]
              }
            }
          ]),
          { status: 200 }
        );
      }
      return defaultFetch(input, init);
    });

    renderApp("/databases/workspace/workflows/1/history");
    await waitForSignedIn();
    expect(await screen.findByRole("button", { name: /^Workflow$/ })).toBeInTheDocument();
    expect(await screen.findByRole("button", { name: /record-review/ })).toBeInTheDocument();
    expect(await screen.findByRole("tab", { name: "History", selected: true })).toBeInTheDocument();
    await waitFor(() => expect(screen.getByRole("button", { name: "Workflow run history" })).toBeInTheDocument());
    expect(screen.getByRole("button", { name: "Workflow run history" })).toHaveTextContent(
      new Date(1781604000000).toLocaleString()
    );
    expect(window.location.pathname).toBe("/databases/workspace/workflows/1/history");
    expect(screen.queryByText("whistory_00000000000000000001_00000000000000000100")).not.toBeInTheDocument();
    expect(screen.queryByText("No runs yet")).not.toBeInTheDocument();
    expect(screen.getAllByText("Run input").length).toBeGreaterThan(0);
    expect(screen.getAllByText("Run output").length).toBeGreaterThan(0);
    expect(screen.getByText(/"name": "Ada"/)).toBeInTheDocument();
    expect(screen.getByText("echo @ intranet")).toBeInTheDocument();
  });

  it("renders read-only workflow and form resources as non-editable", async () => {
    vi.mocked(fetch).mockImplementation(async (input, init) => {
      const url = String(input);
      if (url === "/api/databases/workspace/workflows") {
        return jsonResponse([{ ...workflowFixture[0], permission_level: 1 }]);
      }
      if (url === "/api/databases/workspace/forms") {
        return jsonResponse([{ ...formFixture[0], permission_level: 1 }]);
      }
      return defaultFetch(input, init);
    });

    renderApp();
    await waitForDefaultTableReady();
    await userEvent.click(await screen.findByRole("button", { name: /^Workflow$/ }));
    await screen.findByRole("button", { name: /record-review/ });
    expect(await screen.findByRole("button", { name: "Save" })).toBeDisabled();
    expect(await screen.findByRole("button", { name: "Run" })).toBeDisabled();
    expect(screen.getByLabelText("Workflow JavaScript")).toBeDisabled();
    expect(screen.getByRole("button", { name: "Edit config review_echo" })).toBeDisabled();

    await userEvent.click(await screen.findByRole("button", { name: /^Form$/ }));
    await screen.findByRole("button", { name: /contact-intake/ });
    expect(await screen.findByRole("button", { name: "Save" })).toBeDisabled();
    expect(screen.getByLabelText("Form JavaScript")).toBeDisabled();
    expect(screen.getByRole("button", { name: "Create record" })).not.toBeDisabled();
  });

  it("runs workflows with only the explicit inputs JSON", async () => {
    let runBody: unknown;
    let runSaved = false;
    const savedRun = {
      history_key: "whistory_00000000000000000001_00000000000000000101",
      run: {
        workflow_id: 1,
        timestamp: 1781604000000,
        inputs: {},
        outputs: {},
        steps: []
      }
    };
    vi.mocked(fetch).mockImplementation(async (input, init) => {
      const url = String(input);
      if (url === "/api/workflows/1/runs" && init?.method === "POST") {
        runBody = JSON.parse(String(init.body));
        runSaved = true;
        return jsonResponse(savedRun, 201);
      }
      if (url === "/api/workflows/1/runs") {
        return jsonResponse(runSaved ? [savedRun] : []);
      }
      return defaultFetch(input, init);
    });

    renderApp();
    await waitForDefaultTableReady();
    await userEvent.click(await screen.findByRole("button", { name: /^Workflow$/ }));
    await screen.findByRole("button", { name: /record-review/ });
    await userEvent.click(await findEnabledButton("Run"));
    await waitFor(() => expect(runBody).toEqual({ inputs: {} }));
    await waitFor(() =>
      expect(window.location.pathname).toBe(
        "/databases/workspace/workflows/1/history/whistory_00000000000000000001_00000000000000000101"
      )
    );
    expect(await screen.findByRole("tab", { name: "History", selected: true })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Workflow run history" })).toHaveTextContent(
      new Date(1781604000000).toLocaleString()
    );
  });

  it("shows form JavaScript and preview controls", async () => {
    renderApp();
    await waitForDefaultTableReady();
    await userEvent.click(await screen.findByRole("button", { name: /^Form$/ }));
    expect(await screen.findByRole("button", { name: /quick-status/ })).toBeInTheDocument();
    await waitFor(() => expect((screen.getByLabelText("Form JavaScript") as HTMLTextAreaElement).value).toContain("root.append"));
    await userEvent.type(screen.getByRole("textbox", { name: "Name" }), "Margaret Hamilton");
    expect(screen.getByRole("button", { name: "Create record" })).toBeInTheDocument();
    await userEvent.click(await findEnabledButton("Create record"));
    await userEvent.click(await screen.findByRole("button", { name: /^Table$/ }));
    await waitFor(() => expect(screen.getAllByText("4 of 4 records").length).toBeGreaterThan(0));

    await userEvent.click(await screen.findByRole("button", { name: /^Form$/ }));
    await userEvent.click(await screen.findByRole("button", { name: /quick-status/ }));
    expect(screen.getByRole("button", { name: "Update status" })).toBeInTheDocument();
  });

  it("submits forms to the table declared by the render definition", async () => {
    let submittedURL = "";
    vi.mocked(fetch).mockImplementation(async (input, init) => {
      const url = String(input);
      if (url === "/api/auth/me") {
        return jsonResponse(authUserFixture);
      }
      if (url === "/api/auth/config") {
        return jsonResponse({ password_enabled: true, oidc_enabled: false, oidc_providers: [] });
      }
      if (url === "/api/metadata") {
        return jsonResponse({
          databases: [
            {
              name: "workspace",
              tables: [
                {
                  name: "projects",
                  display_name: "Projects",
                  fields: [{ name: "name", type: "string", deleted: false }],
                  views: []
                },
                {
                  name: "contacts",
                  display_name: "Contacts",
                  fields: [{ name: "name", type: "string", deleted: false }],
                  views: []
                }
              ]
            }
          ]
        });
      }
      if (url === "/api/tables/workspace/projects/rows") {
        return jsonResponse([]);
      }
      if (url === "/api/tables/workspace/contacts/rows" && init?.method === "POST") {
        submittedURL = url;
        return jsonResponse({ record_id: 9, values: JSON.parse(String(init.body)).values }, 201);
      }
      if (url === "/api/databases/workspace/workflows") {
        return jsonResponse([]);
      }
      if (url === "/api/databases/workspace/forms") {
        return jsonResponse([
          {
            id: 9,
            database_name: "workspace",
            name: "targeted-contact",
            script: "function render(api, root) { root.append(api.input({ field: 'name', label: 'Name' }), api.submit('Create contact')); return { table: 'contacts' }; }"
          }
        ]);
      }
      if (url === "/api/databases/workspace/roles") {
        return jsonResponse([]);
      }
      if (url === "/api/workflow/nodes") {
        return jsonResponse(workflowNodeFixture);
      }
      return jsonResponse({ error: `unhandled ${url}` }, 404);
    });

    renderApp();
    await waitForSignedIn();
    await userEvent.click(await screen.findByRole("button", { name: /^Form$/ }));
    await screen.findByRole("button", { name: /targeted-contact/ });
    await userEvent.type(screen.getByRole("textbox", { name: "Name" }), "Ada");
    await userEvent.click(await findEnabledButton("Create contact"));

    await waitFor(() => expect(submittedURL).toBe("/api/tables/workspace/contacts/rows"));
    expect(screen.getAllByText("Form created contacts record 9").length).toBeGreaterThan(0);
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
              permission_level: 0,
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
    expect(await screen.findByRole("button", { name: "Mine" })).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "All records" })).not.toBeInTheDocument();
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
});
