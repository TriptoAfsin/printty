# Printty

A lean print server you leave running on one computer. Phones and laptops on the same network open a web page, upload a file, choose how it prints, and send it. Nothing prints until the owner approves it, every incoming request pings the owner on Telegram so the printer can be readied, and if the printer is off the job waits and prints by itself once it's back.

> Status: **v0.1 works end to end on Windows 11** with a Canon G1010 (USB). Linux/macOS (CUPS) builds and is untested on hardware. See [Roadmap](#roadmap).

## Why

Sharing a USB or home printer with phones is still awkward: enterprise tools (PaperCut), Linux-only projects built on CUPS, or cloud services. Printty is:

- **One file.** A single Go binary (about 11 MB) with the web UI, fonts and icons built in. No runtime, no bundled third-party apps, no cgo.
- **Light enough to run 24/7.** Measured on Windows 11: about **4 MB of RAM when idle** (working set), 6 MB while serving pages, 19 threads. Heavy work happens in a short-lived child process that exits after each job.
- **Native on each OS.** Windows uses its own PDF engine (`Windows.Data.Pdf`) and the Win32 print API; Linux and macOS use CUPS.
- **Honest about the printer.** Windows often reports a switched-off USB printer as "Idle", so Printty also watches for jobs stuck in the spooler.

## Features

- **Permission first.** Admin (you) prints directly and approves others. Members join by invite link and need approval unless you mark them trusted. Guests (off by default) type a name and always need approval; rate-limited per device.
- **Incoming-request alerts** on Telegram with who, what, pages, colour and paper. With a dedicated bot, Approve/Reject work right inside Telegram.
- **Print options:** copies, page ranges, one/two-sided, colour or black and white, paper size (A4 default; the list comes from the printer driver).
- **Manual duplex** for printers without a duplex unit (like the G1010): fronts print, Printty asks you to flip the stack, backs print.
- **Holds jobs while the printer is off** and alerts once; they print when it's back.
- **Installable PWA** with a share target: share a PDF or photo to Printty from any Android app (needs HTTPS). Offline screen when the PC is off.
- **Light and dark mode**, phone-first UI (Ribbon theme, see [DESIGN.md](DESIGN.md)).
- Files are deleted after printing; requests expire after 24 hours.

## Quick start (Windows)

```powershell
go build -trimpath -ldflags "-s -w" -o printty.exe ./cmd/printty
copy printty.env.example printty.env   # then fill in Telegram if you want alerts
.\printty.exe
```

1. The console prints a **setup code**. Open `http://localhost:8631/setup`, enter it, and create the owner account.
2. In **Users**, press **Invite someone** and send the link.
3. Phones on the same Wi-Fi open the address Printty prints at startup, for example:
   ```
   Open Printty on phones and other computers on this network:
     http://192.168.68.53:8631                Ethernet
     http://my-pc.local:8631                  This PC's name (newer phones)
   ```
   The same list, with copy buttons, is on the admin **Requests** page under "Open on phones", and invite links and Telegram links use it automatically. The first time Printty runs, Windows asks whether to allow it on networks: allow **Private** networks.

Printty picks the first real printer (not "Print to PDF" or OneNote) and remembers it; change it in **Settings**. It deliberately doesn't follow the Windows default printer, because Windows moves the default to whichever printer was used last.

## Configuration

Settings come from environment variables or a `printty.env` file (next to the binary, in the working directory, or in the data folder). See [printty.env.example](printty.env.example).

| Variable | Default | What it does |
| --- | --- | --- |
| `PRINTTY_ADDR` | `:8631` | Listen address |
| `PRINTTY_DATA_DIR` | `C:\ProgramData\Printty` | Users, jobs and uploaded files |
| `TELEGRAM_BOT_TOKEN`, `TELEGRAM_CHAT_ID` | (off) | Alerts. Chat IDs can be comma-separated |
| `PRINTTY_PUBLIC_URL` | (none) | Base URL for links in alerts |
| `PRINTTY_TLS_CERT`, `PRINTTY_TLS_KEY` | (none) | Serve HTTPS |
| `PRINTTY_MAX_UPLOAD_MB` | `50` | Upload limit |

Everything else (guests, limits, flip order, alert switches) is in the admin **Settings** page.

**Telegram:** if the bot is also used by another app with a webhook (for example n8n), Telegram sends button presses there, so Printty detects the webhook and sends buttons that open the Requests page instead. For one-tap Approve/Reject in Telegram, create a dedicated bot with @BotFather.

**HTTPS** is needed to install the app and use the phone share sheet. The simplest route is [Tailscale](https://tailscale.com): `tailscale cert` gives a trusted certificate for `<pc>.<tailnet>.ts.net`, and it also gives safe remote access. Never port-forward Printty to the internet.

## How it works

```
phone browser ──► web server + auth ──► job queue ──► printty worker (child process) ──► printer
                                           ▲              Windows: Windows.Data.Pdf → Win32 print API
                                           │              Linux/macOS: CUPS lp
                                   status monitor ◄── Windows status, stuck spooler jobs
                                           │
                                       notifier ──► Telegram
```

Details: [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md). UI system: [DESIGN.md](DESIGN.md).

## Development

```sh
go vet ./... && GOOS=linux go vet ./...
go test ./...
PRINTTY_PRINT_TEST=1 go test ./internal/printer/   # real GDI print to "Microsoft Print to PDF", no paper
```

`PRINTTY_TEST_OUTPUT_DIR=<folder>` makes the Windows backend print into PDF files there (use with the "Microsoft Print to PDF" printer selected in Settings), so the whole flow can be tested without paper.

## Roadmap

| Phase | Deliverable | Status |
| --- | --- | --- |
| 0 | Native Windows PDF printing | Done: `Windows.Data.Pdf` + GDI, tested to PDF |
| 1 | Server, embedded UI, admin setup, upload and print with options | Done |
| 2 | Users, invites, guests, approval queue | Done |
| 3 | Saved queue, status monitor, hold-and-retry, manual duplex, history | Done (needs a real G1010 power-off test) |
| 3a | Telegram alerts with Approve/Reject, limits, cleanup | Done; mDNS name and QR code to do |
| 3b | PWA: manifest, service worker, share target | Done; HTTPS setup to document per platform |
| 4 | Run as a service, CUPS on real hardware, release builds | To do |
| Later | Web push, direct IPP, "PC is off" relay, Office files, passkeys | |

## License

To be decided.
