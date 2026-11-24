# Design: Printty

The locked system for Printty's web UI. Every Hallmark run reads this first, and all screens share it. Change it on purpose; the file is the rule.

## System
- Genre: modern-minimal
- Shape: app screen (a tool someone works in, mostly on a phone)
- Theme: Wayfind (deal 1/3, seed printty-lean-home-print-e18f·5ce6b3eb·2026-11-24·r0), colourway `platform`
- Axes: light/mono/cool · posture: committed
- Why Wayfind: Printty is a status board for one printer. Wayfind is public signage: read at a glance, cobalt for what is live, amber for figures.

## Screens
- Shell: a top bar with the wordmark, the printer's live status, and links to Print and Queue (plus Requests and Settings for the admin, with a count badge on Requests). No sidebar (at most five destinations); on phones the links sit in a bottom tab bar.
- **Sign in / Accept invite / Guest request**: one narrow column, one form each. The guest form says plainly that the owner must approve the print.
- **Print**: file picker (drop zone on desktop), then options as native-sized controls: printer, copies, pages, duplex, colour, paper (A4 preselected). Duplex on a manual-only printer reads "Two-sided (you'll flip the stack)". The primary action says what will happen: "Print" for admin and trusted members, "Send request" for everyone else.
- **Queue** (the signature): a departure-board list of jobs, each row = file name, who sent it, pages, options, state. `held` rows explain why ("Printer off, owner notified 2 min ago"); `pending_approval` rows read "Waiting for approval"; `awaiting_flip` is an ink plate with the flip instruction and a Continue button (admin).
- **Requests** (admin): incoming requests newest first, each with requester, file, page count, options and a first-page thumbnail; Approve (cobalt) and Reject (outline) on every row. Approve means "the printer is ready".
- **Users** (admin, inside Settings): members table (name, role, approval policy, last print, active sessions), invite button that shows a link and QR code, guest access toggle with its limits.
- **Shared file** (arrives from the share sheet): the Print screen with the file already attached, its name and page count on an ink plate, options below, Print as the only action. Cancel discards the draft.
- **Offline shell** (service worker): wordmark plus "Printty is unreachable. Is the computer on?" and a retry button.
- PWA chrome: `theme_color` = `--color-ink`, `background_color` = `--color-paper`; icon is the wordmark on an ink plate, square, maskable safe zone respected. Standalone display; the top bar respects `env(safe-area-inset-top)`.
- **Settings** (admin): printers (with the manual-duplex calibration print), notifications (Telegram/push/email test buttons), users, limits.
- States on every data region: loading (skeleton rows), empty (what goes here + the action that fills it), error (what broke + what to do), full.

## Tokens
`internal/web/static/tokens.css` is the source of truth. The roles:
```css
:root {
  --color-paper: oklch(95.5% 0.006 250);  --color-paper-2: oklch(91% 0.009 250);  --color-paper-3: oklch(99.5% 0.005 250);
  --color-ink: oklch(16% 0.014 262);  --color-ink-2: oklch(29% 0.014 262);  --color-muted: oklch(47% 0.016 260);  --color-rule: oklch(80% 0.011 250);
  --color-accent: oklch(51% 0.22 264);  --color-accent-2: oklch(81% 0.16 82);  --color-accent-ink: oklch(99.5% 0.003 250);  --color-focus: oklch(51% 0.22 264);
  --color-error: oklch(52% 0.19 27);  --color-success: oklch(50% 0.12 150);
  --font-display: "Chivo Mono", ui-monospace, monospace;  --font-body: var(--font-display);
  --radius-card: 0;  --radius-pill: 999px;  --rule-hair: 1px;  --rule-bar: 2px;
}
```
The spacing scale, type scale (ratio 1.2), easings and durations come from the scaffold and live in `tokens.css`. `base.css` holds the Floor's base rules.

## Colour roles
- Cobalt (`--color-accent`): the primary action, the current selection, a printer that is live. Nothing else.
- Amber (`--color-accent-2`): figures on ink plates (page counts, copies, queue position).
- `--color-error`: printer off / job failed. `--color-success`: printed.
- Status is never colour alone: every state has a word.

## Controls
- Primary: cobalt fill, `--color-accent-ink` text · radius 0 · 44px tall (phone first)
- Secondary: 2px ink outline · same radius and height
- Segmented controls for duplex, colour and paper (each is a short closed set); steppers for copies.
- Every control: default, hover, focus-visible, active, disabled, loading, error, success.

## Type
- Chivo Mono throughout, embedded in the binary (SIL OFL). No requests to font CDNs; the UI works offline.
- Body 16px minimum; tabular numerals for counts and times.

## Motion
- Character: severe. State changes only (`--dur-micro`, `--dur-short`); no reveals. Under reduced motion everything ends at once.

## Floor (never waived)
Tokens for every colour and font · no `transition: all` · `:focus-visible` on every control · `overflow-x: clip` on html and body · no tracked-caps labels or eyebrows · no invented figures · content visible without JS where possible.

## Exports
`internal/web/static/tokens.css` and `internal/web/static/base.css`.
