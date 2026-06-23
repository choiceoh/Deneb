// Sample data for the mock gateway. Typed against the domain models so fixtures
// stay honest as the types evolve. Field names mirror the gateway WIRE contract
// (isUnread, due, nextRunAtMs, workfeed body/source/createdAtMs, …) — they're what
// an agent (or a screenshot) sees when running against the mock.
import type { ModelsList, SessionRow, TranscriptMsg } from "@/gateway";
import type { CalEvent, Cron, Mail, Person, ProjectDigest, SearchHit, Todo, WikiPage, WorkItem } from "@/types";
import { calStamp } from "@/format";

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
  {
    id: "e2",
    summary: "연차",
    description: "여름 휴가 — 자리 비움",
    start: { date: "2026-06-22" },
    end: { date: "2026-06-23" },
    allDay: true,
  },
];

function calTimeMs(stamp: ReturnType<typeof calStamp>): number {
  if (!stamp.iso) return Number.NaN;
  if (stamp.allDay) {
    const m = /^(\d{4})-(\d{2})-(\d{2})$/.exec(stamp.iso);
    if (m) return new Date(Number(m[1]), Number(m[2]) - 1, Number(m[3])).getTime();
  }
  return new Date(stamp.iso).getTime();
}

export function eventsInRange(fromIso: string, toIso: string): CalEvent[] {
  const from = new Date(fromIso).getTime();
  const to = new Date(toIso).getTime();
  if (!Number.isFinite(from) || !Number.isFinite(to) || to <= from) return [];
  return events.filter((ev) => {
    const start = calStamp(ev.start);
    if (!start.iso) return false;
    const startMs = calTimeMs(start);
    const end = calStamp(ev.end);
    const endMs = end.iso ? calTimeMs(end) : startMs + (start.allDay ? 86_400_000 : 1);
    if (!Number.isFinite(startMs) || !Number.isFinite(endMs)) return false;
    return startMs < to && Math.max(endMs, startMs + 1) > from;
  });
}

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
    payloadPreview: "오늘 일정·메일 요약을 작성하고 알림",
    nextRunAtMs: 1782201600000,
  },
  { id: "c2", name: "주간 요약", schedule: "매주 금 18:00", enabled: false, payloadKind: "agentTurn" },
  {
    id: "c3",
    name: "환율 수집",
    schedule: "15분마다",
    enabled: true,
    payloadKind: "httpFetch",
    payloadPreview: "GET https://api.example/fx",
    nextRunAtMs: 1782102600000,
    consecutiveErrors: 3,
    lastError: "타임아웃: 응답이 10초를 초과했습니다",
  },
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

// models.list — active model + per-role bindings + grouped picker sections.
export const models: ModelsList = {
  current: "anthropic/claude-opus-4-8",
  roles: [{ role: "main", model: "anthropic/claude-opus-4-8" }],
  sections: [
    {
      title: "Anthropic",
      models: [
        { id: "anthropic/claude-opus-4-8", label: "Claude Opus 4.8", provider: "anthropic", current: true },
        { id: "anthropic/claude-sonnet-4-6", label: "Claude Sonnet 4.6", provider: "anthropic" },
      ],
    },
    {
      title: "로컬",
      models: [{ id: "local/qwen3", label: "Qwen3 30B", provider: "vllm", note: "p95 1.2s · 캐시 88%" }],
    },
  ],
  mainHasVision: true,
};

// sessions.recent — the conversation history drawer's rows.
export const sessions: SessionRow[] = [
  {
    key: "client:main",
    kind: "interactive",
    status: "idle",
    channel: "native",
    model: "claude-opus-4-8",
    label: "메인 대화",
    updatedAtMs: 1782200000000,
  },
  {
    key: "cron:morning-brief",
    kind: "cron",
    status: "done",
    channel: "cron",
    model: "claude-haiku-4-5",
    label: "아침 브리핑",
    updatedAtMs: 1782100000000,
  },
];

// sessions.transcript — message history keyed by session.
export const transcript: Record<string, TranscriptMsg[]> = {
  "client:main": [
    { role: "user", content: "오늘 일정 요약해줘", timestampMs: 1782199000000 },
    {
      role: "assistant",
      content: "**오늘 일정**\n\n- 14:00 기획 리뷰 (회의실 A)\n- 종일 연차 1건",
      timestampMs: 1782199500000,
    },
  ],
};

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
