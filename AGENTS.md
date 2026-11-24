# AGENTS.md

Guidance for AI coding agents working in this repo. Humans: start with [README.md](README.md).

## What this is

Printty: a single Go binary that shares a computer's printers with phones through a web page, and sends a Telegram/email alert when a job is waiting on an offline printer. Read [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) before changing anything structural, and [DESIGN.md](DESIGN.md) before touching the UI.

## Target hardware

v1 must work end to end on the owner's **Canon G1010 series** (USB, Windows 11, Canon driver, no automatic duplex → manual duplex). Default paper is **A4**. Anything that doesn't work on this printer isn't done.

## Hard constraints

These are product decisions, not preferences. Don't change them without the owner's approval.

1. **One binary, `CGO_ENABLED=0`.** No cgo, no DLLs, no bundled third-party executables (no SumatraPDF, Ghostscript, LibreOffice, PDFium). Native OS facilities only: `Windows.Data.Pdf` + Win32 print API on Windows, CUPS on Linux/macOS.
2. **Lean.** Runs 24/7 on someone's PC. Idle memory target ~15–30 MB; no polling tighter than needed; no background work when idle beyond the status monitor.
3. **Minimal dependencies.** Prefer the standard library. Current allowed extras: `golang.org/x/sys`, `golang.org/x/crypto` (argon2id), `github.com/kardianos/service`, a TOML parser, optionally `go.etcd.io/bbolt` and an mDNS library. Ask before adding anything else.
4. **Offline UI.** The web UI is embedded with `go:embed` and never loads from the internet (fonts included).
5. **PWA with share target.** The UI is an installable PWA (`manifest.webmanifest`, `sw.js`) that receives shared files via the Web Share Target API at `POST /share`. Shared files become drafts and never print without the person confirming. This needs HTTPS (see ARCHITECTURE.md § PWA); don't add features that only work over plain HTTP.
6. **LAN only by default.** Never add code that opens ports on the router (UPnP) or exposes the server publicly.
7. **Nothing prints without permission.** No unauthenticated print path. Roles: admin, member (`needs approval` by default, or `trusted`), guest (off by default, always needs approval). Every new request notifies the admin. Role checks happen on the server, never only in the UI. See ARCHITECTURE.md § Users, roles and approval.

## Layout

```
cmd/printty/            entry point, flags, service install/run
internal/config/        printty.toml loading and validation
internal/server/        HTTP handlers, upload limits
internal/auth/          users, roles, invites, sessions, guests, approval policy
internal/web/static/    embedded UI (HTML/CSS/JS, manifest.webmanifest, sw.js, icons); tokens.css + base.css from Hallmark
internal/queue/         persisted jobs and state machine
internal/printer/       Backend interface; printer_windows.go, printer_unix.go
internal/status/        printer readiness from multiple signals
internal/notify/        Telegram, email
docs/                   architecture and design notes
```

## Conventions

- Go 1.27+. `gofmt`, `go vet` clean. Errors wrapped with `%w` and context; no panics outside `main`.
- Platform code goes in `_windows.go` / `_unix.go` files behind a shared interface. Shared code must compile on every target: check with `GOOS=linux go build ./...` and `GOOS=windows go build ./...`.
- Windows syscalls: use `golang.org/x/sys/windows` (`NewLazySystemDLL`), never `syscall.LoadLibrary` on arbitrary paths.
- Logging: `log/slog`, structured, no secrets (tokens, passwords, session IDs, invite links, chat IDs) in logs.
- Tests: table-driven, `go test ./...`. Printing itself is behind the `Backend` interface so the queue and server are tested with a fake backend. Real-printer tests are manual and documented in the PR.
- UI: plain HTML/CSS/JS, no framework, no build step. Every colour and font through the tokens in `tokens.css`. Follow DESIGN.md; it is the locked Hallmark system.

## Commands

```sh
go build ./...                       # build
go test ./...                        # tests
go vet ./...                         # lint
GOOS=linux go build ./...            # cross-compile check
go run ./cmd/printty                 # run locally
```

## Security checklist for any handler

- Requires auth unless it is the login page or static assets.
- Validates upload size before reading the body fully (`http.MaxBytesReader`).
- Detects file type by magic bytes, not extension or `Content-Type`.
- Stores files under the data directory with random names; never uses the client's filename in a path.

## Current phase

Planning is done; next is **phase 0**: a throwaway spike proving `Windows.Data.Pdf` → Win32 printing with duplex/colour/paper options, run as a service. See the roadmap in README.md.
