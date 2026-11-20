import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { deleteLibrary, listLibraries, loadLibrary, publishLibraryVersion, type LibraryDefinition, type LibraryDetail } from "../api";
import type { Notify } from "../notifications";

type UseLibraryWorkspaceOptions = {
  // Libraries are admin-only; nothing is loaded for anyone else.
  enabled: boolean;
  // The library named by the current route; "" shows the list.
  libraryName: string;
  onStatus: Notify;
};

// useLibraryWorkspace loads the instance's libraries and the detail of the
// selected one. Libraries are pushed from the command line; the web UI
// lists them, publishes, rolls back and deletes.
export function useLibraryWorkspace({ enabled, libraryName, onStatus }: UseLibraryWorkspaceOptions) {
  const { t } = useTranslation();
  const [libraries, setLibraries] = useState<LibraryDefinition[]>([]);
  const [librariesReady, setLibrariesReady] = useState(false);
  const [detail, setDetail] = useState<LibraryDetail | null>(null);
  const [busy, setBusy] = useState(false);

  useEffect(() => {
    let cancelled = false;
    setLibraries([]);
    setLibrariesReady(false);
    if (!enabled) {
      return () => {
        cancelled = true;
      };
    }
    void listLibraries()
      .then((next) => {
        if (!cancelled) {
          setLibraries(next);
          setLibrariesReady(true);
        }
      })
      .catch((error) => {
        if (!cancelled) {
          onStatus(error instanceof Error ? error.message : t("status.libraryListFailed"), "error");
        }
      });
    return () => {
      cancelled = true;
    };
  }, [enabled]);

  const listed = librariesReady && libraries.some((item) => item.name === libraryName);

  useEffect(() => {
    let cancelled = false;
    setDetail(null);
    if (!enabled || !libraryName || !listed) {
      return () => {
        cancelled = true;
      };
    }
    void loadLibrary(libraryName)
      .then((next) => {
        if (!cancelled) {
          setDetail(next);
        }
      })
      .catch((error) => {
        if (!cancelled) {
          onStatus(error instanceof Error ? error.message : t("status.libraryLoadFailed"), "error");
        }
      });
    return () => {
      cancelled = true;
    };
  }, [enabled, libraryName, listed]);

  async function publishVersion(version: number) {
    if (!detail) {
      return;
    }
    const name = detail.library.name;
    const rollback = detail.library.published_version > version;
    setBusy(true);
    try {
      const next = await publishLibraryVersion(name, version);
      setDetail(next);
      setLibraries(await listLibraries());
      onStatus(t(rollback ? "status.libraryRolledBack" : "status.libraryPublished", { name: next.library.title || name, version }), "success");
    } catch (error) {
      onStatus(error instanceof Error ? error.message : t("status.libraryPublishFailed"), "error");
    } finally {
      setBusy(false);
    }
  }

  async function removeLibrary(): Promise<boolean> {
    if (!detail) {
      return false;
    }
    const library = detail.library;
    setBusy(true);
    try {
      await deleteLibrary(library.name);
      setLibraries((current) => current.filter((item) => item.id !== library.id));
      setDetail(null);
      onStatus(t("status.libraryDeleted", { name: library.title || library.name }), "success");
      return true;
    } catch (error) {
      onStatus(error instanceof Error ? error.message : t("status.libraryDeleteFailed"), "error");
      return false;
    } finally {
      setBusy(false);
    }
  }

  return { libraries, librariesReady, busy, detail, publishVersion, removeLibrary };
}
