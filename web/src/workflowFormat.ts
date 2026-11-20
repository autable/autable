import type { TFunction } from "i18next";
import type { WorkflowDefinition, WorkflowManualField, WorkflowManualTrigger, WorkflowRun, WorkflowRunStatus, WorkflowTrigger } from "./api";
import type { StatusTone } from "./ui";

// Presentation helpers for workflows v2: trigger summaries, run status and
// timing, and the manual-run input form.

export const runStatuses: WorkflowRunStatus[] = ["queued", "running", "succeeded", "failed", "cancelled"];

export function runStatusTone(status: WorkflowRunStatus): StatusTone {
  switch (status) {
    case "queued":
      return "awaiting";
    case "running":
      return "running";
    case "succeeded":
      return "success";
    case "failed":
      return "error";
    default:
      return "neutral";
  }
}

/** Queued and running runs still change; everything else is final. */
export function isActiveRun(status: WorkflowRunStatus): boolean {
  return status === "queued" || status === "running";
}

/** How long a run took, or has been running so far; undefined before it starts. */
export function runDuration(run: Pick<WorkflowRun, "started_at" | "finished_at">, now = Date.now()): number | undefined {
  if (!run.started_at) {
    return undefined;
  }
  return Math.max(0, (run.finished_at || now) - run.started_at);
}

export function formatDuration(ms: number): string {
  if (ms < 1000) {
    return `${Math.round(ms)} ms`;
  }
  if (ms < 60_000) {
    return `${(ms / 1000).toFixed(ms < 10_000 ? 2 : 1)} s`;
  }
  const seconds = Math.round(ms / 1000);
  if (seconds < 3600) {
    return `${Math.floor(seconds / 60)}m ${String(seconds % 60).padStart(2, "0")}s`;
  }
  return `${Math.floor(seconds / 3600)}h ${String(Math.floor((seconds % 3600) / 60)).padStart(2, "0")}m`;
}

export function formatTimestamp(timestamp: number | undefined): string {
  if (!timestamp) {
    return "";
  }
  const parsed = new Date(timestamp);
  return Number.isNaN(parsed.getTime()) ? String(timestamp) : parsed.toLocaleString(undefined, { dateStyle: "short", timeStyle: "medium" });
}

/** A log line's time: the clock time with milliseconds. */
export function formatLogTime(timestamp: number): string {
  const parsed = new Date(timestamp);
  if (Number.isNaN(parsed.getTime())) {
    return String(timestamp);
  }
  const pad = (value: number, size = 2) => String(value).padStart(size, "0");
  return `${pad(parsed.getHours())}:${pad(parsed.getMinutes())}:${pad(parsed.getSeconds())}.${pad(parsed.getMilliseconds(), 3)}`;
}

/** "3 min ago" style, for last-run columns. */
export function formatRelativeTime(timestamp: number, language: string, now = Date.now()): string {
  const seconds = Math.round((timestamp - now) / 1000);
  const format = new Intl.RelativeTimeFormat(language, { numeric: "auto" });
  const abs = Math.abs(seconds);
  if (abs < 60) {
    return format.format(seconds, "second");
  }
  if (abs < 3600) {
    return format.format(Math.round(seconds / 60), "minute");
  }
  if (abs < 86_400) {
    return format.format(Math.round(seconds / 3600), "hour");
  }
  return format.format(Math.round(seconds / 86_400), "day");
}

export function formatJSON(value: unknown): string {
  if (value === undefined) {
    return "";
  }
  return JSON.stringify(value, null, 2) ?? "";
}

const dayNames: Record<string, number> = { SUN: 0, MON: 1, TUE: 2, WED: 3, THU: 4, FRI: 5, SAT: 6 };

function weekdayNumber(text: string): number | undefined {
  const upper = text.toUpperCase();
  if (upper in dayNames) {
    return dayNames[upper];
  }
  if (/^[0-7]$/.test(text)) {
    return Number(text) % 7;
  }
  return undefined;
}

/** The days a day-of-week field names, or undefined for steps and other forms. */
function weekdays(field: string): number[] | undefined {
  const days = new Set<number>();
  for (const part of field.split(",")) {
    const range = part.split("-");
    if (range.length === 1) {
      const day = weekdayNumber(part);
      if (day === undefined) return undefined;
      days.add(day);
      continue;
    }
    const start = weekdayNumber(range[0]);
    const end = range[1] === "7" ? 7 : weekdayNumber(range[1]);
    if (range.length !== 2 || start === undefined || end === undefined || end < start) return undefined;
    for (let day = start; day <= end; day += 1) days.add(day % 7);
  }
  return [...days].sort((left, right) => left - right);
}

function weekdayName(day: number, language: string, style: "long" | "short"): string {
  // 2024-01-07 was a Sunday.
  return new Intl.DateTimeFormat(language, { weekday: style, timeZone: "UTC" }).format(new Date(Date.UTC(2024, 0, 7 + day)));
}

const isNumber = (field: string) => /^\d+$/.test(field);

/**
 * A cron expression in words for the common shapes ("Every weekday 09:00"),
 * falling back to the expression itself.
 */
