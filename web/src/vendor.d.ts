// pdfmake and html-to-pdfmake ship no types; the PDF module wraps both behind
// its own narrow surface.
declare module "html-to-pdfmake" {
  const htmlToPdfmake: (
    html: string,
    options?: { window?: Window; tableAutoSize?: boolean; ignoreStyles?: string[] }
  ) => unknown;
  export default htmlToPdfmake;
}

declare module "pdfmake/build/pdfmake" {
  const pdfMake: unknown;
  export default pdfMake;
}
