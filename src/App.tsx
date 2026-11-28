import { useEffect, useRef, useState } from "react";
import { RELEASE, SITE, SPECS, STEPS, WHY, FEATURES, CONSOLE, mb, type Asset } from "./content";
import { Mark, GitHub, ArrowRight, Download, Copy, Check, Phone, Computer, Printer, Bell } from "./components/Icons";
import { useDownload, useSystem } from "./download";
import { AppSwitcher } from "./components/AppSwitcher";

function Nav() {
  const [scrolled, setScrolled] = useState(false);
  useEffect(() => {
    const on = () => setScrolled(window.scrollY > 8);
    on();
    window.addEventListener("scroll", on, { passive: true });
    return () => window.removeEventListener("scroll", on);
  }, []);
  return (
    <header className="nav" data-scrolled={scrolled}>
      <div className="nav-inner">
        <div className="brand-group">
          <a className="brand" href="#top" aria-label="Printty, back to top">
            <Mark /> <span>Printty</span>
          </a>
          <AppSwitcher current="printty" />
        </div>
        <nav aria-label="Sections" className="nav-links">
          <a href="#how">How it works</a>
          <a href="#features">Features</a>
          <a href="#printers">Printers</a>
          <a href={SITE.changelog}>Changelog</a>
        </nav>
        <a className="icon-link" href={SITE.repo} aria-label="Printty on GitHub">
          <GitHub />
        </a>
        <a className="btn btn-solid btn-sm" href="#download">
          Download
        </a>
      </div>
    </header>
  );
}

function Hero() {
  const dl = useDownload();
  const onPhone = dl.sys?.os === "mobile";
  return (
    <section className="hero wrap" id="top" aria-labelledby="hero-title">
      <div className="hero-copy">
        <a className="release-pill" href={RELEASE.page}>
          <span className="dot" aria-hidden="true" />
          Version {RELEASE.version} for Windows, Linux and macOS
        </a>
        <h1 id="hero-title" className="hero-title">
          Make your printer wireless.
        </h1>
        <p className="hero-lead">Every phone on your Wi-Fi can print to it. You approve every job.</p>
        <p className="hero-sub">
          A lean print server you leave running on one computer. Phones and laptops on the same network open a web page,
          upload a file, choose how it prints, and send it.
        </p>
        <div className="cta-row">
          <a className="btn btn-solid" href={dl.href}>
            <Download />
            {dl.label}
            {dl.size && <span className="btn-meta">{dl.size} MB</span>}
          </a>
          <a className="btn btn-ghost" href={SITE.repo}>
            <GitHub /> View on GitHub
          </a>
        </div>
        {onPhone ? (
          <p className="cta-note">
            On a phone? There's nothing to install on it. Install Printty on the computer the printer is connected to,
            then open the address it shows.
          </p>
        ) : (
          dl.href !== "#download" && (
            <p className="cta-note">
              Not your system? <a href="#download">All downloads</a>
            </p>
          )
        )}
      </div>

      <div className="spec-card">
        <dl className="specs">
          {SPECS.map((s) => (
            <div className="spec" key={s.label}>
              <dt>{s.label}</dt>
              <dd>
                <span className="mono">{s.value}</span> {s.unit}
              </dd>
            </div>
          ))}
        </dl>
        <p className="source">
          Measured on Windows 11, from the <a href={SITE.readme}>README</a>.
        </p>
      </div>
    </section>
  );
}

/** The product's shape in one row: phones, the one computer running Printty, the printer, and you. */
function Flow() {
  return (
    <div className="wrap flow-wrap">
      <figure className="flow" aria-labelledby="flow-cap">
        <ol className="flow-row">
          <li className="node">
            <span className="node-icon">
              <Phone />
            </span>
            <b>Phones and laptops</b>
            <span>open a web page on your Wi-Fi</span>
          </li>
          <li className="link" aria-hidden="true" />
          <li className="node node-main">
            <span className="node-icon">
              <Computer />
            </span>
            <b>Printty on one computer</b>
            <span>holds the request until you approve</span>
          </li>
          <li className="link" aria-hidden="true" />
          <li className="node">
            <span className="node-icon">
              <Printer />
            </span>
            <b>Your printer</b>
            <span>USB or home printer, prints when it's on</span>
          </li>
        </ol>
        <div className="flow-alert">
          <Bell />
          <span>
            <b>Telegram</b> pings you with who, what, pages, colour and paper.
          </span>
        </div>
        <figcaption id="flow-cap" className="sr-only">
          Phones send a print request to Printty on one computer, Printty tells you on Telegram, and after you approve it
          prints on the printer connected to that computer.
        </figcaption>
      </figure>
    </div>
  );
}

