// The workspace command bus — one vocabulary for everyone who drives the
// workstation: the ⌘K palette, keyboard shortcuts, and Deneb itself (the gateway
// pushes `workspace` events over the proactive SSE channel; see useEvents).
// Executing a command is WorkspaceProvider.runCommand's job; this module is the
// pure grammar: the union, the tolerant parser for gateway payloads, and the
// Korean description shown when a command was machine-initiated.
import { PANES, paneLabel } from "./components/panes";
import type { View } from "./types";
import { isTileable, MAX_TILES } from "./tiling";

export type WorkspaceCommand =
  | { kind: "open"; view: View; ref?: string; query?: string; date?: string }
  | { kind: "wiki"; path: string }
  | { kind: "split"; view: View; ref?: string; date?: string }
  | { kind: "close"; view?: View }
  | { kind: "focus"; view: View }
  | { kind: "layout"; views: View[] }
  // spotlight: open the view at ref AND flash the tile — "여기 보세요".
  | { kind: "spotlight"; view: View; ref: string }
  // prefill: open the 할일 form pre-filled; saving stays the human's click.
  | { kind: "prefill"; view: "todo"; title: string; due?: string; note?: string };

// Day-pager jump dates ride the wire as YYYY-MM-DD only.
const DATE_RE = /^\d{4}-\d{2}-\d{2}$/;

function asDate(v: unknown): string | undefined {
  return typeof v === "string" && DATE_RE.test(v.trim()) ? v.trim() : undefined;
}

const VIEW_KEYS: ReadonlySet<View> = new Set(PANES.map((p) => p.key));

function asView(v: unknown): View | null {
  return typeof v === "string" && VIEW_KEYS.has(v as View) ? (v as View) : null;
}

function asStr(v: unknown): string | undefined {
  return typeof v === "string" && v.trim() !== "" ? v : undefined;
}

// Event kinds that carry a workspace command (the gateway's workstation-control
// push). Anything else on the events stream is a plain proactive nudge.
export function isWorkspaceCommandKind(kind: string | undefined): boolean {
  return kind === "workspace" || kind === "workspace.command";
}

// Parse a gateway push payload into a command. Deliberately tolerant (unknown
// fields ignored) and deliberately narrow: only screen-arrangement verbs are
// accepted from the wire — nothing that would let a pushed event type into the
// chat or mutate data. Returns null for anything malformed.
export function parseWorkspaceCommand(raw: Record<string, unknown>): WorkspaceCommand | null {
  const action = asStr(raw.action) ?? asStr(raw.verb);
  const rawView = asStr(raw.view) ?? asStr(raw.pane);
  const view = asView(rawView);
  switch (action) {
    case "open": {
      // Wiki opens ride the wiki channel (WikiPane consumes wikiTarget, not a
      // pane-target id) — accept the page path in either `path` or, for an
      // explicit wiki view, the event stream's standard `ref` field.
      const path = asStr(raw.path) ?? (view === "wiki" ? asStr(raw.ref) : undefined);
      if (path && (view === "wiki" || !view)) return { kind: "wiki", path };
      if (!view) return null;
      return { kind: "open", view, ref: asStr(raw.ref), query: asStr(raw.query), date: asDate(raw.date) };
    }
    case "wiki": {
      const path = asStr(raw.path) ?? asStr(raw.ref);
      return path ? { kind: "wiki", path } : null;
    }
    case "split":
      return view && isTileable(view) ? { kind: "split", view, ref: asStr(raw.ref), date: asDate(raw.date) } : null;
    case "close":
      // A close naming an UNKNOWN view is malformed — drop it rather than
      // falling through to "close the focused tile" (a drifted gateway command
      // must not mutate the layout). Omitting the view stays the intentional
      // close-focused form.
      if (rawView && !view) return null;
      return { kind: "close", view: view ?? undefined };
    case "focus":
      return view ? { kind: "focus", view } : null;
    case "spotlight": {
      const ref = asStr(raw.ref);
      return view && ref ? { kind: "spotlight", view, ref } : null;
    }
    case "prefill": {
      // Narrow by design: only the 할일 form, only prose fields — a drifted
      // gateway command must not be able to type anywhere else.
      const title = asStr(raw.title);
      if (view !== "todo" || !title) return null;
      return { kind: "prefill", view: "todo", title, due: asDate(raw.due), note: asStr(raw.note) };
    }
    case "layout": {
      const rawViews = Array.isArray(raw.views)
        ? raw.views
        : typeof raw.views === "string"
          ? raw.views.split(",").map((s) => s.trim())
          : [];
      const views: View[] = [];
      for (const item of rawViews) {
        const v = asView(item);
        if (v && isTileable(v) && !views.includes(v)) views.push(v);
        if (views.length >= MAX_TILES) break;
      }
      return views.length > 0 ? { kind: "layout", views } : null;
    }
    default:
      return null;
  }
}

// Human-readable description — shown as a proactive nudge so a machine-driven
// rearrangement is always visible ("Deneb이 화면을 조정했습니다" transparency).
export function describeCommand(cmd: WorkspaceCommand): string {
  switch (cmd.kind) {
    case "open":
      return `${paneLabel(cmd.view)} 화면을 열었습니다${cmd.query ? ` · 검색: ${cmd.query}` : ""}${cmd.date ? ` · ${cmd.date}` : ""}`;
    case "wiki":
      return `위키 문서를 열었습니다: ${cmd.path}`;
    case "split":
      return `${paneLabel(cmd.view)} 화면을 분할로 추가했습니다`;
    case "close":
      return cmd.view ? `${paneLabel(cmd.view)} 분할을 닫았습니다` : "분할 화면을 닫았습니다";
    case "focus":
      return `${paneLabel(cmd.view)} 화면에 포커스했습니다`;
    case "layout":
      return `화면 구성: ${cmd.views.map((v) => paneLabel(v)).join(" · ")}`;
    case "spotlight":
      return `${paneLabel(cmd.view)}에서 항목을 강조했습니다`;
    case "prefill":
      return `할일 초안을 채워 열었습니다: ${cmd.title}`;
  }
}
