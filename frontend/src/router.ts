// router.ts — hash-based routing for the Mini App.
//
// We avoid pulling in a router library; for the 3-route surface a tiny
// hand-rolled parser is enough and keeps the bundle small. Adding routes
// later means extending the discriminated union below.

export type Route =
  | { name: 'home' }
  | { name: 'inbox' }
  | { name: 'detail'; messageId: string }
  | { name: 'memory' }
  | { name: 'sessions' }
  | { name: 'wikiPage'; path: string }
  | { name: 'sessionTranscript'; sessionKey: string };

export function parseRoute(hash: string): Route {
  if (hash === '' || hash === '#' || hash === '#/') return { name: 'home' };
  if (hash === '#/inbox') return { name: 'inbox' };
  if (hash === '#/memory') return { name: 'memory' };
  if (hash === '#/sessions') return { name: 'sessions' };
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
      return { name: 'memory' };
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
  else if (target.name === 'memory') hash = '#/memory';
  else if (target.name === 'sessions') hash = '#/sessions';
  else if (target.name === 'detail') hash = `#/m/${encodeURIComponent(target.messageId)}`;
  else if (target.name === 'wikiPage') hash = `#/wiki/${encodeURIComponent(target.path)}`;
  else if (target.name === 'sessionTranscript')
    hash = `#/session/${encodeURIComponent(target.sessionKey)}`;
  if (location.hash === hash) {
    // hashchange would not fire; force re-render by dispatching ourselves.
    window.dispatchEvent(new HashChangeEvent('hashchange'));
    return;
  }
  location.hash = hash;
}
