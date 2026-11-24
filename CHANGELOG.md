# Changelog

All notable changes to Printty. Versions follow [Semantic Versioning](https://semver.org/); the release workflow takes each release's notes from its section here.

## [0.3.0] - 2026-11-24

### Added
- **Photo layout** (images): image size from 10% to 100%, 1, 2, 3, 4, 6 or 8 copies on one sheet in an automatic grid, and drag the photo in the preview to move it (every copy moves the same way; Centre puts it back). Auto orientation picks the sheet that gives the biggest upright copies.
- **More downloads:** .deb (Debian, Ubuntu, Raspberry Pi OS) and .rpm (Fedora, openSUSE) packages that install a systemd service, an AppImage, and a macOS app in a .dmg.

## [0.2.1] - 2026-11-24

### Changed
- The print page no longer shows the printer's own options (paper type, quality): editing pages, turning them, orientation, fit and paper size cover what people need. Printers still print at their default settings.

## [0.2.0] - 2026-11-24

### Added
- **Print editor.** After choosing a file you see every page and a live preview of each sheet. Tap a page to leave it out, ↻ to turn it. Landscape pages are turned to fill portrait paper automatically.
- Layout options: orientation (auto, portrait, landscape), size on paper (fit, fill, actual size), 1, 2, 4, 6 or 9 pages per sheet. Two-sided manual flips work with several pages per sheet.
- **Printer options** read from the printer itself, under "More printer options": paper type (for example glossy photo paper), paper source and print quality on Windows; every driver option on Linux and macOS (CUPS).
- **Passwords:** everyone can change their own password (Your account); the owner can make a one-time reset link for anyone who forgot theirs. Changing a password signs out other devices.
- **Telegram messages are editable** in Settings, with an emoji palette, placeholders like {who} and {file}, and "Send a sample". New defaults use emoji, and the buttons read ✅ Approve and ❌ Reject.

### Fixed
- Messages after a redirect no longer get lost on pages with a #section in the address.

## [0.1.0] - 2026-11-24

First release. Tested on Windows 11 with a Canon G1010.

### Added
- Share the PC's printer with phones and laptops on the same network through a web page: copies, page ranges, one- or two-sided (manual flip for printers without a duplex unit), colour or black and white, paper size (A4 by default).
- Nothing prints without permission: owner account, invite links for members (they print directly, or wait for approval if you choose), optional guests who always need approval.
- Telegram alerts for every request and when the printer is off; jobs wait and print by themselves when it's back.
- Native printing with no extra software: Windows' own PDF engine and print API; CUPS on Linux and macOS.
- Installable web app (PWA) with a share target, light and dark mode.
- Runs as a Windows service or at sign-in, with a tray icon to open Printty, copy the link for phones, and stop or restart it.
- Windows notifications for new requests, flips and an offline printer; click one to open Requests.
- Confirmation before signing out, cancelling, rejecting or removing access.
- About page with the version (also in the tray menu), and a 12-hour or 24-hour clock.
- Windows installer, portable zip, and Linux/macOS builds.
