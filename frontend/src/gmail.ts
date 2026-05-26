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

// --- Sender context (miniapp.gmail.sender_context) ---

export interface SenderWikiHit {
  path: string;
  title?: string;
  summary?: string;
  category?: string;
}

export interface SenderRecent {
  count: number;
  /** ISO 8601 string, or raw header on parse failure. */
  lastReceivedAt?: string;
  windowDays: number;
}

export interface SenderContext {
  sender: string;
  email?: string;
  displayName?: string;
  recent?: SenderRecent;
  wikiHits: SenderWikiHit[];
  notices?: string[];
}

export function senderContext(initData: string, sender: string): Promise<SenderContext> {
  return call<SenderContext>('miniapp.gmail.sender_context', { sender }, initData);
}

// --- Analyze (miniapp.gmail.analyze) ---

export interface AnalyzeResult {
  id: string;
  subject?: string;
  from?: string;
  date?: string;
  analysis: string;
  durationMs: number;
}

export function analyzeMessage(initData: string, id: string): Promise<AnalyzeResult> {
  return call<AnalyzeResult>('miniapp.gmail.analyze', { id }, initData);
}
