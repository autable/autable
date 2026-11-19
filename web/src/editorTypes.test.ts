import { describe, expect, it } from "vitest";
import { workflowEditorExtraLibs } from "./editorTypes";
import type { WorkflowNodeInfo } from "./api";

const workflowNodes: WorkflowNodeInfo[] = [
  {
    type: "table.record.changed",
    display_name: "Record changed",
    inputs: [{ name: "table", type: "string" }],
    outputs: [
      { name: "record_id", type: "int64" },
      { name: "record", type: "TriggerRecord" },
      { name: "values", type: "object" }
    ],
    stateless: true,
    trigger: true
  },
  {
    type: "dingtalk.robot.send",
    display_name: "DingTalk robot",
    inputs: [
      { name: "content", type: "string" },
      { name: "at_user_ids", type: "string[]" }
    ],
    outputs: [
      { name: "status_code", type: "int" },
      { name: "errmsg", type: "string" }
    ],
    stateless: true,
    trigger: false
  }
];

describe("editorTypes", () => {
  it("generates workflow node and instance declarations from node metadata", () => {
    const libs = workflowEditorExtraLibs({
      workflowNodes,
      workflowInstances: {
        row_change: { node: "table.record.changed" },
        send: { node: "dingtalk.robot.send" }
      },
      workflowTrigger: { instance: "row_change" }
    });
    const content = libs.map((lib) => lib.content).join("\n");

    expect(content).toContain("interface AutableNodeDingtalkRobotSendInput");
    expect(content).toContain("content?: string;");
    expect(content).toContain("at_user_ids?: string[];");
    expect(content).toContain('instance(id: "send"): AutableWorkflowInstance<AutableNodeDingtalkRobotSendInput, AutableNodeDingtalkRobotSendOutput>');
    expect(content).toContain("interface AutableWorkflowRunInputs extends AutableNodeTableRecordChangedOutput");
    expect(content).toContain("record?: AutableTriggerRecord;");
    expect(content).toContain("function stableStringify(value: unknown): string;");
  });
});