function How() {
  return (
    <section className="wrap how" id="how" aria-labelledby="how-title">
      <div className="section-head rise">
        <h2 id="how-title">One computer runs it. Everyone else just opens a page.</h2>
        <p>
          Printty lives on the computer the printer is plugged into. Phones on the same network don't install anything:
          they open its address in a browser.
        </p>
      </div>
      <ol className="steps">
        {STEPS.map((s, i) => (
          <li className="step rise" key={s.title}>
            <span className="step-n mono" aria-hidden="true">
              {i + 1}
            </span>
            <h3>{s.title}</h3>
            <p>{s.body}</p>
            <ul>
              {s.points.map((p) => (
                <li key={p}>{p}</li>
              ))}
            </ul>
          </li>
        ))}
      </ol>
      <div className="console rise">
        <p className="label">What Printty prints when it starts</p>
        <pre className="code" aria-label="Printty's startup message listing the addresses phones can open">
          <code>{CONSOLE}</code>
        </pre>
        <p className="hint">
          The first time it runs, Windows asks whether to allow it on networks: allow Private networks.
        </p>
      </div>
    </section>
  );
}

function Why() {
  return (
    <section className="wrap principles" aria-labelledby="why-title">
      <h2 id="why-title" className="sr-only">
        Why Printty
      </h2>
      {WHY.map((w) => (
        <div className="rise" key={w.title}>
          <h3>{w.title}</h3>
          <p>{w.body}</p>
        </div>
      ))}
    </section>
  );
}

function Features() {
  return (
    <section className="wrap features" id="features" aria-labelledby="features-title">
      <div className="section-head rise">
        <h2 id="features-title">Built for printing from a phone.</h2>
        <p>The web page is phone-first, in light and dark mode. Admin and approvals work from a laptop too.</p>
      </div>
      <div className="feature-grid">
        {FEATURES.map((f) => (
          <article className="feature rise" key={f.title}>
            <h3>{f.title}</h3>
            <p>{f.body}</p>
          </article>
        ))}
      </div>
    </section>
  );
}

function Printers() {
  return (
    <section className="wrap printers" id="printers" aria-labelledby="printers-title">
      <div className="san-copy rise">
        <h2 id="printers-title">Which printers work.</h2>
        <p>
          Printty works end to end on Windows 11 with a Canon PIXMA G1010 over USB. It prints through the standard
          Windows print interface, so any printer with a working Windows driver should print. Linux and macOS go through
          CUPS; those builds are not yet tested on real printers.
        </p>
        <p>
          <a href={SITE.compat}>Compatibility guide</a>. Tried another printer?{" "}
          <a href={SITE.printerReport}>Tell us how it went</a>.
        </p>
      </div>
      <ul className="facts rise">
        <li>
          <b>HTTPS</b> is needed to install the app on a phone and use the share sheet. The simplest route is{" "}
          <a href={SITE.tailscale}>Tailscale</a>: <code>tailscale cert</code> gives a trusted certificate, and safe remote
          access with it.
        </li>
        <li>
          <b>Never port-forward Printty to the internet.</b>
        </li>
        <li>
          <b>Telegram alerts</b> are set up in <code>printty.env</code> in the data folder, then Restart Printty from the
          tray icon. <a href={SITE.config}>Configuration</a>
        </li>
      </ul>
    </section>
  );
}

function CopyLine({ text }: { text: string }) {
  const [done, setDone] = useState(false);
  const t = useRef<number>(undefined);
  const copy = async () => {
    try {
      await navigator.clipboard.writeText(text);
      setDone(true);
      window.clearTimeout(t.current);
      t.current = window.setTimeout(() => setDone(false), 1600);
    } catch {
      /* clipboard refused; the text stays selectable */
    }
  };
  return (
    <div className="copy-line">
      <code>{text}</code>
      <button type="button" className="copy-btn" onClick={copy} aria-label={done ? "Copied" : "Copy command"} data-done={done}>
        <span className="copy-icons" aria-hidden="true">
          <Copy />
          <Check />
        </span>
      </button>
    </div>
  );
}

function DlLink({ a, label }: { a?: Asset; label: string }) {
  if (!a) return null;
  return (
    <a className="dl" href={a.url}>
      <span className="dl-label">{label}</span>
      <span className="dl-meta mono">{mb(a.size)} MB</span>
      <ArrowRight />
    </a>
  );
}

