import { gzipSync } from "node:zlib";

// Builds the gzip-compressed tar that `autablectl app push` uploads (see
// docs/design/apps.md), so specs can create hosted apps through the real API
// without the CLI or a frontend build.

export type AppBundleOptions = {
  database: string;
  name: string;
  title: string;
  // Text the page shows, so a spec can tell versions apart.
  heading: string;
  sdk?: string;
};

export function appBundle({ database, name, title, heading, sdk = "1.0" }: AppBundleOptions): Buffer {
  const manifest = JSON.stringify({ database, name, title, sdk });
  return tarGzip({
    "autable.app.json": manifest,
    "dist/index.html": [
      "<!doctype html>",
      '<html><head><meta charset="utf-8"><title>' + title + "</title>",
      // Relative like a Vite build with base "./"; the server's <base> makes it
      // resolve against the app root in published and preview URLs.
      '<script type="module" src="./assets/app.js"></script></head>',
      "<body><h1>" + heading + '</h1><p id="status">loading</p></body></html>',
      ""
    ].join("\n"),
    "dist/assets/app.js": `document.getElementById("status").textContent = "script ran: ${heading}";\n`,
    "source/autable.app.json": manifest,
    "source/src/main.ts": `export const heading = ${JSON.stringify(heading)};\n`
  });
}

function tarGzip(files: Record<string, string>): Buffer {
  const blocks: Buffer[] = [];
  for (const [name, text] of Object.entries(files)) {
    const body = Buffer.from(text, "utf8");
    blocks.push(tarHeader(name, body.length), body, Buffer.alloc((512 - (body.length % 512)) % 512));
  }
  blocks.push(Buffer.alloc(1024));
  return gzipSync(Buffer.concat(blocks));
}

// A minimal ustar header for a regular file.
function tarHeader(name: string, size: number): Buffer {
  const header = Buffer.alloc(512);
  header.write(name, 0, 100, "utf8");
  header.write("0000644\0", 100, 8, "ascii");
  header.write("0000000\0", 108, 8, "ascii");
  header.write("0000000\0", 116, 8, "ascii");
  header.write(size.toString(8).padStart(11, "0") + "\0", 124, 12, "ascii");
  header.write(Math.floor(Date.now() / 1000).toString(8).padStart(11, "0") + "\0", 136, 12, "ascii");
  header.write("        ", 148, 8, "ascii");
  header.write("0", 156, 1, "ascii");
  header.write("ustar\0", 257, 6, "ascii");
  header.write("00", 263, 2, "ascii");
  let checksum = 0;
  for (const byte of header) {
    checksum += byte;
  }
  header.write(checksum.toString(8).padStart(6, "0") + "\0 ", 148, 8, "ascii");
  return header;
}
