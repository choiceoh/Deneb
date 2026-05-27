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

import {
  getMessage,
  senderContext,
  type GmailMessageDetail,
  type GmailMessageRow,
  type SenderContext,
} from './gmail';

const rowSummaries = new Map<string, GmailMessageRow>();
const inFlightDetails = new Map<string, Promise<GmailMessageDetail>>();
// Sender context cache: keyed by the raw From header so the same
// person hits cache across detail visits within the session, even
// when the server-side cache TTL has lapsed. Different mails from
// the same sender share the entry. Values that have already
// resolved stay in the map so a re-visit gets the result
// synchronously; in-flight promises let the detail view await
// without firing a duplicate request.
const senderContextCache = new Map<string, Promise<SenderContext>>();

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
// Sender-context entries deliberately are NOT cleared here: archiving
// one mail doesn't invalidate everything we know about the sender,
// and the next mail from the same person will want the same context.
export function invalidate(id: string): void {
  rowSummaries.delete(id);
  inFlightDetails.delete(id);
}

// prefetchSenderContext kicks the miniapp.gmail.sender_context RPC for
// a From header as soon as we have an excuse to (typically pointerdown
// on a list row). Idempotent per From header; failures invalidate the
// entry so the detail view will retry.
//
// The cache keeps RESOLVED promises around too, not just in-flight
// ones — sender context is durable enough within a session that re-
// using a result from 30 seconds ago is fine, and lets a re-opened
// detail view paint the sender card synchronously.
export function prefetchSenderContext(initData: string, from: string): void {
  const key = normalizeSenderKey(from);
  if (!key) return;
  if (senderContextCache.has(key)) return;
  const p = senderContext(initData, from).catch((err) => {
    senderContextCache.delete(key);
    throw err;
  });
  senderContextCache.set(key, p);
}

export async function fetchSenderContext(
  initData: string,
  from: string,
): Promise<SenderContext> {
  const key = normalizeSenderKey(from);
  if (key) {
    const existing = senderContextCache.get(key);
    if (existing) return existing;
  }
  const fresh = senderContext(initData, from).catch((err) => {
    if (key) senderContextCache.delete(key);
    throw err;
  });
  if (key) senderContextCache.set(key, fresh);
  return fresh;
}

// normalizeSenderKey lower-cases the email portion of a From header so
// "Alice <alice@x.com>" and "ALICE <Alice@X.COM>" share the cache
// entry, matching the server-side cache's normalization. Falls back
// to the trimmed raw string when there's no angle-bracketed email.
function normalizeSenderKey(from: string): string {
  const trimmed = from.trim();
  if (!trimmed) return '';
  const lt = trimmed.indexOf('<');
  const gt = trimmed.indexOf('>', lt + 1);
  if (lt >= 0 && gt > lt) {
    return trimmed.slice(lt + 1, gt).trim().toLowerCase();
  }
  return trimmed.toLowerCase();
}
