export type FormPdfOptions = {
  html: string;
  name?: string;
  orientation?: "portrait" | "landscape";
  marginMm?: number;
};

const FONT_URL = "/pdf/NotoSansSC-Regular.ttf";
const SUBSETTER_URL = "/pdf/harfbuzz-subset.wasm";
const DEFAULT_MARGIN_MM = 10;
const MM_TO_PT = 72 / 25.4;

type Harfbuzz = {
  memory: WebAssembly.Memory;
  malloc: (size: number) => number;
  free: (pointer: number) => void;
  hb_blob_create: (data: number, length: number, mode: number, userData: number, destroy: number) => number;
  hb_blob_destroy: (blob: number) => void;
  hb_blob_get_data: (blob: number, length: number) => number;
  hb_blob_get_length: (blob: number) => number;
  hb_face_create: (blob: number, index: number) => number;
  hb_face_destroy: (face: number) => void;
  hb_face_reference_blob: (face: number) => number;
  hb_set_add: (set: number, codePoint: number) => void;
  hb_subset_input_create_or_fail: () => number;
  hb_subset_input_destroy: (input: number) => void;
  hb_subset_input_unicode_set: (input: number) => number;
  hb_subset_or_fail: (face: number, input: number) => number;
};

let harfbuzzPromise: Promise<Harfbuzz> | undefined;
let fontPromise: Promise<Uint8Array> | undefined;

// The document is typeset rather than rasterised, so the text stays selectable
// and searchable. That needs the glyphs a viewer cannot be assumed to have, so
// the bundled font is cut down to the characters this document actually uses —
// a few dozen KB instead of the 10 MB the whole font would add to every file.
export async function renderHtmlToPdfFile(options: FormPdfOptions): Promise<File> {
  const html = String(options.html ?? "");
  if (!html.trim()) {
    throw new Error("pdf requires html");
  }

  const [{ default: htmlToPdfmake }, { default: pdfMake }, fontSubset] = await Promise.all([
    import("html-to-pdfmake"),
    import("pdfmake/build/pdfmake"),
    subsetFontFor(usedCharacters(html))
  ]);

  const runtime = pdfMake as unknown as {
    addVirtualFileSystem: (files: Record<string, string>) => void;
    addFonts: (fonts: Record<string, Record<string, string>>) => void;
    createPdf: (definition: unknown) => { getBlob: () => Promise<Blob> };
  };
  runtime.addVirtualFileSystem({ "document.ttf": base64(fontSubset) });
  runtime.addFonts({
    Document: { normal: "document.ttf", bold: "document.ttf", italics: "document.ttf", bolditalics: "document.ttf" }
  });

  const margin = positiveNumber(options.marginMm, DEFAULT_MARGIN_MM) * MM_TO_PT;
  const blob = await runtime
    .createPdf({
      // font-family from the html would be looked up as a pdfmake font name.
      content: htmlToPdfmake(html, { window, tableAutoSize: true, ignoreStyles: ["font-family"] }),
      defaultStyle: { font: "Document", fontSize: 9 },
      pageSize: "A4",
      pageOrientation: options.orientation ?? "portrait",
      pageMargins: [margin, margin, margin, margin]
    })
    .getBlob();

  const name = String(options.name ?? "document.pdf");
  return new File([blob], name.endsWith(".pdf") ? name : `${name}.pdf`, { type: "application/pdf" });
}

// Callers never declare their glyphs: the rendered text is the source of truth.
export function usedCharacters(html: string): string {
  const host = document.createElement("div");
  host.innerHTML = html;
  return [...new Set(host.textContent ?? "")].join("");
}

async function subsetFontFor(characters: string): Promise<Uint8Array> {
  const [harfbuzz, font] = await Promise.all([loadHarfbuzz(), loadFont()]);
  const fontPointer = harfbuzz.malloc(font.length);
  new Uint8Array(harfbuzz.memory.buffer).set(font, fontPointer);
  const blob = harfbuzz.hb_blob_create(fontPointer, font.length, 2, 0, 0);
  const face = harfbuzz.hb_face_create(blob, 0);
  const input = harfbuzz.hb_subset_input_create_or_fail();
  if (!input) {
    throw new Error("font subsetting could not start");
  }
  try {
    const unicodes = harfbuzz.hb_subset_input_unicode_set(input);
    for (const character of characters) {
      harfbuzz.hb_set_add(unicodes, character.codePointAt(0) as number);
    }
    const subsetFace = harfbuzz.hb_subset_or_fail(face, input);
    if (!subsetFace) {
      throw new Error("font subsetting failed");
    }
    const subsetBlob = harfbuzz.hb_face_reference_blob(subsetFace);
    const offset = harfbuzz.hb_blob_get_data(subsetBlob, 0);
    const length = harfbuzz.hb_blob_get_length(subsetBlob);
    // Copy out before the wasm heap is freed or grown.
    const subset = new Uint8Array(harfbuzz.memory.buffer.slice(offset, offset + length));
    harfbuzz.hb_blob_destroy(subsetBlob);
    harfbuzz.hb_face_destroy(subsetFace);
    return subset;
  } finally {
    harfbuzz.hb_subset_input_destroy(input);
    harfbuzz.hb_face_destroy(face);
    harfbuzz.hb_blob_destroy(blob);
    harfbuzz.free(fontPointer);
  }
}

function loadHarfbuzz(): Promise<Harfbuzz> {
  // A failed load must not be remembered, or one flaky fetch would disable
  // PDFs for the rest of the session.
  harfbuzzPromise ??= (async () => {
    const response = await fetch(SUBSETTER_URL);
    if (!response.ok) {
      throw new Error(`font subsetter could not be loaded: ${response.status}`);
    }
    const { instance } = await WebAssembly.instantiate(await response.arrayBuffer(), {});
    return instance.exports as unknown as Harfbuzz;
  })().catch((error) => {
    harfbuzzPromise = undefined;
    throw error;
  });
  return harfbuzzPromise;
}

function loadFont(): Promise<Uint8Array> {
  fontPromise ??= (async () => {
    const response = await fetch(FONT_URL);
    if (!response.ok) {
      throw new Error(`document font could not be loaded: ${response.status}`);
    }
    return new Uint8Array(await response.arrayBuffer());
  })().catch((error) => {
    fontPromise = undefined;
    throw error;
  });
  return fontPromise;
}

function base64(bytes: Uint8Array): string {
  let binary = "";
  const step = 8192;
  for (let offset = 0; offset < bytes.length; offset += step) {
    binary += String.fromCharCode(...bytes.subarray(offset, offset + step));
  }
  return btoa(binary);
}

function positiveNumber(value: unknown, fallback: number): number {
  const parsed = Number(value);
  return Number.isFinite(parsed) && parsed > 0 ? parsed : fallback;
}
