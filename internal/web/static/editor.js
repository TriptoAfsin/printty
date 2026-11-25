// Printty print editor: uploads the chosen file (or photos), then previews how
// the pages land on paper as the options change. The layout rules mirror
// internal/printer/layout.go, which does the real printing on every OS.
(function () {
  "use strict";

  // Step 1: choosing a file uploads it and reopens the page as the editor.
  var upForm = document.querySelector("form[data-upload]");
  if (upForm) {
    var fileIn = upForm.querySelector("input[type=file]");
    fileIn.addEventListener("change", function () {
      var fs = fileIn.files;
      if (!fs || !fs.length) return;
      var out = document.getElementById(fileIn.dataset.name);
      if (fs.length > 1) {
        for (var i = 0; i < fs.length; i++) {
          if (/pdf$/i.test(fs[i].type) || /\.pdf$/i.test(fs[i].name)) {
            if (out) out.textContent = "Choose one PDF, or several photos";
            fileIn.value = "";
            return;
          }
        }
        if (fs.length > 30) {
          if (out) out.textContent = "Choose up to 30 photos at once";
          fileIn.value = "";
          return;
        }
      }
      if (out) out.textContent = fs.length > 1 ? "Preparing " + fs.length + " photos…" : "Preparing " + fs[0].name + "…";
      upForm.classList.add("is-busy");
      heicToJPEG(Array.prototype.slice.call(fs)).then(function (list) {
        if (list) {
          var dt = new DataTransfer();
          list.forEach(function (f) { dt.items.add(f); });
          fileIn.files = dt.files;
        }
        upForm.action = upForm.dataset.upload;
        upForm.submit();
      }, function () {
        upForm.classList.remove("is-busy");
        fileIn.value = "";
        if (out) out.textContent = "This browser can't open HEIC photos. Choose them from Photos on the iPhone, or save them as JPG first";
      });
    });
  }

  // iPhone photos (HEIC/HEIF) are turned into JPEGs here, since the server
  // reads only PDF, JPG and PNG. The browser decodes them (Safari can; most
  // others can't, which rejects the promise). Drawing applies the EXIF
  // rotation, so the JPEG comes out upright. Resolves to null when nothing
  // needed converting.
  function isHEIC(f) {
    return /^image\/hei[cf](-sequence)?$/i.test(f.type) || /\.hei[cf]$/i.test(f.name);
  }
  function heicToJPEG(files) {
    if (!files.some(isHEIC)) return Promise.resolve(null);
    if (typeof DataTransfer !== "function") return Promise.reject(new Error("no DataTransfer"));
    var done = Promise.resolve([]);
    files.forEach(function (f) {
      done = done.then(function (acc) {
        if (!isHEIC(f)) return acc.concat(f);
        return toJPEG(f).then(function (j) { return acc.concat(j); });
      });
    });
    return done;
  }
  var MAX_PIXELS = 16e6; // iOS Safari refuses canvases above ~16.7 MP; still >300 dpi on A4
  function toJPEG(f) {
    return new Promise(function (resolve, reject) {
      var url = URL.createObjectURL(f);
      var img = new Image();
      img.onload = function () {
        var w = img.naturalWidth, h = img.naturalHeight;
        var s = Math.min(1, Math.sqrt(MAX_PIXELS / (w * h)));
        var c = document.createElement("canvas");
        c.width = Math.max(1, Math.round(w * s));
        c.height = Math.max(1, Math.round(h * s));
        c.getContext("2d").drawImage(img, 0, 0, c.width, c.height);
        URL.revokeObjectURL(url);
        c.toBlob(function (b) {
          c.width = c.height = 0; // free the canvas memory before the next photo
          if (!b) return reject(new Error("encode failed"));
          var name = f.name.replace(/\.[^.]*$/, "") + ".jpg";
          resolve(new File([b], name, { type: "image/jpeg", lastModified: f.lastModified }));
        }, "image/jpeg", 0.92);
      };
      img.onerror = function () { URL.revokeObjectURL(url); reject(new Error("decode failed")); };
      img.src = url;
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
  var placeIn = form.querySelector('input[name="place"]');
  var rangeRadio = form.querySelector('input[name="pagemode"][value="range"]');
  var allRadio = form.querySelector('input[name="pagemode"][value="all"]');
  var submit = form.querySelector('button[type="submit"]');
  var album = ed.hasAttribute("data-album");
  var noun = album ? "photo" : "page";
  form.querySelectorAll("[data-range-ui], .range-input").forEach(function (el) { el.hidden = true; });

  var PAPER = { A4: [8.27, 11.69], Letter: [8.5, 11], Legal: [8.5, 14], A5: [5.83, 8.27], "4x6": [4, 6], "5x7": [5, 7] };
  var MARGIN = 0.17; // typical unprintable edge, inches
  var GAP = 0.12;    // between pages on one sheet (layout.go cellGap)
  var skip = {}, rot = {}, lastRot = {}, place = {};

  function val(name) {
    var el = form.querySelector('input[name="' + name + '"]:checked');
    return el ? el.value : "";
  }
  function pageEls() { return strip.querySelectorAll("[data-page]"); }
  function plural(n, word) { return n + " " + word + (n === 1 ? "" : "s"); }

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
    var g = { 2: [2, 1], 3: [3, 1], 4: [2, 2], 6: [3, 2], 8: [4, 2], 9: [3, 3] }[n] || [1, 1];
    return wide ? g : [g[1], g[0]];
  }

  // How big a w×h page gets in an n-slot grid on an A4-shaped sheet.
  function bestScale(n, w, h, wide) {
    var sw = wide ? 11.4 : 8, sh = wide ? 8 : 11.4, g = grid(n, wide);
    var cw = (sw - GAP * (g[0] - 1)) / g[0], ch = (sh - GAP * (g[1] - 1)) / g[1];
    return Math.min(cw / w, ch / h); // upright, like layout.go
  }

  function rotationFor(p, w, h, cell, o) {
    if (rot[p] !== undefined) return rot[p];
    if (o.autoRotate && (w > h) !== (cell.w > cell.h) && Math.abs(w - h) > 0.01) return 90;
    return 0;
  }

  // gridCell and packGrid mirror layout.go: the columns and rows that cover
  // the most paper with every page at Fit, a turned page counting 0.9 so
  // photos stay upright when it's close; ties go to fewer columns.
  function gridCell(cols, rows, areaW, areaH, n) {
    var gap = n > 1 ? GAP : 0;
    return { w: (areaW - gap * (cols - 1)) / cols, h: (areaH - gap * (rows - 1)) / rows };
  }
  function packGrid(pages, areaW, areaH, o) {
    var n = pages.length, best = { cols: 1, rows: Math.max(n, 1), score: 0 };
    for (var c = 1; c <= n; c++) {
      var r = Math.ceil(n / c);
      if (c > 1 && Math.ceil(n / (c - 1)) === r) continue;
      var cell = gridCell(c, r, areaW, areaH, n), total = 0;
      pages.forEach(function (p) {
        var s = size(p), w = s[0], h = s[1], t = rotationFor(p, w, h, cell, o), weight = 1;
        if (t === 90 || t === 270) { var x = w; w = h; h = x; if (rot[p] === undefined) weight = 0.9; }
        var k = Math.min(cell.w / w, cell.h / h);
        total += w * h * k * k * weight;
      });
      if (total > best.score * 1.0001) best = { cols: c, rows: r, score: total };
    }
    return best;
  }

  function sheetOrientation(o, pages) {
    if (o.orientation) return o.orientation;
    var first = pages.length ? size(pages[0]) : [8.27, 11.69];
    if (o.oneSheet) return packGrid(pages, 11.4, 8, o).score > packGrid(pages, 8, 11.4, o).score * 1.0001 ? "landscape" : "portrait";
    if (o.repeat > 1) return bestScale(o.repeat, first[0], first[1], true) > bestScale(o.repeat, first[0], first[1], false) ? "landscape" : "portrait";
    var n = o.perSheet;
    if (n === 2 || n === 3 || n === 6 || n === 8) return first[0] > first[1] ? "portrait" : "landscape";
    return "portrait";
  }

  function clamp1(v) { return Math.max(-1, Math.min(1, +v || 0)); }
  function clamp01(v) { return Math.max(0, Math.min(1, +v || 0)); }

  // placeIn sizes and turns page p for its cell (layout.go placeIn).
  function placeInCell(p, cell, o, offX, offY) {
    var sz = size(p), w = sz[0], h = sz[1], r = rotationFor(p, w, h, cell, o);
    if (r === 90 || r === 270) { var t = w; w = h; h = t; }
    var scale = o.fit === "fill" ? Math.max(cell.w / w, cell.h / h) : o.fit === "actual" ? 1 : Math.min(cell.w / w, cell.h / h);
    if (o.scale > 0 && o.scale < 100) scale *= Math.max(o.scale, 10) / 100;
    var dw = w * scale, dh = h * scale;
    var fx = (1 + clamp1(offX)) / 2, fy = (1 + clamp1(offY)) / 2;
    return { page: p, rotate: r, cell: cell, rect: { x: cell.x + (cell.w - dw) * fx, y: cell.y + (cell.h - dh) * fy, w: dw, h: dh } };
  }

  function intersect(a, b) {
    var x0 = Math.max(a.x, b.x), y0 = Math.max(a.y, b.y);
    var x1 = Math.min(a.x + a.w, b.x + b.w), y1 = Math.min(a.y + a.h, b.y + b.h);
    return { x: x0, y: y0, w: Math.max(0, x1 - x0), h: Math.max(0, y1 - y0) };
  }

  // Every page on one sheet; a moved page goes where it was put (oneSheet).
  function oneSheet(pages, areaW, areaH, o) {
    var g = packGrid(pages, areaW, areaH, o), n = pages.length, gap = n > 1 ? GAP : 0;
    var c = gridCell(g.cols, g.rows, areaW, areaH, n);
    return pages.map(function (p, i) {
      var row = Math.floor(i / g.cols), col = i % g.cols;
      var inRow = Math.min(g.cols, n - row * g.cols);
      var shift = (g.cols - inRow) * (c.w + gap) / 2;
      var pl = placeInCell(p, { x: shift + col * (c.w + gap), y: row * (c.h + gap), w: c.w, h: c.h }, o, 0, 0);
      pl.cell = intersect(pl.cell, pl.rect);
      var at = place[p];
      if (at) {
        var nx = Math.max(0, Math.min(areaW - pl.cell.w, clamp01(at[0]) * areaW - pl.cell.w / 2));
        var ny = Math.max(0, Math.min(areaH - pl.cell.h, clamp01(at[1]) * areaH - pl.cell.h / 2));
        var dx = nx - pl.cell.x, dy = ny - pl.cell.y;
        pl.cell = { x: nx, y: ny, w: pl.cell.w, h: pl.cell.h };
        pl.rect = { x: pl.rect.x + dx, y: pl.rect.y + dy, w: pl.rect.w, h: pl.rect.h };
      }
      return pl;
    });
  }

  function layout(pages, areaW, areaH, o) {
    if (o.oneSheet) return pages.length ? [oneSheet(pages, areaW, areaH, o)] : [];
    var n = o.repeat > 1 ? o.repeat : o.perSheet;
    if (o.repeat > 1) {
      var rep = [];
      pages.forEach(function (p) { for (var k = 0; k < o.repeat; k++) rep.push(p); });
      pages = rep;
    }
    var g = grid(n, areaW > areaH), cols = g[0], rows = g[1];
    var gap = n > 1 ? GAP : 0;
    var cw = (areaW - gap * (cols - 1)) / cols, ch = (areaH - gap * (rows - 1)) / rows;
    var sheets = [];
    for (var s = 0; s < pages.length; s += n) {
      var sheet = [];
      for (var i = 0; i < n && s + i < pages.length; i++) {
        var cell = { x: (i % cols) * (cw + gap), y: Math.floor(i / cols) * (ch + gap), w: cw, h: ch };
        sheet.push(placeInCell(pages[s + i], cell, o, o.offX, o.offY));
      }
      sheets.push(sheet);
    }
    return sheets;
  }

  // Photo controls (images only).
  var repeatIn = form.querySelector('input[name="repeat"]');
  var scaleIn = form.querySelector('input[name="scale"]');
  var offXIn = form.querySelector('input[name="offx"]'), offYIn = form.querySelector('input[name="offy"]');
  var scaleOut = form.querySelector("[data-scale-out]");
  var dragHint = form.querySelector("[data-drag-hint]");
  var center = form.querySelector("[data-center]");
  var photo = !!(repeatIn || album);
  var stripCount = form.querySelector("[data-strip-count]");
  var lastScale = 1, lastSheets = [], lastArea = [1, 1], lastShifts = [], lastOne = false;
  var bindIn = form.querySelector('input[name="bindmm"]');
  var bindOut = form.querySelector("[data-bind-out]"), bindNote = form.querySelector("[data-bind-note]");

  function el(tag, cls) { var e = document.createElement(tag); if (cls) e.className = cls; return e; }

  function options() {
    var ps = val("persheet");
    return {
      orientation: val("orientation"), fit: val("fit"),
      oneSheet: ps === "all",
      perSheet: +(ps === "all" ? 1 : ps || 1),
      repeat: repeatIn ? +(val("repeat") || 1) : 1,
      scale: scaleIn ? +scaleIn.value : 100,
      offX: offXIn ? +offXIn.value : 0, offY: offYIn ? +offYIn.value : 0,
      autoRotate: form.querySelector('input[name="autorotate"]').checked,
      bindSide: val("bind"), bindMM: bindIn ? +bindIn.value : 0,
      twoSided: val("duplex") === "two-sided"
    };
  }

  // Binding margin, as layout.go: it narrows the printable area, and on the
  // back of a two-sided portrait sheet (a long-edge flip) it swaps sides.
  function bindOnLeft(o, i, wide) {
    var left = o.bindSide === "left";
    if (o.twoSided && i % 2 === 1 && !wide) left = !left;
    return left;
  }

  function includedPages() {
    var included = [];
    pageEls().forEach(function (pe) {
      var p = +pe.dataset.page;
      if (!skip[p]) included.push(p);
    });
    return included;
  }

  // plan lays the pages out on the chosen paper with options o.
  function plan(o, included) {
    var paper = PAPER[val("paper")] || PAPER.A4;
    var land = sheetOrientation(o, included) === "landscape";
    var pw = land ? paper[1] : paper[0], ph = land ? paper[0] : paper[1];
    var areaW = pw - 2 * MARGIN, areaH = ph - 2 * MARGIN;
    var bind = o.bindSide && o.bindMM > 0 ? o.bindMM / 25.4 : 0;
    var g = Math.max(0, Math.min(areaW / 2, bind - MARGIN));
    var sheets = layout(included, areaW - g, areaH, o), shifts = [], bands = [];
    sheets.forEach(function (sh, i) {
      var left = bind > 0 && bindOnLeft(o, i, pw > ph);
      shifts.push(left ? g : 0);
      bands.push(bind > 0 ? { left: left, w: bind } : null);
      if (left) sh.forEach(function (pl) { pl.rect.x += g; pl.cell.x += g; });
    });
    return { paper: paper, pw: pw, ph: ph, area: [areaW - g, areaH], sheets: sheets, shifts: shifts, bands: bands };
  }

  // drawSheet draws one sheet of a plan at scale (px per inch). band shows
  // the binding margin: {left, w} with w in inches from the paper's edge.
  function drawSheet(sh, i, pw, ph, scale, canDrag, band) {
    var se = el("div", "sheet" + (canDrag ? " can-drag" : ""));
    se.style.width = (pw * scale) + "px";
    se.style.height = (ph * scale) + "px";
    se.setAttribute("aria-label", "Sheet " + (i + 1));
    if (band) {
      var be = el("div", "bind-band " + (band.left ? "left" : "right"));
      be.style.width = (band.w * scale) + "px";
      se.appendChild(be);
    }
    sh.forEach(function (pl, slot) {
      var ce = el("div", "cell");
      ce.style.left = ((pl.cell.x + MARGIN) * scale) + "px";
      ce.style.top = ((pl.cell.y + MARGIN) * scale) + "px";
      ce.style.width = (pl.cell.w * scale) + "px";
      ce.style.height = (pl.cell.h * scale) + "px";
      var box = el("div", "placed");
      box.dataset.sheet = i;
      box.dataset.slot = slot;
      box.style.left = ((pl.rect.x - pl.cell.x) * scale) + "px";
      box.style.top = ((pl.rect.y - pl.cell.y) * scale) + "px";
      box.style.width = (pl.rect.w * scale) + "px";
      box.style.height = (pl.rect.h * scale) + "px";
      var src = strip.querySelector('[data-page="' + pl.page + '"] img');
      var turned = pl.rotate === 90 || pl.rotate === 270;
      var inner = src ? el("img") : el("span", "placed-blank");
      if (src) { inner.src = src.src; inner.alt = ""; inner.draggable = false; } else { inner.textContent = pl.page; }
      inner.style.width = ((turned ? pl.rect.h : pl.rect.w) * scale) + "px";
      inner.style.height = ((turned ? pl.rect.w : pl.rect.h) * scale) + "px";
      inner.style.transform = "translate(-50%, -50%) rotate(" + pl.rotate + "deg)";
      box.appendChild(inner);
      ce.appendChild(box);
      se.appendChild(ce);
    });
    return se;
  }

  function render() {
    var included = includedPages();
    var o = options();
    lastOne = o.oneSheet;
    if (scaleOut) scaleOut.textContent = o.scale + "%";
    if (dragHint) dragHint.textContent = o.oneSheet ? "Drag a photo in the preview to move it on the sheet." :
      album ? "Drag in the preview to move every photo within its space." : "Drag the photo in the preview to move it.";
    if (center) center.textContent = o.oneSheet ? "Line up" : "Centre";
    var pn = plan(o, included), paper = pn.paper, pw = pn.pw, ph = pn.ph, sheets = pn.sheets;
    lastArea = pn.area;
    lastShifts = pn.shifts;
    if (bindOut) bindOut.textContent = o.bindMM + " mm";
    if (bindNote) bindNote.textContent = "Measured from the edge of the paper." + (o.twoSided && sheets.length > 1 ? (pw > ph ? " It stays on the same side on the back of each sheet." : " On the back of each sheet it moves to the other side, so it lines up with the binding.") : "");

    // Remember the turn each page got, so ↻ continues from what you see.
    lastRot = {};
    sheets.forEach(function (sh) { sh.forEach(function (pl) { lastRot[pl.page] = pl.rotate; }); });

    // Draw up to 24 sheets; a single sheet of photos is drawn bigger.
    var scale = (photo ? (o.oneSheet ? 280 : 200) : 120) / Math.min(paper[0], paper[1]);
    var room = sheetsEl.clientWidth;
    if (room > 0 && pw * scale > room) scale = room / pw;
    lastScale = scale;
    lastSheets = sheets;
    sheetsEl.textContent = "";
    sheets.slice(0, 24).forEach(function (sh, i) {
      var se = drawSheet(sh, i, pw, ph, scale, photo, pn.bands[i]);
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
    var text = !included.length ? "No " + noun + "s chosen" :
      o.repeat > 1 ? o.repeat + " copies on " + plural(paperCount * copies, "sheet") :
      plural(included.length, noun) + " on " + plural(paperCount * copies, "sheet") + (twoSided && sheets.length > 1 ? ", both sides" : "");
    countEl.textContent = text;
    var tooMany = o.oneSheet && included.length > 30;
    if (tooMany) countEl.textContent = "Up to 30 fit on one sheet";
    if (submit) submit.disabled = !included.length || tooMany;

    // Strip: show skipped pages, each page's turn and its place in the order.
    var pos = 0;
    pageEls().forEach(function (pe) {
      var p = +pe.dataset.page, off = !!skip[p];
      if (!off) pos++;
      pe.classList.toggle("off", off);
      var b = pe.querySelector("[data-toggle]");
      var label = (album ? "Photo " : "Page ") + p;
      b.setAttribute("aria-pressed", String(!off));
      b.setAttribute("aria-label", label + (off ? ": left out. Tap to include" : ": " + ordinal(pos) + " in print order. Tap to leave out"));
      if (album) pe.querySelector("[data-pos]").textContent = off ? "–" : pos;
      var img = pe.querySelector("img");
      var r = off ? (rot[p] || 0) : (lastRot[p] || 0);
      if (img) img.style.transform = r ? "rotate(" + r + "deg)" : "";
      pe.classList.toggle("turned", r === 90 || r === 270);
    });
    if (stripCount) {
      var all = pageEls().length;
      stripCount.textContent = included.length === all ? "All " + plural(all, noun) + " in the print" :
        included.length + " of " + plural(all, noun) + " in the print";
    }

    // Hidden fields for the server. The page list carries the order.
    var total = pageEls().length, inOrder = true;
    for (var k = 0; k < included.length; k++) if (included[k] !== k + 1) inOrder = false;
    if (included.length === total && inOrder) {
      allRadio.checked = true;
      pagesIn.value = "";
    } else {
      rangeRadio.checked = true;
      pagesIn.value = ranges(included);
    }
    rotIn.value = Object.keys(rot).map(function (p) { return p + ":" + rot[p]; }).join(",");
    if (placeIn) {
      placeIn.value = o.oneSheet ? Object.keys(place).filter(function (p) { return !skip[p]; }).map(function (p) {
        return p + ":" + place[p][0].toFixed(3) + ":" + place[p][1].toFixed(3);
      }).join(",") : "";
    }
  }

  function ordinal(n) {
    var s = ["th", "st", "nd", "rd"], v = n % 100;
    return n + (s[(v - 20) % 10] || s[v] || s[0]);
  }

  // ranges turns [1,2,3,5] into "1-3,5", keeping the order: [3,1,2] is "3,1-2".
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

  // Drag in the preview. On one sheet each photo moves on its own; otherwise
  // every copy moves the same way within its space.
  var drag = null;
  sheetsEl.addEventListener("pointerdown", function (e) {
    var box = e.target.closest(".placed");
    if (!photo || !box) return;
    var pl = (lastSheets[+box.dataset.sheet] || [])[+box.dataset.slot];
    if (!pl) return;
    e.preventDefault();
    drag = { x: e.clientX, y: e.clientY, pl: pl, one: lastOne };
    if (lastOne) {
      drag.cx = pl.cell.x - (lastShifts[+box.dataset.sheet] || 0) + pl.cell.w / 2; // in the area left beside the margin
      drag.cy = pl.cell.y + pl.cell.h / 2;
    } else {
      drag.offX = +offXIn.value;
      drag.offY = +offYIn.value;
      drag.fsX = (pl.cell.w - pl.rect.w) * lastScale;
      drag.fsY = (pl.cell.h - pl.rect.h) * lastScale;
    }
  });
  window.addEventListener("pointermove", function (e) {
    if (!drag) return;
    if (drag.one) {
      var aw = lastArea[0], ah = lastArea[1], w = drag.pl.cell.w, h = drag.pl.cell.h;
      var cx = Math.max(w / 2, Math.min(aw - w / 2, drag.cx + (e.clientX - drag.x) / lastScale));
      var cy = Math.max(h / 2, Math.min(ah - h / 2, drag.cy + (e.clientY - drag.y) / lastScale));
      place[drag.pl.page] = [cx / aw, cy / ah];
    } else {
      // left = free * (1 + off) / 2, so moving dx changes off by 2·dx/free.
      if (Math.abs(drag.fsX) > 1) offXIn.value = clamp1(drag.offX + 2 * (e.clientX - drag.x) / drag.fsX).toFixed(3);
      if (Math.abs(drag.fsY) > 1) offYIn.value = clamp1(drag.offY + 2 * (e.clientY - drag.y) / drag.fsY).toFixed(3);
    }
    render();
  });
  window.addEventListener("pointerup", function () { drag = null; });
  if (center) center.addEventListener("click", function () {
    if (lastOne) place = {};
    else { offXIn.value = 0; offYIn.value = 0; }
    render();
  });
  if (scaleIn) scaleIn.addEventListener("input", render);
  if (bindIn) bindIn.addEventListener("input", render);

  // Reorder the strip by dragging a page: from its ⠿ handle straight away,
  // from anywhere with a mouse, or after a short hold on a touch screen (so
  // the strip still scrolls). The page lifts and follows the pointer while a
  // dashed slot shows where it will land. Alt+arrow keys move the focused page.
  var sort = null, eatClick = false;
  strip.addEventListener("pointerdown", function (e) {
    var pe = e.target.closest("[data-page]");
    if (!pe || e.target.closest("[data-rotate]") || (e.pointerType === "mouse" && e.button !== 0)) return;
    if (sort && sort.on) return; // a second finger doesn't start a second drag
    endSort();
    sort = { pe: pe, x: e.clientX, y: e.clientY, px: e.clientX, py: e.clientY, on: false, mouse: e.pointerType === "mouse" };
    if (e.target.closest("[data-grip]")) { e.preventDefault(); startSort(); return; }
    if (!sort.mouse) sort.timer = setTimeout(function () { if (sort) startSort(); }, 280);
  });
  function startSort() {
    sort.on = true;
    var box = sort.pe.querySelector(".pg-img"), r = box.getBoundingClientRect();
    var ghost = el("div", "pg-ghost");
    ghost.appendChild(box.cloneNode(true));
    ghost.style.width = r.width + "px";
    ghost.style.height = r.height + "px";
    sort.dx = sort.x - r.left;
    sort.dy = sort.y - r.top;
    document.body.appendChild(ghost);
    sort.ghost = ghost;
    moveGhost();
    sort.pe.classList.add("dragging");
    strip.classList.add("sorting");
    if (navigator.vibrate) navigator.vibrate(10);
    sort.raf = requestAnimationFrame(edgeScroll);
  }
  function moveGhost() {
    sort.ghost.style.transform = "translate(" + (sort.px - sort.dx) + "px," + (sort.py - sort.dy) + "px)";
  }
  // Scroll while the pointer rests near an edge: the strip when it scrolls
  // on its own (wide screens), otherwise the page (phones). The top edge
  // starts below the sticky header.
  function edgeScroll() {
    if (!sort || !sort.on) return;
    var own = strip.scrollHeight > strip.clientHeight + 1;
    var r = own ? strip.getBoundingClientRect() : { top: headerBottom(), bottom: innerHeight };
    var edge = 56, step = 0;
    if (sort.py < r.top + edge) step = -Math.ceil((r.top + edge - sort.py) / 5);
    else if (sort.py > r.bottom - edge) step = Math.ceil((sort.py - r.bottom + edge) / 5);
    if (step) {
      if (own) strip.scrollTop += step; else window.scrollBy(0, step);
      reorderAt(sort.px, sort.py);
    }
    sort.raf = requestAnimationFrame(edgeScroll);
  }
  function headerBottom() {
    var h = document.querySelector("header.top");
    return h && getComputedStyle(h).position === "sticky" ? h.getBoundingClientRect().bottom : 0;
  }
  function endSort() {
    if (!sort) return;
    clearTimeout(sort.timer);
    if (sort.on) {
      eatClick = true;
      setTimeout(function () { eatClick = false; }, 0);
      cancelAnimationFrame(sort.raf);
      sort.ghost.remove();
      sort.pe.classList.remove("dragging");
      strip.classList.remove("sorting");
    }
    sort = null;
  }
  window.addEventListener("pointermove", function (e) {
    if (!sort) return;
    var moved = Math.abs(e.clientX - sort.x) + Math.abs(e.clientY - sort.y);
    sort.px = e.clientX;
    sort.py = e.clientY;
    if (!sort.on) {
      if (sort.mouse && moved > 6) startSort();
      else if (!sort.mouse && moved > 10) { clearTimeout(sort.timer); sort = null; } // a scroll
      if (!sort || !sort.on) return;
    }
    e.preventDefault();
    moveGhost();
    reorderAt(e.clientX, e.clientY);
  });
  function reorderAt(x, y) {
    var under = document.elementFromPoint(x, y);
    var target = under && under.closest("[data-page]");
    if (!target || target === sort.pe || target.parentNode !== strip) return;
    var r = target.getBoundingClientRect();
    var after = x > r.left + r.width / 2;
    var ref = after ? target.nextSibling : target;
    if (ref !== sort.pe && ref !== sort.pe.nextSibling) {
      strip.insertBefore(sort.pe, ref);
      render();
    }
  }
  window.addEventListener("pointerup", endSort);
  window.addEventListener("pointercancel", endSort);
  window.addEventListener("blur", endSort);
  // While sorting on a touch screen, the finger moves the page, not the strip.
  strip.addEventListener("touchmove", function (e) { if (sort && sort.on) e.preventDefault(); }, { passive: false });
  strip.addEventListener("contextmenu", function (e) { if (sort) e.preventDefault(); });
  strip.addEventListener("keydown", function (e) {
    if (!e.altKey || !e.target.closest("[data-toggle]")) return;
    var pe = e.target.closest("[data-page]");
    if (e.key === "ArrowLeft" || e.key === "ArrowUp") {
      if (!pe.previousElementSibling) return;
      strip.insertBefore(pe, pe.previousElementSibling);
    } else if (e.key === "ArrowRight" || e.key === "ArrowDown") {
      if (!pe.nextElementSibling) return;
      strip.insertBefore(pe.nextElementSibling, pe);
    } else {
      return;
    }
    e.preventDefault();
    e.target.focus();
    render();
  });

  strip.addEventListener("click", function (e) {
    if (eatClick) { eatClick = false; return; }
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
  window.addEventListener("resize", render);
  strip.querySelectorAll("img").forEach(function (img) {
    if (!img.complete) img.addEventListener("load", render, { once: true });
  });
  render();

  // Several photos: ask once per draft how they should go on paper. Each
  // choice shows its first sheet with the real photos and the sheet count.
  // Picking one sets "Photos per sheet" below, where it can still change.
  var count = pageEls().length;
  if (album && count > 1 && !seenLayout()) {
    var imgs = Array.prototype.slice.call(strip.querySelectorAll("img"));
    var loaded = Promise.all(imgs.map(function (img) {
      return img.complete ? null : new Promise(function (ok) { img.addEventListener("load", ok); img.addEventListener("error", ok); });
    }));
    Promise.race([loaded, new Promise(function (ok) { setTimeout(ok, 2000); })]).then(layoutDialog);
  }
  function seenLayout() {
    try {
      var k = "printty-layout-" + ed.dataset.job;
      if (sessionStorage.getItem(k)) return true;
      sessionStorage.setItem(k, "1");
    } catch (e) {}
    return false;
  }
  function layoutDialog() {
    var choices = [1, 2, 3, 4, 6, 9].filter(function (n) { return n < count; }).map(String);
    if (count <= 30) choices.push("all");
    var cur = val("persheet") || "1";
    var d = el("dialog", "confirm layout-pick");
    d.setAttribute("aria-labelledby", "lp-title");
    d.innerHTML = '<form method="dialog" class="stack"><h2 class="head" id="lp-title"></h2>' +
      '<p class="small muted">Pick a layout. You can change it later under Photos per sheet.</p>' +
      '<div class="lp-grid"></div><div class="confirm-acts"><button class="btn ghost" value="">Not now</button></div></form>';
    d.querySelector("h2").textContent = "How should the " + count + " photos print?";
    var gridEl = d.querySelector(".lp-grid"), included = includedPages(), base = options();
    choices.forEach(function (c) {
      var o = Object.assign({}, base, { oneSheet: c === "all", perSheet: c === "all" ? 1 : +c });
      var pn = plan(o, included), n = pn.sheets.length;
      var b = el("button", "lp-opt");
      b.value = c;
      b.setAttribute("aria-pressed", String(c === cur));
      var holder = el("span", "lp-sheet");
      var scale = 84 / Math.max(pn.pw, pn.ph);
      var se = drawSheet(pn.sheets[0] || [], 0, pn.pw, pn.ph, scale, false, pn.bands[0]);
      se.removeAttribute("aria-label");
      if (n > 1) se.classList.add("more");
      holder.appendChild(se);
      var name = el("span", "lp-name");
      name.textContent = c === "all" ? "All on one sheet" : c === "1" ? "1 per sheet" : c + " per sheet";
      var sub = el("span", "lp-sub small muted");
      sub.textContent = plural(n, "sheet");
      b.append(holder, name, sub);
      gridEl.appendChild(b);
    });
    d.addEventListener("click", function (e) { if (e.target === d) d.close(""); });
    d.addEventListener("close", function () {
      var c = d.returnValue, radio = c && form.querySelector('input[name="persheet"][value="' + c + '"]');
      if (radio) { radio.checked = true; render(); }
      d.remove();
    });
    document.body.appendChild(d);
    d.showModal();
    (gridEl.querySelector('[aria-pressed="true"]') || gridEl.firstChild).focus();
  }
})();
