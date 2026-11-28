# Printty website

This is the `landing` branch of [TriptoAfsin/printty](https://github.com/TriptoAfsin/printty). It holds **only** the website at <https://printty.t21.dev>. The app lives on `main`.

This branch is an orphan. Never merge it with `main`.

## Stack

- React 19 + TypeScript + Vite, prerendered to static HTML at build time (`src/entry-server.tsx` → `scripts/prerender.mjs`), then hydrated.
- Plain CSS with tokens in `src/styles/tokens.css`, taken from the Ribbon theme in `DESIGN.md` and `internal/web/static/tokens.css` on `main`, in light and dark (follows the OS). Instrument Sans, Figtree and JetBrains Mono are bundled, so the page makes no third-party requests.
- Cloudflare Workers static assets (`wrangler.jsonc`), custom domain `printty.t21.dev`.

## Commands

```bash
npm install
npm run dev       # local dev server
npm run build     # fetch latest release, typecheck, build, prerender
npm run check     # verify SEO tags, OG image, sitemap, alt text in dist/
npm run og        # re-render public/og.png, favicon-32.png and favicon.ico
npm run deploy    # build and `wrangler deploy` (needs `wrangler login`)
```

## Content that comes from the app

- **Release data.** `scripts/release.mjs` asks the GitHub API for the latest release at build time and writes `src/release.json`: the version, date, and every download link and size (Windows installers and portable zips for x64 and Arm, the macOS .dmg and archives, Linux .deb, .rpm, .tar.gz for amd64 and arm64, the AppImage and `SHA256SUMS.txt`). If GitHub is unreachable, the committed `release.json` is used. If the asset names change on `main`, update the regexes in `PICK`.
- **Copy.** Every claim and number comes from the README on `main` (`src/content.ts`). Do not add one that is not there.
- **Icon.** `public/icon.svg`, `icon-192.png`, `icon-512.png` and `apple-touch-icon.png` are copies of Buddy from `internal/web/static/` on `main`. `npm run og` renders the favicons and `og.png` from `icon.svg`.
- **Screenshots.** None yet: `main` has no screenshots of the web UI, so the page shows the flow as a diagram instead of inventing one. When real screenshots exist, add WebP copies under `public/screenshots/`.

## Deploying

Workers Builds: connect this repository, set the production branch to `landing`, build command `npm run build`, deploy command `npx wrangler deploy`. A push here deploys. Re-run the build after each app release so the version and download links update (or trigger a deploy hook from the release workflow on `main`).

## SEO

`index.html` carries the title, description, canonical, Open Graph and Twitter tags and a JSON-LD `SoftwareApplication`; `scripts/prerender.mjs` fills in the version and size, and writes `404.html` and `sitemap.xml`. `public/robots.txt` points at the sitemap. `npm run check` fails if any of that is missing.
