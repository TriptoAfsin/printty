// Renders public/og.png (1200x630) from an SVG with resvg: Buddy, the app icon
// (public/icon.svg, from internal/web/static on main), the README tagline and the
// current version. Also writes favicon-32.png and favicon.ico from the same icon.
// Run `npm run og` after a release or when the icon changes.
import { readFile, writeFile, mkdir } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { createRequire } from "node:module";
import { Resvg } from "@resvg/resvg-js";
import wawoff2 from "wawoff2";

const require = createRequire(import.meta.url);
const root = new URL("..", import.meta.url);
const release = JSON.parse(await readFile(new URL("src/release.json", root), "utf8"));
const icon = await readFile(new URL("public/icon.svg", root), "utf8");

// resvg needs TTF; fontsource ships WOFF2.
const dir = join(tmpdir(), "printty-og-fonts");
await mkdir(dir, { recursive: true });
const fonts = [];
for (const f of [
  "instrument-sans/files/instrument-sans-latin-500-normal",
  "instrument-sans/files/instrument-sans-latin-600-normal",
  "figtree/files/figtree-latin-400-normal",
  "figtree/files/figtree-latin-500-normal",
]) {
  const src = require.resolve(`@fontsource/${f}.woff2`);
  const out = join(dir, f.split("/").pop() + ".ttf");
  await writeFile(out, await wawoff2.decompress(await readFile(src)));
  fonts.push(out);
}

const pill = 40 + (8 + release.version.length) * 10;
const iconData = Buffer.from(icon).toString("base64");
// Ribbon dawn, from DESIGN.md: paper, ink, muted, accent, accent-soft, on-soft.
const C = { paper: "#fbf8f3", ink: "#191714", muted: "#5f5a53", accent: "#1f6b67", soft: "#d9efec", onSoft: "#1c4b48", rule: "#e6e1d8" };

const svg = `<svg xmlns="http://www.w3.org/2000/svg" xmlns:xlink="http://www.w3.org/1999/xlink" width="1200" height="630" viewBox="0 0 1200 630">
  <rect width="1200" height="630" fill="${C.paper}"/>
  <rect x="770" y="0" width="430" height="630" fill="${C.soft}"/>
  <image x="805" y="135" width="360" height="360" xlink:href="data:image/svg+xml;base64,${iconData}"/>
  <text x="72" y="104" font-family="Instrument Sans" font-weight="600" font-size="34" fill="${C.ink}" letter-spacing="-0.6">Printty</text>
  <text font-family="Instrument Sans" font-weight="500" font-size="92" fill="${C.ink}" letter-spacing="-3.2">
    <tspan x="66" y="262">Make your</tspan>
    <tspan x="66" y="358">printer wireless.</tspan>
  </text>
  <text font-family="Figtree" font-weight="400" font-size="32" fill="${C.muted}">
    <tspan x="72" y="438">Every phone on your Wi-Fi can print to it.</tspan>
    <tspan x="72" y="480">You approve every job.</tspan>
  </text>
  <rect x="72" y="532" width="${pill}" height="40" rx="20" fill="${C.soft}"/>
  <text x="92" y="559" font-family="Figtree" font-weight="500" font-size="20" fill="${C.onSoft}">Version ${release.version}</text>
  <text x="${72 + pill + 20}" y="559" font-family="Figtree" font-weight="400" font-size="20" fill="${C.muted}">printty.t21.dev</text>
</svg>`;

const opts = { font: { fontFiles: fonts, loadSystemFonts: false, defaultFontFamily: "Figtree" } };
const png = new Resvg(svg, { ...opts, fitTo: { mode: "width", value: 1200 } }).render().asPng();
await writeFile(new URL("public/og.png", root), png);
console.log(`og: public/og.png (${(png.length / 1024).toFixed(0)} KB, v${release.version})`);

// Favicons: a 32px PNG, and an .ico that wraps 16, 32 and 48px PNGs.
const at = (size) => new Resvg(icon, { fitTo: { mode: "width", value: size } }).render().asPng();
await writeFile(new URL("public/favicon-32.png", root), at(32));
const sizes = [16, 32, 48];
const pngs = sizes.map(at);
const head = Buffer.alloc(6 + 16 * sizes.length);
head.writeUInt16LE(0, 0);
head.writeUInt16LE(1, 2);
head.writeUInt16LE(sizes.length, 4);
let offset = head.length;
sizes.forEach((s, i) => {
  const e = 6 + 16 * i;
  head.writeUInt8(s, e);
  head.writeUInt8(s, e + 1);
  head.writeUInt16LE(1, e + 4);
  head.writeUInt16LE(32, e + 6);
  head.writeUInt32LE(pngs[i].length, e + 8);
  head.writeUInt32LE(offset, e + 12);
  offset += pngs[i].length;
});
await writeFile(new URL("public/favicon.ico", root), Buffer.concat([head, ...pngs]));
console.log("og: public/favicon-32.png, public/favicon.ico");
