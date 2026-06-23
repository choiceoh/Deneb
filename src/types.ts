// Domain row shapes flowing from the Deneb gateway, plus the pane/view key union.
//
// These mirror the gateway's miniapp.* WIRE field names (verified against
// gateway-go + the native client's generated MiniappWireTypes). Every field
// beyond the id is optional — backend payloads omit empties — so display and AI
// serialization fall back gracefully (see format.ts). Keep aligned with DESIGN §5.

export interface Todo {
  id: string | number;
  title: string;
  note?: string;
  due?: string; // RFC3339, "" when unset
  dueAllDay?: boolean;
  done?: boolean;
  doneAt?: string;
}

export interface Mail {
  id: string | number;
  threadId?: string;
  from?: unknown; // "Name <addr@host>" string (tolerate legacy { name, email })
  to?: unknown;
  subject?: string;
  snippet?: string;
  body?: string;
  text?: string;
  plain?: string;
  plainText?: string;
  bodyText?: string;
  html?: string;
  date?: string; // RFC3339
  isUnread?: boolean;
  labels?: string[];
  mailbox?: string;
  hasAttachment?: boolean;
  attachmentCount?: number;
  priority?: string; // "urgent" | "attention"
  priorityHint?: string; // Korean hint, e.g. "낙찰 · 마감 표현"
}

// Calendar timestamps arrive as RFC3339 strings; tolerate Google's nested
// { dateTime } / { date } shape too (calSpan handles both).
export type CalTimestamp = string | { dateTime?: string; date?: string };

export interface CalEvent {
  id: string | number;
  summary?: string;
  title?: string; // legacy alias; gateway sends `summary`
  description?: string;
  start?: CalTimestamp;
  end?: CalTimestamp;
  allDay?: boolean;
  location?: string;
  status?: string;
  local?: boolean; // user-created → editable/deletable (Google events are read-only)
  category?: string; // "mine" | "others" | "deadline"
}

export interface Person {
  id?: string | number;
  email: string;
  name?: string;
  messageCount?: number;
  lastSeen?: string; // RFC3339
  lastSubject?: string;
  wikiPath?: string;
  wikiSummary?: string;
}

export interface Cron {
  id: string | number;
  name?: string;
  enabled?: boolean;
  schedule?: string; // Korean summary, e.g. "15분마다"
  payloadKind?: string;
  payloadPreview?: string;
  nextRunAtMs?: number;
  consecutiveErrors?: number;
  lastError?: string;
}

export interface WorkAction {
  id: string;
  label: string;
  body?: string;
}

export interface WorkItem {
  id: string | number;
  source?: string; // e.g. "deal_question", "proactive"
  sessionKey?: string;
  refId?: string;
  title?: string;
  body?: string;
  actions?: WorkAction[];
  createdAtMs?: number;
  ackedAtMs?: number;
}

// Wiki (memory.*) and unified search are query-driven, handled by custom panes
// calling RPCs directly rather than the CRUD data provider.
export interface WikiPage {
  id?: string;
  path?: string;
  title?: string;
  summary?: string;
  category?: string;
  tags?: string[];
  snippet?: string; // present on memory.search hits
  score?: number;
  body?: string; // present on get_page
}

export interface SearchHit {
  id?: string;
  type?: string; // client-applied bucket label: "위키" | "다이어리" | "인물"
  path?: string;
  title?: string;
  summary?: string;
  category?: string;
  snippet?: string;
}

// Every navigable pane. Resource-backed panes share their key with a Refine
// resource (see resources.ts); `today` is a read-only dashboard aggregating
// several resources; `doc` is a client-only scratch pane; `wiki` and `search`
// are query-driven custom panes.
export type View = "today" | "todo" | "doc" | "mail" | "calendar" | "wiki" | "search" | "people" | "crons" | "workfeed";
