// Fetches the latest GitHub release at build time and writes src/release.json,
// so download buttons link straight to the real files and the page shows real
// sizes. If GitHub is unreachable, the last committed release.json is kept.
import { readFile, writeFile } from "node:fs/promises";

const REPO = "TriptoAfsin/printty";
const OUT = new URL("../src/release.json", import.meta.url);

// Which asset is which download. First match wins.
const PICK = [
  ["windows", /_x64_setup\.exe$/],
  ["windowsArm", /_arm64_setup\.exe$/],
  ["windowsPortable", /_windows_x64_portable\.zip$/],
  ["windowsArmPortable", /_windows_arm64_portable\.zip$/],
  ["mac", /_macos\.dmg$/],
  ["macIntelTar", /_darwin_amd64\.tar\.gz$/],
  ["macArmTar", /_darwin_arm64\.tar\.gz$/],
  ["deb", /_linux_amd64\.deb$/],
  ["debArm", /_linux_arm64\.deb$/],
  ["rpm", /_linux_amd64\.rpm$/],
  ["rpmArm", /_linux_arm64\.rpm$/],
  ["linuxTar", /_linux_amd64\.tar\.gz$/],
  ["linuxArmTar", /_linux_arm64\.tar\.gz$/],
  ["appimage", /_linux_x86_64\.AppImage$/],
  ["checksums", /^SHA256SUMS\.txt$/],
];

async function main() {
  const headers = { "User-Agent": "printty-landing-build", Accept: "application/vnd.github+json" };
  if (process.env.GITHUB_TOKEN) headers.Authorization = `Bearer ${process.env.GITHUB_TOKEN}`;
  const res = await fetch(`https://api.github.com/repos/${REPO}/releases/latest`, { headers });
  if (!res.ok) throw new Error(`GitHub answered ${res.status}`);
  const rel = await res.json();
  const assets = {};
  for (const [key, re] of PICK) {
    const a = rel.assets.find((x) => re.test(x.name));
    if (a) assets[key] = { name: a.name, url: a.browser_download_url, size: a.size };
  }
  const data = {
    version: rel.tag_name.replace(/^v/, ""),
    date: rel.published_at.slice(0, 10),
    page: rel.html_url,
    assets,
  };
  await writeFile(OUT, JSON.stringify(data, null, 2) + "\n");
  console.log(`release: v${data.version} (${Object.keys(assets).length} downloads)`);
}

main().catch(async (err) => {
  try {
    await readFile(OUT);
    console.warn(`release: ${err.message}; keeping the committed release.json`);
  } catch {
    console.error(`release: ${err.message} and no release.json to fall back on`);
    process.exit(1);
  }
});
