import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { renderHtmlToPdfFile, usedCharacters } from "./formPdf";

const htmlToPdfmake = vi.fn();
const addVirtualFileSystem = vi.fn();
const addFonts = vi.fn();
const createPdf = vi.fn();

vi.mock("html-to-pdfmake", () => ({ default: (...args: unknown[]) => htmlToPdfmake(...args) }));
vi.mock("pdfmake/build/pdfmake", () => ({
  default: {
    addVirtualFileSystem: (...args: unknown[]) => addVirtualFileSystem(...args),
    addFonts: (...args: unknown[]) => addFonts(...args),
    createPdf: (...args: unknown[]) => createPdf(...args)
  }
}));

// Stands in for the subsetter: records the code points it was asked to keep and
// hands back a marker "font" so the pdfmake call can be checked.
const SUBSET_BYTES = new Uint8Array([1, 2, 3, 4]);
let requestedCodePoints: number[] = [];

function fakeHarfbuzz() {
  const memory = new WebAssembly.Memory({ initial: 2 });
  const heap = new Uint8Array(memory.buffer);
  heap.set(SUBSET_BYTES, 64);
  return {
    memory,
    malloc: () => 1024,
    free: vi.fn(),
    hb_blob_create: () => 1,
    hb_blob_destroy: vi.fn(),
    hb_blob_get_data: () => 64,
    hb_blob_get_length: () => SUBSET_BYTES.length,
    hb_face_create: () => 2,
    hb_face_destroy: vi.fn(),
    hb_face_reference_blob: () => 3,
    hb_set_add: (_set: number, codePoint: number) => requestedCodePoints.push(codePoint),
    hb_subset_input_create_or_fail: () => 4,
    hb_subset_input_destroy: vi.fn(),
    hb_subset_input_unicode_set: () => 5,
    hb_subset_or_fail: () => 6
  };
}

beforeEach(() => {
  requestedCodePoints = [];
  htmlToPdfmake.mockReturnValue([{ text: "converted" }]);
  createPdf.mockReturnValue({ getBlob: async () => new Blob(["%PDF-1.3"], { type: "application/pdf" }) });
  vi.stubGlobal("fetch", vi.fn(async () => new Response(new Uint8Array([9, 9, 9]))));
  vi.stubGlobal("WebAssembly", {
    Memory: WebAssembly.Memory,
    instantiate: async () => ({ instance: { exports: fakeHarfbuzz() } })
  });
});

afterEach(() => {
  vi.unstubAllGlobals();
  vi.clearAllMocks();
});

describe("usedCharacters", () => {
  it("collects the rendered text, not the markup", () => {
    const characters = usedCharacters('<table><tr><td style="color:red">合计</td><td>12</td></tr></table>');
    expect([...characters].sort().join("")).toBe("12合计");
  });

  it("deduplicates and keeps spaces", () => {
    expect(usedCharacters("<div>a a b</div>")).toBe("a b");
  });
});

describe("renderHtmlToPdfFile", () => {
  it("subsets the bundled font to the characters the document renders", async () => {
    await renderHtmlToPdfFile({ html: "<div>合计 12</div>", name: "月度报表" });

    const requested = String.fromCodePoint(...requestedCodePoints);
    expect([...requested].sort().join("")).toBe(" 12合计");
    // The whole font is never handed to pdfmake.
    expect(addVirtualFileSystem).toHaveBeenCalledWith({ "document.ttf": btoa("\x01\x02\x03\x04") });
    expect(addFonts).toHaveBeenCalledWith({
      Document: expect.objectContaining({ normal: "document.ttf", bold: "document.ttf" })
    });
  });

  it("typesets the converted html at A4 with the bundled font", async () => {
    const file = await renderHtmlToPdfFile({ html: "<div>合计</div>", name: "月度报表" });

    expect(htmlToPdfmake).toHaveBeenCalledWith(
      "<div>合计</div>",
      expect.objectContaining({ tableAutoSize: true, ignoreStyles: ["font-family"] })
    );
    expect(createPdf).toHaveBeenCalledWith(
      expect.objectContaining({
        content: [{ text: "converted" }],
        pageSize: "A4",
        pageOrientation: "portrait",
        defaultStyle: expect.objectContaining({ font: "Document" })
      })
    );
    expect(file.name).toBe("月度报表.pdf");
    expect(file.type).toBe("application/pdf");
  });

  it("honours orientation and margins", async () => {
    await renderHtmlToPdfFile({ html: "<div>x</div>", orientation: "landscape", marginMm: 25.4 });
    const definition = createPdf.mock.calls[0][0] as { pageOrientation: string; pageMargins: number[] };
    expect(definition.pageOrientation).toBe("landscape");
    expect(definition.pageMargins.every((value) => Math.abs(value - 72) < 0.01)).toBe(true);
  });

  it("rejects empty html before loading anything", async () => {
    await expect(renderHtmlToPdfFile({ html: "   " })).rejects.toThrow("pdf requires html");
    expect(fetch).not.toHaveBeenCalled();
  });

  it("fails loudly when a bundled artefact is missing, and retries after", async () => {
    // The loaders memoise, so start from a clean module to exercise a first load.
    vi.resetModules();
    const { renderHtmlToPdfFile: render } = await import("./formPdf");
    vi.stubGlobal("fetch", vi.fn(async () => new Response("", { status: 404 })));
    await expect(render({ html: "<div>x</div>" })).rejects.toThrow("could not be loaded");

    // A failed load is not remembered: the next attempt reaches the network again.
    const retry = vi.fn(async () => new Response(new Uint8Array([9, 9, 9])));
    vi.stubGlobal("fetch", retry);
    await render({ html: "<div>x</div>" });
    expect(retry).toHaveBeenCalled();
  });
});
