import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { WorkflowDefinition, WorkflowDetail } from "../api";
import type { WorkflowTab } from "../appState";
import i18n from "../i18n";
import { NavigationProvider } from "../shell/navigation";
import { TooltipProvider } from "../ui";
import { WorkflowWorkspace, type WorkflowWorkspaceProps } from "./WorkflowWorkspace";

const workflow: WorkflowDefinition = {
  id: 4,
  database_name: "ops",
  name: "order-sync",
  title: "Order sync",
  enabled: true,
  published_version: 2,
  latest_version: 3,
  timeout_seconds: 600,
  history_retention_days: 30,
  triggers: [
    { name: "hook", kind: "webhook", path: "approval", url: "/api/hooks/ops/order-sync/approval" },
    { name: "nightly", kind: "schedule", cron: "0 2 * * *", timezone: "UTC" },
    { name: "start", kind: "manual", fields: [{ name: "orderId", label: "Order", type: "number" }] }
  ]
};

const detail: WorkflowDetail = {
  workflow,
  versions: [1, 2, 3].map((version) => ({
    workflow_id: 4,
    version,
    sdk: version === 3 ? "9.0" : "1.0",
    message: `change ${version}`,
    size: 1000 * version,
    created_by: "u1",
    created_at: 1781600000000 + version,
    published: version === 2,
    supported: version !== 3,
    sdk_error: version === 3 ? "workflow SDK 9.0 is newer than this server's SDK 1.0" : undefined
  })),
  secrets: ["API_TOKEN"],
  vars: { REGION: "eu" },
  sdk: { version: "1.0", min_major: 1 }
};

function handlers() {
  return {
    onDelete: vi.fn(async () => true),
    onPublish: vi.fn(async () => true),
    onRemoveSecret: vi.fn(async () => true),
    onRemoveVar: vi.fn(async () => true),
    onRunStarted: vi.fn(),
    onSaveSecret: vi.fn(async () => true),
    onSaveSettings: vi.fn(async () => true),
    onSaveVar: vi.fn(async () => true),
    onSetEnabled: vi.fn(async () => true)
  };
}

function renderWorkspace(tab: WorkflowTab, extra: Partial<WorkflowWorkspaceProps> = {}) {
  const props: WorkflowWorkspaceProps = {
    busy: false,
    currentUser: { id: "u1", email: "admin@example.com", display_name: "Alex", provider: "password", is_admin: true },
    databaseName: "ops",
    detail,
    workflows: [workflow],
    workflowsReady: true,
    workflowName: "order-sync",
    tab,
    ...handlers(),
    ...extra
  };
  const navigate = vi.fn();
  render(
    <NavigationProvider navigate={navigate}>
      <TooltipProvider>
        <WorkflowWorkspace {...props} />
      </TooltipProvider>
    </NavigationProvider>
  );
  return { props, navigate };
}

beforeEach(async () => {
  vi.restoreAllMocks();
  await i18n.changeLanguage("en-US");
});

