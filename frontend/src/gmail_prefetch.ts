// gmail_prefetch.ts — in-memory caches for mail-row summaries and
// in-flight detail requests. The point is to let the detail view paint
// the subject/from/when fields the operator already saw on the list row
// the moment they tap into a message — no "메일 불러오는 중…" flash —
// and to give the detail RPC a head start whenever the list view can
// guess the user is about to drill in (e.g. pointerdown on a row).
//
// Both caches live for the lifetime of the JS context — Telegram tears
// the WebView down between sessions, so there's no need for explicit
// expiry. Destructive actions (trash, archive) explicitly invalidate
// the entries they no longer want anyone to see.

import { getMessage, type GmailMessageDetail, type GmailMessageRow } from './gmail';

const rowSummaries = new Map<string, GmailMessageRow>();
const inFlightDetails = new Map<string, Promise<GmailMessageDetail>>();

// cacheRowSummary stashes the list-row shape so the detail view can
// paint subject/from/when/snippet immediately when the operator drills
// in. Subsequent visits to the same list overwrite (e.g. the row's
// isUnread flag may have flipped because we marked it read elsewhere).
export function cacheRowSummary(row: GmailMessageRow): void {
  rowSummaries.set(row.id, row);
}

export function getRowSummary(id: string): GmailMessageRow | undefined {
  return rowSummaries.get(id);
}

// prefetchMessage fires the detail RPC if it isn't already in flight.
// Errors are swallowed here (and the entry removed) so a failed
// prefetch doesn't poison the cache; the detail view will retry when
// it calls fetchMessage() and surface any error there.
export function prefetchMessage(initData: string, id: string): void {
  if (inFlightDetails.has(id)) return;
  const p = getMessage(initData, id).catch((err) => {
    inFlightDetails.delete(id);
    throw err;
  });
  inFlightDetails.set(id, p);
}

// fetchMessage returns the in-flight prefetch promise if one exists,
// otherwise fires a fresh request and caches it. The result is NOT
// cached past resolution — once the promise settles the entry is
// dropped so a re-open of the same message hits the network again
// (mail content can change: labels move, body redactions, etc.).
export async function fetchMessage(
  initData: string,
  id: string,
): Promise<GmailMessageDetail> {
  const existing = inFlightDetails.get(id);
  if (existing) {
    try {
      return await existing;
    } finally {
      inFlightDetails.delete(id);
    }
  }
  const fresh = getMessage(initData, id);
  inFlightDetails.set(id, fresh);
  try {
    return await fresh;
  } finally {
    inFlightDetails.delete(id);
  }
}

// invalidate clears both caches for a message id. Call after archive,
// trash, or any other action that moves the message out of view —
// otherwise a re-render could paint the row's last-known state and
// confuse the operator into thinking the action didn't take effect.
export function invalidate(id: string): void {
  rowSummaries.delete(id);
  inFlightDetails.delete(id);
}
