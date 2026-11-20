import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { WorkflowDefinition } from "../api";
import i18n from "../i18n";
import { TooltipProvider } from "../ui";
import { WorkflowRunDialog } from "./WorkflowRunDialog";

function workflow(triggers: WorkflowDefinition["triggers"]): WorkflowDefinition {
  return { id: 1, database_name: "ops", name: "order-sync", title: "Order sync", enabled: true, published_version: 1, triggers };
}

function renderDialog(target: WorkflowDefinition, props: Partial<Parameters<typeof WorkflowRunDialog>[0]> = {}) {
  render(
    <TooltipProvider>
      <WorkflowRunDialog databaseName="ops" workflow={target} open onOpenChange={vi.fn()} {...props} />
    </TooltipProvider>
  );
  return screen.getByRole("dialog", { name: "Run Order sync" });
}

let bodies: unknown[] = [];

beforeEach(async () => {
  vi.restoreAllMocks();
  bodies = [];
  await i18n.changeLanguage("en-US");
  vi.spyOn(globalThis, "fetch").mockImplementation(async (_input, init) => {
    const body = JSON.parse(String(init?.body));
    bodies.push(body);
    return new Response(
      JSON.stringify({ id: 31, workflow_id: 1, trigger: "start", status: "succeeded", output: { ok: true, input: body.input }, created_at: 1 }),
      { status: 200 }
    );
  });
});

describe("WorkflowRunDialog", () => {
  it("builds the form from the manual trigger's fields and shows the result", async () => {
    const onStarted = vi.fn();
    const dialog = renderDialog(
      workflow([
        {
          name: "start",
          kind: "manual",
          fields: [
            { name: "orderId", label: "Order", type: "number" },
            { name: "note", type: "string" },
            { name: "urgent", label: "Urgent", type: "boolean" }
          ]
        }
      ]),
      { onStarted }
    );
    const user = userEvent.setup();
    await user.type(within(dialog).getByLabelText("Order"), "forty");
    await user.click(within(dialog).getByRole("button", { name: "Start run" }));
    expect(within(dialog).getByText("Enter a number.")).toBeInTheDocument();
    expect(bodies).toHaveLength(0);

    await user.clear(within(dialog).getByLabelText("Order"));
    await user.type(within(dialog).getByLabelText("Order"), "42");
    await user.type(within(dialog).getByLabelText("note"), "rush");
    await user.click(within(dialog).getByRole("checkbox", { name: "Urgent" }));
    await user.click(within(dialog).getByRole("button", { name: "Start run" }));
    await waitFor(() => expect(bodies).toEqual([{ input: { orderId: 42, note: "rush", urgent: true }, wait: true }]));
    const result = await within(dialog).findByRole("region", { name: "Run result" });
    expect(within(result).getByText("Succeeded")).toBeInTheDocument();
    expect(within(result).getByLabelText("Output")).toHaveTextContent('"orderId": 42');
    expect(onStarted).toHaveBeenCalledWith(expect.objectContaining({ id: 31 }));
    expect(within(dialog).getByRole("button", { name: "Run again" })).toBeInTheDocument();
  });

  it("takes a JSON object when the trigger declares no fields", async () => {
    const dialog = renderDialog(workflow([{ name: "start", kind: "manual", fields: [] }]));
    const user = userEvent.setup();
    const input = within(dialog).getByLabelText("Input (JSON object)");
    await user.clear(input);
    await user.type(input, "[[1]");
    await user.click(within(dialog).getByRole("button", { name: "Start run" }));
    expect(within(dialog).getByText(/must be a JSON object/)).toBeInTheDocument();
    await user.clear(input);
    await user.type(input, '{{"limit": 5}');
    await user.click(within(dialog).getByRole("button", { name: "Start run" }));
    await waitFor(() => expect(bodies).toEqual([{ input: { limit: 5 }, wait: true }]));
  });

  it("names the trigger when several manual triggers exist, and offers admins the run page", async () => {
    const onOpenRun = vi.fn();
    const dialog = renderDialog(
      workflow([
        { name: "full", kind: "manual", fields: [] },
        { name: "single", kind: "manual", fields: [{ name: "id", type: "number" }] },
        { name: "hook", kind: "webhook", path: "x" }
      ]),
      { onOpenRun }
    );
    const user = userEvent.setup();
    const select = within(dialog).getByLabelText("Trigger");
    expect(Array.from((select as HTMLSelectElement).options).map((option) => option.value)).toEqual(["full", "single"]);
    await user.selectOptions(select, "single");
    await user.type(within(dialog).getByLabelText("id"), "7");
    await user.click(within(dialog).getByRole("button", { name: "Start run" }));
    await waitFor(() => expect(bodies).toEqual([{ trigger: "single", input: { id: 7 }, wait: true }]));
    await user.click(await within(dialog).findByRole("button", { name: "Open run" }));
    expect(onOpenRun).toHaveBeenCalledWith(expect.objectContaining({ id: 31 }));
  });

  it("explains why a run could not start", async () => {
    vi.mocked(fetch).mockImplementation(async () => new Response(JSON.stringify({ error: "workflow is disabled" }), { status: 409 }));
    const dialog = renderDialog(workflow([{ name: "start", kind: "manual" }]));
    await userEvent.click(within(dialog).getByRole("button", { name: "Start run" }));
    expect(await within(dialog).findByText("workflow is disabled")).toBeInTheDocument();
  });
});
