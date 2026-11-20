import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import {
  deleteWorkflow,
  deleteWorkflowSecret,
  deleteWorkflowVar,
  listWorkflows,
  loadWorkflow,
  publishWorkflowVersion,
  setWorkflowSecret,
  setWorkflowVar,
  updateWorkflowSettings,
  type WorkflowDefinition,
  type WorkflowDetail,
  type WorkflowSettings
} from "../api";
import type { Notify } from "../notifications";

type UseWorkflowWorkspaceOptions = {
  currentUserID?: string;
  databaseName: string;
  // Admins manage workflows; everyone else only gets the runnable list.
  isAdmin: boolean;
  // The workflow named by the current route; "" shows the list.
  workflowName: string;
  onStatus: Notify;
};

// useWorkflowWorkspace loads the workflows of a database and, for admins,
// the detail (versions, secrets, vars) of the selected one. Workflows are
// created and uploaded with `autablectl workflow push`; the web UI
// publishes, rolls back, configures, runs and deletes them.
export function useWorkflowWorkspace({ currentUserID, databaseName, isAdmin, workflowName, onStatus }: UseWorkflowWorkspaceOptions) {
  const { t } = useTranslation();
  const [workflows, setWorkflows] = useState<WorkflowDefinition[]>([]);
  const [workflowsReady, setWorkflowsReady] = useState(false);
  const [detail, setDetail] = useState<WorkflowDetail | null>(null);
  const [busy, setBusy] = useState(false);

  useEffect(() => {
    let cancelled = false;
    setWorkflows([]);
    setWorkflowsReady(false);
    if (!databaseName || !currentUserID) {
      return () => {
        cancelled = true;
      };
    }
    void listWorkflows(databaseName)
      .then((next) => {
        if (!cancelled) {
          setWorkflows(next);
          setWorkflowsReady(true);
        }
      })
      .catch((error) => {
        if (!cancelled) {
          onStatus(error instanceof Error ? error.message : t("status.workflowListFailed"), "error");
        }
      });
    return () => {
      cancelled = true;
    };
  }, [currentUserID, databaseName]);

  // Load the detail only for a workflow the list knows, so a stale route
  // name never turns into an error toast (the route falls back instead).
  const listed = workflowsReady && workflows.some((item) => item.name === workflowName);

  useEffect(() => {
    let cancelled = false;
    setDetail(null);
    if (!databaseName || !currentUserID || !isAdmin || !workflowName || !listed) {
      return () => {
        cancelled = true;
      };
    }
    void loadWorkflow(databaseName, workflowName)
      .then((next) => {
        if (!cancelled) {
          setDetail(next);
        }
      })
      .catch((error) => {
        if (!cancelled) {
          onStatus(error instanceof Error ? error.message : t("status.workflowLoadFailed"), "error");
        }
      });
    return () => {
      cancelled = true;
    };
  }, [currentUserID, databaseName, isAdmin, workflowName, listed]);

  async function reload(name: string) {
    const [nextList, nextDetail] = await Promise.all([listWorkflows(databaseName), loadWorkflow(databaseName, name)]);
    setWorkflows(nextList);
    setDetail(nextDetail);
  }

  async function refreshWorkflows(nextDatabaseName = databaseName) {
    if (!currentUserID || !nextDatabaseName) {
      setWorkflows([]);
      setWorkflowsReady(false);
      return;
    }
    const [nextList, nextDetail] = await Promise.all([
      listWorkflows(nextDatabaseName),
      isAdmin && workflowName && nextDatabaseName === databaseName ? loadWorkflow(nextDatabaseName, workflowName).catch(() => null) : null
    ]);
    setWorkflows(nextList);
    setWorkflowsReady(true);
    setDetail(nextDetail);
  }

  // mutate runs one admin change on the selected workflow, reloads it and
  // reports the outcome.
  async function mutate(action: (name: string) => Promise<unknown>, success: (title: string) => string, failure: string): Promise<boolean> {
    if (!detail) {
      return false;
    }
    const { name, title } = detail.workflow;
    setBusy(true);
    try {
      await action(name);
      await reload(name);
      onStatus(success(title || name), "success");
      return true;
    } catch (error) {
      onStatus(error instanceof Error ? error.message : failure, "error");
      return false;
    } finally {
      setBusy(false);
    }
  }

  function publishVersion(version: number) {
    const rollback = (detail?.workflow.published_version ?? 0) > version;
    return mutate(
      (name) => publishWorkflowVersion(databaseName, name, version),
      (name) => t(rollback ? "status.workflowRolledBack" : "status.workflowPublished", { name, version }),
      t("status.workflowPublishFailed")
    );
  }

  function setEnabled(enabled: boolean) {
    return mutate(
      (name) => updateWorkflowSettings(databaseName, name, { enabled }),
      (name) => t(enabled ? "status.workflowEnabled" : "status.workflowDisabled", { name }),
      t("status.workflowUpdateFailed")
    );
  }

  function saveSettings(settings: Pick<WorkflowSettings, "timeout_seconds" | "history_retention_days">) {
    return mutate(
      (name) => updateWorkflowSettings(databaseName, name, settings),
      (name) => t("status.workflowSettingsSaved", { name }),
      t("status.workflowUpdateFailed")
    );
  }

  function saveSecret(key: string, value: string) {
    return mutate(
      (name) => setWorkflowSecret(databaseName, name, key, value),
      () => t("status.workflowSecretSaved", { key }),
      t("status.workflowUpdateFailed")
    );
  }

  function removeSecret(key: string) {
    return mutate(
      (name) => deleteWorkflowSecret(databaseName, name, key),
      () => t("status.workflowSecretDeleted", { key }),
      t("status.workflowUpdateFailed")
    );
  }

  function saveVar(key: string, value: string) {
    return mutate(
      (name) => setWorkflowVar(databaseName, name, key, value),
      () => t("status.workflowVarSaved", { key }),
      t("status.workflowUpdateFailed")
    );
  }

  function removeVar(key: string) {
    return mutate(
      (name) => deleteWorkflowVar(databaseName, name, key),
      () => t("status.workflowVarDeleted", { key }),
      t("status.workflowUpdateFailed")
    );
  }

  async function removeWorkflow(): Promise<boolean> {
    if (!detail) {
      return false;
    }
    const workflow = detail.workflow;
    setBusy(true);
    try {
      await deleteWorkflow(databaseName, workflow.name);
      setWorkflows((current) => current.filter((item) => item.id !== workflow.id));
      setDetail(null);
      onStatus(t("status.workflowDeleted", { name: workflow.title || workflow.name }), "success");
      return true;
    } catch (error) {
      onStatus(error instanceof Error ? error.message : t("status.workflowDeleteFailed"), "error");
      return false;
    } finally {
      setBusy(false);
    }
  }

  // A run changes the workflow's last run; refresh the list (and the open
  // workflow) quietly.
  async function noteRunStarted() {
    if (!databaseName) {
      return;
    }
    const [nextList, nextDetail] = await Promise.all([
      listWorkflows(databaseName).catch(() => null),
      isAdmin && workflowName ? loadWorkflow(databaseName, workflowName).catch(() => null) : null
    ]);
    if (nextList) {
      setWorkflows(nextList);
    }
    if (nextDetail) {
      setDetail(nextDetail);
    }
  }

  return {
    busy,
    detail,
    workflows,
    workflowsReady,
    noteRunStarted,
    publishVersion,
    refreshWorkflows,
    removeSecret,
    removeVar,
    removeWorkflow,
    saveSecret,
    saveSettings,
    saveVar,
    setEnabled
  };
}
