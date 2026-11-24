# Changelog

All notable changes to Printty. Versions follow [Semantic Versioning](https://semver.org/); the release workflow takes each release's notes from its section here.

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
