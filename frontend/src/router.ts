// router.ts — hash-based routing for the Mini App.
//
// We avoid pulling in a router library; for the 3-route surface a tiny
// hand-rolled parser is enough and keeps the bundle small. Adding routes
// later means extending the discriminated union below.
//
// Note on chat: the Mini App used to have its own chat surface (#/chat,
// with context-attach variants like ?ctx=mail:<id>:reply). It was removed
// because Telegram itself is the chat surface for Deneb — duplicating it
// inside the Mini App added friction without adding capability. The
// content-detail views still expose the source on tap, and any follow-up
// conversation happens in the parent Telegram thread.

export type Route =
  | { name: 'home' }
  | { name: 'inbox' }
  | { name: 'detail'; messageId: string }
  | { name: 'search' }
  | { name: 'sessions' }
  | { name: 'topicNew' }
  | { name: 'wikiPage'; path: string }
  | { name: 'sessionTranscript'; sessionKey: string }
  | { name: 'calendar' }
  | { name: 'calendarEvent'; eventId: string }
  | { name: 'settings' }
  | { name: 'modelSelect'; role?: string }
  | { name: 'categories' }
  | { name: 'categoryPages'; category: string }
  | { name: 'crons' }
  | { name: 'cronDetail'; id: string }
  | { name: 'cronEdit'; id: string }
  | { name: 'personDetail'; email: string }
  | { name: 'wikiNew'; category?: string };

// Home is the index; every other route is a drill-down that gets the
// Telegram BackButton. The panorama tab strip is gone — home's
// type-menu lists every destination (including settings), so there's
// no top-of-screen chrome to switch between top-level surfaces.
export function isHomeRoute(route: Route): boolean {
  return route.name === 'home';
}

