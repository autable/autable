import { describe, expect, it } from "vitest";
import i18n from "./i18n";
import {
  describeCron,
  formatDuration,
  manualInputFromForm,
  manualInputFromJSON,
  runDuration,
  runStatusTone,
  triggerSummary,
  webhookAddress
} from "./workflowFormat";

const t = i18n.getFixedT("en-US");
const zh = i18n.getFixedT("zh-CN");

describe("describeCron", () => {
  it("puts common schedules into words", () => {
    expect(describeCron("* * * * *", t, "en-US")).toBe("Every minute");
    expect(describeCron("*/10 * * * *", t, "en-US")).toBe("Every 10 minutes");
    expect(describeCron("5 * * * *", t, "en-US")).toBe("Every hour at :05");
    expect(describeCron("30 8 * * *", t, "en-US")).toBe("Every day 08:30");
    expect(describeCron("0 9 * * 1-5", t, "en-US")).toBe("Every weekday 09:00");
    expect(describeCron("0 9 * * MON-FRI", t, "en-US")).toBe("Every weekday 09:00");
    expect(describeCron("0 10 * * 0,6", t, "en-US")).toBe("Every weekend 10:00");
    expect(describeCron("0 7 * * 1", t, "en-US")).toBe("Every Monday 07:00");
    expect(describeCron("0 7 * * 1,3,5", t, "en-US")).toBe("Every Mon, Wed, Fri 07:00");
    expect(describeCron("15 6 1 * *", t, "en-US")).toBe("Monthly on day 1 06:15");
  });

  it("falls back to the expression for anything else", () => {
    expect(describeCron("0 9-17 * * *", t, "en-US")).toBe("Cron 0 9-17 * * *");
    expect(describeCron("0 9 1 1 *", t, "en-US")).toBe("Cron 0 9 1 1 *");
    expect(describeCron("0 9 * *", t, "en-US")).toBe("Cron 0 9 * *");
  });

  it("speaks Chinese", () => {
    expect(describeCron("0 9 * * 1-5", zh, "zh-CN")).toBe("每个工作日 09:00");
    expect(describeCron("0 7 * * 1", zh, "zh-CN")).toBe("每星期一 07:00");
  });
});

describe("triggerSummary", () => {
  it("summarizes each kind in one line", () => {
    expect(triggerSummary({ name: "daily", kind: "schedule", cron: "0 9 * * 1-5", timezone: "Asia/Shanghai" }, t, "en-US")).toBe(
      "Every weekday 09:00 (Asia/Shanghai)"
    );
    expect(triggerSummary({ name: "changed", kind: "recordChanged", table: "订单", fields: ["状态"] }, t, "en-US")).toBe("On change: 订单 · 状态");
    expect(triggerSummary({ name: "changed", kind: "recordChanged", table: "orders" }, t, "en-US")).toBe("On change: orders");
    expect(triggerSummary({ name: "hook", kind: "webhook", path: "approval" }, t, "en-US")).toBe("Webhook /approval");
    expect(triggerSummary({ name: "start", kind: "manual", fields: [] }, t, "en-US")).toBe("Manual");
  });

  it("builds absolute webhook addresses", () => {
    expect(webhookAddress({ name: "hook", kind: "webhook", path: "a/b", url: "/api/hooks/ops/sync/a/b" }, "https://example.com")).toBe(
      "https://example.com/api/hooks/ops/sync/a/b"
    );
  });
});

describe("runs", () => {
  it("maps statuses to tones and measures durations", () => {
    expect(runStatusTone("queued")).toBe("awaiting");
    expect(runStatusTone("running")).toBe("running");
    expect(runStatusTone("succeeded")).toBe("success");
    expect(runStatusTone("failed")).toBe("error");
    expect(runStatusTone("cancelled")).toBe("neutral");
    expect(runDuration({})).toBeUndefined();
    expect(runDuration({ started_at: 1000, finished_at: 3500 })).toBe(2500);
    expect(runDuration({ started_at: 1000 }, 1600)).toBe(600);
    expect(formatDuration(250)).toBe("250 ms");
    expect(formatDuration(2500)).toBe("2.50 s");
    expect(formatDuration(42_000)).toBe("42.0 s");
    expect(formatDuration(125_000)).toBe("2m 05s");
    expect(formatDuration(3_720_000)).toBe("1h 02m");
  });
});

describe("manual input", () => {
  const fields = [
    { name: "orderId", label: "Order", type: "number" as const },
    { name: "note", type: "string" as const },
    { name: "urgent", type: "boolean" as const }
  ];

  it("types the form values and leaves empty inputs out", () => {
    expect(manualInputFromForm(fields, { orderId: "42", note: "", urgent: true })).toEqual({ input: { orderId: 42, urgent: true }, errors: {} });
    expect(manualInputFromForm(fields, { orderId: "", note: "hi", urgent: false })).toEqual({ input: { note: "hi", urgent: false }, errors: {} });
    expect(manualInputFromForm(fields, { orderId: "forty", note: "", urgent: false }).errors).toEqual({ orderId: "number" });
  });

  it("accepts a JSON object only", () => {
    expect(manualInputFromJSON("")).toEqual({ input: {} });
    expect(manualInputFromJSON('{"a": 1}')).toEqual({ input: { a: 1 } });
    expect(manualInputFromJSON("{")).toEqual({ error: "json" });
    expect(manualInputFromJSON("[1]")).toEqual({ error: "object" });
    expect(manualInputFromJSON("3")).toEqual({ error: "object" });
  });
});
