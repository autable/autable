import { act, renderHook, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import "../i18n";
import { useWorkflowWorkspace } from "./useWorkflowWorkspace";

const workflow = {
  id: 1,
  database_name: "ops",
  name: "order-sync",
  title: "Order sync",
  enabled: true,
  published_version: 2,
  latest_version: 2,
  timeout_seconds: 600,
  history_retention_days: 30,
  triggers: []
};

function detail(published = 2, enabled = true) {
  return {
    workflow: { ...workflow, published_version: published, enabled },
    versions: [
      { workflow_id: 1, version: 1, sdk: "1.0", message: "", size: 1, created_by: "u", created_at: 1, published: published === 1, supported: true },
      { workflow_id: 1, version: 2, sdk: "1.0", message: "", size: 1, created_by: "u", created_at: 2, published: published === 2, supported: true }
    ],
    secrets: [],
    vars: {},
    sdk: { version: "1.0", min_major: 1 }
  };
}

function json(body: unknown, status = 200) {
  return new Response(body === undefined ? null : JSON.stringify(body), { status });
}

type Call = { url: string; method: string; body?: unknown };

function mockServer(handler: (call: Call) => Response | undefined) {
  const calls: Call[] = [];
  vi.spyOn(globalThis, "fetch").mockImplementation(async (input, init) => {
    const call = { url: String(input), method: init?.method ?? "GET", body: init?.body ? JSON.parse(String(init.body)) : undefined };
    calls.push(call);
    return handler(call) ?? json({ error: `unhandled ${call.method} ${call.url}` }, 404);
  });
  return calls;
}

beforeEach(() => {
  vi.restoreAllMocks();
});

describe("useWorkflowWorkspace", () => {
  it("loads the list and, for admins, the selected workflow's detail", async () => {
    mockServer(({ url }) => {
      if (url === "/api/databases/ops/workflows") return json([workflow]);
      if (url === "/api/databases/ops/workflows/order-sync") return json(detail());
      return undefined;
    });
    const onStatus = vi.fn();
    const { result } = renderHook(() =>
      useWorkflowWorkspace({ currentUserID: "u", databaseName: "ops", isAdmin: true, workflowName: "order-sync", onStatus })
    );
    await waitFor(() => expect(result.current.detail?.workflow.name).toBe("order-sync"));
    expect(result.current.workflowsReady).toBe(true);
    expect(result.current.workflows).toHaveLength(1);
    expect(onStatus).not.toHaveBeenCalled();
  });

  it("never loads a detail for users who may only run workflows", async () => {
    const calls = mockServer(({ url }) => (url === "/api/databases/ops/workflows" ? json([workflow]) : undefined));
    const { result } = renderHook(() =>
      useWorkflowWorkspace({ currentUserID: "u", databaseName: "ops", isAdmin: false, workflowName: "order-sync", onStatus: vi.fn() })
    );
    await waitFor(() => expect(result.current.workflowsReady).toBe(true));
    expect(result.current.detail).toBeNull();
    expect(calls.map((call) => call.url)).toEqual(["/api/databases/ops/workflows"]);
  });

  it("rolls back, toggles and configures the workflow and reloads it", async () => {
    let current = detail();
    const calls = mockServer(({ url, method, body }) => {
      if (url === "/api/databases/ops/workflows") return json([current.workflow]);
      if (url === "/api/databases/ops/workflows/order-sync" && method === "GET") return json(current);
      if (url === "/api/databases/ops/workflows/order-sync/publish") {
        current = detail((body as { version: number }).version);
        return json(current.workflow);
      }
      if (url === "/api/databases/ops/workflows/order-sync" && method === "PATCH") {
        current = detail(current.workflow.published_version, (body as { enabled?: boolean }).enabled ?? true);
        return json(current.workflow);
      }
      if (url.startsWith("/api/databases/ops/workflows/order-sync/secrets/") || url.startsWith("/api/databases/ops/workflows/order-sync/vars/")) {
        return json(undefined, 204);
      }
      return undefined;
    });
    const onStatus = vi.fn();
    const { result } = renderHook(() =>
      useWorkflowWorkspace({ currentUserID: "u", databaseName: "ops", isAdmin: true, workflowName: "order-sync", onStatus })
    );
    await waitFor(() => expect(result.current.detail).not.toBeNull());

    await act(async () => {
      expect(await result.current.publishVersion(1)).toBe(true);
    });
    expect(result.current.detail?.workflow.published_version).toBe(1);
    expect(onStatus).toHaveBeenLastCalledWith("Rolled back Order sync to version 1", "success");

    await act(async () => {
      await result.current.setEnabled(false);
    });
    expect(calls.find((call) => call.method === "PATCH")?.body).toEqual({ enabled: false });
    expect(result.current.detail?.workflow.enabled).toBe(false);
    expect(onStatus).toHaveBeenLastCalledWith("Disabled Order sync", "success");

    await act(async () => {
      await result.current.saveSecret("API_TOKEN", "s3cret");
      await result.current.removeVar("REGION");
    });
    expect(calls).toEqual(
      expect.arrayContaining([
        { url: "/api/databases/ops/workflows/order-sync/secrets/API_TOKEN", method: "PUT", body: { value: "s3cret" } },
        { url: "/api/databases/ops/workflows/order-sync/vars/REGION", method: "DELETE", body: undefined }
      ])
    );
    expect(onStatus).toHaveBeenLastCalledWith("Deleted var REGION", "success");
  });

  it("reports the server's reason when a change fails", async () => {
    mockServer(({ url, method }) => {
      if (url === "/api/databases/ops/workflows") return json([workflow]);
      if (url === "/api/databases/ops/workflows/order-sync" && method === "GET") return json(detail());
      if (url.endsWith("/publish")) return json({ error: 'trigger "changed": table "orders" does not exist' }, 422);
      return undefined;
    });
    const onStatus = vi.fn();
    const { result } = renderHook(() =>
      useWorkflowWorkspace({ currentUserID: "u", databaseName: "ops", isAdmin: true, workflowName: "order-sync", onStatus })
    );
    await waitFor(() => expect(result.current.detail).not.toBeNull());
    await act(async () => {
      expect(await result.current.publishVersion(1)).toBe(false);
    });
    expect(onStatus).toHaveBeenLastCalledWith('trigger "changed": table "orders" does not exist', "error");
  });
});
