import { ChevronRight, CircleStop, ScrollText } from "lucide-react";
import { useEffect, useState, type ReactNode } from "react";
import { useTranslation } from "react-i18next";
import type { AuthUser, WorkflowLogEntry, WorkflowRun } from "../api";
import { Button, Callout, Card, cn, Skeleton, StatusBadge } from "../ui";
import { formatDuration, formatJSON, formatLogTime, formatTimestamp, isActiveRun, runDuration, runStatusTone } from "../workflowFormat";

/** A JSON value in a scrolling monospace block with its label. */
export function JsonBlock({ label, text, className }: { label: string; text: string; className?: string }) {
  return (
    <div className={cn("flex min-w-0 flex-col gap-1.5", className)}>
      <span className="text-12 font-medium text-muted">{label}</span>
      <pre
        aria-label={label}
        className="max-h-80 overflow-auto rounded-inner bg-chip px-3 py-2.5 font-mono text-12 leading-relaxed text-fg [overflow-wrap:anywhere] whitespace-pre-wrap"
      >
        {text}
      </pre>
    </div>
  );
}

/** A collapsible section: closed by default for bulky payloads such as the event. */
function Disclosure({ summary, children, defaultOpen = false }: { summary: ReactNode; children: ReactNode; defaultOpen?: boolean }) {
  return (
    <details className="group rounded-inner border border-border-soft" open={defaultOpen}>
      <summary className="flex cursor-pointer select-none list-none items-center gap-1.5 px-3 py-2 text-12 font-medium text-muted hover:text-fg [&::-webkit-details-marker]:hidden">
        <ChevronRight className="size-3.5 transition-transform duration-150 group-open:rotate-90" aria-hidden />
        {summary}
      </summary>
      <div className="border-t border-border-soft p-3">{children}</div>
    </details>
  );
}

const levelClass: Record<WorkflowLogEntry["level"], string> = {
  info: "text-muted",
  warn: "text-warning",
  error: "text-error"
};

export function RunLogs({ logs }: { logs: WorkflowLogEntry[] }) {
  const { t } = useTranslation();
  if (logs.length === 0) {
    return <p className="px-4 py-3 text-13 text-muted">{t("workflow.runs.noLogs")}</p>;
  }
  return (
    <ol aria-label={t("workflow.runs.logs")} className="divide-y divide-border-soft font-mono text-12">
      {logs.map((entry, index) => (
        <li key={index} className={cn("px-4 py-1.5", entry.level === "error" && "bg-error-bg/50", entry.level === "warn" && "bg-warning-bg/50")}>
          <div className="flex min-w-0 items-baseline gap-3">
            <time className="shrink-0 tabular-nums text-subtle" dateTime={new Date(entry.time).toISOString()} title={formatTimestamp(entry.time)}>
              {formatLogTime(entry.time)}
            </time>
            <span className={cn("w-10 shrink-0 font-medium uppercase", levelClass[entry.level] ?? "text-muted")}>{entry.level}</span>
            <span className="min-w-0 flex-1 whitespace-pre-wrap text-fg [overflow-wrap:anywhere]">{entry.message}</span>
          </div>
          {entry.data !== undefined && (
            <details className="group ml-[8.5rem] mt-1">
              <summary className="flex cursor-pointer list-none items-center gap-1 text-subtle hover:text-fg [&::-webkit-details-marker]:hidden">
                <ChevronRight className="size-3 transition-transform duration-150 group-open:rotate-90" aria-hidden />
                {t("workflow.runs.logData")}
              </summary>
              <pre className="mt-1 max-h-60 overflow-auto rounded-inner bg-chip px-2.5 py-2 leading-relaxed text-fg whitespace-pre-wrap [overflow-wrap:anywhere]">
                {formatJSON(entry.data)}
              </pre>
            </details>
          )}
        </li>
      ))}
    </ol>
  );
}

function Meta({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div className="flex min-w-0 flex-col gap-0.5">
      <dt className="text-12 text-muted">{label}</dt>
      <dd className="min-w-0 truncate text-13 text-fg">{children || <span className="text-subtle">—</span>}</dd>
    </div>
  );
}

/** Ticks once a second while a run is going, so its duration counts up. */
function useNow(active: boolean) {
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    if (!active) {
      return;
    }
    const timer = window.setInterval(() => setNow(Date.now()), 1000);
    return () => window.clearInterval(timer);
  }, [active]);
  return now;
}

