// Pure derivations for the 오늘 cockpit — KPI counts, the day-timeline layout,
// and the merged deadline radar. No React: everything here is unit-tested
// against fixed clocks, and TodayPane just renders the results.
import type { CalEvent, GroupwareApprovalRow, ProjectDigest, Todo, WorkItem } from "@/types";
import type { PaneTarget } from "@/workspaceContext";
import { dayKey, eventDayKeys, eventStartMs } from "@/format";

// ---- KPI strip ----

export interface Kpi {
  key: string;
  label: string;
  value: number;
  // Secondary line under the number (e.g. next event time). Optional.
  hint?: string;
  // Danger = needs action now (미결/기한 지남); warn = attention.
  tone?: "danger" | "warn";
  target: PaneTarget;
}

export function buildKpis(input: {
  approvals: GroupwareApprovalRow[];
  workItems: WorkItem[];
  events: CalEvent[];
  todos: Todo[];
  unreadMails: number;
  now: number;
}): Kpi[] {
  const { approvals, workItems, events, todos, unreadMails, now } = input;

  const pendingApprovals = approvals.filter((a) => a.canAct).length;
  // 질문 대기: the agent explicitly waits on an answer and the card is unsettled.
  const openQuestions = workItems.filter((w) => Boolean(w.question) && !w.ackedAtMs).length;

  const todayKey = dayKeyOf(now);
  const todayEvents = events.filter((ev) => eventDayKeys(ev.start, ev.end).includes(todayKey));
  const upcoming = todayEvents
    .map((ev) => ({ ev, start: eventStartMs(ev.start) }))
    .filter((x): x is { ev: CalEvent; start: number } => typeof x.start === "number" && x.start >= now)
    .sort((a, b) => a.start - b.start);
  const next = upcoming[0];
  const nextLabel = next
    ? `다음 ${new Date(next.start).toLocaleTimeString("ko-KR", { hour: "2-digit", minute: "2-digit", hour12: false })} ${next.ev.summary ?? next.ev.title ?? ""}`.trim()
    : undefined;

  const overdue = todos.filter((t) => {
    if (t.done || !t.due) return false;
    const ts = new Date(t.due).getTime();
    return Number.isFinite(ts) && !Number.isNaN(ts) && ts < now;
  }).length;

  return [
    {
      key: "approvals",
      label: "미결 결재",
      value: pendingApprovals,
      tone: pendingApprovals > 0 ? "danger" : undefined,
      target: { view: "approvals", query: "pending" },
    },
    {
      key: "questions",
      label: "질문 대기",
      value: openQuestions,
      tone: openQuestions > 0 ? "warn" : undefined,
      // Deep-link into the feed's 질문 대기 inbox (WorkfeedPane query="questions").
      target: { view: "workfeed", query: "questions" },
    },
    {
      key: "events",
      label: "오늘 일정",
      value: todayEvents.length,
      hint: nextLabel,
      target: { view: "calendar" },
    },
    {
      key: "overdue",
      label: "기한 지남",
      value: overdue,
      tone: overdue > 0 ? "danger" : undefined,
      target: { view: "todo" },
    },
    {
      key: "unread",
      label: "안 읽은 메일",
      value: unreadMails,
      target: { view: "mail" },
    },
  ];
}

export function kpiText(kpis: Kpi[]): string {
  return kpis.map((k) => `${k.label} ${k.value}${k.hint ? ` (${k.hint})` : ""}`).join(" · ");
}

// ---- Day timeline ----

export const TIMELINE_START_HOUR = 7;
export const TIMELINE_END_HOUR = 20;
const MAX_LANES = 3;

export interface TimelineBlock {
  id: string | number;
  title: string;
  leftPct: number;
  widthPct: number;
  lane: number; // 0-based row for overlapping events
  timeLabel: string;
  clampedStart: boolean; // began before the visible window
  target: PaneTarget;
}

