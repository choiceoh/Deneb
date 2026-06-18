// Sample data for the mock gateway. Typed against the domain models so fixtures
// stay honest as the types evolve. Keep these realistic — they're what an agent
// (or a screenshot) sees when running against the mock.
import type { CalEvent, Cron, Mail, Person, SearchHit, Todo, WikiPage, WorkItem } from "@/types";

export const todos: Todo[] = [
  { id: "t1", title: "분기 보고서 초안 작성", done: false, dueDate: "2026-06-20" },
  { id: "t2", title: "팀 회고 안건 정리", done: false },
  { id: "t3", title: "경비 정산 제출", done: true },
];

export const mail: Mail[] = [
  {
    id: "m1",
    subject: "분기 리뷰 일정 확정",
    from: { name: "김리드", email: "lead@corp.example" },
    date: "2026-06-17T09:12:00Z",
    snippet: "다음 주 화요일 오후 2시로 확정합니다. 자료 미리…",
    unread: true,
  },
  {
    id: "m2",
    subject: "[뉴스레터] 이번 주 업계 동향",
    from: "news@digest.example",
    date: "2026-06-16T22:01:00Z",
    snippet: "이번 주 주요 소식을 정리했습니다.",
  },
];

export const events: CalEvent[] = [
  { id: "e1", title: "기획 리뷰", start: "2026-06-18T05:00:00Z", end: "2026-06-18T06:00:00Z", location: "회의실 A" },
  { id: "e2", title: "연차", start: { date: "2026-06-22" }, end: { date: "2026-06-23" } },
];

export const people: Person[] = [
  { id: "p1", name: "김리드", email: "lead@corp.example", org: "기획팀", role: "팀장" },
  { id: "p2", name: "박개발", email: "dev@corp.example", org: "엔지니어링" },
];

export const crons: Cron[] = [
  { id: "c1", name: "아침 브리핑", schedule: "0 8 * * *", enabled: true, nextRun: "2026-06-19T08:00:00Z" },
  { id: "c2", name: "주간 요약", schedule: "0 18 * * FRI", enabled: false },
];

export const workfeed: WorkItem[] = [
  { id: "w1", title: "분기 리뷰 자료 검토 요청", kind: "review", ts: "2026-06-17T10:00:00Z" },
  { id: "w2", title: "미답장 메일 3건", summary: "24시간 이상 경과", kind: "followup", ts: "2026-06-17T08:00:00Z" },
];

export const pages: WikiPage[] = [
  { id: "wk1", path: "projects/andromeda", title: "Andromeda 설계 노트" },
  { id: "wk2", path: "team/onboarding", title: "팀 온보딩 가이드" },
];

export function searchHits(query: string): SearchHit[] {
  return [
    { id: "s1", type: "mail", title: "분기 리뷰 일정 확정", snippet: `"${query}" 관련 메일` },
    { id: "s2", type: "wiki", title: "Andromeda 설계 노트", snippet: `"${query}" 언급 위키` },
  ];
}
