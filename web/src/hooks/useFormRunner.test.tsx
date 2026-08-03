import { act, renderHook } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { useFormRunner } from "./useFormRunner";

const uploadFile = vi.fn();
const renderHtmlToPdfFile = vi.fn();

vi.mock("../api", async (importOriginal) => ({
  ...(await importOriginal<typeof import("../api")>()),
  uploadFile: (...args: unknown[]) => uploadFile(...args)
}));

vi.mock("../formPdf", () => ({
  renderHtmlToPdfFile: (...args: unknown[]) => renderHtmlToPdfFile(...args)
}));

const script = `
function render(api, root) {
  root.append(
    api.input({ field: "备注", label: "备注" }),
    api.file({ field: "附件", label: "附件" }),
    api.button("check", (formAPI) => formAPI.values())
  );
  return { table: "记录表" };
}
`;

const pdfScript = `
function render(api, root) {
  root.append(
    api.button("print", async (formAPI) => {
      return formAPI.pdf({ html: "<div>报表</div>", name: "月度报表", record_id: 7 });
    }),
    api.button("printElsewhere", async (formAPI) => {
      return formAPI.pdf({ html: "<div>报表</div>", table: "附件表" });
    })
  );
  return { table: "记录表" };
}
`;

describe("useFormRunner", () => {
  it("includes file element values in action values", async () => {
    const { result } = renderHook(() =>
      useFormRunner({ databaseName: "db", script, onStatus: () => undefined })
    );

    act(() => {
      result.current.updateValue("附件", "12");
      result.current.updateValue("备注", "x");
    });
    await act(async () => {
      await result.current.execute("button_1");
    });

    expect(result.current.result).toEqual({ 备注: "x", 附件: "12" });
  });

  it("uploads a generated pdf against the form table and returns the stored file", async () => {
    const file = new File(["%PDF"], "月度报表.pdf", { type: "application/pdf" });
    renderHtmlToPdfFile.mockResolvedValue(file);
    uploadFile.mockResolvedValue({ id: 31, name: "月度报表.pdf", size: 4 });

    const { result } = renderHook(() =>
      useFormRunner({ databaseName: "db", script: pdfScript, onStatus: () => undefined })
    );
    await act(async () => {
      await result.current.execute("button_1");
    });

    expect(renderHtmlToPdfFile).toHaveBeenCalledWith(
      expect.objectContaining({ html: "<div>报表</div>", name: "月度报表", record_id: 7 })
    );
    expect(uploadFile).toHaveBeenCalledWith(file, "db", "记录表", 7);
    expect(result.current.result).toEqual({ id: 31, name: "月度报表.pdf", size: 4 });
  });

  it("files the pdf against an explicit table with no record", async () => {
    renderHtmlToPdfFile.mockResolvedValue(new File(["%PDF"], "document.pdf"));
    uploadFile.mockResolvedValue({ id: 32, name: "document.pdf", size: 4 });

    const { result } = renderHook(() =>
      useFormRunner({ databaseName: "db", script: pdfScript, onStatus: () => undefined })
    );
    await act(async () => {
      await result.current.execute("button_2");
    });

    expect(uploadFile).toHaveBeenCalledWith(expect.any(File), "db", "附件表", 0);
  });
});
