import { useCallback, useEffect, useRef, useState } from "react";
import { cancelWorkflowRun, listWorkflowRuns, loadWorkflowRun, type WorkflowRun, type WorkflowRunStatus } from "../api";
import { isActiveRun } from "../workflowFormat";

/** How often runs that are still queued or running are fetched again. */
export const runPollIntervalMs = 1500;
export const runPageSize = 25;

/**
 * The runs of one workflow, newest first, filtered by status and paged with
 * "load more". While a listed run is queued or running the visible pages are
 * fetched again until every run is final.
 */
export function useWorkflowRuns(databaseName: string, workflowName: string, enabled = true) {
  const [status, setStatus] = useState<WorkflowRunStatus | "">("");
  const [runs, setRuns] = useState<WorkflowRun[]>([]);
  const [ready, setReady] = useState(false);
  const [hasMore, setHasMore] = useState(false);
  const [loadingMore, setLoadingMore] = useState(false);
  const [error, setError] = useState("");
  const generation = useRef(0);

  const fetchFirst = useCallback(
    async (limit: number) => {
      const current = ++generation.current;
      try {
        const next = await listWorkflowRuns(databaseName, workflowName, { status, limit });
        if (current !== generation.current) {
          return;
        }
        setRuns(next);
        setHasMore(next.length >= limit);
        setError("");
      } catch (loadError) {
        if (current === generation.current) {
          setError(loadError instanceof Error ? loadError.message : String(loadError));
        }
      } finally {
        if (current === generation.current) {
          setReady(true);
        }
      }
    },
    [databaseName, workflowName, status]
  );

  useEffect(() => {
    setRuns([]);
    setReady(false);
    setHasMore(false);
    if (!enabled || !databaseName || !workflowName) {
      generation.current += 1;
      return;
    }
    void fetchFirst(runPageSize);
  }, [enabled, fetchFirst, databaseName, workflowName]);

  const active = runs.some((run) => isActiveRun(run.status));
  const visible = runs.length;
  useEffect(() => {
    if (!enabled || !active) {
      return;
    }
    const timer = window.setTimeout(() => void fetchFirst(Math.max(runPageSize, visible)), runPollIntervalMs);
    return () => window.clearTimeout(timer);
  }, [enabled, active, runs, visible, fetchFirst]);

  async function loadMore() {
    const last = runs[runs.length - 1];
    if (!last) {
      return;
    }
    setLoadingMore(true);
    const current = generation.current;
    try {
      const next = await listWorkflowRuns(databaseName, workflowName, { status, limit: runPageSize, before: last.id });
      if (current !== generation.current) {
        return;
      }
      setRuns((existing) => [...existing, ...next.filter((run) => !existing.some((item) => item.id === run.id))]);
      setHasMore(next.length >= runPageSize);
    } catch (loadError) {
      setError(loadError instanceof Error ? loadError.message : String(loadError));
    } finally {
      setLoadingMore(false);
    }
  }

  /** Fetches the visible runs again, e.g. after starting or cancelling one. */
  function refresh() {
    return fetchFirst(Math.max(runPageSize, runs.length));
  }

  return { error, hasMore, loadMore, loadingMore, ready, refresh, runs, setStatus, status };
}

/** One run with its event and logs, fetched again while it is queued or running. */
export function useWorkflowRun(databaseName: string, workflowName: string, runID: number | undefined) {
  const [run, setRun] = useState<WorkflowRun | null>(null);
  const [error, setError] = useState("");
  const [cancelling, setCancelling] = useState(false);
  const generation = useRef(0);

  const fetchRun = useCallback(async () => {
    if (!runID) {
      return;
    }
    const current = ++generation.current;
    try {
      const next = await loadWorkflowRun(databaseName, workflowName, runID);
      if (current === generation.current) {
        setRun(next);
        setError("");
      }
    } catch (loadError) {
      if (current === generation.current) {
        setError(loadError instanceof Error ? loadError.message : String(loadError));
      }
    }
  }, [databaseName, workflowName, runID]);

  useEffect(() => {
    setRun(null);
    setError("");
    if (!runID || !databaseName || !workflowName) {
      generation.current += 1;
      return;
    }
    void fetchRun();
  }, [fetchRun, databaseName, workflowName, runID]);

  useEffect(() => {
    if (!run || !isActiveRun(run.status)) {
      return;
    }
    const timer = window.setTimeout(() => void fetchRun(), runPollIntervalMs);
    return () => window.clearTimeout(timer);
  }, [run, fetchRun]);

  async function cancel(): Promise<WorkflowRun | null> {
    if (!runID) {
      return null;
    }
    setCancelling(true);
    try {
      const next = await cancelWorkflowRun(databaseName, workflowName, runID);
      generation.current += 1;
      setRun((current) => (current ? { ...current, ...next } : next));
      return next;
    } finally {
      setCancelling(false);
    }
  }

  return { cancel, cancelling, error, refresh: fetchRun, run };
}

