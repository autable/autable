import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { renderHtmlToPdfFile } from "./formPdf";

const addImage = vi.fn();
const addPage = vi.fn();
const html2canvas = vi.fn();

vi.mock("jspdf", () => ({
  jsPDF: class {
    internal = {
      pageSize: { getWidth: () => 210, getHeight: () => 297 }
    };
    addImage = addImage;
    addPage = addPage;
    output() {
      return new Blob(["%PDF-1.4"], { type: "application/pdf" });
    }
  }
}));

vi.mock("html2canvas-pro", () => ({ default: (...args: unknown[]) => html2canvas(...args) }));

function fakeCanvas(width: number, height: number) {
  return {
    width,
    height,
    toDataURL: () => `data:image/jpeg;base64,whole-${width}x${height}`,
    getContext: () => ({
      fillStyle: "",
      fillRect: vi.fn(),
      drawImage: vi.fn()
    })
  };
}

beforeEach(() => {
  addImage.mockClear();
  addPage.mockClear();
  html2canvas.mockReset();
  // Slice canvases are created through document.createElement.
  const originalCreate = document.createElement.bind(document);
  vi.spyOn(document, "createElement").mockImplementation((tag: string) => {
    if (tag === "canvas") {
      return fakeCanvas(0, 0) as unknown as HTMLCanvasElement;
    }
    return originalCreate(tag);
  });
});

afterEach(() => {
  vi.restoreAllMocks();
});

describe("renderHtmlToPdfFile", () => {
  it("rasterises off-screen html and returns a named pdf file", async () => {
    html2canvas.mockImplementation(async (element: HTMLElement) => {
      // The host must be in the document while html2canvas runs.
      expect(element.isConnected).toBe(true);
      expect(element.textContent).toContain("月度报表");
      expect(element.style.width).toBe("900px");
      return fakeCanvas(1800, 500);
    });

    const file = await renderHtmlToPdfFile({
      html: "<div>月度报表</div>",
      name: "月度报表",
      width: 900
    });

    expect(file.name).toBe("月度报表.pdf");
    expect(file.type).toBe("application/pdf");
    expect(html2canvas).toHaveBeenCalledWith(
      expect.anything(),
      expect.objectContaining({ backgroundColor: "#ffffff", scale: 2, width: 900 })
    );
    expect(addImage).toHaveBeenCalledTimes(1);
    expect(addPage).not.toHaveBeenCalled();
    // Nothing is left behind in the document.
    expect(document.body.querySelector("[aria-hidden='true']")).toBeNull();
  });

  it("splits a tall canvas across pages", async () => {
    // A4 portrait with 10mm margins fits 277mm of a 190mm-wide page, so a
    // canvas 3x taller than that ratio needs three pages.
    html2canvas.mockResolvedValue(fakeCanvas(1000, Math.ceil((1000 / 190) * 277 * 2.5)));

    await renderHtmlToPdfFile({ html: "<div>long</div>" });

    expect(addPage).toHaveBeenCalledTimes(2);
    expect(addImage).toHaveBeenCalledTimes(3);
  });

  it("removes the off-screen host when rendering fails", async () => {
    html2canvas.mockRejectedValue(new Error("boom"));
    await expect(renderHtmlToPdfFile({ html: "<div>x</div>" })).rejects.toThrow("boom");
    expect(document.body.querySelector("[aria-hidden='true']")).toBeNull();
  });

  it("rejects empty html", async () => {
    await expect(renderHtmlToPdfFile({ html: "   " })).rejects.toThrow("pdf requires html");
    expect(html2canvas).not.toHaveBeenCalled();
  });
});