export interface DayTimeline {
  blocks: TimelineBlock[];
  allDay: { id: string | number; title: string; target: PaneTarget }[];
  laneCount: number;
  nowPct: number | null; // current-time marker position, null outside the window
}

// Reuse format.ts's dayKey so keys compare equal with eventDayKeys output
// (that format is UNPADDED — "2026-7-17"; a padded key silently matches nothing).
function dayKeyOf(ms: number): string {
  return dayKey(new Date(ms));
}

// Lay today's events on a 07:00–20:00 horizontal track. Overlaps stack into up
// to MAX_LANES rows (greedy, earliest-fitting lane); events entirely outside
// the window are dropped, partial overlaps clamp to the edge.
export function buildDayTimeline(events: CalEvent[], now: number): DayTimeline {
  const todayKey = dayKeyOf(now);
  const base = new Date(now);
  base.setHours(0, 0, 0, 0);
  const winStart = base.getTime() + TIMELINE_START_HOUR * 3_600_000;
  const winEnd = base.getTime() + TIMELINE_END_HOUR * 3_600_000;
  const span = winEnd - winStart;

  const allDay: DayTimeline["allDay"] = [];
  const timed: { id: string | number; title: string; start: number; end: number; target: PaneTarget }[] = [];

  for (const ev of events) {
    if (!eventDayKeys(ev.start, ev.end).includes(todayKey)) continue;
    const title = ev.summary ?? ev.title ?? "(제목 없음)";
    const target: PaneTarget = { view: "calendar", id: ev.id, dayKey: todayKey };
    const start = eventStartMs(ev.start);
    if (ev.allDay || start == null) {
      allDay.push({ id: ev.id, title, target });
      continue;
    }
    const endRaw = eventStartMs(ev.end);
    // A missing/invalid end renders as a 30-minute block — enough to see and click.
    const end = endRaw != null && endRaw > start ? endRaw : start + 30 * 60_000;
    if (end <= winStart || start >= winEnd) {
      // Outside the visible window (early morning / late night) — treat like
      // an all-day chip so it stays discoverable.
      allDay.push({ id: ev.id, title, target });
      continue;
    }
    timed.push({ id: ev.id, title, start, end, target });
  }

  timed.sort((a, b) => a.start - b.start || a.end - b.end);
  const laneEnds: number[] = [];
  const blocks: TimelineBlock[] = [];
  for (const t of timed) {
    const s = Math.max(t.start, winStart);
    const e = Math.min(t.end, winEnd);
    let lane = laneEnds.findIndex((endMs) => endMs <= s);
    if (lane === -1) {
      if (laneEnds.length >= MAX_LANES)
        lane = laneEnds.length - 1; // overflow shares the last lane
      else {
        lane = laneEnds.length;
        laneEnds.push(0);
      }
    }
    laneEnds[lane] = Math.max(laneEnds[lane], e);
    blocks.push({
      id: t.id,
      title: t.title,
      leftPct: ((s - winStart) / span) * 100,
      widthPct: Math.max(((e - s) / span) * 100, 1.5),
      lane,
      timeLabel: new Date(t.start).toLocaleTimeString("ko-KR", { hour: "2-digit", minute: "2-digit", hour12: false }),
      clampedStart: t.start < winStart,
      target: t.target,
    });
  }

  const nowPct = now >= winStart && now <= winEnd ? ((now - winStart) / span) * 100 : null;
  return { blocks, allDay, laneCount: Math.max(1, laneEnds.length), nowPct };
}

export function timelineText(tl: DayTimeline): string {
  const rows = tl.blocks.map((b) => `- ${b.timeLabel} ${b.title}`);
  for (const a of tl.allDay) rows.push(`- (종일/기타) ${a.title}`);
  return rows.join("\n");
}

// ---- Deadline radar ----

export interface DeadlineEntry {
  key: string;
  title: string;
  kind: "프로젝트" | "할일";
  dday: number; // 0 = today, negative = overdue
  dateLabel: string;
  target: PaneTarget;
}