export function describeCron(cron: string, t: TFunction, language: string): string {
  const fields = cron.trim().split(/\s+/);
  if (fields.length !== 5) {
    return t("workflow.cron.custom", { cron });
  }
  const [minute, hour, dayOfMonth, month, dayOfWeek] = fields;
  const restAny = dayOfMonth === "*" && month === "*" && dayOfWeek === "*";
  if (restAny && hour === "*") {
    if (minute === "*") {
      return t("workflow.cron.everyMinute");
    }
    const step = /^\*\/(\d+)$/.exec(minute);
    if (step) {
      return t("workflow.cron.everyNMinutes", { count: Number(step[1]) });
    }
    if (isNumber(minute)) {
      return t("workflow.cron.hourly", { minute: minute.padStart(2, "0") });
    }
  }
  if (!isNumber(minute) || !isNumber(hour) || month !== "*") {
    return t("workflow.cron.custom", { cron });
  }
  const time = `${hour.padStart(2, "0")}:${minute.padStart(2, "0")}`;
  if (dayOfMonth === "*" && dayOfWeek === "*") {
    return t("workflow.cron.daily", { time });
  }
  if (dayOfWeek === "*" && isNumber(dayOfMonth)) {
    return t("workflow.cron.monthly", { day: Number(dayOfMonth), time });
  }
  if (dayOfMonth !== "*") {
    return t("workflow.cron.custom", { cron });
  }
  const days = weekdays(dayOfWeek);
  if (!days || days.length === 0) {
    return t("workflow.cron.custom", { cron });
  }
  if (days.join(",") === "1,2,3,4,5") {
    return t("workflow.cron.weekdays", { time });
  }
  if (days.join(",") === "0,6") {
    return t("workflow.cron.weekends", { time });
  }
  if (days.length === 7) {
    return t("workflow.cron.daily", { time });
  }
  const style = days.length === 1 ? "long" : "short";
  const names = days.map((day) => weekdayName(day, language, style));
  return t("workflow.cron.weekly", { days: new Intl.ListFormat(language, { type: "conjunction", style: "narrow" }).format(names), time });
}

/** One line per trigger for lists: "Every weekday 09:00 (Asia/Shanghai)", "Webhook /approval". */
export function triggerSummary(trigger: WorkflowTrigger, t: TFunction, language: string): string {
  switch (trigger.kind) {
    case "schedule":
      return t("workflow.trigger.scheduleSummary", {
        schedule: describeCron(trigger.cron, t, language),
        timezone: trigger.timezone || "UTC"
      });
    case "recordChanged":
      return trigger.fields?.length
        ? t("workflow.trigger.changeFieldsSummary", { table: trigger.table, fields: trigger.fields.join(", ") })
        : t("workflow.trigger.changeSummary", { table: trigger.table });
    case "webhook":
      return t("workflow.trigger.webhookSummary", { path: trigger.path });
    case "manual":
      return t("workflow.trigger.manualSummary");
  }
}

/** The address a webhook trigger answers on, absolute for copying. */
export function webhookAddress(trigger: Extract<WorkflowTrigger, { kind: "webhook" }>, origin = window.location.origin): string {
  return trigger.url ? `${origin}${trigger.url}` : "";
}

export function manualTriggers(workflow: Pick<WorkflowDefinition, "triggers">): WorkflowManualTrigger[] {
  return (workflow.triggers ?? []).filter((trigger): trigger is WorkflowManualTrigger => trigger.kind === "manual");
}

export type ManualFormValues = Record<string, string | boolean>;

export function initialManualValues(fields: WorkflowManualField[]): ManualFormValues {
  return Object.fromEntries(fields.map((field) => [field.name, field.type === "boolean" ? false : ""]));
}

/**
 * The input object for a manual run from the generated form. Empty text and
 * number inputs are left out; numbers must parse.
 */
export function manualInputFromForm(
  fields: WorkflowManualField[],
  values: ManualFormValues
): { input: Record<string, unknown>; errors: Record<string, "number"> } {
  const input: Record<string, unknown> = {};
  const errors: Record<string, "number"> = {};
  for (const field of fields) {
    const value = values[field.name];
    if (field.type === "boolean") {
      input[field.name] = value === true;
      continue;
    }
    const text = typeof value === "string" ? value : "";
    if (text.trim() === "") {
      continue;
    }
    if (field.type === "number") {
      const parsed = Number(text);
      if (!Number.isFinite(parsed)) {
        errors[field.name] = "number";
        continue;
      }
      input[field.name] = parsed;
      continue;
    }
    input[field.name] = text;
  }
  return { input, errors };
}

/** The input object from the JSON box shown when a manual trigger declares no fields. */
export function manualInputFromJSON(text: string): { input?: Record<string, unknown>; error?: "json" | "object" } {
  if (text.trim() === "") {
    return { input: {} };
  }
  let parsed: unknown;
  try {
    parsed = JSON.parse(text);
  } catch {
    return { error: "json" };
  }
  if (parsed === null || typeof parsed !== "object" || Array.isArray(parsed)) {
    return { error: "object" };
  }
  return { input: parsed as Record<string, unknown> };
}
