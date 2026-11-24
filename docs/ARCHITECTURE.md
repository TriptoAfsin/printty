# Architecture

Printty is one Go binary with six parts: web server, users and auth, job queue, print backend, status monitor and notifier. Each OS's printing code sits behind one interface and is chosen at build time.

## Components

| Part | Package | Responsibility |
| --- | --- | --- |
| Web server + UI | `internal/server`, `internal/web` | HTTP API, uploads with size limits, embedded static UI (`go:embed`) |
| Users and auth | `internal/auth` | Accounts, roles, invites, sessions, guest requests, approval policy |
| Job queue | `internal/queue` | Persist jobs to disk, run the job state machine, retry held jobs |
| Print backend | `internal/printer` | List printers and capabilities, print a job with options |
| Status monitor | `internal/status` | Decide whether each printer is really ready, from several signals |
| Notifier | `internal/notify` | Telegram Bot API and SMTP email, with a cooldown so one outage sends one message |
| Config | `internal/config` | Load and validate `printty.toml` |
| Entry point | `cmd/printty` | Flags, service install/uninstall/run via `kardianos/service` |

## Target hardware (v1)

**Canon G1010 series** on Windows 11, USB (`USB001`), Canon driver. Colour inkjet, paper up to A4/Legal (A4 is Printty's default), photo sizes (4x6, 5x7, L, 2L). **No automatic duplex unit**: the driver's "Duplex" is Canon's manual mode, so Printty runs manual duplex itself (below). USB means Windows usually reports "Idle" even when the printer is off, so the stuck-job signal is the main offline detector.

## Users, roles and approval

| Role | How they get in | Printing |
| --- | --- | --- |
| Admin | Created on first run (setup screen on the host or a one-time setup code printed to the console) | Prints directly; approves or rejects others; manages users and settings |
| Member | Invite link or QR code made by the admin (single use, expires in 7 days); sets a name and password | Sends requests. Per member: `needs approval` (default) or `trusted` (auto-approved) |
| Guest | Optional, off by default. No account: enters a name and an optional note | Every request needs approval; rate-limited per IP (default 3 per hour); max pages per request |

- Passwords hashed with argon2id (`golang.org/x/crypto`). Sessions are random 32-byte tokens in an `HttpOnly`, `Secure`, `SameSite=Strict` cookie, stored server-side and revocable. Passkeys are a later option.
- Admins can disable a member, revoke their sessions, or switch them between `needs approval` and `trusted`.
- Every request records who sent it (user or guest name + IP), and the job history shows it.

## Job lifecycle

```
submitted ──► pending_approval ──► approved ──► queued ──► printing ──► done
   │                 │                            │  ▲        │   ▲
   │ (admin/trusted) ▼                            ▼  │        ▼   │ (admin taps Continue)
   └──────────► approved              rejected   held     awaiting_flip
                                       expired   (printer   (manual duplex:
                                                  offline)   odd pages done)
                                                                  printing ──► failed
```

- `pending_approval` is the request inbox. Approving is the admin's signal that the printer is ready (powered on, paper loaded). Requests not acted on expire after a configurable time (default 24 h) and their files are deleted.
- `held` jobs resume automatically when the status monitor reports the printer ready.
- The requester sees their job's state live on the Queue screen (server-sent events).
- Uploaded files are deleted when a job reaches `done` or `failed`, and by a sweep after a configurable age.

## Print backend interface

```go
type Backend interface {
    Printers(ctx context.Context) ([]Printer, error)          // name, default, capabilities
    Print(ctx context.Context, job Job) error                  // file + Options
}

type Options struct {
    Copies    int
    Pages     string     // "1-3,5", empty = all
    Duplex    Duplex     // Simplex | LongEdge | ShortEdge
    Color     bool       // false = monochrome
    PaperSize string     // "A4" (default), "Letter", "4x6", ...
}
```

`Printers()` reports each printer's capabilities, including whether duplex is automatic or manual only. The UI shows only the options a printer supports.

### Manual duplex

For printers without a duplex unit (the G1010):

1. Print the odd pages (1, 3, 5, …).
2. Job goes to `awaiting_flip`; the admin is notified: "Flip the stack and put it back in the tray, then tap Continue."
3. Print the even pages. The order and rotation needed for the reinsert depend on the printer's paper path, so they are a per-printer setting, set once with a two-page calibration print in Settings.
4. Odd page count: the last back side is skipped.

### Windows (`printer_windows.go`)

1. PDF to page images: `Windows.Data.Pdf` (WinRT, built into Windows 10/11), called from Go through COM/WinRT interop. No bundled PDF engine.
2. Images (JPG/PNG): decoded with Go's standard library; skip step 1.
3. Printing: Win32 via `golang.org/x/sys/windows` — `OpenPrinter`, `DocumentProperties` to get and modify the printer's `DEVMODE` (`dmDuplex`, `dmColor`, `dmPaperSize`, `dmCopies`), `CreateDC`, `StartDoc`/`StartPage`, `StretchDIBits`, `EndPage`/`EndDoc`.
4. Pages are rendered at the printer's DPI (300 by default) and scaled to the printable area.

Risk to retire in phase 0: `Windows.Data.Pdf` must work in a service running in session 0 with no user logged in.

### Linux and macOS (`printer_unix.go`)

`lp -d <printer> -n <copies> -P <pages> -o sides=<one-sided|two-sided-long-edge|two-sided-short-edge> -o print-color-mode=<color|monochrome> -o media=<size> <file>`. Printer list and status from `lpstat`. CUPS renders PDFs itself.

### Later: direct IPP

For network printers that accept `application/pdf`, send the file over IPP with the same options as standard IPP attributes. Skips rendering on every OS.

## Status monitor

A printer is "ready" only when all available signals agree:

| Signal | Source | Catches |
| --- | --- | --- |
| OS status | WMI `Win32_Printer` (`PrinterStatus`, `WorkOffline`, `DetectedErrorState`) / `lpstat -p` | Paper out, error, paused, offline (when the OS knows) |
| Stuck job | Our own job sitting in the OS spooler longer than a threshold | USB printers switched off while Windows still says "Idle" |
| Reachability | TCP connect to port 631/9100 for network printers | Network printer off or unplugged |

When a printer goes not-ready while jobs are queued or printing, the queue holds them and the notifier fires once (cooldown per printer).

## Notifications

Events that notify the admin:

| Event | Message | Actions |
| --- | --- | --- |
| Incoming request | "Sam wants to print report.pdf: 6 pages, colour, A4, 2 copies" | Approve, Reject |
| Incoming job from admin/trusted member | "Printing report.pdf for Sam (6 pages). Make sure the printer is on." | none |
| Printer offline with jobs waiting | "Canon G1010 seems off. 2 jobs waiting." | none |
| Manual duplex flip | "Flip the stack for report.pdf, then tap Continue." | Continue, Cancel |
| Job failed | which job, why | none |

Channels:

- **Telegram** (primary): `sendMessage` with an inline keyboard for the actions. Button presses arrive through **long polling** (`getUpdates`), so no public webhook or open port is needed. Only presses from the configured admin chat IDs are accepted; each callback carries a signed, single-use token.
- **Web Push** to the admin's installed PWA (VAPID keys generated on first run). Tapping the notification opens the request.
- **Email** (fallback): plain SMTP with STARTTLS; no actions, just a link.
- Requesters are told the outcome in the web UI (and by push, if they installed the PWA).
- Messages are short and actionable. Offline alerts have a per-printer cooldown so one outage sends one message.

## PWA and share target

The UI is an installable Progressive Web App, so people can share files to Printty from other apps.

- `manifest.webmanifest`: name, icons, `display: standalone`, and a `share_target`:
  ```json
  "share_target": {
    "action": "/share",
    "method": "POST",
    "enctype": "multipart/form-data",
    "params": { "title": "title", "text": "text", "files": [{ "name": "file", "accept": ["application/pdf", "image/jpeg", "image/png"] }] }
  }
  ```
- `POST /share`: the server stores the shared file as a **draft** (not yet a job), then redirects `303` to `/print?draft=<id>`, where the person picks options and confirms. Shared files never print without that confirmation. Drafts expire after an hour.
- Auth: if the session cookie is missing on `/share`, the server keeps the draft, redirects to login (or to the guest form, if guests are enabled), and returns to the draft afterwards. A shared file then follows the same approval rules as an upload.
- Service worker (`sw.js`): caches the app shell so the PWA opens instantly and shows "Printty is unreachable" when the server is down, instead of a browser error. It does not cache API responses or uploaded files.
- Platform support: Web Share Target works in Chrome/Edge on Android and desktop. iOS Safari can install the PWA but does not support share targets; iOS users upload from the page (a later option is an iOS Shortcut that posts to `/share`).

### HTTPS is required

Service workers, installation and share targets only work on a secure origin. `http://<LAN IP>` is not one. Options, in order of preference:

1. **Tailscale HTTPS**: `tailscale cert` issues a publicly trusted certificate for `<machine>.<tailnet>.ts.net`. No setup on phones beyond the Tailscale app; also gives remote access. Recommended default.
2. **Own domain + Let's Encrypt DNS-01**: e.g. `printty.example.com` resolving to the LAN IP; Printty renews the cert via the DNS provider's API. Trusted everywhere, no app needed.
3. **Local CA** (self-signed root installed on each device): works offline, but installing a root on phones is awkward (iOS needs a profile + trust toggle). Last resort.

Printty serves HTTPS itself (`crypto/tls`), loading the cert from a configured path and reloading it on change; plain HTTP on the same port only redirects to HTTPS.

## Security

- LAN only by default; bind address configurable. Remote access through Tailscale, never port forwarding.
- Every endpoint except login, invite redemption, the guest request form (when enabled) and static assets requires a session. Roles are checked on the server for every action.
- CSRF: `SameSite=Strict` cookies plus an `Origin` check on every state-changing request.
- Login and guest endpoints are rate-limited per IP.
- Upload limit (default 50 MB), allow-list of content types checked by magic bytes, files stored outside the web root with random names.

## Storage

A data directory (`%ProgramData%\Printty` on Windows, `/var/lib/printty` on Linux, `~/Library/Application Support/Printty` on macOS) holds uploaded files and a small job index (JSON or bbolt). No database server.

## Build

- `CGO_ENABLED=0` everywhere; platform code split by build tags (`_windows.go`, `_unix.go`).
- Release targets: `windows/amd64`, `windows/arm64`, `linux/amd64`, `linux/arm64`, `darwin/arm64`.
- The UI font (Chivo Mono, SIL OFL) is embedded in the binary; the UI never loads anything from the internet.
