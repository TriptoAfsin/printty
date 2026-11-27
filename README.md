<p align="center">
  <img src="internal/web/static/icon.svg" alt="Printty" width="128" height="128">
</p>

<h1 align="center">Printty</h1>

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
- **Print editor:** page previews and a live sheet preview; leave pages out or turn them; landscape pages turn to fill portrait paper by themselves.
- **Photo layout:** image size, 1 to 8 copies on one sheet arranged automatically, and drag to reposition.
- **Several photos at once:** pick (or share) up to 30 photos and they become one document, a page each. Drag the pages into any order, then print 1, 2, 3, 4, 6 or 9 per sheet, or all of them on one sheet in the grid that shows them biggest, and drag each photo to move it on that sheet.
- **Print options:** copies, one/two-sided, colour or black and white, paper size (A4 default), orientation, fit / fill / actual size, 1 to 9 pages per sheet, any page order, print quality (draft 150 dpi / standard 300 dpi / high 600 dpi) and paper type from the printer's driver. The layout engine (`internal/printer/layout.go`) is shared by every OS.
- **Accounts:** change your own password; the owner makes one-time reset links.
- **Editable Telegram messages** with emoji and placeholders.
- **Manual duplex** for printers without a duplex unit (like the G1010): fronts print, Printty asks you to flip the stack, backs print.
- **Holds jobs while the printer is off** and alerts once; they print when it's back.
- **Installable PWA** with a share target: share a PDF or photos to Printty from any Android app (needs HTTPS). Offline screen when the PC is off.
- **Light and dark mode**, phone-first UI (Ribbon theme, see [DESIGN.md](DESIGN.md)).
- Files are deleted after printing; requests expire after 24 hours.

## Install

Download from [Releases](https://github.com/TriptoAfsin/printty/releases/latest):

| System | Download | What it does |
| --- | --- | --- |
| **Windows** | `Printty_<version>_x64_setup.exe` (`arm64` for Windows on Arm) | Installs Printty as a service that starts with Windows, adds the tray icon and a Start menu entry, allows it through the firewall on Private networks, and opens the setup page. One administrator prompt. Upgrades in place; your users and settings are kept. |
| Windows, no install | `Printty_<version>_windows_x64_portable.zip` | Unzip, run `printty.exe`. Install the service later from **Settings**. |
| Linux: Debian, Ubuntu, Raspberry Pi OS | `Printty_<version>_linux_amd64.deb` (or `arm64`) | `sudo apt install ./Printty_<version>_linux_amd64.deb`: a systemd service that starts at boot, plus the CUPS client. |
| Linux: Fedora, openSUSE | `Printty_<version>_linux_amd64.rpm` (or `arm64`) | `sudo dnf install ./Printty_<version>_linux_amd64.rpm` |
| Linux, any | `Printty_<version>_linux_x86_64.AppImage`, or the `.tar.gz` | The AppImage runs Printty while it's open; the archive has `install.sh` for systemd. |
| macOS | `Printty_<version>_macos.dmg` | Drag Printty to Applications and open it; Printty runs while the app is open. Not yet tested on a Mac. |

The builds aren't code-signed yet: if Windows SmartScreen says "Windows protected your PC", choose **More info › Run anyway**. Checksums are in `SHA256SUMS.txt`.

After installing, the setup page opens on the PC with the setup code already filled in: create the owner account, and you're done. Telegram alerts: copy `printty.env.example` (in the install folder) to `C:\ProgramData\Printty\printty.env`, fill it in, and choose **Restart Printty** from the tray icon.

## Build from source (Windows)

```powershell
go build -trimpath -ldflags "-s -w" -o printty.exe ./cmd/printty
copy printty.env.example printty.env   # then fill in Telegram if you want alerts
.\printty.exe
```

1. Open `http://localhost:8631` on the same PC: the setup code is filled in (it's also printed in the console and saved to `setup-code.txt` in the data folder). Create the owner account.
2. In **Users**, press **Invite someone** and send the link.
3. Phones on the same Wi-Fi open the address Printty prints at startup, for example:
   ```
   Open Printty on phones and other computers on this network:
     http://192.168.68.53:8631                Ethernet
     http://my-pc.local:8631                  This PC's name (newer phones)
   ```
   The same list, with copy buttons, is on the admin **Requests** page under "Open on phones", and invite links and Telegram links use it automatically. The first time Printty runs, Windows asks whether to allow it on networks: allow **Private** networks.

Printty picks the first real printer (not "Print to PDF" or OneNote) and remembers it; change it in **Settings**. It deliberately doesn't follow the Windows default printer, because Windows moves the default to whichever printer was used last.

## Start with Windows

In **Settings › Start with Windows**:

- **Install as a Windows service** (recommended): starts at boot before anyone signs in, restarts itself if it stops, and opens port 8631 for Private networks in Windows Firewall. Windows shows one permission prompt on the PC's screen; the running copy hands over to the service on the same address. **Remove the service** undoes all of it.
- **Start when I sign in**: no admin needed; runs hidden (`-background`) while you're signed in.

### The tray icon

Printty shows its printer icon in the notification area (next to the clock; Windows 11 may tuck new icons under **^**, drag it out to keep it visible). Hover for the status and address. **Click** it to open Printty. **Right-click** for the menu:

| Running as | Menu |
| --- | --- |
| A Windows service | Open Printty · Copy link for phones · Restart · Stop · Hide this icon. When the service is stopped the icon turns grey and a click starts it again. |
| Started by hand or at sign-in | Open Printty · Copy link for phones · Show log · Exit Printty |

A service runs in its own session and can't draw on your desktop, so installing the service also adds a small helper (`printty tray`) that Windows starts at every sign-in; it controls the service and quits by itself when the service is removed. Signed-in users can start, stop and restart the service without an admin prompt.

From a terminal: `printty service install -start`, `printty service uninstall`, `printty service start|stop|restart|status` (install and uninstall ask for admin rights themselves). The service runs as LocalSystem, reads `printty.env` next to `printty.exe` and in the data folder, and logs to `C:\ProgramData\Printty\printty.log`. On first run without a console, the setup code is in `setup-code.txt` in the data folder. **Check printing works** in Settings renders a test page without printing, to confirm the PDF engine works in your setup.

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

`tools/winres.sh <version>` rebuilds printty.exe's icon and version details (`cmd/printty/rsrc_windows_*.syso`, committed). `go run ./tools/mkicon` rebuilds `packaging/windows/printty.ico` from the app icon.

**Releasing:** add a section to [CHANGELOG.md](CHANGELOG.md), then push a tag: `git tag v0.2.0 && git push origin v0.2.0`. [release.yml](.github/workflows/release.yml) tests, builds the installers (Inno Setup), portable zips and Linux/macOS archives, and publishes the release. Run the workflow by hand for a dry run (downloads as workflow artifacts, no release).

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
| 4 | Run as a Windows service or at sign-in, tray icon, installer and release builds | Done (tested: install from the UI, PDF engine inside the service, tray start/stop, removal). CUPS on real hardware to do |
| Later | Web push, direct IPP, "PC is off" relay, Office files, passkeys | |

## License

To be decided.