type StartedRun = { workflow: string; id: number };

export const ownRunsLimit = 20;

function ownRunsKey(userID: string, databaseName: string) {
  return `autable.workflowRuns:${userID}:${databaseName}`;
}

function readStartedRuns(key: string): StartedRun[] {
  try {
    const parsed = JSON.parse(window.localStorage.getItem(key) ?? "[]") as unknown;
    return Array.isArray(parsed)
      ? parsed.filter((item): item is StartedRun => typeof item?.workflow === "string" && typeof item?.id === "number")
      : [];
  } catch {
    return [];
  }
}

export type OwnRun = WorkflowRun & { workflow: string };

/**
 * The runs the signed-in user started in this browser. Users with a run
 * grant cannot list runs, only read the runs they started, so the ids are
 * remembered locally (newest first) and each run is fetched by id.
 */
export function useOwnWorkflowRuns(userID: string | undefined, databaseName: string) {
  const key = userID && databaseName ? ownRunsKey(userID, databaseName) : "";
  const [started, setStarted] = useState<StartedRun[]>(() => (key ? readStartedRuns(key) : []));
  const [runs, setRuns] = useState<Record<string, OwnRun>>({});
  const [ready, setReady] = useState(false);

  useEffect(() => {
    setStarted(key ? readStartedRuns(key) : []);
    setRuns({});
    setReady(false);
  }, [key]);

  const fetchRuns = useCallback(
    async (entries: StartedRun[]) => {
      const results = await Promise.all(
        entries.map(async (entry) => {
          try {
            return { entry, run: await loadWorkflowRun(databaseName, entry.workflow, entry.id) };
          } catch {
            // Deleted workflows and pruned runs answer 404; forget them.
            return { entry, run: null };
          }
        })
      );
      setRuns((current) => {
        const next = { ...current };
        for (const { entry, run } of results) {
          if (run) {
            next[`${entry.workflow}:${entry.id}`] = { ...run, workflow: entry.workflow };
          }
        }
        return next;
      });
      const missing = results.filter((result) => !result.run).map((result) => result.entry);
      if (missing.length > 0 && key) {
        setStarted((current) => {
          const next = current.filter((item) => !missing.some((gone) => gone.workflow === item.workflow && gone.id === item.id));
          window.localStorage.setItem(key, JSON.stringify(next));
          return next;
        });
      }
      setReady(true);
    },
    [databaseName, key]
  );

  useEffect(() => {
    if (!key) {
      return;
    }
    void fetchRuns(readStartedRuns(key));
  }, [key, fetchRuns]);

  const list = started.map((entry) => runs[`${entry.workflow}:${entry.id}`]).filter((run): run is OwnRun => Boolean(run));
  const activeEntries = list.filter((run) => isActiveRun(run.status)).map((run) => ({ workflow: run.workflow, id: run.id }));
  const activeKey = activeEntries.map((entry) => `${entry.workflow}:${entry.id}`).join(",");

  useEffect(() => {
    if (!activeKey) {
      return;
    }
    const timer = window.setTimeout(() => void fetchRuns(activeEntries), runPollIntervalMs);
    return () => window.clearTimeout(timer);
    // activeKey names the entries; the array itself is rebuilt every render.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [activeKey, runs, fetchRuns]);

  function remember(workflow: string, run: WorkflowRun) {
    if (!key) {
      return;
    }
    const entry = { workflow, id: run.id };
    setRuns((current) => ({ ...current, [`${workflow}:${run.id}`]: { ...run, workflow } }));
    setStarted((current) => {
      const next = [entry, ...current.filter((item) => item.workflow !== workflow || item.id !== run.id)].slice(0, ownRunsLimit);
      window.localStorage.setItem(key, JSON.stringify(next));
      return next;
    });
  }

  return { ready, remember, runs: list };
}
