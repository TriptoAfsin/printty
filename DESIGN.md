# Design: Printty

The locked system for Printty's web UI (client and admin). Every Hallmark run reads this first, and all screens share it. Change it on purpose; the file is the rule.

## System
- Genre: atmospheric (theme default), used as a calm app screen
- Shape: app screen: a tool someone works in, mostly on a phone (client) and a laptop (admin)
- Theme: Ribbon (chosen 2026-11-24 over Ultraviolet, after Wayfind, Terrazzo and Darkroom were rejected as too square)
- Axes: light/grotesk-sans/chromatic-teal · posture: committed
- Modes: **light + dark**, parity per Hallmark `dark-mode.md`. Light = Ribbon `dawn`; dark = blue-black, re-derived, same teal hue.
- Feel: modern, elegant, softly rounded: never square, never bubbly. 14px cards, 10px buttons and inputs (7px for parts inside them). Pills only for small status things: tags, counts, switches, progress. (Toned down on 2026-11-24 at the owner's request: pills everywhere read as too round.)
- Approved mock-up: the "Ribbon" rows of the Printty UI mock-ups canvas; its stylesheet is kept as `docs/mockup-reference.css` (`.app.rb` blocks).

## Tokens
`internal/web/static/tokens.css` is the source of truth: Ribbon's scaffold block, then Printty's additions, then the dark block. The roles:
```css
:root {
  --color-paper: oklch(98.2% 0.008 82);  --color-paper-2: oklch(95.8% 0.011 82);  --color-paper-3: oklch(92.3% 0.014 82);  --color-raised: oklch(99.7% 0.004 85);
  --color-ink: oklch(18% 0.006 80);  --color-ink-2: oklch(30% 0.008 80);  --color-muted: oklch(47% 0.012 80);
  --color-rule: oklch(89.5% 0.011 82);  --color-ring: oklch(80% 0.012 82);
  --color-accent: oklch(44% 0.075 190);  --color-accent-deep: oklch(38% 0.07 190);  --color-accent-soft: oklch(92% 0.028 190);
  --color-accent-ink: oklch(98% 0.006 85);  --color-on-soft: oklch(34% 0.06 190);  --color-focus: oklch(52% 0.09 190);
  --color-warn / --color-warn-soft, --color-ok / --color-ok-soft, --color-error
  --font-display: "Instrument Sans";  --font-body: "Figtree";  --font-outlier: "JetBrains Mono";
  --radius-screen: 24px;  --radius-card: 14px;  --radius-inner: 10px;  --radius-control: 10px;  --radius-control-in: 7px;  --radius-pill: 999px;
}
[data-mode="dark"] { paper 15% / paper-2 18.5% / paper-3 22% / raised 25% (hue 230), ink 94%, accent oklch(74% 0.085 190) with dark text, shadows replaced by a 1px inset ring, body weight 350 }
```
Spacing, type scale (1.2), easings and durations come from the scaffold. `base.css` holds the Floor's base rules.

## App icon and mascot
- **Buddy** (chosen 2026-11-24): a cream printer with a face, blush cheeks and a page peeking out, on a Ribbon-teal rounded square with soft depth.
- Files in `internal/web/static/`: `icon.svg` (master, rounded), `icon-maskable.svg` (full-bleed square for Android masks and iOS), PNGs at 192, 512, maskable 512 and `apple-touch-icon.png` (180). PNGs are rendered from the SVGs with headless Edge; re-render them when the SVG changes.
- Buddy is also the mascot on sign-in, setup, invite, guest and offline screens (72px). Keep its face simple: two eyes with highlights, cheeks, a small smile.

## Mode switching
- Default follows the OS (`prefers-color-scheme`). A sun/moon button in the top bar sets `data-mode` on `<html>` and stores it in `localStorage`; a 4-line inline script in `<head>` applies it before first paint (no flash).
- PWA `theme_color` is set per mode with two `<meta name="theme-color" media="(prefers-color-scheme: …)">` tags.

## Surfaces
- Cards: `--color-raised`, `--radius-card`, soft `--shadow-card` in light; in dark, cards are lighter than the ground with a 1px inset ring, never a drop shadow.
- Grouped choices: a `--color-paper-2` pill track; the chosen option is a raised pill (`--color-raised` + `--shadow-card`).
- Notes and the flip prompt: `--color-accent-soft` with `--color-on-soft` text, `--radius-inner`.
- Status tags (pills, always with a word): waiting/printing = accent-soft; held/printer off = warn-soft; printed = ok-soft; neutral = paper-2.
- Progress: a 6px pill track in accent-soft with an accent fill.
- No textures, no gradients, no machined parts or faders from the full Ribbon theme: Printty takes its palette, faces, radii and calm only.

## Screens
- Phone shell: wordmark top left, printer status chip top right (`Printer ready` in ok-soft, `Printer off` in warn-soft), floating tab bar at the bottom (Print, Queue): an icon over a small label, the current tab on accent-soft in the on-soft colour, so it reads as navigation and never as a choice (choices are the paper-2 tracks).
- **Sign in**: username, password, Sign in; "or"; Ask as a guest (ghost); invite hint at the foot.
- **Print**: file card with a page thumbnail; copies stepper and pages; sides ("Two-sided is a manual flip"); colour; paper (A4 preselected); approval note. The Send request (or Print) button lives in a **sticky action bar**: a raised card stuck to the bottom of the screen (above the tab bar on phones) that also says what will print ("5 photos on 3 sheets" over "1 copy, A4, one-sided, colour").
- **Queue** (client): one card per job: name, status tag, one line of detail; progress bar while printing.
- Admin shell: top bar with wordmark, pill nav (Requests with a count badge, Queue, Users, Settings), mode toggle, avatar.
- **Requests** (admin, the signature): one card per request: first-page thumbnail, requester + role tag + time, file, guest note in italics, setting tags, Reject (ghost) and Approve (primary).
- Admin side column: printer card (Windows status vs spooler, default paper, two-sided mode), flip-needed card, alerts card with switches and Send a test.
- **Users**: card with a table (avatar, name, role, approval policy, last print), Invite someone, guest-requests switch.
- Still to design: Accept invite, Guest request form, Settings, offline shell, shared-file landing, empty/loading/error states.
- States on every data region: loading (skeleton cards), empty (what goes here + the one action), error (what broke + what to do), full.

## Controls
- Primary: accent fill, `--color-accent-ink` label, pill, 44px (54px full-width on phones); hover `--color-accent-deep`.
- Choices that can be pictured (orientation, size on paper, sides, binding margin, colour) carry a 24px line icon above the label (`.seg.icons`); the chosen one's icon turns accent. Icons come from the shared sprite in `layout.html` (1.8 stroke, round caps). Numbers and paper names stay text.
- On/off settings are switches, not checkboxes.
- Secondary (ghost): transparent with a 1.5px inset `--color-ring`, ink on hover.
- Inputs: pill, 52px, 1.5px inset ring, raised fill. Switches: 46×28 pill, accent when on, `role="switch"`.
- Every control: default, hover, focus-visible (2px `--color-focus` ring, offset 2px), active, disabled, loading, error, success. Touch targets ≥ 44px.

## Type
- Instrument Sans 500 for headings (tracking −0.035em, sentence case); Figtree for everything else (400 light, 350 dark); JetBrains Mono only for codes if ever needed.
- All three fonts embedded in the binary (SIL OFL). No requests to font CDNs; the UI works offline.
- Body 16px minimum; tabular numerals for counts and times.

## Motion
- Character: calm. Colour and background changes over `--dur-micro`; switch knob over `--dur-short`. No reveals. Under reduced motion everything ends at once.

## Floor (never waived)
Tokens for every colour and font · no `transition: all` · `:focus-visible` on every control · `overflow-x: clip` on html and body · no tracked-caps labels or eyebrows · no middle-dot meta strings · no invented figures · content readable without JS where possible.

## Exports
`internal/web/static/tokens.css` and `internal/web/static/base.css`; mock-up stylesheet for reference in `docs/mockup-reference.css`.
