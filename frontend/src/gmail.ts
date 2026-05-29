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
  /** Opaque cursor for the next page; empty/missing when no more pages. */
  nextPageToken?: string;
}

interface ActionResult {
  ok: boolean;
  labels: string[];
}

export function listRecent(
  initData: string,
  opts: { query?: string; limit?: number; pageToken?: string } = {},
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

export function trash(initData: string, id: string): Promise<{ ok: boolean }> {
  return call<{ ok: boolean }>('miniapp.gmail.trash', { id }, initData);
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
  /** Free-form wiki-graph snapshot from graphify CLI. Omitted when empty. */
  wikiFacts?: string;
  notices?: string[];
}

export function senderContext(initData: string, sender: string): Promise<SenderContext> {
  return call<SenderContext>('miniapp.gmail.sender_context', { sender }, initData);
}

// --- Analyze (miniapp.gmail.analyze) ---

// ProjectRef is a related project wiki page cited by an analysis: the path
// plus enriched title/summary for chip rendering.
export interface ProjectRef {
  path: string;
  title?: string;
  summary?: string;
}

export interface AnalyzeResult {
  id: string;
  subject?: string;
  from?: string;
  date?: string;
  analysis: string;
  durationMs: number;
  // cached=true when the gateway returned a previously stored analysis
  // instead of running the LLM again. createdAt is the wall-clock time
  // the analysis was first produced (ISO 8601, UTC). Both fields are
  // always present in v2+ responses; older callers tolerate omission.
  cached?: boolean;
  createdAt?: string;
  // relatedProjects: project wiki pages the analyzer linked to this email.
  relatedProjects?: ProjectRef[];
}

// analyzeMessage runs (or fetches the cached) LLM analysis for an email.
// Pass force=true to bypass the cache and force a fresh LLM call — the
// "🔄 다시 분석" button uses this; the default "🔍 분석" tap lets the
// gateway serve from cache when available.
export function analyzeMessage(
  initData: string,
  id: string,
  force = false,
): Promise<AnalyzeResult> {
  return call<AnalyzeResult>('miniapp.gmail.analyze', { id, force }, initData);
}

// CachedAnalysis is the analysis_cached response: a stored analysis served
// without running the LLM. cached=false (with empty analysis) on a miss.
export interface CachedAnalysis {
  id: string;
  analysis: string;
  relatedProjects?: ProjectRef[];
  cached: boolean;
  createdAt?: string;
}

// analysisCached fetches a pre-computed analysis for an email (from the
// autonomous poller or a prior manual run) without ever triggering the LLM.
// Used on detail open to show the analysis + related projects instantly; a
// miss (cached=false) means the operator must tap analyze to generate one.
export function analysisCached(initData: string, id: string): Promise<CachedAnalysis> {
  return call<CachedAnalysis>('miniapp.gmail.analysis_cached', { id }, initData);
}
