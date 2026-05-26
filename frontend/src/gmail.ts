// gmail.ts — typed client for the miniapp.gmail.* RPC surface.
//
// Backend contract lives in
// gateway-go/internal/runtime/rpc/handler/handlerminiapp/gmail.go.
// Keep these types in sync with the JSON shapes that file emits.

import { call } from './rpc';

export interface GmailMessageRow {
  id: string;
  threadId: string;
  from: string;
  subject: string;
  snippet: string;
  /** ISO 8601 string, or the raw Gmail Date header when parsing fails. */
  date: string;
  isUnread: boolean;
  labels: string[];
}

export interface GmailAttachment {
  id: string;
  filename: string;
  mimeType: string;
  size: number;
}

export interface GmailMessageDetail {
  id: string;
  threadId: string;
  from: string;
  to: string;
  cc?: string;
  subject: string;
  date: string;
  body: string;
  /** Original character count before any server-side truncation. */
  bodyTotal: number;
  labels: string[];
  attachments: GmailAttachment[];
}

interface ListResult {
  messages: GmailMessageRow[];
}

interface ActionResult {
  ok: boolean;
  labels: string[];
}

export function listRecent(
  initData: string,
  opts: { query?: string; limit?: number } = {},
): Promise<ListResult> {
  return call<ListResult>('miniapp.gmail.list_recent', opts, initData);
}

export function getMessage(initData: string, id: string): Promise<GmailMessageDetail> {
  return call<GmailMessageDetail>('miniapp.gmail.get', { id }, initData);
}

export function markRead(initData: string, id: string): Promise<ActionResult> {
  return call<ActionResult>('miniapp.gmail.mark_read', { id }, initData);
}

export function archive(initData: string, id: string): Promise<ActionResult> {
  return call<ActionResult>('miniapp.gmail.archive', { id }, initData);
}
