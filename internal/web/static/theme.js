// Applies the saved light/dark choice before first paint, so the page never flashes.
try { var m = localStorage.getItem("printty-mode"); if (m === "light" || m === "dark") document.documentElement.dataset.mode = m; } catch (e) {}
