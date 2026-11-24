// Printty: small progressive enhancements. Every form works without this file.
(function () {
  "use strict";
  var root = document.documentElement;

  // Light/dark toggle: flips the current look and remembers it.
  document.querySelectorAll("[data-mode-toggle]").forEach(function (b) {
    b.addEventListener("click", function () {
      var dark = root.dataset.mode ? root.dataset.mode === "dark" : matchMedia("(prefers-color-scheme: dark)").matches;
      var next = dark ? "light" : "dark";
      root.dataset.mode = next;
      try { localStorage.setItem("printty-mode", next); } catch (e) {}
    });
  });

  // Copies stepper.
  document.querySelectorAll("[data-step]").forEach(function (b) {
    b.addEventListener("click", function () {
      var input = document.getElementById(b.getAttribute("aria-controls"));
      var v = Math.min(20, Math.max(1, (parseInt(input.value, 10) || 1) + parseInt(b.dataset.step, 10)));
      input.value = v;
    });
  });

  // Show the chosen file's name.
  document.querySelectorAll("input[type=file][data-name]").forEach(function (input) {
    input.addEventListener("change", function () {
      var f = input.files && input.files[0];
      var out = document.getElementById(input.dataset.name);
      if (f && out) out.textContent = f.name + " (" + Math.max(1, Math.round(f.size / 1024)) + " KB)";
    });
  });

  // Disable a submit button while its form is sending.
  document.querySelectorAll("form").forEach(function (form) {
    form.addEventListener("submit", function () {
      var b = form.querySelector("button[type=submit][data-busy]");
      if (b) { b.disabled = true; b.textContent = b.dataset.busy; }
    });
  });

  // Live regions: refresh [data-live] from the server while the page is visible.
  var live = document.querySelector("[data-live]");
  if (live) {
    var every = parseInt(live.dataset.live, 10) || 5000;
    setInterval(function () {
      if (document.hidden) return;
      fetch(location.href, { headers: { "X-Live": "1" }, credentials: "same-origin" })
        .then(function (r) { return r.ok ? r.text() : null; })
        .then(function (html) {
          if (!html) return;
          var doc = new DOMParser().parseFromString(html, "text/html");
          var fresh = doc.querySelector("[data-live]");
          if (fresh && fresh.innerHTML !== live.innerHTML) live.innerHTML = fresh.innerHTML;
        })
        .catch(function () {});
    }, every);
  }

  // Copy buttons.
  document.querySelectorAll("[data-copy]").forEach(function (b) {
    b.addEventListener("click", function () {
      var el = document.getElementById(b.dataset.copy);
      el.select();
      if (navigator.clipboard) navigator.clipboard.writeText(el.value);
      b.textContent = "Copied";
    });
  });

  if ("serviceWorker" in navigator && window.isSecureContext) {
    navigator.serviceWorker.register("/sw.js").catch(function () {});
  }
})();
