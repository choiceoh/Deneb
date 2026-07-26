package ai.deneb.deneb

/**
 * On-device scroll diagnostics for the in-app browser.
 *
 * Why this exists: the in-app browser is Android-only (the desktop target is a
 * stub), so the headless harness cannot open it, and a page that refuses to
 * scroll on the device has repeatedly resisted diagnosis from the server side.
 * Remote probing of the same URL in mobile Chromium (2026-07-26) showed a
 * perfectly scrollable document — no lock, no overlay — which means the
 * difference lives in the WebView, not in the page as served. That can only be
 * measured where it happens.
 *
 * This collects the handful of facts that separate the remaining explanations:
 *
 *  - `doc` — the scroll-lock candidates (`overflow`, `position`, `touch-action`)
 *    on html/body, plus which element is actually `document.scrollingElement`.
 *  - `size` — content height vs viewport height. If content fits, "won't scroll"
 *    is not a bug at all; if it does not, the page is scrollable in principle.
 *  - `overlays` — fixed/sticky nodes covering most of the viewport, with their
 *    `pointer-events`, because an overlay that swallows touches produces exactly
 *    the "no movement whatsoever" symptom.
 *  - `move` — the decisive test: script sets scrollTop and reads it back. If the
 *    document moves programmatically but not by finger, the document is fine and
 *    the touch pipeline is the problem (WebView/Compose), which is a completely
 *    different fix from any page-level workaround.
 *  - `listeners` — whether the page registered non-passive touchmove/wheel
 *    handlers, the standard way a page cancels scrolling.
 *
 * A second symptom (2026-07-26) forced the instrument wider: content fades out
 * mid-page and the rest of the screen is empty, and a visible "댓글 더 보기"
 * button will not respond. Neither is a fixed overlay, so `overlays` reported
 * nothing both times. Content gating is done with a *clipped* container — a
 * `mask-image` gradient, or `max-height` + `overflow:hidden` — which is invisible
 * to an overlay scan, and a button can be unpressable simply because something
 * else wins the hit test at its own centre. So we also collect:
 *
 *  - `clips` — wide containers that hide part of their own content, with the
 *    mask and the clipped-away height. This is what a fade-to-nothing looks like.
 *  - `column` — what is under a vertical line of probe points down the screen.
 *    Where page content stops and something else (or nothing) begins is exactly
 *    the boundary the screenshot shows.
 *  - `cta` — "more/log in"-style buttons, each hit-tested at its own centre. If
 *    `hit` is false the tap is landing on `hitEl`, not the button, which names
 *    the culprit outright.
 *  - `atEnd` — whether the blank region below the fade is simply the end of the
 *    document (nothing left to show) or unpainted content that should be there.
 *
 * The remote harness cannot substitute for any of this: probing the same URL in
 * mobile Chromium (2026-07-26, logged out) returned `body{overflow-y:scroll}`
 * and no gate at all, so the gated state only exists on the device.
 */
