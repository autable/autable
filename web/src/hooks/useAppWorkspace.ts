import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { deleteApp, listApps, loadApp, publishAppVersion, type AppDefinition, type AppDetail } from "../api";
import type { Notify } from "../notifications";

type UseAppWorkspaceOptions = {
  currentUserID?: string;
  databaseName: string;
  // The app named by the current route; "" shows the app list.
  appName: string;
  onStatus: Notify;
};

// useAppWorkspace loads the hosted apps of a database and the detail
// (versions) of the selected one. Apps are created and uploaded from the
// command line; the web UI only lists, publishes, rolls back and deletes.
export function useAppWorkspace({ currentUserID, databaseName, appName, onStatus }: UseAppWorkspaceOptions) {
  const { t } = useTranslation();
  const [apps, setApps] = useState<AppDefinition[]>([]);
  const [appsReady, setAppsReady] = useState(false);
  const [detail, setDetail] = useState<AppDetail | null>(null);
  const [busy, setBusy] = useState(false);

  useEffect(() => {
    let cancelled = false;
    setApps([]);
    setAppsReady(false);
    if (!databaseName || !currentUserID) {
      return () => {
        cancelled = true;
      };
    }
    void listApps(databaseName)
      .then((next) => {
        if (!cancelled) {
          setApps(next);
          setAppsReady(true);
        }
      })
      .catch((error) => {
        if (!cancelled) {
          onStatus(error instanceof Error ? error.message : t("status.appListFailed"), "error");
        }
      });
    return () => {
      cancelled = true;
    };
  }, [currentUserID, databaseName]);

  // Load the detail only for an app the list knows, so a stale or foreign
  // route name never turns into an error toast (the route falls back instead).
  const appListed = appsReady && apps.some((item) => item.name === appName);

  useEffect(() => {
    let cancelled = false;
    setDetail(null);
    if (!databaseName || !currentUserID || !appName || !appListed) {
      return () => {
        cancelled = true;
      };
    }
    void loadApp(databaseName, appName)
      .then((next) => {
        if (!cancelled) {
          setDetail(next);
        }
      })
      .catch((error) => {
        if (!cancelled) {
          onStatus(error instanceof Error ? error.message : t("status.appLoadFailed"), "error");
        }
      });
    return () => {
      cancelled = true;
    };
  }, [currentUserID, databaseName, appName, appListed]);

  async function refreshApps(nextDatabaseName = databaseName) {
    if (!currentUserID || !nextDatabaseName) {
      setApps([]);
      setAppsReady(false);
      return;
    }
    const [nextApps, nextDetail] = await Promise.all([
      listApps(nextDatabaseName),
      appName && nextDatabaseName === databaseName ? loadApp(nextDatabaseName, appName).catch(() => null) : null
    ]);
    setApps(nextApps);
    setAppsReady(true);
    setDetail(nextDetail);
  }

  async function publishVersion(version: number) {
    if (!detail) {
      return;
    }
    const name = detail.app.name;
    const rollback = detail.app.published_version > version;
    setBusy(true);
    try {
      await publishAppVersion(databaseName, name, version);
      const [nextApps, nextDetail] = await Promise.all([listApps(databaseName), loadApp(databaseName, name)]);
      setApps(nextApps);
      setDetail(nextDetail);
      onStatus(t(rollback ? "status.appRolledBack" : "status.appPublished", { name: detail.app.title || name, version }));
    } catch (error) {
      onStatus(error instanceof Error ? error.message : t("status.appPublishFailed"), "error");
    } finally {
      setBusy(false);
    }
  }

  async function removeApp(): Promise<boolean> {
    if (!detail) {
      return false;
    }
    const app = detail.app;
    setBusy(true);
    try {
      await deleteApp(databaseName, app.name);
      setApps((current) => current.filter((item) => item.id !== app.id));
      setDetail(null);
      onStatus(t("status.appDeleted", { name: app.title || app.name }));
      return true;
    } catch (error) {
      onStatus(error instanceof Error ? error.message : t("status.appDeleteFailed"), "error");
      return false;
    } finally {
      setBusy(false);
    }
  }

  return { apps, appsReady, busy, detail, publishVersion, refreshApps, removeApp };
}
