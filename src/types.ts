// Domain row shapes flowing from the Deneb gateway, plus the pane/view key union.
//
// Back-end payloads are dynamic, so every field beyond `id` is optional — display
// and AI serialization fall back gracefully (see format.ts). Keep these aligned
// with DESIGN §5's resource↔RPC mapping.

export interface Todo {
  id: string | number;
  title: string;
  done?: boolean;
  dueDate?: string;
}

export interface Mail {
  id: string | number;
  subject?: string;
  from?: unknown; // string or { name, email }
  sender?: unknown;
  date?: string;
  snippet?: string;
  unread?: boolean;
}

// Google Calendar-shaped events nest the timestamp as { dateTime } or { date }.
export type CalTimestamp = string | { dateTime?: string; date?: string };

export interface CalEvent {
  id: string | number;
  title?: string;
  summary?: string;
  start?: CalTimestamp;
  end?: CalTimestamp;
  location?: string;
}

export interface Person {
  id: string | number;
  name?: string;
  email?: string;
  org?: string;
  role?: string;
}

export interface Cron {
  id: string | number;
  name?: string;
  schedule?: string;
  enabled?: boolean;
  nextRun?: string;
}

export interface WorkItem {
  id: string | number;
  title?: string;
  summary?: string;
  kind?: string;
  ts?: string;
}

// Wiki (memory.*) and unified search are query-driven, so they're handled by
// custom panes calling RPCs directly rather than the CRUD data provider.
export interface WikiPage {
  id?: string;
  path?: string;
  title?: string;
  category?: string;
  // memory.search hit extras (path is the stable key; no id server-side).
  summary?: string;
  snippet?: string;
  score?: number;
}

export interface SearchHit {
  id?: string;
  type?: string;
  title?: string;
  snippet?: string;
}

// Every navigable pane. Resource-backed panes share their key with a Refine
// resource (see resources.ts); `today` is a read-only dashboard aggregating
// several resources; `doc` is a client-only scratch pane; `wiki` and `search`
// are query-driven custom panes.
export type View = "today" | "todo" | "doc" | "mail" | "calendar" | "wiki" | "search" | "people" | "crons" | "workfeed";
