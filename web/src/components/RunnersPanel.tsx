import { Check, Copy, KeyRound, Server } from "lucide-react";
import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { fetchRunners, resetRunnerToken, type RunnersResponse } from "../api";
import { Button, Callout, Dialog, DialogClose, DialogContent, Skeleton, StatusDot } from "../ui";

// Runners are database-scoped: the panel manages the current database's
// runner token and lists the runners connected with it. Only admins reach
// it, from the sidebar's Manage group and the admin overview, which control
// whether it is open.
export function RunnersPanel({
  databaseName,
  open,
  onOpenChange
}: {
  databaseName: string;
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) {
  const { t, i18n } = useTranslation();
  const [runnersInfo, setRunnersInfo] = useState<RunnersResponse | null>(null);
  const [freshToken, setFreshToken] = useState("");
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const [copied, setCopied] = useState(false);

  async function refresh() {
    try {
      setRunnersInfo(await fetchRunners(databaseName));
      setError("");
    } catch (loadError) {
      setError(loadError instanceof Error ? loadError.message : t("runners.loadFailed"));
    }
  }

  async function reset() {
    setBusy(true);
    try {
      const result = await resetRunnerToken(databaseName);
      setFreshToken(result.token);
      setCopied(false);
      await refresh();
    } catch (resetError) {
      setError(resetError instanceof Error ? resetError.message : t("runners.resetFailed"));
    } finally {
      setBusy(false);
    }
  }

  useEffect(() => {
    if (open && databaseName) {
      setFreshToken("");
      setRunnersInfo(null);
      setError("");
      void refresh();
    }
    // Reload whenever the dialog opens, or the database changes under it.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open, databaseName]);

  const runners = runnersInfo?.runners ?? [];
  const formatTime = (timestamp: number) => new Date(timestamp).toLocaleString(i18n.language);

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent
        title={t("runners.title")}
        description={t("runners.description", { database: databaseName })}
        className="max-w-lg"
        footer={
          <DialogClose asChild>
            <Button>{t("common.close")}</Button>
          </DialogClose>
        }
      >
        <div className="flex flex-col gap-5">
          {error !== "" && <Callout tone="danger">{error}</Callout>}
          {runnersInfo === null && error === "" ? (
            // Until the first load answers, the panel knows neither the
            // runners nor the token state, so it claims nothing.
            <div role="status" aria-label={t("runners.loading")} className="flex flex-col gap-2">
              <Skeleton className="h-12" />
              <Skeleton className="h-12" />
            </div>
          ) : (
            <>
              <section className="flex flex-col gap-2">
                <h3 className="text-13 font-medium text-fg">{t("runners.connected")}</h3>
                {runners.length === 0 ? (
                  <p className="rounded-container border border-dashed border-border px-4 py-4 text-center text-13 text-muted">{t("runners.empty")}</p>
                ) : (
                  <ul aria-label={t("runners.connected")} className="divide-y divide-border-soft rounded-container border border-border">
                    {runners.map((runner) => (
                      <li key={`${runner.name}-${runner.connected_at}`} className="flex items-center gap-3 px-3.5 py-2.5">
                        <span className="flex size-8 shrink-0 items-center justify-center rounded-inner bg-chip text-description" aria-hidden>
                          <Server className="size-4" strokeWidth={1.75} />
                        </span>
                        <span className="min-w-0 flex-1">
                          <span className="flex items-center gap-2">
                            <span className="truncate text-13 font-medium text-fg">{runner.name}</span>
                            <StatusDot tone="success" label={t("runners.online")} />
                          </span>
                          <span className="block truncate text-12 text-muted">
                            {t("runners.details", {
                              version: runner.version,
                              nodes: runner.node_types.length,
                              connectedAt: formatTime(runner.connected_at)
                            })}
                          </span>
                        </span>
                      </li>
                    ))}
                  </ul>
                )}
              </section>
              <section className="flex flex-col gap-2">
                <h3 className="text-13 font-medium text-fg">{t("runners.token")}</h3>
                <div className="flex items-center gap-3 rounded-container border border-border px-3.5 py-3">
                  <span className="flex size-8 shrink-0 items-center justify-center rounded-inner bg-chip text-description" aria-hidden>
                    <KeyRound className="size-4" strokeWidth={1.75} />
                  </span>
                  <span className="min-w-0 flex-1 text-13 text-description">
                    {runnersInfo?.token?.exists
                      ? t("runners.tokenCreatedAt", { createdAt: formatTime(runnersInfo.token.created_at ?? 0) })
                      : t("runners.tokenMissing")}
                  </span>
                  {runnersInfo !== null && (
                    <Button size="sm" variant={runnersInfo.token?.exists ? "secondary" : "cta"} loading={busy} onClick={() => void reset()}>
                      {runnersInfo.token?.exists ? t("runners.reset") : t("runners.generate")}
                    </Button>
                  )}
                </div>
                {freshToken !== "" && (
                  <Callout tone="warning" title={t("runners.tokenShownOnce")}>
                    <div className="mt-1.5 flex items-center gap-2">
                      <code aria-label={t("runners.tokenValue")} className="min-w-0 flex-1 break-all rounded-inner bg-elevated px-2.5 py-1.5 font-mono text-12 text-fg">
                        {freshToken}
                      </code>
                      <Button
                        variant="quiet"
                        size="icon-sm"
                        aria-label={t("runners.copy")}
                        onClick={() => {
                          void navigator.clipboard?.writeText(freshToken).then(() => setCopied(true));
                        }}
                      >
                        {copied ? <Check /> : <Copy />}
                      </Button>
                    </div>
                  </Callout>
                )}
              </section>
            </>
          )}
        </div>
      </DialogContent>
    </Dialog>
  );
}
