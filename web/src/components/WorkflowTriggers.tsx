import { CalendarClock, Check, Copy, DatabaseZap, MousePointerClick, Play, Webhook, type LucideIcon } from "lucide-react";
import { useState, type ReactNode } from "react";
import { useTranslation } from "react-i18next";
import type { WorkflowTrigger, WorkflowTriggerKind } from "../api";
import { normalizeLanguage } from "../i18n";
import { Badge, Button, Card, cn, Tooltip } from "../ui";
import { describeCron, triggerSummary, webhookAddress } from "../workflowFormat";

export const triggerIcons: Record<WorkflowTriggerKind, LucideIcon> = {
  schedule: CalendarClock,
  recordChanged: DatabaseZap,
  webhook: Webhook,
  manual: MousePointerClick
};

/** Trigger icons only, for narrow list rows. */
export function TriggerIcons({ triggers, className }: { triggers: WorkflowTrigger[]; className?: string }) {
  const { t, i18n } = useTranslation();
  const language = normalizeLanguage(i18n.resolvedLanguage ?? i18n.language);
  return (
    <span className={cn("flex items-center gap-1.5 text-muted", className)}>
      {triggers.map((trigger) => {
        const Icon = triggerIcons[trigger.kind];
        return <Icon key={trigger.name} className="size-3.5" strokeWidth={1.75} aria-label={triggerSummary(trigger, t, language)} />;
      })}
    </span>
  );
}

/** Compact trigger lines for lists: an icon and a short description each. */
export function TriggerSummaryList({ triggers, className }: { triggers: WorkflowTrigger[]; className?: string }) {
  const { t, i18n } = useTranslation();
  const language = normalizeLanguage(i18n.resolvedLanguage ?? i18n.language);
  if (triggers.length === 0) {
    return <span className="text-13 text-subtle">{t("workflow.trigger.none")}</span>;
  }
  return (
    <ul className={cn("flex min-w-0 flex-col gap-1", className)} aria-label={t("workflow.trigger.listLabel")}>
      {triggers.map((trigger) => {
        const Icon = triggerIcons[trigger.kind];
        const summary = triggerSummary(trigger, t, language);
        return (
          <li key={trigger.name} className="flex min-w-0 items-center gap-2 text-13 text-description" title={summary}>
            <Icon className="size-3.5 shrink-0 text-muted" strokeWidth={1.75} aria-label={t(`workflow.trigger.kinds.${trigger.kind}`)} />
            <span className="min-w-0 truncate">{summary}</span>
          </li>
        );
      })}
    </ul>
  );
}

function CopyButton({ value, label }: { value: string; label: string }) {
  const { t } = useTranslation();
  const [copied, setCopied] = useState(false);
  return (
    <Tooltip content={copied ? t("workflow.trigger.copied") : label}>
      <Button
        variant="quiet"
        size="icon-sm"
        aria-label={label}
        onClick={() => {
          void navigator.clipboard?.writeText(value).then(() => {
            setCopied(true);
            window.setTimeout(() => setCopied(false), 1500);
          });
        }}
      >
        {copied ? <Check /> : <Copy />}
      </Button>
    </Tooltip>
  );
}

function Detail({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div className="grid min-w-0 grid-cols-1 items-baseline gap-0.5 py-1.5 sm:grid-cols-[7.5rem_minmax(0,1fr)] sm:gap-3">
      <dt className="text-12 text-muted">{label}</dt>
      <dd className="min-w-0 text-13 text-fg">{children}</dd>
    </div>
  );
}

function Chips({ items }: { items: string[] }) {
  return (
    <span className="flex flex-wrap gap-1">
      {items.map((item) => (
        <Badge key={item}>{item}</Badge>
      ))}
    </span>
  );
}

/** Every trigger of the published version with its full declaration. */
export function TriggerCards({
  triggers,
  onRun
}: {
  triggers: WorkflowTrigger[];
  /** Starts a manual trigger; absent when the workflow cannot be run. */
  onRun?: () => void;
}) {
  return (
    <div className="flex flex-col gap-3">
      {triggers.map((trigger) => (
        <TriggerCard key={trigger.name} trigger={trigger} onRun={onRun} />
      ))}
    </div>
  );
}

