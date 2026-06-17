import { describe, expect, it } from "vitest";
import { formEditorExtraLibs, workflowEditorExtraLibs } from "./editorTypes";
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

    expect(content).toContain("interface CodeTableNodeDingtalkRobotSendInput");
    expect(content).toContain("content?: string;");
    expect(content).toContain("at_user_ids?: string[];");
    expect(content).toContain('instance(id: "send"): CodeTableWorkflowInstance<CodeTableNodeDingtalkRobotSendInput, CodeTableNodeDingtalkRobotSendOutput>');
    expect(content).toContain("interface CodeTableWorkflowRunInputs extends CodeTableNodeTableRecordChangedOutput");
    expect(content).toContain("record?: CodeTableTriggerRecord;");
  });

  it("generates form runtime declarations", () => {
    const content = formEditorExtraLibs().map((lib) => lib.content).join("\n");

    expect(content).toContain("interface CodeTableFormAPI");
    expect(content).toContain("function render(api: CodeTableFormAPI, root: CodeTableFormRoot): CodeTableFormDefinition");
  });
});
