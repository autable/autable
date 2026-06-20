import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { FluentProvider, webLightTheme } from "@fluentui/react-components";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { App } from "./App";
import i18n from "./i18n";

const catalogFixture = {
  databases: [
    {
      name: "workspace",
      sqlite_path: "./data/workspace.sqlite",
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
  if (url === "/api/auth/oidc/providers") {
    return jsonResponse([]);
  }
  if (url === "/api/metadata") {
    return jsonResponse(catalogFixture);
  }
  if (url === "/api/tables/workspace/contacts/rows" && init?.method === "POST") {
    return jsonResponse({ record_id: 4, values: JSON.parse(String(init.body)).values }, 201);
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
  if (url === "/api/workflows/1/runs") {
    return jsonResponse([]);
  }
  return jsonResponse({ error: `unhandled ${url}` }, 404);
}

beforeEach(async () => {
  vi.useRealTimers();
  vi.restoreAllMocks();
  window.localStorage.clear();
  await i18n.changeLanguage("en-US");
  vi.spyOn(globalThis, "fetch").mockImplementation(defaultFetch);
});

function renderApp() {
  return render(
    <FluentProvider theme={webLightTheme}>
      <App />
    </FluentProvider>
  );
}

describe("App", () => {
  it("renders table view first", async () => {
    renderApp();
    expect(await screen.findByRole("button", { name: authUserFixture.email })).toBeInTheDocument();
    expect(await screen.findByRole("button", { name: /^Table$/ })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /Contacts/ })).toBeInTheDocument();
    await waitFor(() => expect(screen.getAllByText("3 of 3 records").length).toBeGreaterThan(0));
  });

  it("requests temporary table sorting from the rows API", async () => {
    const requests: string[] = [];
    vi.mocked(fetch).mockImplementation(async (input, init) => {
      requests.push(String(input));
      return defaultFetch(input, init);
    });

    const user = userEvent.setup();
    renderApp();
    const sortButton = await screen.findByRole("button", { name: "Toggle name sort" });

    await user.click(sortButton);
    await waitFor(() =>
      expect(requests).toContain("/api/tables/workspace/contacts/rows?sort_field=name&sort_direction=desc")
    );

    await user.click(sortButton);
    await waitFor(() =>
      expect(requests).toContain("/api/tables/workspace/contacts/rows?sort_field=name&sort_direction=asc")
    );

    await user.click(sortButton);
    await waitFor(() => {
      const rowRequests = requests.filter((url) => url.startsWith("/api/tables/workspace/contacts/rows"));
      expect(rowRequests.at(-1)).toBe("/api/tables/workspace/contacts/rows");
    });
  });

  it("does not load protected workspace resources before authentication", async () => {
    const requests: string[] = [];
    vi.mocked(fetch).mockImplementation(async (input) => {
      const url = String(input);
      requests.push(url);
      if (url === "/api/auth/me") {
        return jsonResponse({ error: "not authenticated" }, 401);
      }
      if (url === "/api/auth/oidc/providers") {
        return jsonResponse([]);
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
      if (url === "/api/auth/oidc/providers") {
        return new Response(
          JSON.stringify([{ name: "example", issuer_url: "https://accounts.example.com", scopes: ["openid"] }]),
          { status: 200 }
        );
      }
      return defaultFetch(input);
    });

    renderApp();
    await userEvent.click(screen.getByRole("button", { name: "Login" }));
    expect(await screen.findByRole("button", { name: "Continue with example" })).toBeInTheDocument();
  });

  it("loads table rows from the backend when available", async () => {
    vi.mocked(fetch).mockImplementation(async (input, init) => {
      const url = String(input);
      if (url === "/api/auth/me") {
        return jsonResponse(authUserFixture);
      }
      if (url === "/api/auth/oidc/providers") {
        return new Response(JSON.stringify([]), { status: 200 });
      }
      if (url === "/api/tables/workspace/contacts/rows") {
        return new Response(
          JSON.stringify([{ record_id: 42, values: { name: "Backend Row", email: "backend@example.com" } }]),
          { status: 200 }
        );
      }
      return defaultFetch(input, init);
    });

    renderApp();
    await waitFor(() => expect(screen.getAllByText("1 of 1 records").length).toBeGreaterThan(0));
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
    await screen.findByRole("button", { name: authUserFixture.email });
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
    await screen.findByRole("button", { name: authUserFixture.email });
    await userEvent.click(screen.getByRole("button", { name: "Collapse sidebar" }));

    expect(screen.queryByRole("button", { name: "Permission" })).not.toBeInTheDocument();
  });

  it("disables row creation outside all records", async () => {
    renderApp();
    await screen.findByRole("button", { name: authUserFixture.email });
    await userEvent.click(await screen.findByRole("button", { name: "Active" }));

    expect(screen.getByRole("button", { name: "Row" })).toBeDisabled();
  });

  it("creates a database from the sidebar and selects it", async () => {
    vi.mocked(fetch).mockImplementation(async (input, init) => {
      const url = String(input);
      if (url === "/api/auth/me") {
        return jsonResponse(authUserFixture);
      }
      if (url === "/api/auth/oidc/providers") {
        return new Response(JSON.stringify([]), { status: 200 });
      }
      if (url.startsWith("/api/tables/workspace/contacts/rows")) {
        return new Response(JSON.stringify({ error: "permission denied" }), { status: 403 });
      }
      if (url === "/api/databases" && init?.method === "POST") {
        return new Response(JSON.stringify({ name: "sales", sqlite_path: "./data/sales.sqlite", tables: [] }), {
          status: 201
        });
      }
      if (url === "/api/metadata") {
        return new Response(
          JSON.stringify({
            databases: [
              {
                name: "sales",
                sqlite_path: "./data/sales.sqlite",
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
    await screen.findByRole("button", { name: authUserFixture.email });
    await waitFor(() => expect(screen.getByRole("button", { name: "Create DB" })).toBeEnabled());
    await userEvent.click(screen.getByRole("button", { name: "Create DB" }));
    await userEvent.type(screen.getByRole("textbox", { name: "New database name" }), "sales");
    await userEvent.click(screen.getByRole("button", { name: "Save" }));
    await waitFor(() => expect(screen.getByRole("button", { name: "sales" })).toHaveAttribute("aria-expanded", "true"));
    expect(screen.getByText("Created database sales")).toBeInTheDocument();
  });

  it("creates a table in the selected database", async () => {
    vi.mocked(fetch).mockImplementation(async (input, init) => {
      const url = String(input);
      if (url === "/api/auth/me") {
        return jsonResponse(authUserFixture);
      }
      if (url === "/api/auth/oidc/providers") {
        return new Response(JSON.stringify([]), { status: 200 });
      }
      if (url.startsWith("/api/tables/workspace/contacts/rows")) {
        return new Response(JSON.stringify({ error: "permission denied" }), { status: 403 });
      }
      if (url === "/api/tables/workspace/projects/rows") {
        return new Response(JSON.stringify([]), { status: 200 });
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
                sqlite_path: "./data/workspace.sqlite",
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

    renderApp();
    await screen.findByRole("button", { name: authUserFixture.email });
    await userEvent.click(screen.getByRole("button", { name: "Create Table" }));
    await userEvent.type(screen.getByRole("textbox", { name: "New table name" }), "projects");
    await userEvent.click(screen.getByRole("button", { name: "Save" }));
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
    await screen.findByRole("button", { name: authUserFixture.email });
    await userEvent.click(screen.getByRole("button", { name: "Create View" }));
    await userEvent.type(screen.getByRole("textbox", { name: "New view name" }), "Needs Review");
    await userEvent.click(screen.getByRole("button", { name: "Save" }));

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
      if (url === "/api/auth/oidc/providers") {
        return new Response(JSON.stringify([]), { status: 200 });
      }
      if (url === "/api/tables/workspace/contacts/rows") {
        return new Response(
          JSON.stringify([{ record_id: 42, values: { name: "Backend Row", email: "backend@example.com" } }]),
          { status: 200 }
        );
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
    await waitFor(() => expect(screen.getAllByText("1 of 1 records").length).toBeGreaterThan(0));
    await userEvent.click(screen.getByRole("button", { name: "History" }));
    expect(await screen.findByRole("tab", { name: "History", selected: true })).toBeInTheDocument();
    expect(screen.getByText("Record change")).toBeInTheDocument();
    expect(screen.queryByText("rhistory_workspace_contacts_00000000000000000042_00000000000000000100")).not.toBeInTheDocument();
    expect(screen.getAllByText(/Backend Row/).length).toBeGreaterThan(0);
  });

  it("shows workflow JavaScript as the workflow view", async () => {
    renderApp();
    await userEvent.click(await screen.findByRole("button", { name: /^Workflow$/ }));
    expect(screen.getByRole("button", { name: /welcome-contact/ })).toBeInTheDocument();
    expect((screen.getByLabelText("Workflow JavaScript") as HTMLTextAreaElement).value).toContain(
      'info.instance("review_echo").exec'
    );
    await userEvent.click(screen.getByRole("button", { name: "Workflow nodes" }));
    expect(screen.getByRole("dialog", { name: "Workflow node catalog" })).toBeInTheDocument();
    expect(screen.getAllByText("dingtalk.robot.send").length).toBeGreaterThan(0);
    expect(screen.getAllByText("DingTalk robot").length).toBeGreaterThan(0);
    expect(screen.getByText(/DingTalk custom robot access token/)).toBeInTheDocument();
    await userEvent.click(screen.getByRole("button", { name: /table\.record\.changed/ }));
    expect(screen.getByText("Record changed")).toBeInTheDocument();
    expect(screen.getByText(/run\(info\)\.inputs/)).toBeInTheDocument();
    await userEvent.keyboard("{Escape}");
    await act(async () => {
      await i18n.changeLanguage("zh-CN");
    });
    await userEvent.click(screen.getByRole("button", { name: "工作流节点" }));
    expect(await screen.findByRole("dialog", { name: "工作流节点目录" })).toBeInTheDocument();
    await userEvent.click(await screen.findByRole("button", { name: "dingtalk.robot.send" }));
    expect(screen.getByText("钉钉机器人")).toBeInTheDocument();
    expect(screen.getByText(/钉钉自定义机器人的 access token/)).toBeInTheDocument();
    await userEvent.keyboard("{Escape}");
    await act(async () => {
      await i18n.changeLanguage("en-US");
    });
    await userEvent.click(screen.getByRole("button", { name: "Edit config review_echo" }));
    expect(screen.getByLabelText("Variable review_echo.CHANNEL")).toHaveValue("ops");
    expect(screen.getByLabelText("Secret review_echo.TOKEN")).toHaveValue("x".repeat(12));
    expect(screen.queryByText(/Saved secret length/)).not.toBeInTheDocument();
    await userEvent.clear(screen.getByLabelText("Variable review_echo.CHANNEL"));
    fireEvent.change(screen.getByLabelText("Variable review_echo.CHANNEL"), { target: { value: "support" } });
    expect(screen.getByLabelText("Variable review_echo.CHANNEL")).toHaveValue("support");
    await userEvent.click(screen.getByRole("button", { name: "Save config" }));
    expect(screen.getAllByText("echo").length).toBeGreaterThan(0);
    expect(screen.getAllByText("review_echo").length).toBeGreaterThan(0);
    await userEvent.click(screen.getByRole("tab", { name: "History" }));
    expect(screen.getAllByText("No runs yet").length).toBeGreaterThan(0);
    await userEvent.click(screen.getByRole("tab", { name: "Editor" }));
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
    await userEvent.click(screen.getByRole("button", { name: "Edit config ding" }));
    expect(screen.getByLabelText("Secret ding.access_token")).toBeInTheDocument();
  });

  it("loads persisted workflow runs and renders their flow", async () => {
    vi.mocked(fetch).mockImplementation(async (input, init) => {
      const url = String(input);
      if (url === "/api/auth/me") {
        return jsonResponse(authUserFixture);
      }
      if (url === "/api/auth/oidc/providers") {
        return new Response(JSON.stringify([]), { status: 200 });
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
                steps: [{ node_id: "echo", input: { value: "Ada" }, output: { value: "Ada" } }]
              }
            }
          ]),
          { status: 200 }
        );
      }
      return defaultFetch(input, init);
    });

    renderApp();
    await userEvent.click(await screen.findByRole("button", { name: /^Workflow$/ }));
    await userEvent.click(screen.getByRole("tab", { name: "History" }));
    await waitFor(() => expect(screen.getByRole("button", { name: "Workflow run history" })).toBeInTheDocument());
    expect(screen.getByRole("button", { name: "Workflow run history" })).toHaveTextContent(
      new Date(1781604000000).toLocaleString()
    );
    expect(screen.queryByText("whistory_00000000000000000001_00000000000000000100")).not.toBeInTheDocument();
    expect(screen.queryByText("No runs yet")).not.toBeInTheDocument();
    expect(screen.getAllByText("Run input").length).toBeGreaterThan(0);
    expect(screen.getAllByText("Run output").length).toBeGreaterThan(0);
    expect(screen.getByText(/"name": "Ada"/)).toBeInTheDocument();
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
    await userEvent.click(await screen.findByRole("button", { name: /^Workflow$/ }));
    expect(screen.getByRole("button", { name: "Save" })).toBeDisabled();
    expect(screen.getByRole("button", { name: "Run" })).toBeDisabled();
    expect(screen.getByLabelText("Workflow JavaScript")).toBeDisabled();
    expect(screen.getByRole("button", { name: "Edit config review_echo" })).toBeDisabled();

    await userEvent.click(screen.getByRole("button", { name: /^Form$/ }));
    expect(screen.getByRole("button", { name: "Save" })).toBeDisabled();
    expect(screen.getByLabelText("Form JavaScript")).toBeDisabled();
    expect(screen.getByRole("button", { name: "Create record" })).not.toBeDisabled();
  });

  it("runs workflows with only the explicit inputs JSON", async () => {
    let runBody: unknown;
    vi.mocked(fetch).mockImplementation(async (input, init) => {
      const url = String(input);
      if (url === "/api/workflows/1/runs" && init?.method === "POST") {
        runBody = JSON.parse(String(init.body));
        return new Response(
          JSON.stringify({
            history_key: "whistory_00000000000000000001_00000000000000000101",
            run: {
              workflow_id: 1,
              timestamp: 1781604000000,
              inputs: {},
              outputs: {},
              steps: []
            }
          }),
          { status: 201 }
        );
      }
      return defaultFetch(input, init);
    });

    renderApp();
    await userEvent.click(await screen.findByRole("button", { name: /^Workflow$/ }));
    await userEvent.click(screen.getByRole("button", { name: "Run" }));
    await waitFor(() => expect(runBody).toEqual({ inputs: {} }));
  });

  it("shows form JavaScript and preview controls", async () => {
    renderApp();
    await userEvent.click(await screen.findByRole("button", { name: /^Form$/ }));
    expect(screen.getByRole("button", { name: /quick-status/ })).toBeInTheDocument();
    expect((screen.getByLabelText("Form JavaScript") as HTMLTextAreaElement).value).toContain("root.append");
    await userEvent.type(screen.getByRole("textbox", { name: "Name" }), "Margaret Hamilton");
    expect(screen.getByRole("button", { name: "Create record" })).toBeInTheDocument();
    await userEvent.click(screen.getByRole("button", { name: "Create record" }));
    await userEvent.click(screen.getByRole("button", { name: /^Table$/ }));
    await waitFor(() => expect(screen.getAllByText("4 of 4 records").length).toBeGreaterThan(0));

    await userEvent.click(screen.getByRole("button", { name: /^Form$/ }));
    await userEvent.click(screen.getByRole("button", { name: /quick-status/ }));
    expect(screen.getByRole("button", { name: "Update status" })).toBeInTheDocument();
  });

  it("submits forms to the table declared by the render definition", async () => {
    let submittedURL = "";
    vi.mocked(fetch).mockImplementation(async (input, init) => {
      const url = String(input);
      if (url === "/api/auth/me") {
        return jsonResponse(authUserFixture);
      }
      if (url === "/api/auth/oidc/providers") {
        return jsonResponse([]);
      }
      if (url === "/api/metadata") {
        return jsonResponse({
          databases: [
            {
              name: "workspace",
              sqlite_path: "./data/workspace.sqlite",
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
        return jsonResponse([]);
      }
      return jsonResponse({ error: `unhandled ${url}` }, 404);
    });

    renderApp();
    await userEvent.click(await screen.findByRole("button", { name: /^Form$/ }));
    await userEvent.type(screen.getByRole("textbox", { name: "Name" }), "Ada");
    await userEvent.click(screen.getByRole("button", { name: "Create contact" }));

    await waitFor(() => expect(submittedURL).toBe("/api/tables/workspace/contacts/rows"));
    expect(screen.getByText("Form created contacts record 9")).toBeInTheDocument();
  });
});
