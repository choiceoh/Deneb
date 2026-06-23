// Sample data for the mock gateway. Typed against the domain models so fixtures
// stay honest as the types evolve. Field names mirror the gateway WIRE contract
// (isUnread, due, nextRunAtMs, workfeed body/source/createdAtMs, …) — they're what
// an agent (or a screenshot) sees when running against the mock.
import type { CalEvent, Cron, Mail, Person, ProjectDigest, SearchHit, Todo, WikiPage, WorkItem } from "@/types";

export const todos: Todo[] = [
  { id: "t1", title: "분기 보고서 초안 작성", done: false, due: "2026-06-20T00:00:00Z" },
  { id: "t2", title: "팀 회고 안건 정리", done: false },
  { id: "t3", title: "경비 정산 제출", done: true, doneAt: "2026-06-18T05:00:00Z" },
];

export const mail: Mail[] = [
  {
    id: "m1",
    threadId: "th1",
    subject: "분기 리뷰 일정 확정",
    from: "김리드 <lead@corp.example>",
    date: "2026-06-17T09:12:00Z",
    snippet: "다음 주 화요일 오후 2시로 확정합니다. 자료 미리…",
    body: "다음 주 화요일 오후 2시로 분기 리뷰 일정을 확정합니다.\n\n회의 전까지 초안 자료를 공유해 주세요.",
    isUnread: true,
    priority: "attention",
    priorityHint: "마감 표현",
  },
  {
    id: "m2",
    threadId: "th2",
    subject: "[뉴스레터] 이번 주 업계 동향",
    from: "news@digest.example",
    date: "2026-06-16T22:01:00Z",
    snippet: "이번 주 주요 소식을 정리했습니다.",
  },
];

export const events: CalEvent[] = [
  {
    id: "e1",
    summary: "기획 리뷰",
    start: "2026-06-18T05:00:00Z",
    end: "2026-06-18T06:00:00Z",
    location: "회의실 A",
    local: true,
  },
  { id: "e2", summary: "연차", start: { date: "2026-06-22" }, end: { date: "2026-06-23" }, allDay: true },
];

export const people: Person[] = [
  {
    email: "lead@corp.example",
    name: "김리드",
    messageCount: 12,
    lastSeen: "2026-06-17T09:12:00Z",
    lastSubject: "분기 리뷰 일정 확정",
    wikiPath: "인물/김리드",
    wikiSummary: "기획팀 팀장",
  },
  {
    email: "dev@corp.example",
    name: "박개발",
    messageCount: 4,
    lastSeen: "2026-06-15T03:00:00Z",
    lastSubject: "API 스펙 질문",
  },
];

export const crons: Cron[] = [
  {
    id: "c1",
    name: "아침 브리핑",
    schedule: "매일 08:00",
    enabled: true,
    payloadKind: "agentTurn",
    nextRunAtMs: 1782201600000,
  },
  { id: "c2", name: "주간 요약", schedule: "매주 금 18:00", enabled: false, payloadKind: "agentTurn" },
];

export const workfeed: WorkItem[] = [
  {
    id: "w1",
    source: "deal_question",
    title: "분기 리뷰 자료 검토 요청",
    body: "검토 후 승인 여부를 알려주세요.",
    createdAtMs: 1782100800000,
    actions: [
      { id: "approve", label: "승인" },
      { id: "hold", label: "보류" },
    ],
  },
  {
    id: "w2",
    source: "followup",
    title: "미답장 메일 3건",
    body: "24시간 이상 경과한 메일이 있습니다.",
    createdAtMs: 1782090000000,
  },
];

// project.digests — each active project's distilled latest-progress card
// (headline + bullets + optional due), newest-first, keyed by wiki path.
export const digests: ProjectDigest[] = [
  {
    project: "Andromeda 워크스테이션",
    headline: "진행상황 패널 추가 — 게이트웨이 계약 정합 중",
    bullets: ["project.digests RPC 연동", "카드 레이아웃 + AI 컨텍스트 투영"],
    due: "이번 주",
    updatedAtMs: 1782180000000,
    path: "projects/andromeda",
  },
  {
    project: "데네브 게이트웨이",
    headline: "CORS 허용으로 브라우저 워크스테이션 연결 복구",
    bullets: ["Tauri 네이티브 HTTP 우회 제거 검토"],
    updatedAtMs: 1782090000000,
    path: "projects/deneb",
  },
];

// memory.search hits — keyed by path (no server-side id), with snippet/score
// like the real gateway response.
export const pages: WikiPage[] = [
  {
    path: "projects/andromeda",
    title: "Andromeda 설계 노트",
    summary: "3분할 워크스테이션",
    snippet: "3분할 워크스테이션 설계…",
    score: 0.91,
  },
  {
    path: "team/onboarding",
    title: "팀 온보딩 가이드",
    summary: "신규 입사자 안내",
    snippet: "신규 입사자 안내…",
    score: 0.74,
  },
];

// search.all fans out to wiki / diary / people buckets (the gateway shape).
export function searchAll(query: string): { wiki: SearchHit[]; diary: SearchHit[]; people: SearchHit[] } {
  return {
    wiki: [
      {
        path: "projects/andromeda",
        title: "Andromeda 설계 노트",
        category: "projects",
        snippet: `"${query}" 관련 위키`,
      },
    ],
    diary: [{ path: "diary/2026-06-17.md", title: "2026-06-17", snippet: `"${query}" 언급된 일지` }],
    people: [{ path: "인물/김리드", title: "김리드", snippet: `lead@corp.example · "${query}"` }],
  };
}
