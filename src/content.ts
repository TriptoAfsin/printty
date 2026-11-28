import release from "./release.json";

// Every claim and number on the page is from README.md on `main`. Do not add one that is not there.
const GH = "https://github.com/TriptoAfsin/printty";
export const SITE = {
  url: "https://printty.t21.dev",
  repo: GH,
  readme: `${GH}#readme`,
  changelog: `${GH}/blob/main/CHANGELOG.md`,
  compat: `${GH}/blob/main/docs/COMPATIBILITY.md`,
  architecture: `${GH}/blob/main/docs/ARCHITECTURE.md`,
  config: `${GH}#configuration`,
  roadmap: `${GH}#roadmap`,
  printerReport: `${GH}/issues/new?template=printer-report.md`,
  tailscale: "https://tailscale.com",
};

export type Asset = { name: string; url: string; size: number };
export type AssetKey =
  | "windows"
  | "windowsArm"
  | "windowsPortable"
  | "windowsArmPortable"
  | "mac"
  | "macIntelTar"
  | "macArmTar"
  | "deb"
  | "debArm"
  | "rpm"
  | "rpmArm"
  | "linuxTar"
  | "linuxArmTar"
  | "appimage"
  | "checksums";
export const RELEASE = release as { version: string; date: string; page: string; assets: Partial<Record<AssetKey, Asset>> };

/** Bytes to MB the way Windows Explorer shows it (MiB), one decimal. */
export const mb = (bytes: number) => (bytes / 1048576).toFixed(1);

export const installerMB = RELEASE.assets.windows ? mb(RELEASE.assets.windows.size) : "5.8";

// The README's "Why" list, measured on Windows 11.
export const SPECS: { label: string; value: string; unit: string }[] = [
  { label: "RAM when idle (working set)", value: "4", unit: "MB" },
  { label: "RAM while serving pages", value: "6", unit: "MB" },
  { label: "The single Go binary, web UI included", value: "~11", unit: "MB" },
  { label: "Default port", value: "8631", unit: "" },
];

// The console lines Printty prints at startup (README, "Build from source").
export const CONSOLE = `Open Printty on phones and other computers on this network:
  http://192.168.68.53:8631                Ethernet
  http://my-pc.local:8631                  This PC's name (newer phones)`;

export const STEPS: { title: string; body: string; points: string[] }[] = [
  {
    title: "Install it on the computer the printer is plugged into",
    body: "The Windows installer sets Printty up as a service that starts with Windows, adds a tray icon and allows it through the firewall on Private networks. One administrator prompt.",
    points: [
      "The setup page opens with the setup code already filled in",
      "Create the owner account and you're done",
      "Printty picks the first real printer and remembers it",
    ],
  },
  {
    title: "Phones open its address",
    body: "Printty prints the addresses phones can use when it starts. The same list, with copy buttons, is on the admin Requests page under “Open on phones”. Nothing to install on the phone.",
    points: ["Invite people with a link from Users", "Guests can ask with just a name, if you turn that on", "Upload a file, choose how it prints, send it"],
  },
  {
    title: "You approve, it prints",
    body: "Nothing prints until the owner approves it. Every incoming request pings you on Telegram with who, what, pages, colour and paper, so the printer can be readied.",
    points: [
      "With a dedicated bot, Approve and Reject work inside Telegram",
      "If the printer is off, the job waits and prints once it's back",
      "Files are deleted after printing; requests expire after 24 hours",
    ],
  },
];

export const WHY: { title: string; body: string }[] = [
  {
    title: "One file.",
    body: "A single Go binary with the web UI, fonts and icons built in. No runtime, no bundled third-party apps, no cgo.",
  },
  {
    title: "Light enough to run 24/7.",
    body: "Heavy work happens in a short-lived child process that exits after each job.",
  },
  {
    title: "Native on each OS.",
    body: "Windows uses its own PDF engine (Windows.Data.Pdf) and the Win32 print API. Linux and macOS use CUPS.",
  },
  {
    title: "Honest about the printer.",
    body: "Windows often reports a switched-off USB printer as “Idle”, so Printty also watches for jobs stuck in the spooler.",
  },
];

export const FEATURES: { title: string; body: string }[] = [
  {
    title: "Permission first",
    body: "You print directly and approve others. Members join by invite link and need approval unless you mark them trusted. Guests (off by default) type a name, always need approval and are rate-limited per device.",
  },
  {
    title: "Print editor",
    body: "Page previews and a live sheet preview. Leave pages out or turn them; landscape pages turn to fill portrait paper by themselves.",
  },
  {
    title: "Photos",
    body: "Pick or share up to 30 photos and they become one document. Drag the pages into order and print 1, 2, 3, 4, 6 or 9 per sheet, or 1 to 8 copies of one photo on a sheet, and drag each photo into place.",
  },
  {
    title: "Print options",
    body: "Copies, one or two-sided, colour or black and white, paper size (A4 by default), orientation, fit, fill or actual size, pages per sheet, page order, print quality and paper type from the driver.",
  },
  {
    title: "Manual duplex",
    body: "For printers without a duplex unit: the fronts print, Printty asks you to flip the stack, the backs print.",
  },
  {
    title: "Installable on phones",
    body: "A PWA with a share target: share a PDF or photos to Printty from any Android app (needs HTTPS). An offline screen when the PC is off.",
  },
];
