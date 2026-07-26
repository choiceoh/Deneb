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
 *  - `overlays` — positioned nodes covering most of the viewport, with their
 *    `pointer-events`, because an overlay that swallows touches produces exactly
 *    the "no movement whatsoever" symptom. Any non-`static` position counts: the
 *    element that actually caused this bug was `absolute`, and an earlier
 *    fixed/sticky filter reported an empty list twice while it sat in plain sight.
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

    function name(e) { return e ? (e.tagName.toLowerCase() + '.' + String(e.className || '').slice(0, 40)) : null; }

    // One pass for both scans: each calls getComputedStyle on every node, and on
    // a 20,000px thread that is the expensive part of the whole probe.
    //
    // `position` is deliberately NOT restricted to fixed/sticky. The element that
    // actually caused this bug — Reddit's app-install sheet covering the entire
    // viewport — is absolutely positioned, so a fixed/sticky filter reported an
    // empty overlay list twice while the culprit sat in plain sight.
    var vw = innerWidth, vh = innerHeight, ov = [], clips = [];
    var all = document.querySelectorAll('*'), CAP = 25000;
    var i = 0;
    for (; i < all.length && i < CAP; i++) {
      var e = all[i], s = cs(e), r = null;
      if (ov.length < 8 && s.position !== 'static') {
        r = e.getBoundingClientRect();
        var w = Math.min(r.right, vw) - Math.max(r.left, 0);
        var h = Math.min(r.bottom, vh) - Math.max(r.top, 0);
        if (w > 0 && h > 0) {
          var cover = (w * h) / (vw * vh);
          if (cover >= 0.4) {
            ov.push({ tag: e.tagName.toLowerCase(), cls: String(e.className || '').slice(0, 40),
                      pos: s.position, z: s.zIndex, pe: s.pointerEvents,
                      cover: Math.round(cover * 100) / 100 });
          }
        }
      }
      if (clips.length >= 6) continue;
      var mask = s.maskImage && s.maskImage !== 'none' ? s.maskImage
               : (s.webkitMaskImage && s.webkitMaskImage !== 'none' ? s.webkitMaskImage : '');
      var cut = (s.overflowY === 'hidden' || s.overflowY === 'clip') && e.scrollHeight > e.clientHeight + 8;
      if (!mask && !cut) continue;
      if (!r) r = e.getBoundingClientRect();
      // A gate spans the column and swallows a lot; a cookie badge clipping its
      // own 22px is noise that would bury the finding.
      if (r.width < vw * 0.6) continue;
      if (!mask && e.scrollHeight - e.clientHeight < 32) continue;
      clips.push({ tag: e.tagName.toLowerCase(), cls: String(e.className || '').slice(0, 50),
                   maxH: s.maxHeight, mask: String(mask).slice(0, 60),
                   shown: e.clientHeight, hidden: e.scrollHeight - e.clientHeight });
    }
    out.overlays = ov;
    out.clips = clips;
    // A truncated scan that found nothing reads exactly like a clean page, so it
    // has to say when it gave up.
    out.scanCapped = i >= CAP;
    out.nodes = all.length;

    // What is under the finger in the middle of the screen? If this is not page
    // content, something invisible is on top.
    var mid = document.elementFromPoint(vw / 2, vh / 2);
    out.centerEl = name(mid);

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
