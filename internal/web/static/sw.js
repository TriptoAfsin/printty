// Printty service worker: caches the app shell so the installed app opens
// instantly and says plainly when the computer running Printty is off.
const CACHE = "printty-v4";
const SHELL = ["/static/tokens.css", "/static/base.css", "/static/app.css", "/static/app.js", "/static/editor.js", "/static/theme.js",
  "/static/icon.svg", "/static/icon-192.png", "/static/fonts/figtree.woff2", "/static/fonts/instrument-sans.woff2", "/static/offline.html"];

self.addEventListener("install", (e) => {
  e.waitUntil(caches.open(CACHE).then((c) => c.addAll(SHELL)).then(() => self.skipWaiting()));
});

self.addEventListener("activate", (e) => {
  e.waitUntil(caches.keys().then((keys) => Promise.all(keys.filter((k) => k !== CACHE).map((k) => caches.delete(k)))).then(() => self.clients.claim()));
});

self.addEventListener("fetch", (e) => {
  const req = e.request;
  if (req.method !== "GET") return; // uploads and the share target go straight to the server
  const url = new URL(req.url);
  if (url.pathname.startsWith("/static/")) {
    e.respondWith(caches.match(req).then((hit) => hit || fetch(req)));
    return;
  }
  if (req.mode === "navigate") {
    e.respondWith(fetch(req).catch(() => caches.match("/static/offline.html")));
  }
});
