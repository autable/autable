import { act, renderHook } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { useFormRunner } from "./useFormRunner";

const script = `
function render(api, root) {
  root.append(
    api.input({ field: "备注", label: "备注" }),
    api.file({ field: "合同文件", label: "合同" }),
    api.button("check", (formAPI) => formAPI.values())
  );
  return { table: "合同表" };
}
`;

describe("useFormRunner", () => {
  it("includes file element values in action values", async () => {
    const { result } = renderHook(() =>
      useFormRunner({ databaseName: "db", script, onStatus: () => undefined })
    );

    act(() => {
      result.current.updateValue("合同文件", "12");
      result.current.updateValue("备注", "x");
    });
    await act(async () => {
      await result.current.execute("button_1");
    });

    expect(result.current.result).toEqual({ 备注: "x", 合同文件: "12" });
  });
});
