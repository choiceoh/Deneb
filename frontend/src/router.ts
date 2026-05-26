// router.ts — hash-based routing for the Mini App.
//
// We avoid pulling in a router library; for the 3-route surface a tiny
// hand-rolled parser is enough and keeps the bundle small. Adding routes
// later means extending the discriminated union below.

export type Route =
  | { name: 'home' }
  | { name: 'inbox' }
  | { name: 'detail'; messageId: string };

export function parseRoute(hash: string): Route {
  if (hash === '' || hash === '#' || hash === '#/') return { name: 'home' };
  if (hash === '#/inbox') return { name: 'inbox' };
  const match = hash.match(/^#\/m\/(.+)$/);
  if (match) return { name: 'detail', messageId: decodeURIComponent(match[1]) };
  return { name: 'home' };
}

export function navigate(target: Route): void {
  let hash = '#/';
  if (target.name === 'inbox') hash = '#/inbox';
  if (target.name === 'detail') hash = `#/m/${encodeURIComponent(target.messageId)}`;
  if (location.hash === hash) {
    // hashchange would not fire; force re-render by dispatching ourselves.
    window.dispatchEvent(new HashChangeEvent('hashchange'));
    return;
  }
  location.hash = hash;
}
