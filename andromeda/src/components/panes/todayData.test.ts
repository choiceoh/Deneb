import { describe, expect, it } from "vitest";
import type { CalEvent, GroupwareApprovalRow, ProjectDigest, Todo, WorkItem } from "@/types";
import {
  buildDayTimeline,
  buildDeadlineRadar,
  buildKpis,
  ddayLabel,
  kpiText,
  parseDueMs,
  radarText,
  timelineText,
} from "./todayData";

// Fixed clock: 2026-07-17 (금) 10:00 local.
const NOW = new Date(2026, 6, 17, 10, 0).getTime();

const iso = (h: number, m = 0) => new Date(2026, 6, 17, h, m).toISOString();

describe("buildKpis", () => {
  it("counts pending approvals, open questions, today's events, overdue todos, unread mail", () => {
    const approvals = [
      { docId: "a1", canAct: true },
      { docId: "a2", canAct: false },
      { docId: "a3", canAct: true },
    ] as GroupwareApprovalRow[];
    const workItems: WorkItem[] = [
      { id: 1, question: true }, // open
      { id: 2, question: true, ackedAtMs: 1 }, // settled
      { id: 3 }, // not a question
    ];
    const events: CalEvent[] = [
      { id: "e1", summary: "오전 회의", start: iso(9), end: iso(9, 30) }, // already past (not next, still today)
      { id: "e2", summary: "현장 방문", start: iso(14), end: iso(15) },
      { id: "e3", summary: "내일 것", start: new Date(2026, 6, 18, 9).toISOString() },
    ];
    const todos: Todo[] = [
      { id: 1, title: "지난 것", due: new Date(2026, 6, 16, 9).toISOString() },
      { id: 2, title: "완료된 것", due: new Date(2026, 6, 15).toISOString(), done: true },
      { id: 3, title: "미래", due: new Date(2026, 6, 20).toISOString() },
      { id: 4, title: "기한 없음" },
    ];

    const kpis = buildKpis({ approvals, workItems, events, todos, unreadMails: 5, now: NOW });
    const byKey = Object.fromEntries(kpis.map((k) => [k.key, k]));

    expect(byKey.approvals.value).toBe(2);
    expect(byKey.approvals.tone).toBe("danger");
    expect(byKey.questions.value).toBe(1);
    expect(byKey.events.value).toBe(2); // today only
    expect(byKey.events.hint).toContain("14:00");
    expect(byKey.events.hint).toContain("현장 방문");
    expect(byKey.overdue.value).toBe(1);
    expect(byKey.unread.value).toBe(5);
    expect(byKey.approvals.target).toEqual({ view: "approvals", query: "pending" });
    expect(byKey.questions.target).toEqual({ view: "workfeed", query: "questions" });

    expect(kpiText(kpis)).toContain("미결 결재 2");
  });

  it("keeps zero counts toneless", () => {
    const kpis = buildKpis({ approvals: [], workItems: [], events: [], todos: [], unreadMails: 0, now: NOW });
    for (const k of kpis) expect(k.tone).toBeUndefined();
  });
});

describe("buildDayTimeline", () => {
  it("places today's timed events with clamping and lanes for overlaps", () => {
    const events: CalEvent[] = [
      { id: "a", summary: "아침 러닝", start: iso(6), end: iso(8) }, // clamps to 07:00
      { id: "b", summary: "회의", start: iso(9), end: iso(10) },
      { id: "c", summary: "겹침", start: iso(9, 30), end: iso(10, 30) }, // overlaps b → lane 1
      { id: "d", summary: "저녁 늦게", start: iso(21), end: iso(22) }, // outside window → chip
      { id: "e", summary: "종일", start: "2026-07-17", allDay: true },
      { id: "f", summary: "내일", start: new Date(2026, 6, 18, 9).toISOString() }, // not today
    ];
    const tl = buildDayTimeline(events, NOW);

    expect(tl.blocks.map((b) => String(b.id))).toEqual(["a", "b", "c"]);
    const a = tl.blocks[0];
    expect(a.leftPct).toBe(0);
    expect(a.clampedStart).toBe(true);
    const b = tl.blocks[1];
    const c = tl.blocks[2];
    expect(b.lane).toBe(0);
    expect(c.lane).toBe(1);
    expect(tl.laneCount).toBe(2);
    expect(tl.allDay.map((x) => String(x.id)).sort()).toEqual(["d", "e"]);
    // 10:00 in a 07–20 window sits at 3/13 of the track.
    expect(tl.nowPct).toBeCloseTo((3 / 13) * 100, 1);
    expect(timelineText(tl)).toContain("09:00 회의");
    expect(timelineText(tl)).toContain("(종일/기타) 종일");
  });

  it("marks now as null outside the window", () => {
    const lateNight = new Date(2026, 6, 17, 22, 30).getTime();
    expect(buildDayTimeline([], lateNight).nowPct).toBeNull();
  });
});

