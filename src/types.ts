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

// Every navigable pane. Resource-backed panes share their key with a Refine
// resource (see resources.ts); `doc` is a client-only scratch pane.
export type View = "todo" | "doc" | "mail" | "calendar";