function TriggerCard({ trigger, onRun }: { trigger: WorkflowTrigger; onRun?: () => void }) {
  const { t, i18n } = useTranslation();
  const language = normalizeLanguage(i18n.resolvedLanguage ?? i18n.language);
  const Icon = triggerIcons[trigger.kind];
  return (
    <Card role="group" aria-label={t("workflow.trigger.cardLabel", { name: trigger.name })} className="overflow-hidden">
      <div className="flex items-center gap-3 px-4 py-3">
        <span className="flex size-8 shrink-0 items-center justify-center rounded-inner bg-chip text-description" aria-hidden>
          <Icon className="size-4" strokeWidth={1.75} />
        </span>
        <span className="min-w-0 flex-1">
          <span className="block truncate text-14 font-medium text-fg">{triggerSummary(trigger, t, language)}</span>
          <span className="flex items-center gap-1.5 text-12 text-muted">
            {t(`workflow.trigger.kinds.${trigger.kind}`)}
            <span aria-hidden>·</span>
            <code className="font-mono">{trigger.name}</code>
          </span>
        </span>
        {trigger.kind === "manual" && onRun && (
          <Button size="sm" onClick={onRun}>
            <Play aria-hidden />
            {t("workflow.run.action")}
          </Button>
        )}
      </div>
      <dl className="border-t border-border-soft px-4 py-2">
        {trigger.kind === "schedule" && (
          <>
            <Detail label={t("workflow.trigger.schedule")}>{describeCron(trigger.cron, t, language)}</Detail>
            <Detail label={t("workflow.trigger.cron")}>
              <code className="font-mono text-12">{trigger.cron}</code>
            </Detail>
            <Detail label={t("workflow.trigger.timezone")}>{trigger.timezone || "UTC"}</Detail>
          </>
        )}
        {trigger.kind === "recordChanged" && (
          <>
            <Detail label={t("workflow.trigger.table")}>{trigger.table}</Detail>
            <Detail label={t("workflow.trigger.operations")}>
              <Chips items={(trigger.operations ?? ["create", "update", "delete"]).map((operation) => t(`workflow.trigger.operation.${operation}`))} />
            </Detail>
            <Detail label={t("workflow.trigger.fields")}>
              {trigger.fields?.length ? <Chips items={trigger.fields} /> : <span className="text-description">{t("workflow.trigger.anyField")}</span>}
            </Detail>
            <Detail label={t("workflow.trigger.ownWrites")}>
              {trigger.includeOwnWrites ? t("workflow.trigger.ownWritesIncluded") : t("workflow.trigger.ownWritesIgnored")}
            </Detail>
          </>
        )}
        {trigger.kind === "webhook" && (
          <>
            <Detail label={t("workflow.trigger.url")}>
              <span className="flex min-w-0 items-center gap-1">
                <code className="min-w-0 break-all font-mono text-12 text-description">{webhookAddress(trigger)}</code>
                <CopyButton value={webhookAddress(trigger)} label={t("workflow.trigger.copyURL")} />
              </span>
            </Detail>
            <Detail label={t("workflow.trigger.methods")}>{t("workflow.trigger.anyMethod")}</Detail>
          </>
        )}
        {trigger.kind === "manual" && (
          <Detail label={t("workflow.trigger.inputs")}>
            {trigger.fields?.length ? (
              <ul className="flex flex-col gap-1">
                {trigger.fields.map((field) => (
                  <li key={field.name} className="flex flex-wrap items-center gap-2">
                    <span>{field.label || field.name}</span>
                    {field.label && <code className="font-mono text-12 text-subtle">{field.name}</code>}
                    <Badge>{t(`workflow.trigger.fieldType.${field.type}`)}</Badge>
                  </li>
                ))}
              </ul>
            ) : (
              <span className="text-description">{t("workflow.trigger.jsonInput")}</span>
            )}
          </Detail>
        )}
      </dl>
    </Card>
  );
}
