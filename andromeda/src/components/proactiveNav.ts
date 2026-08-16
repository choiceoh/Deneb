import type { View } from "@/types";
import type { ProactiveEvent } from "@/events";

export interface ProactiveNav {
  view: View;
  ref?: string;
}

// Pane keys a proactive nudge may target. The gateway sets event.kind to one of
// these in the push payload (clientPushEvent), so the nudge can open the relevant
// pane; absent/unknown kinds — the SSE "push" event-name fallback, errors — aren't
// navigable and render as plain text. ref is the resource id (or wiki path).
const NAV_VIEWS = new Set<View>([
  "mail",
  "calendar",
  "todo",
  "workfeed",
  "fleet",
  "wiki",
  "people",
  "crons",
  "files",
  "search",
  "notebook",
  "today",
]);

export function proactiveNav(ev: ProactiveEvent): ProactiveNav | null {
  const kind = ev.kind;
  if (!kind || !NAV_VIEWS.has(kind as View)) return null;
  return { view: kind as View, ref: ev.ref };
}