describe("parseDueMs", () => {
  it("reads ISO-ish and Korean dates, rejects prose", () => {
    expect(parseDueMs("2026-08-01", NOW)).toBe(new Date(2026, 7, 1).getTime());
    expect(parseDueMs("2026.8.1 완료 목표", NOW)).toBe(new Date(2026, 7, 1).getTime());
    expect(parseDueMs("8월 1일", NOW)).toBe(new Date(2026, 7, 1).getTime());
    expect(parseDueMs("2027년 1월 5일", NOW)).toBe(new Date(2027, 0, 5).getTime());
    expect(parseDueMs("이번 분기 내", NOW)).toBeNull();
    expect(parseDueMs(undefined, NOW)).toBeNull();
  });

  it("rejects overflow components instead of letting Date normalize them", () => {
    expect(parseDueMs("2026-13-40", NOW)).toBeNull();
    expect(parseDueMs("2026-02-30", NOW)).toBeNull();
    expect(parseDueMs("13월 40일", NOW)).toBeNull();
  });

  it("rolls an un-yeared past date into next year", () => {
    const december = new Date(2026, 11, 20).getTime();
    expect(parseDueMs("1월 10일", december)).toBe(new Date(2027, 0, 10).getTime());
  });
});

describe("buildDeadlineRadar", () => {
  it("merges digests and todos, sorted by D-day with overdue first", () => {
    const digests: ProjectDigest[] = [
      { project: "천안 현장", due: "2026-07-20" }, // D-3
      { project: "마감 없음" },
      { project: "먼 미래", due: "2026-12-01" }, // >30d — dropped
    ];
    const todos: Todo[] = [
      { id: 1, title: "서류 제출", due: new Date(2026, 6, 16).toISOString() }, // D+1 (overdue)
      { id: 2, title: "완료됨", due: new Date(2026, 6, 18).toISOString(), done: true },
    ];
    const radar = buildDeadlineRadar(digests, todos, NOW);

    expect(radar.map((e) => e.title)).toEqual(["서류 제출", "천안 현장"]);
    expect(radar[0].dday).toBe(-1);
    expect(ddayLabel(radar[0].dday)).toBe("D+1");
    expect(ddayLabel(radar[1].dday)).toBe("D-3");
    expect(radar[0].target).toEqual({ view: "todo", id: 1 });
    expect(radarText(radar)).toContain("D+1 [할일] 서류 제출");
  });

  it("returns the FULL list — display truncation is the card's job (honest totals)", () => {
    const todos: Todo[] = Array.from({ length: 12 }, (_, i) => ({
      id: i,
      title: `t${i}`,
      due: new Date(2026, 6, 18 + (i % 5)).toISOString(),
    }));
    expect(buildDeadlineRadar([], todos, NOW)).toHaveLength(12);
  });

  it("keys project entries by path first so same-named digests don't collide", () => {
    const digests: ProjectDigest[] = [
      { project: "증축", due: "2026-07-20", path: "프로젝트/A/증축.md" },
      { project: "증축", due: "2026-07-21", path: "프로젝트/B/증축.md" },
    ];
    const keys = buildDeadlineRadar(digests, [], NOW).map((e) => e.key);
    expect(new Set(keys).size).toBe(2);
  });
});
