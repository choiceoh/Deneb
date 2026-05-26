// format.ts — small UI formatters shared by views.

import { RpcError } from './rpc';

/**
 * formatRpcError stringifies an error caught from an RPC call for a
 * user-facing banner. RpcError → "<CODE> — <message>" so the operator
 * sees the gRPC-ish code (UNAVAILABLE, NOT_FOUND, …); generic Error →
 * its `.message`; everything else → the fallback. Use this for
 * full-view error states. For inline / row-level flash messages prefer
 * `errorMessage(err)` which omits the code prefix.
 */
export function formatRpcError(err: unknown, fallback = '알 수 없는 오류'): string {
  if (err instanceof RpcError) return `${err.code} — ${err.message}`;
  if (err instanceof Error) return err.message;
  return fallback;
}

/**
 * errorMessage returns the bare `.message` of an error — without the
 * RPC code prefix. Used by inline row-level flashes (보관 실패, 읽음
 * 처리 실패) where the surrounding label already gives context and the
 * extra code noise would compete with it visually.
 */
export function errorMessage(err: unknown, fallback = '알 수 없는 오류'): string {
  if (err instanceof RpcError) return err.message;
  if (err instanceof Error) return err.message;
  return fallback;
}

/**
 * relativeTime returns a Korean human-readable "N분 전" style string for
 * dates within ~30 days, falling back to YYYY-MM-DD HH:mm otherwise.
 * Accepts ISO 8601 or anything Date can parse; returns the raw input on
 * parse failure so we never render "Invalid Date" to users.
 */
export function relativeTime(raw: string): string {
  if (!raw) return '';
  const t = Date.parse(raw);
  if (Number.isNaN(t)) return raw;
  const diffMs = Date.now() - t;
  if (diffMs < 0) return new Date(t).toLocaleString('ko-KR');

  const sec = Math.floor(diffMs / 1000);
  if (sec < 60) return '방금';
  const min = Math.floor(sec / 60);
  if (min < 60) return `${min}분 전`;
  const hour = Math.floor(min / 60);
  if (hour < 24) return `${hour}시간 전`;
  const day = Math.floor(hour / 24);
  if (day < 30) return `${day}일 전`;
  return new Date(t).toLocaleString('ko-KR');
}

/** Human-readable byte size for attachment chips: 512 B, 12 KB, 3.4 MB. */
export function humanSize(bytes: number): string {
  if (!Number.isFinite(bytes) || bytes < 0) return '';
  if (bytes < 1024) return `${bytes} B`;
  if (bytes < 1024 * 1024) return `${Math.round(bytes / 1024)} KB`;
  return `${(bytes / 1024 / 1024).toFixed(1)} MB`;
}

/** Extracts the display name from "Display Name <email@host>" or returns input. */
export function shortFrom(raw: string): string {
  const m = raw.match(/^(.+?)\s*<[^>]+>$/);
  return m ? m[1].trim().replace(/^"|"$/g, '') : raw;
}