const DAY_MS = 86_400_000;

function startOfDayMs(ms: number): number {
  const d = new Date(ms);
  d.setHours(0, 0, 0, 0);
  return d.getTime();
}

// Best-effort parse of a digest's free-form due string. Accepts RFC3339/ISO
// dates and Korean "M월 D일" (nearest future occurrence). Anything else → null
// (the radar shows only what it can honestly place on a calendar).
// A real calendar day — new Date(y, m, d) silently NORMALIZES overflow
// ("13월 40일" would roll forward), so accept only components that round-trip.
function ymdMs(year: number, month: number, day: number): number | null {
  const d = new Date(year, month - 1, day);
  const ok = d.getFullYear() === year && d.getMonth() === month - 1 && d.getDate() === day;
  return ok ? d.getTime() : null;
}

export function parseDueMs(raw: string | undefined, now: number): number | null {
  if (!raw) return null;
  const s = raw.trim();
  const iso = s.match(/(\d{4})[-./](\d{1,2})[-./](\d{1,2})/);
  if (iso) return ymdMs(Number(iso[1]), Number(iso[2]), Number(iso[3]));
  const kr = s.match(/(?:(\d{4})년\s*)?(\d{1,2})월\s*(\d{1,2})일/);
  if (kr) {
    const year = kr[1] ? Number(kr[1]) : new Date(now).getFullYear();
    let t = ymdMs(year, Number(kr[2]), Number(kr[3]));
    // Un-yeared dates more than ~6 months past roll to next year (a "1월 10일"
    // seen in December means the coming January).
    if (t != null && !kr[1] && t < startOfDayMs(now) - 180 * DAY_MS) t = ymdMs(year + 1, Number(kr[2]), Number(kr[3]));
    return t;
  }
  const parsed = Date.parse(s);
  return Number.isNaN(parsed) ? null : parsed;
}

// Merge project-digest deadlines and open todo due dates into one D-day-sorted
// radar. Window: overdue (kept, most urgent first) through +30 days. Returns
// the FULL list — display truncation is the card's job, so header counts stay
// honest (Brief.total contract).
export function buildDeadlineRadar(digests: ProjectDigest[], todos: Todo[], now: number): DeadlineEntry[] {
  const today = startOfDayMs(now);
  const out: DeadlineEntry[] = [];

  for (const d of digests) {
    const ms = parseDueMs(d.due, now);
    if (ms == null) continue;
    const dday = Math.round((startOfDayMs(ms) - today) / DAY_MS);
    if (dday > 30) continue;
    out.push({
      // path is the stable unique identity other panes key on; code/project can
      // collide across digests.
      key: `p:${d.path ?? d.code ?? d.project}`,
      title: d.project,
      kind: "프로젝트",
      dday,
      dateLabel: new Date(ms).toLocaleDateString("ko-KR", { month: "numeric", day: "numeric" }),
      target: { view: "projects" },
    });
  }
  for (const t of todos) {
    if (t.done || !t.due) continue;
    const ms = new Date(t.due).getTime();
    if (Number.isNaN(ms)) continue;
    const dday = Math.round((startOfDayMs(ms) - today) / DAY_MS);
    if (dday > 30) continue;
    out.push({
      key: `t:${t.id}`,
      title: t.title,
      kind: "할일",
      dday,
      dateLabel: new Date(ms).toLocaleDateString("ko-KR", { month: "numeric", day: "numeric" }),
      target: { view: "todo", id: t.id },
    });
  }

  out.sort((a, b) => a.dday - b.dday || a.title.localeCompare(b.title));
  return out;
}

export function ddayLabel(dday: number): string {
  if (dday === 0) return "D-DAY";
  return dday < 0 ? `D+${-dday}` : `D-${dday}`;
}

export function radarText(entries: DeadlineEntry[]): string {
  return entries.map((e) => `- ${ddayLabel(e.dday)} [${e.kind}] ${e.title} (${e.dateLabel})`).join("\n");
}
