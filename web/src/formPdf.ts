export type FormPdfOptions = {
  html: string;
  name?: string;
  // CSS pixel width the html is laid out at before it is scaled onto the page.
  width?: number;
  // Device pixel ratio of the rasterised page; 2 keeps small print readable.
  scale?: number;
  orientation?: "portrait" | "landscape";
  marginMm?: number;
};

const DEFAULT_WIDTH = 820;
const DEFAULT_SCALE = 2;
const DEFAULT_MARGIN_MM = 10;

// Rasterising the DOM is what lets the PDF carry Chinese text without
// embedding a ~10 MB CJK font: the browser draws the glyphs and the PDF
// carries the picture. jsPDF and html2canvas together are ~1 MB, so they load
// on demand rather than with the app.
export async function renderHtmlToPdfFile(options: FormPdfOptions): Promise<File> {
  const html = String(options.html ?? "");
  if (!html.trim()) {
    throw new Error("pdf requires html");
  }
  const [{ default: html2canvas }, { jsPDF }] = await Promise.all([
    import("html2canvas-pro"),
    import("jspdf")
  ]);

  const width = positiveNumber(options.width, DEFAULT_WIDTH);
  const scale = positiveNumber(options.scale, DEFAULT_SCALE);
  const host = document.createElement("div");
  host.setAttribute("aria-hidden", "true");
  host.style.position = "fixed";
  host.style.top = "0";
  host.style.left = "-20000px";
  host.style.width = `${width}px`;
  host.style.background = "#ffffff";
  host.innerHTML = html;
  document.body.appendChild(host);

  try {
    const canvas = await html2canvas(host, { backgroundColor: "#ffffff", scale, width });
    const pdf = new jsPDF({ unit: "mm", format: "a4", orientation: options.orientation ?? "portrait" });
    const margin = positiveNumber(options.marginMm, DEFAULT_MARGIN_MM);
    const drawWidth = pdf.internal.pageSize.getWidth() - margin * 2;
    const drawHeight = pdf.internal.pageSize.getHeight() - margin * 2;
    // How many canvas pixels fit on one page once scaled to drawWidth.
    const pageHeightPx = Math.floor((canvas.width / drawWidth) * drawHeight);

    for (let offset = 0, page = 0; offset < canvas.height; offset += pageHeightPx, page += 1) {
      const sliceHeight = Math.min(pageHeightPx, canvas.height - offset);
      if (page > 0) {
        pdf.addPage();
      }
      pdf.addImage(
        sliceDataURL(canvas, offset, sliceHeight),
        "JPEG",
        margin,
        margin,
        drawWidth,
        (sliceHeight / canvas.width) * drawWidth
      );
    }

    const name = String(options.name ?? "document.pdf");
    return new File([pdf.output("blob")], name.endsWith(".pdf") ? name : `${name}.pdf`, {
      type: "application/pdf"
    });
  } finally {
    host.remove();
  }
}

// Each page carries only its own slice; re-adding the whole canvas per page
// would multiply the file size by the page count.
function sliceDataURL(canvas: HTMLCanvasElement, offset: number, height: number): string {
  if (offset === 0 && height === canvas.height) {
    return canvas.toDataURL("image/jpeg", 0.94);
  }
  const slice = document.createElement("canvas");
  slice.width = canvas.width;
  slice.height = height;
  const context = slice.getContext("2d");
  if (!context) {
    throw new Error("canvas 2d context is unavailable");
  }
  context.fillStyle = "#ffffff";
  context.fillRect(0, 0, slice.width, slice.height);
  context.drawImage(canvas, 0, offset, canvas.width, height, 0, 0, canvas.width, height);
  return slice.toDataURL("image/jpeg", 0.94);
}

function positiveNumber(value: unknown, fallback: number): number {
  const parsed = Number(value);
  return Number.isFinite(parsed) && parsed > 0 ? parsed : fallback;
}
