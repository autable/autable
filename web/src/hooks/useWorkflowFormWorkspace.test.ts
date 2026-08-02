import { describe, expect, it } from "vitest";
import type { WorkflowRunResponse } from "../api";
import { mergeWorkflowRuns } from "./useWorkflowFormWorkspace";

function summaryRun(key: string): WorkflowRunResponse {
  return { history_key: key, run: { workflow_id: 1, timestamp: 1, steps: [] }, summary: true };
}

function fullRun(key: string, outputs: Record<string, unknown>): WorkflowRunResponse {
  return { history_key: key, run: { workflow_id: 1, timestamp: 1, outputs, steps: [] } };
}

describe("mergeWorkflowRuns", () => {
  it("keeps an already-loaded run detail when a list refresh delivers its summary", () => {
    // Regression: the history panel intermittently showed {} because a list
    // refresh, merging against a stale snapshot, clobbered the loaded detail.
    const loaded = fullRun("whistory_1", { record_id: 1 });
    const merged = mergeWorkflowRuns([loaded], [summaryRun("whistory_1")]);
    expect(merged).toEqual([loaded]);
  });

  it("passes through summaries that have no loaded detail", () => {
    const merged = mergeWorkflowRuns([], [summaryRun("whistory_1"), summaryRun("whistory_2")]);
    expect(merged.every((run) => run.summary)).toBe(true);
    expect(merged.map((run) => run.history_key)).toEqual(["whistory_1", "whistory_2"]);
  });

  it("prefers an incoming full run over the cached one", () => {
    const stale = fullRun("whistory_1", { record_id: 1 });
    const fresh = fullRun("whistory_1", { record_id: 2 });
    expect(mergeWorkflowRuns([stale], [fresh])).toEqual([fresh]);
  });

  it("drops cached runs that the incoming list no longer contains", () => {
    const loaded = fullRun("whistory_old", { record_id: 1 });
    const merged = mergeWorkflowRuns([loaded], [summaryRun("whistory_new")]);
    expect(merged.map((run) => run.history_key)).toEqual(["whistory_new"]);
  });
});
