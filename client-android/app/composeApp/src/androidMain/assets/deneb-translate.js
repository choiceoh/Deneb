/*
 * deneb-translate.js — in-place web-page translation for the Deneb in-app
 * browser (en/ru → ko). Injected into every loaded page; the native side wires
 * window.DenebTranslateBridge (Kotlin @JavascriptInterface) and calls back into
 * window.DenebTranslate.applyBatch(...) with the translations.
 *
 * Design / invariants:
 *  - Walk visible TEXT NODES only; skip script/style/code/editable and nodes
 *    that are already Korean (Hangul) — those need no translation and would
 *    waste model calls.
 *  - Each text node gets a stable id; the native↔model round-trip returns
 *    translations keyed by that id, so replacement is exact and order-free.
 *  - Cache by original text: identical strings (nav items, repeated labels) are
 *    translated once.
 *  - Debounce + a MutationObserver pick up dynamically loaded / infinite-scroll
 *    content without re-walking the whole DOM each time.
 *  - Toggle: applyBatch only mutates when translation is ON; turning OFF
 *    restores originals from the per-node saved text.
 *
 * The native bridge contract:
 *   window.DenebTranslateBridge.translate(requestId, jsonSegments)
 *     → native calls miniapp.web.translate, then
 *   window.DenebTranslate.applyBatch(requestId, jsonTranslations)
 */
(function () {
  if (window.DenebTranslate && window.DenebTranslate.__installed) return;

  var ATTR = 'data-deneb-tid';
  var nextId = 1;
  var nodes = {};            // tid -> { node, original }
  var cache = {};            // originalText -> translatedText
  var pending = {};          // requestId -> [tids]
  var nextRequestId = 1;
  var enabled = true;
  var debounceTimer = null;
  var HANGUL = /[가-힣]/;
  var SKIP_TAGS = { SCRIPT: 1, STYLE: 1, NOSCRIPT: 1, CODE: 1, PRE: 1, TEXTAREA: 1, KBD: 1, SAMP: 1 };
  var MAX_SEGMENTS_PER_BATCH = 40;

  function translatable(text) {
    var t = (text || '').trim();
    if (t.length < 2) return false;        // skip whitespace / single glyphs
    if (HANGUL.test(t)) return false;       // already Korean
    if (!/[A-Za-zЀ-ӿ]/.test(t)) return false; // no Latin/Cyrillic → nothing to do
    return true;
  }

  function skipParent(node) {
    var p = node.parentNode;
    while (p && p.nodeType === 1) {
      if (SKIP_TAGS[p.tagName]) return true;
      if (p.isContentEditable) return true;
      p = p.parentNode;
    }
    return false;
  }

  // Collect untranslated text nodes under root, assigning each a stable tid.
  function collect(root) {
    var fresh = [];
    var walker = document.createTreeWalker(root, NodeFilter.SHOW_TEXT, null, false);
    var n;
    while ((n = walker.nextNode())) {
      if (n.__denebSeen) continue;
      if (!translatable(n.nodeValue)) { n.__denebSeen = true; continue; }
      if (skipParent(n)) { n.__denebSeen = true; continue; }
      n.__denebSeen = true;
      var tid = String(nextId++);
      var original = n.nodeValue;
      nodes[tid] = { node: n, original: original };
      if (n.parentElement) n.parentElement.setAttribute(ATTR, tid);
      fresh.push(tid);
    }
    return fresh;
  }

  function dispatch(tids) {
    if (!enabled || !tids.length) return;
    if (!window.DenebTranslateBridge) return;
    // Split into bounded batches; serve cache hits immediately, only ship misses.
    var batch = [];
    for (var i = 0; i < tids.length; i++) {
      var rec = nodes[tids[i]];
      if (!rec) continue;
      var cached = cache[rec.original];
      if (cached != null) { replace(rec, cached); continue; }
      batch.push(tids[i]);
      if (batch.length >= MAX_SEGMENTS_PER_BATCH) { ship(batch); batch = []; }
    }
    if (batch.length) ship(batch);
  }

  function ship(tids) {
    var rid = String(nextRequestId++);
    pending[rid] = tids.slice();
    var segments = [];
    for (var i = 0; i < tids.length; i++) segments.push(nodes[tids[i]].original);
    try {
      window.DenebTranslateBridge.translate(rid, JSON.stringify(segments));
    } catch (e) {
      delete pending[rid];
    }
  }

  function replace(rec, translated) {
    if (!enabled || translated == null) return;
    if (rec.node && rec.node.nodeValue !== translated) rec.node.nodeValue = translated;
  }

  // Called by native after the model returns. translations is a JSON array the
  // SAME length/order as the shipped segments; a count mismatch means the
  // gateway kept originals, so we no-op rather than risk misaligned text.
  function applyBatch(requestId, translationsJson) {
    var tids = pending[requestId];
    delete pending[requestId];
    if (!tids) return;
    var translations;
    try { translations = JSON.parse(translationsJson); } catch (e) { return; }
    if (!Array.isArray(translations) || translations.length !== tids.length) return;
    for (var i = 0; i < tids.length; i++) {
      var rec = nodes[tids[i]];
      if (!rec) continue;
      var tr = translations[i];
      if (typeof tr !== 'string' || tr === rec.original) continue;
      cache[rec.original] = tr;
      replace(rec, tr);
    }
  }

  function scan(root) {
    dispatch(collect(root || document.body));
  }

  function scheduleScan() {
    if (debounceTimer) clearTimeout(debounceTimer);
    debounceTimer = setTimeout(function () { scan(document.body); }, 400);
  }

  function setEnabled(on) {
    enabled = !!on;
    if (enabled) { scan(document.body); return; }
    // Restore originals.
    for (var tid in nodes) {
      if (!nodes.hasOwnProperty(tid)) continue;
      var rec = nodes[tid];
      if (rec.node && rec.node.nodeValue !== rec.original) rec.node.nodeValue = rec.original;
    }
  }

  var observer = new MutationObserver(function () { if (enabled) scheduleScan(); });

  window.DenebTranslate = {
    __installed: true,
    applyBatch: applyBatch,
    setEnabled: setEnabled,
    start: function () {
      enabled = true;
      scan(document.body);
      try {
        observer.observe(document.documentElement || document.body, { childList: true, subtree: true, characterData: false });
      } catch (e) {}
    },
  };

  if (document.readyState === 'loading') {
    document.addEventListener('DOMContentLoaded', function () { window.DenebTranslate.start(); });
  } else {
    window.DenebTranslate.start();
  }
})();