function Downloads() {
  const a = RELEASE.assets;
  const sys = useSystem();
  const yours = (os: string) => (sys?.os === os ? { "data-yours": "" } : {});
  const tag = (os: string) => sys?.os === os && <span className="yours-tag">Your system</span>;
  const date = new Date(RELEASE.date + "T00:00:00Z").toLocaleDateString("en-GB", {
    day: "numeric",
    month: "long",
    year: "numeric",
    timeZone: "UTC",
  });
  return (
    <section className="wrap downloads" id="download" aria-labelledby="dl-title">
      <div className="section-head rise">
        <h2 id="dl-title">Download Printty.</h2>
        <p>
          Version {RELEASE.version}, released {date}. Install it on the computer the printer is connected to.{" "}
          <a href={RELEASE.page}>Release notes</a>
        </p>
      </div>
      <div className="dl-grid">
        <div className="dl-col rise" {...yours("windows")}>
          <h3>Windows {tag("windows")}</h3>
          <DlLink a={a.windows} label="Installer, x64 (.exe)" />
          <DlLink a={a.windowsArm} label="Installer, Arm (.exe)" />
          <DlLink a={a.windowsPortable} label="Portable, x64 (.zip)" />
          <DlLink a={a.windowsArmPortable} label="Portable, Arm (.zip)" />
          <p className="dl-note">
            The installer runs Printty as a service that starts with Windows and upgrades in place, keeping your users and
            settings. Not code-signed yet: if SmartScreen warns, choose More info, then Run anyway.
          </p>
        </div>
        <div className="dl-col rise" {...yours("linux")}>
          <h3>Linux {tag("linux")}</h3>
          <DlLink a={a.deb} label="Debian, Ubuntu, x64 (.deb)" />
          <DlLink a={a.debArm} label="Debian, Raspberry Pi OS, Arm (.deb)" />
          <DlLink a={a.rpm} label="Fedora, openSUSE, x64 (.rpm)" />
          <DlLink a={a.rpmArm} label="Fedora, openSUSE, Arm (.rpm)" />
          <DlLink a={a.appimage} label="AppImage, x64" />
          <DlLink a={a.linuxTar} label="Archive, x64 (.tar.gz)" />
          <DlLink a={a.linuxArmTar} label="Archive, Arm (.tar.gz)" />
          <p className="dl-note">
            The .deb and .rpm install a systemd service that starts at boot. The AppImage runs Printty while it's open;
            the archive has <code>install.sh</code> for systemd.
          </p>
        </div>
        <div className="dl-col rise" {...yours("mac")}>
          <h3>macOS {tag("mac")}</h3>
          <DlLink a={a.mac} label="Disk image (.dmg)" />
          <DlLink a={a.macArmTar} label="Archive, Apple silicon (.tar.gz)" />
          <DlLink a={a.macIntelTar} label="Archive, Intel (.tar.gz)" />
          <p className="dl-note">
            Drag Printty to Applications and open it; Printty runs while the app is open. Not yet tested on a Mac.
          </p>
        </div>
      </div>
      {a.deb && a.rpm && (
        <div className="pkgs rise">
          <h3>Install a Linux package</h3>
          <div className="pkg-grid">
            <div className="pkg">
              <p className="label">Debian, Ubuntu, Raspberry Pi OS</p>
              <CopyLine text={`sudo apt install ./${a.deb.name}`} />
            </div>
            <div className="pkg">
              <p className="label">Fedora, openSUSE</p>
              <CopyLine text={`sudo dnf install ./${a.rpm.name}`} />
            </div>
          </div>
        </div>
      )}
      {a.checksums && (
        <p className="checksums">
          Check what you downloaded against the SHA-256 of every file in <a href={a.checksums.url}>SHA256SUMS.txt</a>.
        </p>
      )}
    </section>
  );
}

function Footer() {
  return (
    <footer className="footer">
      <div className="wrap footer-inner">
        <p className="footer-statement">
          <Mark size={36} /> Printty
        </p>
        <p className="footer-by">
          Make your printer wireless. By <a href="https://github.com/TriptoAfsin">TriptoAfsin</a>.
        </p>
        <nav className="footer-links" aria-label="Footer">
          <a href={SITE.repo}>GitHub</a>
          <a href={SITE.changelog}>Changelog</a>
          <a href={SITE.compat}>Compatibility</a>
          <a href={SITE.roadmap}>Roadmap</a>
          <a href="https://t21.dev">t21.dev</a>
        </nav>
      </div>
    </footer>
  );
}

export default function App() {
  return (
    <>
      <a className="skip" href="#main">
        Skip to content
      </a>
      <Nav />
      <main id="main">
        <Hero />
        <Flow />
        <How />
        <Why />
        <Features />
        <Printers />
        <Downloads />
      </main>
      <Footer />
    </>
  );
}