export function parseRoute(hash: string): Route {
  if (hash === '' || hash === '#' || hash === '#/') return { name: 'home' };
  if (hash === '#/inbox') return { name: 'inbox' };
  if (hash === '#/search') return { name: 'search' };
  if (hash === '#/sessions') return { name: 'sessions' };
  if (hash === '#/topic-new') return { name: 'topicNew' };
  // Legacy hashes map forward so stale BackButton stack entries and
  // deep-links resolve cleanly instead of falling through to home:
  //   #/more   — the more hub is gone (subsumed by home).
  //   #/memory — the dedicated memory-search view is gone; search
  //              now unifies wiki + diary + people.
  //   #/diary / #/people — listing views are gone; the same content
  //              is reachable by typing into the unified search box.
  if (hash === '#/more') return { name: 'home' };
  if (hash === '#/memory' || hash === '#/diary' || hash === '#/people') {
    return { name: 'search' };
  }
  if (hash === '#/settings') return { name: 'settings' };
  if (hash === '#/settings/model') return { name: 'modelSelect' };
  const roleSel = hash.match(/^#\/settings\/model\/(main|lightweight|fallback)$/);
  if (roleSel) return { name: 'modelSelect', role: roleSel[1] };
  if (hash === '#/categories') return { name: 'categories' };
  if (hash === '#/crons') return { name: 'crons' };
  // Edit must be matched before the detail catch-all below — the detail
  // regex's greedy (.+) would otherwise capture "<id>/edit" as the id.
  const cronEdit = hash.match(/^#\/crons\/(.+)\/edit$/);
  if (cronEdit) {
    try {
      return { name: 'cronEdit', id: decodeURIComponent(cronEdit[1]) };
    } catch {
      return { name: 'crons' };
    }
  }
  const cron = hash.match(/^#\/crons\/(.+)$/);
  if (cron) {
    try {
      return { name: 'cronDetail', id: decodeURIComponent(cron[1]) };
    } catch {
      return { name: 'crons' };
    }
  }
  if (hash === '#/wiki-new') return { name: 'wikiNew' };
  const newCat = hash.match(/^#\/wiki-new\?category=(.+)$/);
  if (newCat) {
    try {
      return { name: 'wikiNew', category: decodeURIComponent(newCat[1]) };
    } catch {
      return { name: 'wikiNew' };
    }
  }
  const person = hash.match(/^#\/person\/(.+)$/);
  if (person) {
    try {
      return { name: 'personDetail', email: decodeURIComponent(person[1]) };
    } catch {
      return { name: 'search' };
    }
  }
  const catPages = hash.match(/^#\/category\/(.+)$/);
  if (catPages) {
    try {
      return { name: 'categoryPages', category: decodeURIComponent(catPages[1]) };
  } catch {
      return { name: 'categories' };
    }
  }
  // Accept '#/calendar', '#/calendar/' (trailing slash), and
  // '#/calendar/<id>' — the trailing-slash variant falls back to the
  // list view instead of the catch-all home.
  if (hash === '#/calendar' || hash === '#/calendar/') return { name: 'calendar' };
  const cal = hash.match(/^#\/calendar\/(.+)$/);
  if (cal) {
    try {
      return { name: 'calendarEvent', eventId: decodeURIComponent(cal[1]) };
    } catch {
      return { name: 'calendar' };
    }
  }
  const match = hash.match(/^#\/m\/(.+)$/);
  if (match) {
    // decodeURIComponent throws URIError on malformed percent-encoding
    // (e.g. a pasted deep-link with truncated escapes). Catch it so the
    // Mini App falls back to home rather than crashing dispatch.
    try {
      return { name: 'detail', messageId: decodeURIComponent(match[1]) };
    } catch {
      return { name: 'home' };
    }
  }
  const wiki = hash.match(/^#\/wiki\/(.+)$/);
  if (wiki) {
    try {
      return { name: 'wikiPage', path: decodeURIComponent(wiki[1]) };
    } catch {
      return { name: 'search' };
    }
  }
  const sess = hash.match(/^#\/session\/(.+)$/);
  if (sess) {
    try {
      return { name: 'sessionTranscript', sessionKey: decodeURIComponent(sess[1]) };
    } catch {
      return { name: 'sessions' };
    }
  }
  return { name: 'home' };
}

/**
 * isCurrentHash returns true when the URL hash is still the value the
 * caller captured before kicking off an async fetch. Views use this as a
 * stale-data guard: after `await rpc()`, check `isCurrentHash(expected)`
 * before mutating the DOM. If the user has navigated to a different
 * route in the meantime, the result is no longer relevant and writing
 * it would render the previous view's data into the next view's DOM.
 *
 * Note: this does not cancel the in-flight network request. A future
 * pass can wire AbortController through the RPC layer for true
 * cancellation; for now we just suppress stale writes.
 */
export function isCurrentHash(expected: string): boolean {
  return location.hash === expected;
}

export function navigate(target: Route): void {
  let hash = '#/';
  if (target.name === 'inbox') hash = '#/inbox';
  else if (target.name === 'search') hash = '#/search';
  else if (target.name === 'sessions') hash = '#/sessions';
  else if (target.name === 'topicNew') hash = '#/topic-new';
  else if (target.name === 'settings') hash = '#/settings';
  else if (target.name === 'modelSelect')
    hash =
      target.role && target.role !== 'main'
        ? `#/settings/model/${target.role}`
        : '#/settings/model';
  else if (target.name === 'detail') hash = `#/m/${encodeURIComponent(target.messageId)}`;
  else if (target.name === 'wikiPage') hash = `#/wiki/${encodeURIComponent(target.path)}`;
  else if (target.name === 'sessionTranscript')
    hash = `#/session/${encodeURIComponent(target.sessionKey)}`;
  else if (target.name === 'calendar') hash = '#/calendar';
  else if (target.name === 'calendarEvent')
    hash = `#/calendar/${encodeURIComponent(target.eventId)}`;
  else if (target.name === 'categories') hash = '#/categories';
  else if (target.name === 'categoryPages')
    hash = `#/category/${encodeURIComponent(target.category)}`;
  else if (target.name === 'crons') hash = '#/crons';
  else if (target.name === 'cronDetail') hash = `#/crons/${encodeURIComponent(target.id)}`;
  else if (target.name === 'cronEdit') hash = `#/crons/${encodeURIComponent(target.id)}/edit`;
  else if (target.name === 'personDetail')
    hash = `#/person/${encodeURIComponent(target.email)}`;
  else if (target.name === 'wikiNew') {
    hash = target.category
      ? `#/wiki-new?category=${encodeURIComponent(target.category)}`
      : '#/wiki-new';
  }
  if (location.hash === hash) {
    // hashchange would not fire; force re-render by dispatching ourselves.
    window.dispatchEvent(new HashChangeEvent('hashchange'));
    return;
  }
  location.hash = hash;
}
