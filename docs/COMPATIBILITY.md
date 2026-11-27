# Printer compatibility

Which printers work with Printty, and how sure we are. Only one printer has been tested on real paper so far, so everything below that row is a reasoned estimate, not a promise. If you try Printty with another printer, please [report it](https://github.com/TriptoAfsin/printty/issues/new?template=printer-report.md) so it can move up the list.

Last reviewed: 27 November 2026 (Printty 0.7.1).

## Confidence levels

| Level | Means |
| --- | --- |
| ✅ **Tested** | Printed on real paper with this exact printer and system. |
| 🟢 **Expected** | Not tested, but it uses the same driver and paper path as a tested printer. Very likely to work fully. |
| 🟡 **Likely** | Printty only uses the standard Windows print interface, which every Windows printer driver supports. Printing should work; some extras depend on the driver (see [What depends on the driver](#what-depends-on-the-driver)). |
| 🟠 **Experimental** | The code is written and builds, but it hasn't been tried on hardware. |
| ⛔ **Not supported** | Known not to work, or out of scope. |

## At a glance

| Printer | System | Connection | Confidence | Notes |
| --- | --- | --- | --- | --- |
| **Canon PIXMA G1010 series** | Windows 11 | USB | ✅ Tested | The reference printer. Canon's own driver (Printer Driver 1.03). Manual duplex, Draft / Standard / High quality, paper types from the driver. Still to do: a switch-off-mid-job test on paper. |
| "Microsoft Print to PDF" (virtual) | Windows 10/11 | — | ✅ Tested | Used for automated and end-to-end tests (`PRINTTY_TEST_OUTPUT_DIR`). Printty doesn't pick it on its own, but you can choose it in Settings. |
| Other Canon PIXMA and MegaTank inkjets (G1020, G2010, G2020, G3010, G3020, G4010, TS and TR series...) | Windows 10/11 | USB | 🟢 Expected | The same Canon IJ driver family as the G1010, so quality, paper type and paper size should map the same way. G3010, G3020 and G4010 have Wi-Fi too: see the network row. |
| Any printer with a working Windows driver: HP (DeskJet, Smart Tank, LaserJet), Epson (EcoTank, WorkForce), Brother (HL, DCP, MFC), Samsung/Xerox, Pantum... | Windows 10/11 | USB | 🟡 Likely | Printty prints through the standard Win32 print API (`CreateDC`, `StartDoc`, `StretchDIBits`), which every Windows driver accepts. Check the manual duplex flip order once (Settings › Print backs last page first) and whether Draft / High change anything on your driver. |
| Any of the above on the network (Wi-Fi or Ethernet) | Windows 10/11 | Network | 🟡 Likely | Printing works the same way. "Printer is off" detection depends on what Windows reports for network printers; Printty's stuck-job check still catches most cases. |
| Printers using Windows' built-in **Microsoft IPP Class Driver** (Mopria) | Windows 11 | Network or IPP-over-USB | 🟡 Likely | Windows has preferred this driver for new printers since 1 July 2026. It accepts the same print calls but offers fewer driver options (paper types, quality modes). |
| Printers with a duplex unit (automatic two-sided) | Windows 10/11 | Any | 🟡 Likely | Turn on **Settings › Automatic two-sided** and Printty asks the driver to print both sides. Fixed in the next release; up to 0.7.1, Windows printed these jobs one-sided. Not yet tested on a duplex printer. |
| Driverless printers: IPP Everywhere, AirPrint, Mopria (most network printers made since about 2015) | Linux | Network, or USB with `ipp-usb` | 🟠 Experimental | The CUPS backend builds and is unit-tested; not yet tried on a real printer. Driverless printers are the safest bet on Linux. |
| Printers with a CUPS / Gutenprint / vendor Linux driver | Linux | USB or network | 🟠 Experimental | Should work wherever `lp` works from a terminal. |
| Any printer CUPS can use | macOS | Any | 🟠 Experimental | Not yet run on a Mac at all. |
| Canon PIXMA G1010 | Linux | USB | 🟠 Experimental | No free driver confirmed: Canon's Linux IJ driver lists name the G1020, G2020, G3020 and others but not the G1010, and linux-hardware.org finds no driver for its USB ID. TurboPrint (commercial) lists it. |
| Canon PIXMA G1010 | macOS | USB | ⛔ Not supported | Canon lists macOS as not supported for this model. |
| Any printer once **Windows protected print mode** is turned on | Windows 11 | Any | 🟠 Experimental | That mode removes third-party drivers (including Canon's) and only keeps Mopria-certified printers. Printing through the class driver should still work but is untested. A USB-only printer without IPP-over-USB, like the G1010, stops working in that mode no matter which app prints. |
| Scanning, fax, copying | Any | — | ⛔ Not supported | Printty only prints. |
| Label, receipt (thermal) and large-format printers | Any | — | ⛔ Not supported (for now) | They may print, but Printty's layout assumes office and photo paper (A4, Letter, Legal, A5, 4×6, 5×7). |

## Why most Windows printers should work

Printty doesn't talk to printers directly. On Windows it renders each page to an image with Windows' own PDF engine (`Windows.Data.Pdf`), then hands it to the printer's driver through the classic Win32 print API: the same path almost every desktop app has used for decades. Any printer that can print from Notepad or a photo viewer on that PC should accept Printty's jobs.

What Printty reads from the driver, and what it falls back to:

| Printty asks the driver for | If the driver doesn't say |
| --- | --- |
| Colour or black-and-white only (`DC_COLORDEVICE`) | Colour is offered |
| Paper sizes by name (`DC_PAPERNAMES`), matched to A4, Letter, Legal, A5, 4×6 (10×15, KG), 5×7 (13×18, 2L) | A4 and Letter |
| Paper types (`DC_MEDIATYPENAMES`) | The choice is hidden |
| Paper sources / trays (`DC_BINNAMES`) | The choice is hidden |
| Printable area and margins (`GetDeviceCaps`) | The job fails with "printer reported no printable area" |

## What depends on the driver

These work on the G1010 and are the things to check on any other printer:

1. **Manual duplex flip order.** Printers feed paper differently. On the G1010 you put the printed stack back without reordering it, and Printty prints the backs last page first (the default, **Settings › Print backs last page first**). Print a short two-sided document once on a new printer; if the backs come out in the wrong order, turn that switch off.
2. **Print quality.** Printty sends the standard Windows values (draft, medium, high). Canon's driver maps each to its own mode. Some drivers ignore them and use whatever is set in their own preferences. Pages are still rendered at 150, 300 or 600 dpi either way.
3. **Paper type.** Comes straight from the driver's own list. Generic and class drivers often list only "Plain paper".
4. **Paper size names.** Matched by name. A driver that names sizes unusually (for example "Hagaki" or a custom photo size) may only offer A4 and Letter in Printty.
5. **"Printer is off" detection.** Windows often reports a switched-off USB printer as "Idle". Printty also treats its own job sitting in the queue for 45 seconds without progress as "printer off", holds the job and alerts once. This is how the G1010 behaves; other USB printers usually do the same. Network printers often report "Offline" properly.
6. **Borderless printing.** Not offered yet. Printty prints within the area the driver reports as printable.

## Windows driver changes in 2026 and 2027

Microsoft is moving Windows to its own IPP class driver ([end-of-servicing plan](https://learn.microsoft.com/en-us/windows-hardware/drivers/print/end-of-servicing-plan-for-third-party-printer-drivers-on-windows)):

| Date | What happens | Effect on Printty |
| --- | --- | --- |
| 15 Jan 2026 | No new third-party printer drivers on Windows Update (Windows 11, Server 2025). | None. Vendor installers still work. |
| 1 Jul 2026 | Windows prefers its IPP class driver when a printer supports it. | New network printers may get the class driver: fewer paper types and quality modes in Printty. Printing is the same. |
| 1 Jul 2027 | Third-party driver updates stop, except security fixes. | Existing drivers keep working and can still be installed from the maker's own package. |

Microsoft says existing drivers stay installable after these dates. The real risk is **Windows protected print mode** (optional in Windows 11 24H2 and later), which removes third-party drivers entirely. USB printers that don't support IPP-over-USB, the G1010 included, don't work in that mode with any app.

## Linux and macOS

On Linux and macOS, Printty builds each finished sheet as a PDF (pdfcpu) and sends it with CUPS `lp`, so any printer CUPS can print to should work. Best odds:

- **Driverless printers** (IPP Everywhere, AirPrint, Mopria) over the network. Most printers sold in the last ten years.
- **USB printers that speak IPP-over-USB**, with the [`ipp-usb`](https://github.com/OpenPrinting/ipp-usb) daemon (installed by default on recent Fedora and RHEL, a package on Debian and Ubuntu).
- Older USB printers need a CUPS driver (Gutenprint, HPLIP for HP, Canon's `cnijfilter2`, Epson's `epson-inkjet-printer-escpr`). If `lp -d <printer> file.pdf` works from a terminal, Printty should too.

Printer status there comes from `lpstat`. A switched-off USB printer usually shows up as a job that never finishes, which Printty treats the same way as on Windows.

## Report a printer

Open an issue with the **Printer report** template, or include:

- Printer model and how it's connected (USB, Wi-Fi, Ethernet)
- System (Windows 10/11, Linux distribution, macOS) and the driver (maker's driver, Microsoft IPP Class Driver, CUPS driver name)
- Printty version
- What you tried: one page, several pages, two-sided (manual or automatic), photo on photo paper, Draft and High quality, switching the printer off with a job waiting
- What came out wrong, with a photo if you can

## Sources

- Canon PIXMA G1010 specifications: [asia.canon](https://asia.canon/en/consumer/pixma-g1010/specification) (USB 2.0 only, macOS not supported, paper sizes)
- Canon G1010 Printer Driver 1.03 for Windows: [asia.canon](https://asia.canon/en/support/0100911416)
- Canon IJ Printer Driver 6.10 for Linux (model list): [sg.canon](https://sg.canon/en/support/0101091901?model=4469C)
- G1010 USB ID 04a9:183c, no Linux driver found: [linux-hardware.org](https://linux-hardware.org/?id=usb%3A04a9-183c)
- TurboPrint supported Canon printers: [turboprint.info](https://www.turboprint.info/printers_Canon.html)
- Microsoft, End of servicing plan for third-party printer drivers: [learn.microsoft.com](https://learn.microsoft.com/en-us/windows-hardware/drivers/print/end-of-servicing-plan-for-third-party-printer-drivers-on-windows)
- Microsoft, Windows protected print mode: [learn.microsoft.com](https://learn.microsoft.com/en-us/windows/modern-print/windows-protected-print-mode/windows-protected-print-mode)
- Microsoft, Known issues with third-party drivers and IPP-over-USB: [learn.microsoft.com](https://learn.microsoft.com/en-us/troubleshoot/windows-hardware/drivers/known-issues-third-party-drivers)
- Red Hat, Driverless USB printing with ipp-usb: [docs.redhat.com](https://docs.redhat.com/en/documentation/Red_Hat_Enterprise_Linux/10/html/configuring_and_using_a_cups_printing_server/driverless-usb-printing-and-scanning)
- Debian wiki, CUPS driverless printing: [wiki.debian.org](https://wiki.debian.org/CUPSDriverlessPrinting)
