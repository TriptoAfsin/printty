# Printty

A lean print server you leave running on one computer. Phones and laptops on the same network open a web page, upload a file, choose how it prints, and send it. If the printer is off, the job waits and Printty messages someone on Telegram (or email) to switch it on.

> Status: planning. Nothing is built yet. See [Roadmap](#roadmap).

## Why

Sharing a USB or home printer with phones is still awkward. The options are either enterprise tools (PaperCut), Linux-only projects built on CUPS, or cloud services. Printty aims to be:

- **One file.** A single Go binary with the web UI built in. No runtime, no bundled third-party apps.
- **Light enough to run 24/7.** Target: about 15–30 MB of memory when idle.
- **Native on each OS.** Windows printing uses Windows' own PDF engine and print API; Linux and macOS use CUPS.
- **Honest about the printer.** Printty checks several signals, because Windows often reports a switched-off USB printer as "Idle".

## Features (v1 scope)

First target: one **Canon G1010** (USB, colour inkjet, no automatic duplex) on Windows 11. Default paper: **A4**.

- **Nothing prints without permission.** Every request is checked against who sent it:
  - **Admin** (the owner): prints directly and approves everyone else.
  - **Members** (invited by the admin): sign in and send print requests; each member is set to "needs approval" (default) or "trusted" (auto-approved).
  - **Guests** (optional, off by default): anyone on the network can send a request with their name; it always needs approval and is rate-limited.
- **Incoming-job alert**: every new request notifies the admin right away (Telegram and push to the installed PWA), with who, what, pages, colour and paper, so the printer can be readied. Approve or reject from the Telegram message or the web page.
- **Manual duplex** for printers without a duplex unit: odd pages print, Printty asks you to flip the stack, then even pages print.
- Installable PWA: add Printty to the home screen, then **share a PDF or photo straight to Printty** from any app's share sheet (Android and desktop Chromium; iOS falls back to upload in the page).
- Upload PDF, JPG or PNG from any phone browser.
- Options: printer, copies, page range, duplex (off / long edge / short edge), colour or black and white, paper size.
- Job queue that survives restarts; jobs wait while the printer is off and print once it is back.
- Telegram alert when a job is stuck on an offline printer; email fallback.
- Accounts with invite links, upload size limit, files deleted after printing.
- Found on the network as `printty.local`, with a QR code for quick access.

Not in v1: Word/Office files, appearing in the phone's built-in print menu (IPP Everywhere / AirPrint), internet exposure (use [Tailscale](https://tailscale.com) for remote access), alerts when the host PC itself is off.

## How it works

```
phone browser ──► web server + UI ──► job queue ──► print backend ──► printer
                                         ▲              │
                                         │       Windows: Windows.Data.Pdf → Win32 print API (DEVMODE)
                                         │       Linux/macOS: CUPS `lp`
                                  status monitor ◄── printer status (WMI / lpstat, stuck jobs, IPP ping)
                                         │
                                      notifier ──► Telegram / email
```

Technical details: [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md). UI design system: [DESIGN.md](DESIGN.md).

## Requirements

- To run: Windows 10/11, Linux with CUPS, or macOS. Nothing else.
- To build: Go 1.27+.

## Build and run

Not available yet. The planned commands:

```sh
go build -o printty ./cmd/printty
./printty                     # run in the foreground, reads printty.toml
./printty service install     # install as a Windows service / systemd / launchd unit
```

## Roadmap

| Phase | Deliverable | Done when |
| --- | --- | --- |
| 0 | Spike: native Windows PDF printing | A 2-page duplex PDF prints correctly from a service with no user logged in |
| 1 | Skeleton: server, embedded UI, admin login, printer list, upload and print with options | The admin's phone prints a PDF and a photo on the G1010 with chosen options |
| 2 | Users, roles, invites, guest requests, approval queue | A member's request waits until the admin approves it on the web page |
| 3 | Saved queue, status monitor, hold-and-retry, manual duplex, job history | A job sent while the printer is off prints after it is switched on; a duplex job pauses for the flip |
| 3a | Telegram (incoming-job alerts with Approve/Reject buttons, printer-off alerts), web push, email fallback, limits, cleanup, mDNS, QR code | A guest's request pings Telegram and tapping Approve prints it |
| 3b | PWA: HTTPS, manifest, service worker, share target | On Android, sharing a PDF from the Files app to Printty opens it ready to print |
| 4 | Service install, CUPS backend, release builds | The same release installs and prints on Windows 11 and Linux |
| Later | Direct IPP to network printers, cloud relay for "PC is off" alerts, Office files, native phone print menu | — |

## License

To be decided.
