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

  // Confirm before destructive actions: forms with data-confirm ask first in
  // a dialog (Escape or Keep cancels). Delegated, so it covers live-refreshed
  // content too, and runs in the capture phase, before the busy-button handler.
  var dlg;
  function confirmDialog() {
    if (dlg) return dlg;
    dlg = document.createElement("dialog");
    dlg.className = "confirm";
    dlg.innerHTML = '<form method="dialog" class="stack"><h2 class="head"></h2><p class="small muted"></p>' +
      '<div class="confirm-acts"><button class="btn ghost" value="no">Keep</button><button class="btn danger" value="yes"></button></div></form>';
    dlg.addEventListener("click", function (e) { if (e.target === dlg) dlg.close("no"); }); // click outside
    document.body.appendChild(dlg);
    return dlg;
  }
  document.addEventListener("submit", function (e) {
    var form = e.target;
    if (!form.dataset || !form.dataset.confirm) return;
    if (form.dataset.confirmed) { delete form.dataset.confirmed; return; }
    e.preventDefault();
    e.stopPropagation();
    var d = confirmDialog();
    d.querySelector("h2").textContent = form.dataset.confirm;
    d.querySelector("p").textContent = form.dataset.confirmDetail || "";
    d.querySelector("p").hidden = !form.dataset.confirmDetail;
    var ok = d.querySelector('[value="yes"]');
    ok.textContent = form.dataset.confirmOk || "OK";
    d.returnValue = "";
    d.onclose = function () {
      if (d.returnValue !== "yes") return;
      form.dataset.confirmed = "1";
      if (form.requestSubmit) form.requestSubmit(); else form.submit();
    };
    d.showModal();
    d.querySelector('[value="no"]').focus(); // the safe choice has focus
  }, true);

  // Copies stepper.
  document.querySelectorAll("[data-step]").forEach(function (b) {
    b.addEventListener("click", function () {
      var input = document.getElementById(b.getAttribute("aria-controls"));
      var v = Math.min(20, Math.max(1, (parseInt(input.value, 10) || 1) + parseInt(b.dataset.step, 10)));
      input.value = v;
    });
  });

  // Print quality: say what the chosen level trades off.
  var qualityHints = { draft: "Faster, uses less ink", standard: "Balanced speed and quality", high: "Sharpest, slower, more ink" };
  document.querySelectorAll("[data-quality-hint]").forEach(function (out) {
    var set = out.closest("fieldset");
    function show() {
      var c = set.querySelector("input:checked");
      out.textContent = (c && qualityHints[c.value]) || "";
    }
    set.addEventListener("change", show);
    show();
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

  // Live updates while the page is visible: the [data-live] region (every few
  // seconds) and the header's [data-sync] parts (printer status, request
  // count), so they never disagree. Pages without a live region check every 30 s.
  var live = document.querySelector("[data-live]");
  var syncs = document.querySelectorAll("[data-sync]");
  if (live || syncs.length) {
    var every = live ? parseInt(live.dataset.live, 10) || 5000 : 30000;
    setInterval(function () {
      if (document.hidden) return;
      fetch(location.href, { headers: { "X-Live": "1" }, credentials: "same-origin" })
        .then(function (r) { return r.ok && !r.redirected ? r.text() : null; })
        .then(function (html) {
          if (!html) return;
          var doc = new DOMParser().parseFromString(html, "text/html");
          var fresh = live && doc.querySelector("[data-live]");
          if (fresh && fresh.innerHTML !== live.innerHTML) live.innerHTML = fresh.innerHTML;
          syncs.forEach(function (el) {
            var f = doc.querySelector('[data-sync="' + el.dataset.sync + '"]');
            if (f && f.innerHTML !== el.innerHTML) el.innerHTML = f.innerHTML;
          });
        })
        .catch(function () {});
    }, every);
  }

  // Emoji palette: inserts into the message box last used (Settings).
  var lastTpl = null;
  document.addEventListener("focusin", function (e) { if (e.target.matches && e.target.matches("[data-tpl]")) lastTpl = e.target; });
  document.querySelectorAll("[data-emoji]").forEach(function (b) {
    b.addEventListener("mousedown", function (e) { e.preventDefault(); }); // keep the caret where it is
    b.addEventListener("click", function () {
      var t = lastTpl || document.querySelector("[data-tpl]");
      if (!t) return;
      var s = t.selectionStart != null ? t.selectionStart : t.value.length, end = t.selectionEnd != null ? t.selectionEnd : s;
      t.value = t.value.slice(0, s) + b.dataset.emoji + t.value.slice(end);
      t.focus();
      t.selectionStart = t.selectionEnd = s + b.dataset.emoji.length;
    });
  });

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