internal const val BROWSER_SCROLL_DIAGNOSTIC_JS: String = """
(function () {
  try {
    var de = document.documentElement, b = document.body, cs = function (e) { return getComputedStyle(e); };
    var out = { url: location.href, ua: navigator.userAgent };
    function box(e, name) {
      if (!e) return null;
      var s = cs(e);
      return { el: name, overflow: s.overflow, overflowY: s.overflowY, position: s.position,
               height: s.height, touchAction: s.touchAction, overscroll: s.overscrollBehaviorY };
    }
    out.doc = [box(de, 'html'), box(b, 'body')];
    var se = document.scrollingElement;
    out.scrollingElement = se ? (se === de ? 'html' : (se === b ? 'body' : se.tagName)) : null;
    out.size = { contentH: de.scrollHeight, viewportH: de.clientHeight, innerH: window.innerHeight,
                 bodyH: b ? b.scrollHeight : 0, scrollable: de.scrollHeight > de.clientHeight + 4 };

    var vw = innerWidth, vh = innerHeight, ov = [];
    var all = document.querySelectorAll('*');
    for (var i = 0; i < all.length && ov.length < 8; i++) {
      var e = all[i], s = cs(e);
      if (s.position !== 'fixed' && s.position !== 'sticky') continue;
      var r = e.getBoundingClientRect();
      var w = Math.min(r.right, vw) - Math.max(r.left, 0);
      var h = Math.min(r.bottom, vh) - Math.max(r.top, 0);
      if (w <= 0 || h <= 0) continue;
      var cover = (w * h) / (vw * vh);
      if (cover < 0.4) continue;
      ov.push({ tag: e.tagName.toLowerCase(), cls: String(e.className || '').slice(0, 40),
                pos: s.position, z: s.zIndex, pe: s.pointerEvents, cover: Math.round(cover * 100) / 100 });
    }
    out.overlays = ov;

    // What is under the finger in the middle of the screen? If this is not page
    // content, something invisible is on top.
    function name(e) { return e ? (e.tagName.toLowerCase() + '.' + String(e.className || '').slice(0, 40)) : null; }
    var mid = document.elementFromPoint(vw / 2, vh / 2);
    out.centerEl = name(mid);

    // Content gating hides content *inside* a normal container, so it never
    // shows up as an overlay: a gradient mask, or max-height + overflow hidden.
    var clips = [], scanned = 0, CAP = 6000;
    for (var j = 0; j < all.length && clips.length < 6; j++) {
      if (++scanned > CAP) break;
      var ce = all[j], ms = cs(ce);
      var mask = ms.maskImage && ms.maskImage !== 'none' ? ms.maskImage
               : (ms.webkitMaskImage && ms.webkitMaskImage !== 'none' ? ms.webkitMaskImage : '');
      var cut = (ms.overflowY === 'hidden' || ms.overflowY === 'clip') && ce.scrollHeight > ce.clientHeight + 8;
      if (!mask && !cut) continue;
      var cr = ce.getBoundingClientRect();
      // A gate spans the column and swallows a lot; a cookie badge clipping its
      // own 22px is noise that would bury the finding.
      if (cr.width < vw * 0.6) continue;
      if (!mask && ce.scrollHeight - ce.clientHeight < 32) continue;
      clips.push({ tag: ce.tagName.toLowerCase(), cls: String(ce.className || '').slice(0, 50),
                   maxH: ms.maxHeight, mask: String(mask).slice(0, 60),
                   shown: ce.clientHeight, hidden: ce.scrollHeight - ce.clientHeight });
    }
    out.clips = clips;
    out.clipScanCapped = scanned > CAP;

    // Where does content stop? A vertical line of probes says what replaced it.
    var col = [];
    var marks = [0.05, 0.2, 0.35, 0.5, 0.65, 0.8, 0.95];
    for (var m = 0; m < marks.length; m++) {
      var y = Math.round(vh * marks[m]);
      var pe2 = document.elementFromPoint(vw / 2, y);
      col.push({ y: y, el: name(pe2), text: pe2 ? (pe2.textContent || '').trim().slice(0, 20) : '' });
    }
    out.column = col;

    // A button that "does not respond" is usually not the button's fault: hit
    // test its own centre and report whatever actually wins.
    // Two buckets, because the button the operator is actually tapping is the
    // on-screen one: filling a single cap in document order would push it out
    // behind a dozen collapsed "답글 N개 더 보기" further up the thread.
    var onCta = [], offCta = [];
    var clickable = document.querySelectorAll('button, a, [role=button]');
    // Bounded: with no on-screen match the loop would otherwise read
    // textContent off every anchor on a huge thread.
    for (var k = 0; k < clickable.length && k < 3000 && onCta.length < 6; k++) {
      var ct = clickable[k], tx = (ct.textContent || '').trim();
      if (!tx || tx.length > 40) continue;
      if (!/더 ?보기|더보기|More|로그인|Log ?in|Continue|Expand/i.test(tx)) continue;
      var br = ct.getBoundingClientRect();
      // Zero-height copies are the shadow-DOM slot duplicates, not real buttons.
      if (br.height <= 0) continue;
      // Report off-screen buttons too, without a hit test: "the button exists
      // but sits below the blank region" is itself the answer to why a tap on
      // where it *looks* like it should be does nothing.
      var onScreen = br.bottom > 0 && br.top < vh;
      var win = onScreen ? document.elementFromPoint(Math.round(br.left + br.width / 2),
                                                     Math.round(br.top + br.height / 2)) : null;
      var bs = cs(ct);
      var row = { t: tx.slice(0, 24), top: Math.round(br.top), h: Math.round(br.height),
                  pe: bs.pointerEvents, vis: bs.visibility, onScreen: onScreen,
                  hit: onScreen ? !!(win && (win === ct || ct.contains(win))) : null,
                  hitEl: name(win) };
      if (onScreen) onCta.push(row);
      else if (offCta.length < 3) offCta.push(row);
    }
    out.cta = onCta.concat(offCta);

    // Blank space below the fade: end of the document, or content that is there
    // but not being shown?
    var st = (se || de).scrollTop;
    out.atEnd = st + de.clientHeight >= de.scrollHeight - 4;

    // Our own scroll-unlock quirk is a suspect like any other: it defeats
    // `overflow:hidden` unconditionally, so if a page legitimately locks the
    // body for a dialog we would let the reader scroll off into empty space.
    // Report whether it is in force and what the page itself asked for, so a
    // capture can convict it instead of leaving it assumed innocent.
    out.quirk = { applied: !!document.getElementById('__deneb-scroll-unlock'),
                  bodyInline: b ? (b.style.overflowY || b.style.overflow || '') : '',
                  htmlInline: de.style.overflowY || de.style.overflow || '' };

    // Decisive: can the document be moved by script?
    var target = se || de;
    var before = target.scrollTop;
    target.scrollTop = before + 200;
    var after = target.scrollTop;
    target.scrollTop = before;
    out.move = { before: before, after: after, moved: after !== before };

    out.listeners = { hasOnTouchMove: !!document.ontouchmove, hasOnWheel: !!document.onwheel };
    return JSON.stringify(out);
  } catch (e) {
    return JSON.stringify({ error: String(e) });
  }
})();
"""