/**
 * One run as admins see it: status and timing, the error, the output, the
 * triggering event (collapsed) and the log.
 */
export function WorkflowRunDetail({
  run,
  error,
  currentUser,
  cancelling,
  onCancel
}: {
  run: WorkflowRun | null;
  error?: string;
  currentUser?: AuthUser | null;
  cancelling?: boolean;
  onCancel?: () => void;
}) {
  const { t } = useTranslation();
  const active = Boolean(run && isActiveRun(run.status));
  const now = useNow(active);
  if (error && !run) {
    return (
      <Card className="p-5">
        <Callout tone="danger">{error}</Callout>
      </Card>
    );
  }
  if (!run) {
    return (
      <Card role="status" aria-label={t("workflow.runs.loadingRun")} className="flex flex-col gap-3 p-5">
        <Skeleton className="h-6 w-40" />
        <Skeleton className="h-16" />
        <Skeleton className="h-32" />
      </Card>
    );
  }
  const duration = runDuration(run, now);
  const startedBy = run.started_by && run.started_by === currentUser?.id ? currentUser.display_name : run.started_by;
  return (
    <Card aria-label={t("workflow.runs.runLabel", { id: run.id })} role="region" className="min-w-0 overflow-hidden">
      <div className="flex flex-wrap items-center justify-between gap-3 border-b border-border-soft px-5 py-3.5">
        <h3 className="flex items-center gap-2.5 text-15 font-medium text-fg">
          {t("workflow.runs.runLabel", { id: run.id })}
          <StatusBadge tone={runStatusTone(run.status)} label={t(`workflow.status.${run.status}`)} pulse={active} />
        </h3>
        {active && onCancel && (
          <Button size="sm" variant="danger-quiet" loading={cancelling} onClick={onCancel}>
            <CircleStop aria-hidden />
            {t("workflow.runs.cancel")}
          </Button>
        )}
      </div>
      <dl className="grid grid-cols-2 gap-x-6 gap-y-3 px-5 py-4 sm:grid-cols-3">
        <Meta label={t("workflow.runs.trigger")}>
          <code className="font-mono text-12">{run.trigger}</code>
        </Meta>
        <Meta label={t("workflow.runs.version")}>{run.version ? t("app.versionLabel", { version: run.version }) : ""}</Meta>
        <Meta label={t("workflow.runs.startedBy")}>{startedBy}</Meta>
        <Meta label={t("workflow.runs.queued")}>{formatTimestamp(run.created_at)}</Meta>
        <Meta label={t("workflow.runs.started")}>{formatTimestamp(run.started_at)}</Meta>
        <Meta label={t("workflow.runs.duration")}>{duration === undefined ? "" : formatDuration(duration)}</Meta>
      </dl>
      <div className="flex flex-col gap-4 border-t border-border-soft px-5 py-4">
        {run.error && (
          <Callout tone="danger" title={t("workflow.runs.error")}>
            <span className="whitespace-pre-wrap font-mono text-12 [overflow-wrap:anywhere]">{run.error}</span>
          </Callout>
        )}
        {run.output !== undefined ? (
          <JsonBlock label={t("workflow.runs.output")} text={formatJSON(run.output)} />
        ) : (
          !active && !run.error && <p className="text-13 text-muted">{t("workflow.runs.noOutput")}</p>
        )}
        {run.event !== undefined && (
          <Disclosure summary={t("workflow.runs.event")}>
            <pre
              aria-label={t("workflow.runs.event")}
              className="max-h-80 overflow-auto font-mono text-12 leading-relaxed text-fg whitespace-pre-wrap [overflow-wrap:anywhere]"
            >
              {formatJSON(run.event)}
            </pre>
          </Disclosure>
        )}
      </div>
      {run.logs !== undefined && (
        <section aria-labelledby={`run-${run.id}-logs`} className="border-t border-border-soft">
          <h4 id={`run-${run.id}-logs`} className="flex items-center gap-1.5 px-5 pb-1 pt-3.5 text-12 font-medium text-muted">
            <ScrollText className="size-3.5" aria-hidden />
            {t("workflow.runs.logsCount", { count: run.logs.length })}
          </h4>
          <div className="px-1 pb-2">
            <RunLogs logs={run.logs} />
          </div>
        </section>
      )}
    </Card>
  );
}
