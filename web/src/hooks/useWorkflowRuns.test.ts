import { act, renderHook, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { WorkflowRun } from "../api";
import { runPageSize, useOwnWorkflowRuns, useWorkflowRun, useWorkflowRuns } from "./useWorkflowRuns";

function run(id: number, status: WorkflowRun["status"] = "succeeded"): WorkflowRun {
  return { id, workflow_id: 1, trigger: "start", status, created_at: id };
}

function json(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), { status });
}

beforeEach(() => {
  vi.restoreAllMocks();
  window.localStorage.clear();
});

afterEach(() => {
  vi.useRealTimers();
});

describe("useWorkflowRuns", () => {
  it("filters by status and pages backwards with before", async () => {
    const urls: string[] = [];
    vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => {
      const url = String(input);
      urls.push(url);
      const params = new URL(url, "http://x").searchParams;
      if (params.get("before")) {
        return json([run(3), run(2)]);
      }
      return json(Array.from({ length: runPageSize }, (_, index) => run(100 - index)));
    });
    const { result } = renderHook(() => useWorkflowRuns("ops", "order-sync"));
    await waitFor(() => expect(result.current.ready).toBe(true));
    expect(result.current.hasMore).toBe(true);
    expect(urls[0]).toBe(`/api/databases/ops/workflows/order-sync/runs?limit=${runPageSize}`);

    await act(async () => {
      await result.current.loadMore();
    });
    expect(urls.at(-1)).toBe(`/api/databases/ops/workflows/order-sync/runs?limit=${runPageSize}&before=${100 - runPageSize + 1}`);
    expect(result.current.runs).toHaveLength(runPageSize + 2);
    expect(result.current.hasMore).toBe(false);

    act(() => result.current.setStatus("failed"));
    await waitFor(() => expect(urls.at(-1)).toBe(`/api/databases/ops/workflows/order-sync/runs?status=failed&limit=${runPageSize}`));
  });

  it("refreshes while a listed run is still going", async () => {
    let calls = 0;
    vi.spyOn(globalThis, "fetch").mockImplementation(async () => {
      calls += 1;
      return json([run(5, calls < 3 ? "running" : "succeeded"), run(4)]);
    });
    const { result } = renderHook(() => useWorkflowRuns("ops", "order-sync"));
    await waitFor(() => expect(result.current.runs[0]?.status).toBe("succeeded"), { timeout: 5000 });
    expect(calls).toBe(3);
    // Final runs stop the polling.
    await new Promise((resolve) => setTimeout(resolve, 1700));
    expect(calls).toBe(3);
  }, 10_000);
});

describe("useWorkflowRun", () => {
  it("polls a queued run until it finishes and cancels it", async () => {
    let status: WorkflowRun["status"] = "queued";
    const methods: string[] = [];
    vi.spyOn(globalThis, "fetch").mockImplementation(async (input, init) => {
      methods.push(`${init?.method ?? "GET"} ${String(input)}`);
      if (init?.method === "POST") {
        status = "cancelled";
        return json({ ...run(9, "cancelled"), error: "cancelled" });
      }
      return json({ ...run(9, status), logs: [] });
    });
    const { result } = renderHook(() => useWorkflowRun("ops", "order-sync", 9));
    await waitFor(() => expect(result.current.run?.status).toBe("queued"));
    await act(async () => {
      await result.current.cancel();
    });
    expect(methods).toContain("POST /api/databases/ops/workflows/order-sync/runs/9/cancel");
    expect(result.current.run?.status).toBe("cancelled");
    expect(result.current.run?.logs).toEqual([]);
  });
});

describe("useOwnWorkflowRuns", () => {
  it("remembers started runs per user and database and forgets the ones that are gone", async () => {
    window.localStorage.setItem(
      "autable.workflowRuns:u1:ops",
      JSON.stringify([
        { workflow: "order-sync", id: 2 },
        { workflow: "deleted", id: 1 }
      ])
    );
    vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => {
      const url = String(input);
      if (url === "/api/databases/ops/workflows/order-sync/runs/2") return json(run(2));
      if (url === "/api/databases/ops/workflows/order-sync/runs/3") return json(run(3));
      return json({ error: "workflow run not found" }, 404);
    });
    const { result } = renderHook(() => useOwnWorkflowRuns("u1", "ops"));
    await waitFor(() => expect(result.current.ready).toBe(true));
    expect(result.current.runs.map((item) => [item.workflow, item.id])).toEqual([["order-sync", 2]]);
    expect(JSON.parse(window.localStorage.getItem("autable.workflowRuns:u1:ops") ?? "")).toEqual([{ workflow: "order-sync", id: 2 }]);

    act(() => result.current.remember("order-sync", run(3)));
    expect(result.current.runs.map((item) => item.id)).toEqual([3, 2]);
    expect(JSON.parse(window.localStorage.getItem("autable.workflowRuns:u1:ops") ?? "")).toEqual([
      { workflow: "order-sync", id: 3 },
      { workflow: "order-sync", id: 2 }
    ]);
    expect(window.localStorage.getItem("autable.workflowRuns:u2:ops")).toBeNull();
  });
});
