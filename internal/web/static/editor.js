// Printty print editor: uploads the chosen file, then previews how its pages
// land on paper as the options change. The layout rules mirror
// internal/printer/layout.go, which does the real printing on every OS.
(function () {
  "use strict";

  // Step 1: choosing a file uploads it and reopens the page as the editor.
  var upForm = document.querySelector("form[data-upload]");
  if (upForm) {
    var fileIn = upForm.querySelector("input[type=file]");
    fileIn.addEventListener("change", function () {
      if (!fileIn.files || !fileIn.files.length) return;
      var out = document.getElementById(fileIn.dataset.name);
      if (out) out.textContent = "Preparing " + fileIn.files[0].name + "…";
      upForm.classList.add("is-busy");
      upForm.action = upForm.dataset.upload;
      upForm.submit();
    });
  }

  var ed = document.querySelector("[data-editor]");
  if (!ed) return;
  var form = ed.closest("form");
  var strip = form.querySelector("[data-pagestrip]");
  var sheetsEl = ed.querySelector("[data-sheets]");
  var countEl = ed.querySelector("[data-sheet-count]");
  var rotIn = form.querySelector('input[name="rotate"]');
  var pagesIn = form.querySelector('input[name="pages"]');
  var rangeRadio = form.querySelector('input[name="pagemode"][value="range"]');
  var allRadio = form.querySelector('input[name="pagemode"][value="all"]');
  var submit = form.querySelector('button[type="submit"]');
  form.querySelectorAll("[data-range-ui], .range-input").forEach(function (el) { el.hidden = true; });

  var PAPER = { A4: [8.27, 11.69], Letter: [8.5, 11], Legal: [8.5, 14], A5: [5.83, 8.27], "4x6": [4, 6], "5x7": [5, 7] };
  var MARGIN = 0.17; // typical unprintable edge, inches
  var GAP = 0.12;    // between pages on one sheet (layout.go cellGap)
  var skip = {}, rot = {}, lastRot = {};

  function val(name) {
    var el = form.querySelector('input[name="' + name + '"]:checked');
    return el ? el.value : "";
  }
  function pageEls() { return strip.querySelectorAll("[data-page]"); }

  // Page size in inches. Previews give the shape; the long side is assumed A4.
  function size(p) {
    var img = strip.querySelector('[data-page="' + p + '"] img');
    if (img && img.naturalWidth && img.naturalHeight) {
      var k = 11.69 / Math.max(img.naturalWidth, img.naturalHeight);
      return [img.naturalWidth * k, img.naturalHeight * k];
    }
    return [8.27, 11.69];
  }

  function grid(n, wide) {
    var g = { 2: [2, 1], 4: [2, 2], 6: [3, 2], 9: [3, 3] }[n] || [1, 1];
    return wide ? g : [g[1], g[0]];
  }

  function sheetOrientation(o, first) {
    if (o.orientation) return o.orientation;
    if (o.perSheet === 2 || o.perSheet === 6) return first[0] > first[1] ? "portrait" : "landscape";
    return "portrait";
  }

  function layout(pages, areaW, areaH, o) {
    var n = o.perSheet, g = grid(n, areaW > areaH), cols = g[0], rows = g[1];
    var gap = n > 1 ? GAP : 0;
    var cw = (areaW - gap * (cols - 1)) / cols, ch = (areaH - gap * (rows - 1)) / rows;
    var sheets = [];
    for (var s = 0; s < pages.length; s += n) {
      var sheet = [];
      for (var i = 0; i < n && s + i < pages.length; i++) {
        var p = pages[s + i];
        var cell = { x: (i % cols) * (cw + gap), y: Math.floor(i / cols) * (ch + gap), w: cw, h: ch };
        var sz = size(p), w = sz[0], h = sz[1], r = 0;
        if (rot[p] !== undefined) r = rot[p];
        else if (o.autoRotate && (w > h) !== (cell.w > cell.h) && Math.abs(w - h) > 0.01) r = 90;
        if (r === 90 || r === 270) { var t = w; w = h; h = t; }
        var scale = o.fit === "fill" ? Math.max(cell.w / w, cell.h / h) : o.fit === "actual" ? 1 : Math.min(cell.w / w, cell.h / h);
        var dw = w * scale, dh = h * scale;
        sheet.push({ page: p, rotate: r, cell: cell, rect: { x: cell.x + (cell.w - dw) / 2, y: cell.y + (cell.h - dh) / 2, w: dw, h: dh } });
      }
      sheets.push(sheet);
    }
    return sheets;
  }

  function el(tag, cls) { var e = document.createElement(tag); if (cls) e.className = cls; return e; }

  function render() {
    var included = [];
    pageEls().forEach(function (pe) {
      var p = +pe.dataset.page;
      if (!skip[p]) included.push(p);
    });
    var o = {
      orientation: val("orientation"), fit: val("fit"),
      perSheet: +(val("persheet") || 1),
      autoRotate: form.querySelector('input[name="autorotate"]').checked
    };
    var paper = PAPER[val("paper")] || PAPER.A4;
    var first = included.length ? size(included[0]) : [8.27, 11.69];
    var land = sheetOrientation(o, first) === "landscape";
    var pw = land ? paper[1] : paper[0], ph = land ? paper[0] : paper[1];
    var sheets = layout(included, pw - 2 * MARGIN, ph - 2 * MARGIN, o);

    // Remember the turn each page got, so ↻ continues from what you see.
    lastRot = {};
    sheets.forEach(function (sh) { sh.forEach(function (pl) { lastRot[pl.page] = pl.rotate; }); });

    // Draw up to 24 sheets.
    var scale = 120 / Math.min(paper[0], paper[1]);
    sheetsEl.textContent = "";
    sheets.slice(0, 24).forEach(function (sh, i) {
      var se = el("div", "sheet");
      se.style.width = (pw * scale) + "px";
      se.style.height = (ph * scale) + "px";
      se.setAttribute("aria-label", "Sheet " + (i + 1));
      sh.forEach(function (pl) {
        var ce = el("div", "cell");
        ce.style.left = ((pl.cell.x + MARGIN) * scale) + "px";
        ce.style.top = ((pl.cell.y + MARGIN) * scale) + "px";
        ce.style.width = (pl.cell.w * scale) + "px";
        ce.style.height = (pl.cell.h * scale) + "px";
        var box = el("div", "placed");
        box.style.left = ((pl.rect.x - pl.cell.x) * scale) + "px";
        box.style.top = ((pl.rect.y - pl.cell.y) * scale) + "px";
        box.style.width = (pl.rect.w * scale) + "px";
        box.style.height = (pl.rect.h * scale) + "px";
        var src = strip.querySelector('[data-page="' + pl.page + '"] img');
        var turned = pl.rotate === 90 || pl.rotate === 270;
        var inner = src ? el("img") : el("span", "placed-blank");
        if (src) { inner.src = src.src; inner.alt = ""; } else { inner.textContent = pl.page; }
        inner.style.width = ((turned ? pl.rect.h : pl.rect.w) * scale) + "px";
        inner.style.height = ((turned ? pl.rect.w : pl.rect.h) * scale) + "px";
        inner.style.transform = "translate(-50%, -50%) rotate(" + pl.rotate + "deg)";
        box.appendChild(inner);
        ce.appendChild(box);
        se.appendChild(ce);
      });
      var num = el("span", "sheet-num");
      num.textContent = i + 1;
      se.appendChild(num);
      sheetsEl.appendChild(se);
    });
    if (sheets.length > 24) {
      var more = el("span", "small muted");
      more.textContent = "+" + (sheets.length - 24) + " more";
      sheetsEl.appendChild(more);
    }

    // Summary.
    var copies = Math.max(1, +(form.querySelector('input[name="copies"]').value || 1));
    var twoSided = val("duplex") !== "one-sided";
    var paperCount = twoSided ? Math.ceil(sheets.length / 2) : sheets.length;
    var text = !included.length ? "No pages chosen" :
      included.length + (included.length === 1 ? " page" : " pages") + " on " + paperCount * copies +
      (paperCount * copies === 1 ? " sheet" : " sheets") + (twoSided ? ", both sides" : "");
    countEl.textContent = text;
    if (submit) submit.disabled = !included.length;

    // Strip: show skipped pages and each page's turn.
    pageEls().forEach(function (pe) {
      var p = +pe.dataset.page, off = !!skip[p];
      pe.classList.toggle("off", off);
      var b = pe.querySelector("[data-toggle]");
      b.setAttribute("aria-pressed", String(!off));
      b.setAttribute("aria-label", "Page " + p + (off ? ": left out. Tap to include" : ": included. Tap to leave out"));
      var img = pe.querySelector("img");
      var r = off ? (rot[p] || 0) : (lastRot[p] || 0);
      if (img) img.style.transform = r ? "rotate(" + r + "deg)" : "";
      pe.classList.toggle("turned", r === 90 || r === 270);
    });

    // Hidden fields for the server.
    var total = pageEls().length;
    if (included.length === total) {
      allRadio.checked = true;
      pagesIn.value = "";
    } else {
      rangeRadio.checked = true;
      pagesIn.value = ranges(included);
    }
    rotIn.value = Object.keys(rot).map(function (p) { return p + ":" + rot[p]; }).join(",");
  }

  // ranges turns [1,2,3,5] into "1-3,5".
  function ranges(list) {
    var out = [], i = 0;
    while (i < list.length) {
      var j = i;
      while (j + 1 < list.length && list[j + 1] === list[j] + 1) j++;
      out.push(i === j ? String(list[i]) : list[i] + "-" + list[j]);
      i = j + 1;
    }
    return out.join(",");
  }

  strip.addEventListener("click", function (e) {
    var pe = e.target.closest("[data-page]");
    if (!pe) return;
    var p = +pe.dataset.page;
    if (e.target.closest("[data-rotate]")) {
      var cur = rot[p] !== undefined ? rot[p] : (lastRot[p] || 0);
      rot[p] = (cur + 90) % 360;
      if (skip[p]) delete skip[p];
    } else if (e.target.closest("[data-toggle]")) {
      if (skip[p]) delete skip[p]; else skip[p] = true;
    } else {
      return;
    }
    render();
  });
  form.addEventListener("change", render);
  form.addEventListener("click", function (e) { if (e.target.closest("[data-step]")) setTimeout(render, 0); });
  strip.querySelectorAll("img").forEach(function (img) {
    if (!img.complete) img.addEventListener("load", render, { once: true });
  });
  render();
})();
