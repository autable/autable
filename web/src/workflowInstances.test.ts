import { describe, expect, it } from "vitest";
import { evaluateWorkflowInstances, evaluateWorkflowTrigger } from "./workflowInstances";

describe("workflowInstances", () => {
  it("evaluates workflow instances locally from script", () => {
    const result = evaluateWorkflowInstances(
      `function instances(info) {
        return {
          primary_sender: {
            node: "message.send",
            variables: [{ name: "channel", type: "string" }],
            secrets: [{ name: "token", type: "string" }],
            params: { database: info.database_name }
          },
          fallback_sender: "message.send"
        };
      }
      function run(info) { return info.instance("primary_sender").exec({}); }`,
      { workflow_id: 7, database_name: "workspace" }
    );

    expect(result.ok).toBe(true);
    if (!result.ok) {
      throw new Error(result.error);
    }
    expect(result.value.primary_sender.node).toBe("message.send");
    expect(result.value.primary_sender.variables?.[0].name).toBe("channel");
    expect(result.value.primary_sender.secrets?.[0].name).toBe("token");
    expect(result.value.primary_sender.params?.database).toBe("workspace");
    expect(result.value.fallback_sender.node).toBe("message.send");
  });

  it("returns an error when instances is missing", () => {
    const result = evaluateWorkflowInstances("function run() { return {}; }", { database_name: "workspace" });

    expect(result.ok).toBe(false);
    if (!result.ok) {
      expect(result.error).toContain("instances");
    }
  });

  it("evaluates workflow trigger locally from script", () => {
    const result = evaluateWorkflowTrigger(
      `function trigger(info) {
        return { instance: "row_change", params: { table: info.database_name } };
      }`,
      { database_name: "workspace" }
    );

    expect(result.ok).toBe(true);
    if (!result.ok) {
      throw new Error(result.error);
    }
    expect(result.value?.instance).toBe("row_change");
    expect(result.value?.params?.table).toBe("workspace");
  });

  it("allows workflows without triggers", () => {
    const result = evaluateWorkflowTrigger("function run() { return {}; }", { database_name: "workspace" });

    expect(result).toEqual({ ok: true, value: null });
  });
});
