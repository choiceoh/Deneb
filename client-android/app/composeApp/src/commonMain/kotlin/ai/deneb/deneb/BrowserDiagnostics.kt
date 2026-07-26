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
    var mid = document.elementFromPoint(vw / 2, vh / 2);
    out.centerEl = mid ? (mid.tagName.toLowerCase() + '.' + String(mid.className || '').slice(0, 40)) : null;

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