describe("WorkflowWorkspace", () => {
  it("shows the empty state with the command line loop", () => {
    renderWorkspace("overview", { workflowName: "", workflows: [], detail: null });
    expect(screen.getByText("No workflows yet")).toBeInTheDocument();
    expect(screen.getByText("autablectl workflow init my-workflow --database ops --name my-workflow")).toBeInTheDocument();
    expect(screen.getByText(/autablectl workflow push --publish/)).toBeInTheDocument();
    expect(screen.getByText("autablectl skill install")).toBeInTheDocument();
    expect(screen.getByText("autablectl skill export ./autable-skill")).toBeInTheDocument();
  });

  it("details triggers with a copyable webhook URL and edits the settings", async () => {
    const { props } = renderWorkspace("overview");
    const hook = screen.getByRole("group", { name: "Trigger hook" });
    const address = `${window.location.origin}/api/hooks/ops/order-sync/approval`;
    expect(within(hook).getByText(address)).toBeInTheDocument();
    const user = userEvent.setup();
    await user.click(within(hook).getByRole("button", { name: "Copy webhook URL" }));
    // user-event installs a clipboard stub.
    expect(await navigator.clipboard.readText()).toBe(address);
    expect(within(screen.getByRole("group", { name: "Trigger nightly" })).getByText("Every day 02:00")).toBeInTheDocument();

    await user.click(screen.getByRole("switch", { name: "Enabled" }));
    expect(props.onSetEnabled).toHaveBeenCalledWith(false);

    const settings = screen.getByRole("form", { name: "Settings" });
    const timeout = within(settings).getByLabelText("Run time limit (seconds)");
    await user.clear(timeout);
    await user.type(timeout, "0");
    await user.click(within(settings).getByRole("button", { name: "Save settings" }));
    expect(within(settings).getByText("Enter whole seconds from 1 to 86400.")).toBeInTheDocument();
    await user.clear(timeout);
    await user.type(timeout, "120");
    await user.click(within(settings).getByRole("button", { name: "Save settings" }));
    expect(props.onSaveSettings).toHaveBeenCalledWith({ timeout_seconds: 120, history_retention_days: 30 });
  });

  it("publishes newer versions and confirms rollbacks", async () => {
    const { props } = renderWorkspace("versions");
    const versions = screen.getByRole("table", { name: "Versions" });
    const v3 = within(versions).getByRole("row", { name: "v3" });
    expect(within(v3).getByText("Unsupported")).toBeInTheDocument();
    expect(within(v3).getByRole("button", { name: "Publish version 3" })).toBeDisabled();
    expect(within(versions).getByRole("row", { name: "v2" })).toHaveTextContent("Published");
    expect(within(versions).getByRole("link", { name: "Download source of version 1" })).toHaveAttribute(
      "href",
      "/api/databases/ops/workflows/order-sync/versions/1/source"
    );
    const user = userEvent.setup();
    await user.click(within(versions).getByRole("button", { name: "Roll back to version 1" }));
    const dialog = screen.getByRole("dialog", { name: "Roll back workflow" });
    expect(dialog).toHaveTextContent("New runs currently use version 2. Roll back to version 1?");
    await user.click(within(dialog).getByRole("button", { name: "Roll back" }));
    expect(props.onPublish).toHaveBeenCalledWith(1);
  });

  it("sets secrets without showing values and edits vars", async () => {
    const { props } = renderWorkspace("config");
    const user = userEvent.setup();
    const secrets = screen.getByRole("list", { name: "Secrets" });
    expect(within(secrets).getByText("API_TOKEN")).toBeInTheDocument();
    await user.click(within(secrets).getByRole("button", { name: "Replace secret API_TOKEN" }));
    const form = screen.getByRole("form", { name: "Set secret" });
    expect(within(form).getByLabelText("Secret name")).toHaveValue("API_TOKEN");
    const value = within(form).getByLabelText("Secret value");
    expect(value).toHaveAttribute("type", "password");
    await user.type(value, "n3w");
    await user.click(within(form).getByRole("button", { name: "Replace secret" }));
    expect(props.onSaveSecret).toHaveBeenCalledWith("API_TOKEN", "n3w");

    await user.click(within(secrets).getByRole("button", { name: "Delete secret API_TOKEN" }));
    await user.click(within(screen.getByRole("dialog", { name: "Delete secret" })).getByRole("button", { name: "Delete" }));
    expect(props.onRemoveSecret).toHaveBeenCalledWith("API_TOKEN");

    const region = screen.getByLabelText("Value of REGION");
    await user.clear(region);
    await user.type(region, "us");
    await user.click(screen.getByRole("button", { name: "Save REGION" }));
    expect(props.onSaveVar).toHaveBeenCalledWith("REGION", "us");

    const addVar = screen.getByRole("form", { name: "Add var" });
    await user.type(within(addVar).getByLabelText("Var name"), "1bad");
    await user.click(within(addVar).getByRole("button", { name: "Add var" }));
    expect(within(addVar).getByText(/starting with a letter or _/)).toBeInTheDocument();
  });

  it("lists runs, opens one and cancels it while it runs", async () => {
    const calls: string[] = [];
    let cancelled = false;
    vi.spyOn(globalThis, "fetch").mockImplementation(async (input, init) => {
      const url = String(input);
      calls.push(`${init?.method ?? "GET"} ${url}`);
      const status = cancelled ? "cancelled" : "running";
      if (url.startsWith("/api/databases/ops/workflows/order-sync/runs?")) {
        return new Response(JSON.stringify([{ id: 8, workflow_id: 4, trigger: "start", status, created_at: 1, started_at: 2 }]));
      }
      if (url.endsWith("/runs/8/cancel")) {
        cancelled = true;
        return new Response(JSON.stringify({ id: 8, workflow_id: 4, trigger: "start", status: "cancelled", created_at: 1, logs: [] }));
      }
      return new Response(
        JSON.stringify({
          id: 8,
          workflow_id: 4,
          version: 2,
          trigger: "start",
          status,
          event: { input: { orderId: 3 } },
          created_at: 1,
          started_at: 2,
          logs: [{ time: 3, level: "error", message: "lookup failed", data: { code: 500 } }]
        })
      );
    });
    const { navigate } = renderWorkspace("runs", { runID: 8 });
    const user = userEvent.setup();
    await user.click(await screen.findByRole("button", { name: "Run 8, Running" }));
    expect(navigate).toHaveBeenCalledWith(expect.objectContaining({ workflowTab: "runs", workflowRunID: 8 }));
    const run = await screen.findByRole("region", { name: "Run #8" });
    expect(within(run).getByText("lookup failed")).toBeInTheDocument();
    expect(within(run).getByText("error")).toBeInTheDocument();
    expect(within(run).getByLabelText("Trigger event")).toHaveTextContent('"orderId": 3');
    await user.click(within(run).getByRole("button", { name: "Cancel run" }));
    await waitFor(() => expect(within(run).getByText("Cancelled")).toBeInTheDocument());
    expect(calls).toContain("POST /api/databases/ops/workflows/order-sync/runs/8/cancel");

    await user.selectOptions(screen.getByLabelText("Filter runs by status"), "failed");
    await waitFor(() => expect(calls.at(-1)).toBe("GET /api/databases/ops/workflows/order-sync/runs?status=failed&limit=25"));
  });

  it("starts the manual trigger from the page header", async () => {
    vi.spyOn(globalThis, "fetch").mockImplementation(
      async () => new Response(JSON.stringify({ id: 9, workflow_id: 4, trigger: "start", status: "succeeded", output: { ok: true }, created_at: 1 }))
    );
    const { props } = renderWorkspace("overview");
    const user = userEvent.setup();
    const header = screen.getAllByRole("button", { name: "Run" });
    await user.click(header[header.length - 1]);
    const dialog = screen.getByRole("dialog", { name: "Run Order sync" });
    await user.type(within(dialog).getByLabelText("Order"), "11");
    await user.click(within(dialog).getByRole("button", { name: "Start run" }));
    expect(await within(dialog).findByLabelText("Output")).toHaveTextContent('"ok": true');
    expect(props.onRunStarted).toHaveBeenCalled();
  });
});
