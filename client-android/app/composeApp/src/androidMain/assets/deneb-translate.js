/*
 * deneb-translate.js — in-place web-page translation for the Deneb in-app
 * browser (en/ru → ko). Injected into every loaded page; the native side wires
 * window.DenebTranslateBridge (Kotlin @JavascriptInterface) and calls back into
 * window.DenebTranslate.applyBatch(...) with the translations.
 *
 * Design / invariants:
 *  - Walk visible TEXT NODES only; skip script/style/code/editable and nodes
 *    that are already Korean (Hangul) — those need no translation and would
 *    waste gateway calls.
 *  - Each text node gets a stable id; the native↔gateway round-trip returns
 *    translations keyed by that id, so replacement is exact and order-free.
 *  - Cache by original text: identical strings (nav items, repeated labels) are
 *    translated once. Persist page, site, and reusable short-label caches in
 *    localStorage so reload/back/revisit and repeated site chrome can apply
 *    known translations before asking the gateway again.
 *  - Body-first + viewport-first: article/main/body candidates near the current
 *    viewport are shipped before menus, footers, and off-screen text. Known CMS
 *    CONTENT_SELECTORS still win; when none match, a Readability-style score
 *    (text/paragraph density, link density, class noise — keep in sync with
 *    BrowserContentScore.kt) promotes a primary root without a new site patch.
 *  - When a text node is part of a paragraph/list/table block, ship a small
 *    same-block context envelope. The gateway translates only the node text, but
 *    can use the context for better terminology and sentence flow.
 *  - Debounce + MutationObserver (incl. characterData) pick up dynamically
 *    loaded / infinite-scroll content and SPA text swaps. A scroll listener
 *    retries newly visible nodes first.
 *  - Coverage beyond the light DOM: open Shadow DOM (closed roots are
 *    inaccessible), same-origin iframes (max 8, nest depth 2; cross-origin
 *    skipped), and SPA soft navigation (history push/replace + popstate /
 *    hashchange + native onLocationChange hint).
 *  - Toggle: OFF by default — translation starts only when the native chrome
 *    calls setEnabled(true). Turning OFF restores originals; turning ON again
 *    re-applies cached translations + translates anything new (applyAll).
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
  var nextBlockId = 1;
  var nodes = {};            // tid -> { node, original }
  var cache = {};            // originalText -> translatedText
  var persistentStores = {}; // localStorageKey -> cache store
  var persistentDirtyStores = {};
  var inFlight = {};         // tid -> true while a native/gateway batch is pending
  var pending = {};          // requestId -> [{ tids, ... }]
  var nextRequestId = 1;
  var enabled = false; // OFF by default — the native chrome calls setEnabled(true) per the toggle
  var debounceTimer = null;
  var viewportTimer = null;
  var lastPageKey = '';
  var HANGUL = /[가-힣]/;
  var SKIP_TAGS = { SCRIPT: 1, STYLE: 1, NOSCRIPT: 1, CODE: 1, PRE: 1, TEXTAREA: 1, KBD: 1, SAMP: 1 };
  var MAX_SEGMENTS_PER_BATCH = 40;
  var MAX_PERSISTENT_CACHE_ENTRIES = 700;
  var MAX_SITE_CACHE_ENTRIES = 1600;
  var MAX_GLOBAL_CACHE_ENTRIES = 1000;
  var MAX_CONTEXT_CHARS = 420;
  var MAX_GROUP_PARTS = 8;
  var MAX_GROUP_CHARS = 800;
  var VIEWPORT_MARGIN = 900;
  // Max forced-layout (getBoundingClientRect) measurements per dispatch pass, so a
  // large still-untranslated page can't reflow-storm on every scroll tick. Covers
  // a few screens (incl. VIEWPORT_MARGIN prefetch); overflow ships without ranking.
  var REFLOW_BUDGET = 80;
  var MAX_IFRAMES = 8;
  var MAX_IFRAME_DEPTH = 2;
  var SEGMENT_PAYLOAD_PREFIX = '\uE000deneb_translate_segment:v1:';
  var PARTS_RESULT_PREFIX = '\uE000deneb_translate_parts:v1:';
  // Include DLE (.full-story-text) and Forumotion (.post-content) containers so
  // link-split sentence fragments group into one DeepL unit instead of 3 orphans.
  var BLOCK_SELECTOR = 'p,li,blockquote,figcaption,caption,td,th,dt,dd,h1,h2,h3,h4,h5,h6,article,section,div.full-story-text,div.post-content,div.postbody,div.quote,div.available-content,div.markup,div.body.markup,div.entry-content,div.td-post-content';
  var CONTENT_SELECTORS = [
    // DLE CMS article body (topwar.ru, topcor.ru) — prefer over wrapping <article>
    '.full-story-text',
    '#full-story',
    '.full-story-cont',
    // Forumotion posts (russiadefence.net)
    '.post-content',
    '.postbody',
    '#page-body',
    // Substack post body (*.substack.com)
    '.available-content',
    '.body.markup',
    '.markup',
    '.single-post',
    '.newsletter-post',
    '[data-testid="post-content"]',
    // tagDiv Newspaper (eurasiantimes.com) — <article> also wraps share/related chrome
    '.td-post-content',
    'article',
    'main',
    '[role="main"]',
    '[itemprop="articleBody"]',
    '.article-body',
    '.article-content',
    '.entry-content',
    '.main-content',
    '.story-body',
    '.content-body',
    '.markdown-body',
    '.article',
    '.post',
    '.story',
    '#article',
    '#content',
    '#main'
  ];
  // Leftover ad/CMP chrome after network blocking — never ship to DeepL.
  var SKIP_SELECTOR = '.banner-full-story,.banner-block,.banner-block *,[id^="yandex_rtb"],[id^="adfox"],[id*="yandex_rtb"],.dle_b_floor_ad,.adsbygoogle,.taboola,[id^="taboola"],.consentframework,[class*="consentframework"],.subscribe-widget,[class*="subscribe-widget"],.paywall,[class*="paywall"],.modal-container,[data-testid="subscribe-widget"],.wp_rp_content,.related_post,.cat-post-widget,.category-posts-internal,[class*="yandex-rtb"],.ya-site-form,.td-a-rec,.td-g-rec,amp-embed,.td-post-sharing,.td-post-sharing-top,.td-post-sharing-bottom,.td-post-next-prev,.td_block_related_posts_mob,.td-related-row,.wp-gdpr-cookie-notice-content-wrap';
  var OBSERVE_OPTS = { childList: true, subtree: true, characterData: true };

  function translatable(text) {
    var t = (text || '').trim();
    if (t.length < 2) return false;        // skip whitespace / single glyphs
    if (HANGUL.test(t)) return false;       // already Korean
    if (!/[A-Za-zЀ-ӿ]/.test(t)) return false; // no Latin/Cyrillic → nothing to do
    // Inline script leftovers / CMP stubs sometimes land as text nodes.
    if (/^(window\.|var\s|function\s|Ya\.|_taboola|FA_pbjs|yaContextCb)/.test(t)) return false;
    return true;
  }

  function pageCacheKey() {
    var href = '';
    try {
      href = String(window.location.href || '');
    } catch (e) {
      href = '';
    }
    href = href.split('#')[0];
    return 'denebTranslate:v1:' + href;
  }

  function siteCacheKey() {
    var origin = '';
    try {
      origin = String(window.location.origin || '');
    } catch (e) {
      origin = '';
    }
    if (!origin) origin = pageCacheKey();
    return 'denebTranslateSite:v1:' + origin;
  }

  function globalCacheKey() {
    return 'denebTranslateGlobal:v1';
  }

  function normalizedText(text) {
    return String(text || '').replace(/\s+/g, ' ').trim();
  }

  function restoreOriginalSpacing(original, translated) {
    var lead = (String(original || '').match(/^\s*/) || [''])[0];
    var tail = (String(original || '').match(/\s*$/) || [''])[0];
    return lead + String(translated || '').trim() + tail;
  }

  function reusableGlobalText(text) {
    var t = normalizedText(text);
    if (!t || t.length > 80) return false;
    var words = t.split(/\s+/).length;
    if (t.length <= 36 || words <= 6) return true;
    return false;
  }

  function textHash(text) {
    var h = 2166136261;
    for (var i = 0; i < text.length; i++) {
      h ^= text.charCodeAt(i);
      h = Math.imul(h, 16777619);
    }
    return (h >>> 0).toString(36);
  }

  function loadPersistentStore(storageKey) {
    if (persistentStores[storageKey]) return persistentStores[storageKey];
    var store = {};
    try {
      var raw = window.localStorage && window.localStorage.getItem(storageKey);
      var parsed = raw ? JSON.parse(raw) : null;
      if (parsed && typeof parsed === 'object') store = parsed;
    } catch (e) {
      store = {};
    }
    persistentStores[storageKey] = store;
    return store;
  }

  function cacheLimit(storageKey) {
    if (storageKey === globalCacheKey()) return MAX_GLOBAL_CACHE_ENTRIES;
    if (storageKey.indexOf('denebTranslateSite:v1:') === 0) return MAX_SITE_CACHE_ENTRIES;
    return MAX_PERSISTENT_CACHE_ENTRIES;
  }

  function trimPersistentCache(store, limit) {
    var keys = [];
    for (var k in store) if (store.hasOwnProperty(k)) keys.push(k);
    if (keys.length <= limit) return;
    keys.sort(function (a, b) {
      return ((store[a] && store[a].at) || 0) - ((store[b] && store[b].at) || 0);
    });
    for (var i = 0; i < keys.length - limit; i++) delete store[keys[i]];
  }

  function flushPersistentStoreSoon(storageKey) {
    if (persistentDirtyStores[storageKey]) return;
    persistentDirtyStores[storageKey] = true;
    window.setTimeout(function () {
      delete persistentDirtyStores[storageKey];
      var store = loadPersistentStore(storageKey);
      trimPersistentCache(store, cacheLimit(storageKey));
      try {
        if (window.localStorage) window.localStorage.setItem(storageKey, JSON.stringify(store));
      } catch (e) {}
    }, 300);
  }

  function cachedTranslation(original) {
    var inMemory = cache[original];
    if (inMemory != null) return inMemory;
    var now = Date.now();
    var pageKey = pageCacheKey();
    var entry = loadPersistentStore(pageKey)[textHash(original)];
    if (entry && entry.s === original && typeof entry.t === 'string') {
      cache[original] = entry.t;
      entry.at = now;
      flushPersistentStoreSoon(pageKey);
      return entry.t;
    }
    var normalized = normalizedText(original);
    if (!normalized) return null;
    var normalizedKey = textHash(normalized);
    var siteKey = siteCacheKey();
    var siteEntry = loadPersistentStore(siteKey)[normalizedKey];
    if (siteEntry && siteEntry.n === normalized && typeof siteEntry.t === 'string') {
      var siteTranslation = restoreOriginalSpacing(original, siteEntry.t);
      cache[original] = siteTranslation;
      siteEntry.at = now;
      flushPersistentStoreSoon(siteKey);
      return siteTranslation;
    }
    if (reusableGlobalText(original)) {
      var globalKey = globalCacheKey();
      var globalEntry = loadPersistentStore(globalKey)[normalizedKey];
      if (globalEntry && globalEntry.n === normalized && typeof globalEntry.t === 'string') {
        var globalTranslation = restoreOriginalSpacing(original, globalEntry.t);
        cache[original] = globalTranslation;
        globalEntry.at = now;
        flushPersistentStoreSoon(globalKey);
        return globalTranslation;
      }
    }
    return null;
  }

  function rememberTranslation(original, translated) {
    cache[original] = translated;
    var now = Date.now();
    var pageKey = pageCacheKey();
    var pageStore = loadPersistentStore(pageKey);
    pageStore[textHash(original)] = { s: original, t: translated, at: now };
    flushPersistentStoreSoon(pageKey);
    var normalized = normalizedText(original);
    if (!normalized) return;
    var shared = String(translated || '').trim();
    var normalizedKey = textHash(normalized);
    var siteKey = siteCacheKey();
    var siteStore = loadPersistentStore(siteKey);
    siteStore[normalizedKey] = { n: normalized, t: shared, at: now };
    flushPersistentStoreSoon(siteKey);
    if (reusableGlobalText(original)) {
      var globalKey = globalCacheKey();
      var globalStore = loadPersistentStore(globalKey);
      globalStore[normalizedKey] = { n: normalized, t: shared, at: now };
      flushPersistentStoreSoon(globalKey);
    }
  }

  function nodeView(node) {
    var doc = node && (node.nodeType === 9 ? node : node.ownerDocument);
    return (doc && doc.defaultView) || window;
  }

  function ownerBody(node) {
    var doc = node && (node.nodeType === 9 ? node : node.ownerDocument);
    return (doc && doc.body) || null;
  }

  function rootDocument(root) {
    if (!root) return document;
    if (root.nodeType === 9) return root;
    return root.ownerDocument || document;
  }

  function skipParent(node) {
    var p = node.parentNode;
    while (p && p.nodeType === 1) {
      if (SKIP_TAGS[p.tagName]) return true;
      if (p.isContentEditable) return true;
      try {
        if (p.matches && p.matches(SKIP_SELECTOR)) return true;
      } catch (e) {}
      p = p.parentNode;
    }
    return false;
  }

  function hiddenParent(node) {
    var p = node.parentNode;
    var view = nodeView(node);
    while (p && p.nodeType === 1) {
      if (p.getAttribute && p.getAttribute('aria-hidden') === 'true') return true;
      var style = view.getComputedStyle ? view.getComputedStyle(p) : null;
      if (style && (style.display === 'none' || style.visibility === 'hidden')) return true;
      p = p.parentNode;
    }
    return false;
  }

  function nearestTextBlock(node) {
    var el = node && node.parentElement;
    var body = ownerBody(node);
    while (el && el !== body && el.nodeType === 1) {
      try {
        if (el.matches && el.matches(BLOCK_SELECTOR)) return el;
      } catch (e) {}
      el = el.parentElement;
    }
    return node && node.parentElement;
  }

  function groupTextBlock(node) {
    var el = node && node.parentElement;
    var body = ownerBody(node);
    while (el && el !== body && el.nodeType === 1) {
      try {
        if (el.matches && el.matches(BLOCK_SELECTOR)) return el;
      } catch (e) {}
      el = el.parentElement;
    }
    return null;
  }

  function blockGroupKey(rec) {
    var block = groupTextBlock(rec && rec.node);
    if (!block) return '';
    if (!block.__denebTranslateBlockId) block.__denebTranslateBlockId = String(nextBlockId++);
    return block.__denebTranslateBlockId;
  }

  function clippedContext(text) {
    var t = normalizedText(text);
    if (t.length <= MAX_CONTEXT_CHARS) return t;
    var half = Math.floor((MAX_CONTEXT_CHARS - 5) / 2);
    return t.slice(0, half) + ' ... ' + t.slice(t.length - half);
  }

  function blockContext(rec) {
    var block = nearestTextBlock(rec && rec.node);
    if (!block) return '';
    var text = '';
    try {
      text = block.innerText || block.textContent || '';
    } catch (e) {
      text = '';
    }
    var context = clippedContext(text);
    var original = normalizedText(rec.original);
    if (!context || context === original) return '';
    if (context.length < Math.max(20, original.length + 8)) return '';
    return context;
  }

  function segmentPayload(rec) {
    if (!rec) return '';
    if (!rec.context) rec.context = blockContext(rec);
    if (!rec.context) return rec.original;
    try {
      return SEGMENT_PAYLOAD_PREFIX + JSON.stringify({
        text: rec.original,
        context: rec.context,
        role: rec.primary ? 'body' : 'chrome'
      });
    } catch (e) {
      return rec.original;
    }
  }

  function buildShipUnits(tids) {
    var units = [];
    for (var i = 0; i < tids.length; i++) {
      var tid = tids[i];
      var rec = nodes[tid];
      if (!rec) continue;
      var key = blockGroupKey(rec);
      var chars = String(rec.original || '').length;
      var last = units.length ? units[units.length - 1] : null;
      if (key && last && last.key === key && last.tids.length < MAX_GROUP_PARTS && last.chars + chars <= MAX_GROUP_CHARS) {
        last.tids.push(tid);
        last.chars += chars;
        last.primary = last.primary || !!rec.primary;
        continue;
      }
      units.push({ key: key, tids: [tid], chars: chars, primary: !!rec.primary });
    }
    return units;
  }

  function unitPayload(unit) {
    if (!unit || unit.tids.length === 0) return '';
    if (unit.tids.length === 1) return segmentPayload(nodes[unit.tids[0]]);
    var parts = [];
    var context = '';
    for (var i = 0; i < unit.tids.length; i++) {
      var rec = nodes[unit.tids[i]];
      if (!rec) continue;
      parts.push(rec.original);
      if (!context) {
        if (!rec.context) rec.context = blockContext(rec);
        context = rec.context || '';
      }
    }
    if (parts.length !== unit.tids.length) return segmentPayload(nodes[unit.tids[0]]);
    try {
      return SEGMENT_PAYLOAD_PREFIX + JSON.stringify({
        parts: parts,
        context: context,
        role: unit.primary ? 'body' : 'chrome'
      });
    } catch (e) {
      return segmentPayload(nodes[unit.tids[0]]);
    }
  }

  function unitTids(units) {
    var tids = [];
    for (var i = 0; i < units.length; i++) {
      for (var j = 0; j < units[i].tids.length; j++) tids.push(units[i].tids[j]);
    }
    return tids;
  }

  function translatedParts(value, want) {
    if (typeof value !== 'string' || value.indexOf(PARTS_RESULT_PREFIX) !== 0) return null;
    try {
      var parts = JSON.parse(value.slice(PARTS_RESULT_PREFIX.length));
      if (!Array.isArray(parts) || parts.length !== want) return null;
      for (var i = 0; i < parts.length; i++) {
        if (typeof parts[i] !== 'string') return null;
      }
      return parts;
    } catch (e) {
      return null;
    }
  }

  function applyTranslationToTid(tid, translated) {
    var rec = nodes[tid];
    if (!rec) return;
    if (typeof translated !== 'string' || translated === rec.original) return;
    rememberTranslation(rec.original, translated);
    replace(rec, translated);
  }

  function textLength(el) {
    return ((el && (el.innerText || el.textContent)) || '').trim().length;
  }

  // Keep numeric formula in sync with BrowserContentScore.kt (MIN_CONTENT_SCORE=20).
  var MIN_CONTENT_SCORE = 20;
  var POSITIVE_CLASS_RE = /article|content|story|post|entry|body|markup|full[-_]?story|td[-_]?post|available[-_]?content|postbody|post[-_]?content/gi;
  var NEGATIVE_CLASS_RE = /nav|menu|sidebar|aside|footer|header|comment|related|share|social|promo|banner|ads?|subscribe|widget|cookie|consent|newsletter|recommend|popular|trending/gi;

  function countReMatches(re, text) {
    if (!text) return 0;
    var n = 0;
    re.lastIndex = 0;
    while (re.exec(text)) {
      n++;
      if (n > 8) break;
    }
    return n;
  }

  /** Metrics for one candidate element (mirrors ContentScoreMetrics). */
  function contentMetrics(el) {
    var textLen = textLength(el);
    var linkTextLength = 0;
    try {
      var links = el.querySelectorAll('a');
      for (var i = 0; i < links.length; i++) {
        linkTextLength += textLength(links[i]);
      }
    } catch (e) {}
    var paragraphCount = 0;
    try {
      paragraphCount = el.querySelectorAll('p').length;
    } catch (e2) {}
    var className = '';
    if (el.className) {
      className = typeof el.className === 'string' ? el.className : (el.className.baseVal || String(el.className));
    }
    return {
      tag: (el.tagName || '').toUpperCase(),
      className: className,
      id: el.id || '',
      textLength: textLen,
      linkTextLength: linkTextLength,
      paragraphCount: paragraphCount
    };
  }

  /** Readability-inspired score — keep in sync with scoreContentCandidate (Kotlin). */
  function scoreContentCandidate(m) {
    if (!m || m.textLength < 80) return -1e9;
    var score = 0;
    var tag = m.tag;
    if (tag === 'ARTICLE' || tag === 'MAIN') score += 30;
    else if (tag === 'SECTION') score += 15;
    else if (tag === 'DIV') score += 5;

    var classId = (m.className || '') + ' ' + (m.id || '');
    var positiveHits = Math.min(countReMatches(POSITIVE_CLASS_RE, classId), 3);
    score += positiveHits * 25;
    var negativeHits = Math.min(countReMatches(NEGATIVE_CLASS_RE, classId), 4);
    score -= negativeHits * 40;

    score += Math.min(m.paragraphCount * 3, 50);
    score += Math.min(Math.floor(m.textLength / 100), 40);

    var denom = Math.max(m.textLength, 1);
    var linkDensity = m.linkTextLength / denom;
    if (linkDensity > 0.35) score -= 50;
    else if (linkDensity > 0.2) score -= 20;

    return score;
  }

  function contentRank(el) {
    if (!el || !el.matches) return CONTENT_SELECTORS.length;
    for (var i = 0; i < CONTENT_SELECTORS.length; i++) {
      try {
        if (el.matches(CONTENT_SELECTORS[i])) return i;
      } catch (e) {}
    }
    return CONTENT_SELECTORS.length;
  }

  function pushRootIfFree(roots, el) {
    if (!el || textLength(el) < 80) return false;
    for (var j = 0; j < roots.length; j++) {
      if (roots[j] === el || roots[j].contains(el) || el.contains(roots[j])) return false;
    }
    roots.push(el);
    return true;
  }

  function isSkippableContentCandidate(el) {
    if (!el || !el.matches) return true;
    try {
      if (el.matches(SKIP_SELECTOR)) return true;
    } catch (e) {}
    return skipParent(el);
  }

  /**
   * Score article/main/section/div candidates when CONTENT_SELECTORS miss.
   * Returns elements sorted by score descending (already filtered by MIN_CONTENT_SCORE).
   */
  function scoredContentElements(doc) {
    var nodes;
    try {
      nodes = doc.querySelectorAll('article, main, section, div');
    } catch (e) {
      return [];
    }
    var scored = [];
    for (var i = 0; i < nodes.length; i++) {
      var el = nodes[i];
      if (isSkippableContentCandidate(el)) continue;
      var m = contentMetrics(el);
      var score = scoreContentCandidate(m);
      if (score < MIN_CONTENT_SCORE) continue;
      scored.push({ el: el, score: score, textLength: m.textLength });
    }
    scored.sort(function (a, b) {
      if (b.score !== a.score) return b.score - a.score;
      return b.textLength - a.textLength;
    });
    return scored;
  }

  // contentRoots is recomputed on every mutation-triggered scan; its candidate sort and
  // Readability fallback call innerText/getBoundingClientRect (forced layout). Cache the
  // main document's roots per page and reuse while they stay connected, so a mutating
  // page (Reddit) does not re-score its body on every scan. Invalidated by URL change
  // (onLocationChange) or when a cached root detaches.
  var cachedRoots = null;
  function contentRoots(doc) {
    doc = doc || document;
    var mainDoc = doc === document;
    if (mainDoc && cachedRoots && cachedRoots.key === pageCacheKey() && cachedRoots.roots.length) {
      var stillOk = true;
      for (var v = 0; v < cachedRoots.roots.length; v++) {
        if (!cachedRoots.roots[v].isConnected) { stillOk = false; break; }
      }
      if (stillOk) return cachedRoots.roots;
    }
    var roots = [];
    var candidates = [];
    try {
      candidates = Array.prototype.slice.call(doc.querySelectorAll(CONTENT_SELECTORS.join(',')));
    } catch (e) {
      candidates = [];
    }
    candidates.sort(function (a, b) {
      var ar = contentRank(a);
      var br = contentRank(b);
      if (ar !== br) return ar - br;
      return textLength(b) - textLength(a);
    });
    for (var i = 0; i < candidates.length && roots.length < 12; i++) {
      pushRootIfFree(roots, candidates[i]);
    }
    // Selector miss (or only thin chrome matched): promote Readability-scored bodies.
    if (roots.length === 0) {
      var scored = scoredContentElements(doc);
      for (var s = 0; s < scored.length && roots.length < 12; s++) {
        pushRootIfFree(roots, scored[s].el);
      }
    }
    if (mainDoc && roots.length) cachedRoots = { key: pageCacheKey(), roots: roots };
    return roots;
  }

  function isInViewport(rec) {
    var el = rec && rec.node && rec.node.parentElement;
    if (!el || !el.getBoundingClientRect) return false;
    var rect = el.getBoundingClientRect();
    var view = nodeView(el);
    var h = view.innerHeight || (view.document && view.document.documentElement && view.document.documentElement.clientHeight) || 0;
    var w = view.innerWidth || (view.document && view.document.documentElement && view.document.documentElement.clientWidth) || 0;
    if ((rect.width <= 0 && rect.height <= 0) || rect.bottom < -VIEWPORT_MARGIN || rect.top > h + VIEWPORT_MARGIN) return false;
    return rect.right >= -80 && rect.left <= w + 80;
  }

  function registerTextNode(n, primary, fresh) {
    n.__denebSeen = true;
    var tid = String(nextId++);
    n.__denebTid = tid;
    var original = n.nodeValue;
    nodes[tid] = { node: n, original: original, primary: !!primary };
    if (n.parentElement) {
      try { n.parentElement.setAttribute(ATTR, tid); } catch (e) {}
    }
    fresh.push(tid);
  }

  // SPA soft-nav / characterData: a seen node whose text was swapped to new
  // Latin/Cyrillic content must re-enter the translate queue.
  function maybeReadmit(n, primary, fresh) {
    var tid = n.__denebTid;
    var rec = tid && nodes[tid];
    if (!rec) {
      if (translatable(n.nodeValue) && !skipParent(n) && !hiddenParent(n)) {
        registerTextNode(n, primary, fresh);
      }
      return;
    }
    if (primary) rec.primary = true;
    var cur = n.nodeValue;
    if (cur === rec.original) return;
    var applied = cache[rec.original];
    if (applied != null && cur === applied) return;
    if (!translatable(cur) || skipParent(n) || hiddenParent(n)) return;
    delete inFlight[tid];
    rec.original = cur;
    rec.context = null;
    fresh.push(tid);
  }

  function shadowHostsUnder(root) {
    var hosts = [];
    try {
      if (root.nodeType === 1 && root.shadowRoot) hosts.push(root);
      if (root.querySelectorAll) {
        var els = root.querySelectorAll('*');
        for (var i = 0; i < els.length; i++) {
          if (els[i].shadowRoot) hosts.push(els[i]);
        }
      }
    } catch (e) {}
    return hosts;
  }

  // Collect untranslated text nodes under root (light DOM), then recurse into
  // open shadow roots. Closed shadow roots are inaccessible and skipped.
  function collect(root, primary) {
    var fresh = [];
    if (!root) return fresh;
    var doc = rootDocument(root);
    var walker;
    try {
      walker = doc.createTreeWalker(root, NodeFilter.SHOW_TEXT, null, false);
    } catch (e) {
      return fresh;
    }
    var n;
    while ((n = walker.nextNode())) {
      if (n.__denebSeen) {
        maybeReadmit(n, primary, fresh);
        continue;
      }
      if (!translatable(n.nodeValue)) { n.__denebSeen = true; continue; }
      if (skipParent(n)) { n.__denebSeen = true; continue; }
      if (hiddenParent(n)) continue;
      registerTextNode(n, primary, fresh);
    }
    var hosts = shadowHostsUnder(root);
    for (var h = 0; h < hosts.length; h++) {
      var sr = hosts[h].shadowRoot;
      if (!sr) continue;
      observeRoot(sr);
      var nested = collect(sr, primary);
      for (var k = 0; k < nested.length; k++) fresh.push(nested[k]);
    }
    return fresh;
  }

  function unique(tids) {
    var seen = {};
    var out = [];
    for (var i = 0; i < tids.length; i++) {
      var tid = tids[i];
      if (!tid || seen[tid]) continue;
      seen[tid] = true;
      out.push(tid);
    }
    return out;
  }

  function knownTids() {
    var out = [];
    for (var tid in nodes) if (nodes.hasOwnProperty(tid)) out.push(tid);
    return out;
  }

  function tryContentDocument(frame) {
    try {
      return frame.contentDocument || (frame.contentWindow && frame.contentWindow.document) || null;
    } catch (e) {
      return null;
    }
  }

  // Main document + same-origin iframes (cross-origin throws → skipped).
  function accessibleDocuments() {
    var out = [];
    function addDoc(doc, depth) {
      if (!doc) return;
      for (var i = 0; i < out.length; i++) if (out[i] === doc) return;
      out.push(doc);
      if (depth >= MAX_IFRAME_DEPTH) return;
      if (out.length >= 1 + MAX_IFRAMES) return;
      var frames;
      try {
        frames = doc.querySelectorAll('iframe, frame');
      } catch (e) {
        return;
      }
      for (var j = 0; j < frames.length; j++) {
        if (out.length >= 1 + MAX_IFRAMES) break;
        var child = tryContentDocument(frames[j]);
        if (child) addDoc(child, depth + 1);
      }
    }
    addDoc(document, 0);
    return out;
  }

  function bindFrameLoads(doc) {
    if (!doc || !doc.querySelectorAll) return;
    var frames;
    try {
      frames = doc.querySelectorAll('iframe, frame');
    } catch (e) {
      return;
    }
    for (var i = 0; i < frames.length; i++) {
      var frame = frames[i];
      if (frame.__denebLoadBound) continue;
      frame.__denebLoadBound = true;
      try {
        frame.addEventListener('load', function () {
          var child = tryContentDocument(this);
          if (!child) return;
          observeRoot(child.documentElement || child.body);
          bindFrameLoads(child);
          if (enabled) scheduleScan();
        });
      } catch (e) {}
      var ready = tryContentDocument(frame);
      if (ready) {
        observeRoot(ready.documentElement || ready.body);
        bindFrameLoads(ready);
      }
    }
  }

  function pruneDetached() {
    for (var tid in nodes) {
      if (!nodes.hasOwnProperty(tid)) continue;
      var rec = nodes[tid];
      var n = rec && rec.node;
      if (!n || !n.isConnected) {
        delete nodes[tid];
        delete inFlight[tid];
      }
    }
  }

  function collectPage() {
    bindFrameLoads(document);
    var docs = accessibleDocuments();
    for (var d = 0; d < docs.length; d++) {
      var doc = docs[d];
      observeRoot(doc.documentElement || doc.body);
      var roots = contentRoots(doc);
      for (var i = 0; i < roots.length; i++) collect(roots[i], true);
      if (doc.body) collect(doc.body, false);
    }
  }

  // A node is "applied" when it already shows its own translation. Applied nodes need
  // no re-measure and no re-ship, so skipping them keeps steady-state scans and scroll
  // scans O(untranslated) instead of O(all-nodes). This is what stops a large reactive
  // page (Reddit) from freezing: without it, every scroll/mutation re-measured every
  // already-translated node via getBoundingClientRect, forcing thousands of reflows.
  function isApplied(rec) {
    if (!rec || !rec.node) return false;
    var tr = cache[rec.original];
    return tr != null && rec.node.nodeValue === tr;
  }

  function dispatch(tids) {
    if (!enabled || !tids.length) return;
    if (!window.DenebTranslateBridge) return;
    // Split into bounded batches; serve cache hits immediately, only ship misses.
    var batch = [];
    tids = unique(tids);
    for (var i = 0; i < tids.length; i++) {
      var rec = nodes[tids[i]];
      if (!rec) continue;
      if (inFlight[tids[i]] || isApplied(rec)) continue;
      var cached = cachedTranslation(rec.original);
      if (cached != null) { replace(rec, cached); continue; }
      inFlight[tids[i]] = true;
      batch.push(tids[i]);
      if (batch.length >= MAX_SEGMENTS_PER_BATCH) { ship(batch); batch = []; }
    }
    if (batch.length) ship(batch);
  }

  function clearInFlight(tids) {
    for (var i = 0; i < tids.length; i++) delete inFlight[tids[i]];
  }

  function dispatchPrioritized(tids) {
    var primaryVisible = [];
    var visible = [];
    var primaryRest = [];
    var rest = [];
    tids = unique(tids);
    // isInViewport forces layout (getBoundingClientRect). Skipping settled nodes
    // (above) makes the APPLIED page cheap, but while a large page is still being
    // translated — the first enable, or Reddit loading fresh comments on scroll —
    // there can be thousands of UNAPPLIED nodes, and measuring them all on every
    // 140ms scroll tick is an O(untranslated) reflow storm that freezes scrolling
    // (measured: ~thousands of forced reflows/scroll on a big thread). Cap the
    // forced-layout budget per pass: once spent, remaining untranslated nodes are
    // deprioritized to `rest` (shipped WITHOUT a reflow — dispatch() never measures)
    // so everything still translates, just without viewport ranking this pass.
    var reflows = 0;
    for (var i = 0; i < tids.length; i++) {
      var rec = nodes[tids[i]];
      if (!rec) continue;
      if (inFlight[tids[i]] || isApplied(rec)) continue;
      var near;
      if (reflows >= REFLOW_BUDGET) {
        near = false;
      } else {
        near = isInViewport(rec);
        reflows++;
      }
      if (rec.primary && near) primaryVisible.push(tids[i]);
      else if (near) visible.push(tids[i]);
      else if (rec.primary) primaryRest.push(tids[i]);
      else rest.push(tids[i]);
    }

    // Nothing left to translate (steady state): return before scheduling empty passes,
    // so a scroll over a fully translated page costs a cheap filter and no timers.
    if (!primaryVisible.length && !visible.length && !primaryRest.length && !rest.length) return;

    // Main readable text in/near the viewport gets the first translation calls. If no
    // readable-body node is visible yet, visible chrome still translates so the
    // current screen is not left blank while off-screen body text waits.
    dispatch(primaryVisible.length ? primaryVisible : visible);
    window.setTimeout(function () { if (enabled) dispatch(primaryRest); }, 120);
    if (primaryVisible.length) window.setTimeout(function () { if (enabled) dispatch(visible); }, 180);
    window.setTimeout(function () { if (enabled) dispatch(rest); }, 700);
  }

  function ship(tids) {
    var units = buildShipUnits(tids);
    if (!units.length) {
      clearInFlight(tids);
      return;
    }
    var rid = String(nextRequestId++);
    pending[rid] = units;
    var segments = [];
    for (var i = 0; i < units.length; i++) segments.push(unitPayload(units[i]));
    try {
      window.DenebTranslateBridge.translate(rid, JSON.stringify(segments));
    } catch (e) {
      delete pending[rid];
      clearInFlight(unitTids(units));
    }
    window.setTimeout(function () {
      if (!pending[rid]) return;
      delete pending[rid];
      clearInFlight(unitTids(units));
    }, 45000);
  }

  function replace(rec, translated) {
    if (!enabled || translated == null) return;
    if (rec.node && rec.node.nodeValue !== translated) rec.node.nodeValue = translated;
  }

  // A reactive framework (e.g. Reddit's Lit web components) re-renders from its own
  // data model and REVERTS our translated text back to the original. We already hold
  // that node's translation in cache, so rather than re-running the whole scan +
  // translate pipeline on every revert — a translator<->framework fight whose full
  // DOM walks and forced-layout viewport ranking pinned the main thread and froze
  // scrolling (measured: a runaway loop) — we re-apply the cached translation in
  // place. The re-apply is batched onto an animation frame, so even a framework that
  // reverts on every mutation is throttled to one cheap string write per frame (no
  // reflow, no gateway round-trip) instead of an unbounded loop — and the text stays
  // translated instead of being abandoned to the site's own language.
  var reapplyQueue = {};
  var reapplyScheduled = false;
  var requestFrame = (window.requestAnimationFrame && window.requestAnimationFrame.bind(window)) ||
    function (fn) { return window.setTimeout(fn, 16); };
  function flushReapply() {
    reapplyScheduled = false;
    var q = reapplyQueue;
    reapplyQueue = {};
    if (!enabled) return;
    for (var tid in q) {
      if (!q.hasOwnProperty(tid)) continue;
      var rec = nodes[tid];
      if (rec && rec.node && rec.node.isConnected && rec.node.nodeValue !== q[tid]) {
        rec.node.nodeValue = q[tid];
      }
    }
  }
  function queueReapply(tid, translated) {
    reapplyQueue[tid] = translated;
    if (reapplyScheduled) return;
    reapplyScheduled = true;
    requestFrame(flushReapply);
  }

  // Called by native after the gateway returns. translations is a JSON array the
  // SAME length/order as the shipped units. A unit can be one text node or a
  // grouped block-part payload; any count mismatch no-ops rather than risking
  // misaligned text.
  function applyBatch(requestId, translationsJson) {
    var units = pending[requestId];
    delete pending[requestId];
    if (!units) return;
    var tids = unitTids(units);
    var translations;
    try {
      translations = JSON.parse(translationsJson);
    } catch (e) {
      clearInFlight(tids);
      return;
    }
    if (!Array.isArray(translations) || translations.length !== units.length) {
      clearInFlight(tids);
      return;
    }
    clearInFlight(tids);
    for (var i = 0; i < units.length; i++) {
      var unit = units[i];
      var tr = translations[i];
      if (unit.tids.length === 1) {
        applyTranslationToTid(unit.tids[0], tr);
        continue;
      }
      var parts = translatedParts(tr, unit.tids.length);
      if (!parts) continue;
      for (var j = 0; j < unit.tids.length; j++) applyTranslationToTid(unit.tids[j], parts[j]);
    }
  }

  function scan(root) {
    dispatchPrioritized(collect(root || document.body, false));
  }

  var scanDeferredSince = 0;
  function scheduleScan() {
    var now = Date.now();
    if (!scanDeferredSince) scanDeferredSince = now;
    if (debounceTimer) clearTimeout(debounceTimer);
    // Debounce 400ms, but never defer past ~1.5s: a page that mutates faster than the
    // debounce (Reddit while scrolling) could otherwise reset the timer forever and
    // never translate freshly loaded content.
    var delay = (now - scanDeferredSince) >= 1500 ? 0 : 400;
    debounceTimer = setTimeout(function () {
      debounceTimer = null;
      scanDeferredSince = 0;
      pruneDetached();
      collectPage();
      dispatchPrioritized(knownTids());
    }, delay);
  }

  function scheduleViewportScan() {
    if (!enabled || viewportTimer) return;
    viewportTimer = setTimeout(function () {
      viewportTimer = null;
      dispatchPrioritized(knownTids());
    }, 140);
  }

  // Re-apply translation to the whole page (used when turning the toggle ON):
  // already-translated text returns instantly from cache, text reverted by a
  // prior OFF is re-shipped, and brand-new nodes are collected by scan(). This is
  // why re-enabling after a disable actually re-translates — collect() alone only
  // returns never-seen nodes, so the old scan()-only path left the page in originals.
  function applyAll() {
    pruneDetached();
    collectPage();
    dispatchPrioritized(knownTids());
  }

  function setEnabled(on) {
    if (!!on === enabled) return;
    enabled = !!on;
    if (enabled) {
      applyAll();
      // Diagnostic + UX: report how many translatable nodes were found this enable
      // (0 → "no text"; >0 → "translating N…"). Guarded so a missing bridge is a no-op.
      try {
        var n = 0;
        for (var k in nodes) if (nodes.hasOwnProperty(k)) n++;
        if (window.DenebTranslateBridge && window.DenebTranslateBridge.onEnable) {
          window.DenebTranslateBridge.onEnable(n);
        }
      } catch (e) {}
      return;
    }
    // Restore originals.
    for (var tid in nodes) {
      if (!nodes.hasOwnProperty(tid)) continue;
      var rec = nodes[tid];
      if (rec.node && rec.node.nodeValue !== rec.original) rec.node.nodeValue = rec.original;
    }
  }

  function onLocationChange() {
    pruneDetached();
    cachedRoots = null; // page changed → re-detect content roots for the new URL
    var key = pageCacheKey();
    if (key !== lastPageKey) lastPageKey = key;
    bindFrameLoads(document);
    if (enabled) scheduleScan();
  }

  function installHistoryHooks() {
    if (window.__denebHistoryHooked) return;
    window.__denebHistoryHooked = true;
    function wrap(fn) {
      return function () {
        var ret = fn.apply(this, arguments);
        try { onLocationChange(); } catch (e) {}
        return ret;
      };
    }
    try {
      if (history && history.pushState) history.pushState = wrap(history.pushState.bind(history));
      if (history && history.replaceState) history.replaceState = wrap(history.replaceState.bind(history));
    } catch (e) {}
    try {
      window.addEventListener('popstate', onLocationChange);
      window.addEventListener('hashchange', onLocationChange);
    } catch (e) {}
  }

  // Classify a batch of mutations. For each characterData change on a node we have
  // already translated there are three cases:
  //   - value === our translation → our own applied write; ignore (no rescan). We
  //     observe characterData, so without this every batch we apply would re-fire the
  //     observer → a self-feeding rescan loop that never settles on a live page.
  //   - value === the original    → a reactive framework (Reddit's Lit) reverted us.
  //     Re-apply the cached translation directly (queueReapply — cheap, frame-batched,
  //     no reflow, no gateway round-trip) so the text stays translated without dragging
  //     the scan pipeline into a fight. Does NOT trigger a scan.
  //   - value is something else    → genuinely new text; translate it (scan).
  // childList adds and never-seen nodes also need a scan. Returns whether to scan.
  function handleRecords(records) {
    var needScan = false;
    for (var i = 0; i < records.length; i++) {
      var r = records[i];
      if (r.type !== 'characterData') { needScan = true; continue; }
      var t = r.target;
      var tid = t && t.__denebTid;
      var rec = tid ? nodes[tid] : null;
      if (!rec) { needScan = true; continue; }
      var mine = cache[rec.original];
      if (mine != null) {
        if (t.nodeValue === mine) continue;          // our own applied translation
        if (t.nodeValue === rec.original) {           // framework reverted us → re-apply cached
          queueReapply(tid, mine);
          continue;
        }
      }
      needScan = true;                                // new / changed content → translate it
    }
    return needScan;
  }

  var observer = new MutationObserver(function (records) {
    if (enabled && handleRecords(records)) scheduleScan();
  });

  function observeRoot(root) {
    if (!root || root.__denebObserved) return;
    root.__denebObserved = true;
    try {
      observer.observe(root, OBSERVE_OPTS);
    } catch (e) {}
  }

  window.DenebTranslate = {
    __installed: true,
    applyBatch: applyBatch,
    setEnabled: setEnabled,
    onLocationChange: onLocationChange,
    start: function () {
      // Install observers/hooks only; translation stays OFF until the native chrome
      // calls setEnabled(true). So a page browsed with translation off never ships
      // a translate request on load.
      lastPageKey = pageCacheKey();
      observeRoot(document.documentElement || document.body);
      installHistoryHooks();
      bindFrameLoads(document);
      try {
        window.addEventListener('scroll', scheduleViewportScan, { passive: true });
      } catch (e) {}
    },
  };

  if (document.readyState === 'loading') {
    document.addEventListener('DOMContentLoaded', function () { window.DenebTranslate.start(); });
  } else {
    window.DenebTranslate.start();
  }
})();
