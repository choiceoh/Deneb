import { useEffect } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { DataProvider } from "@/crud";
import { fakeProvider, renderWithProviders } from "@/test/util";
import { startOfDay } from "@/format";
import { useWorkspace } from "@/workspaceContext";
import { WorkfeedPane } from "./WorkfeedPane";

// Stand-in for AIPanel's ask-sink registration so a test can observe prompts the
// pane routes into the AI panel (cards with no asking session).
function AskSinkProbe({ onAsk }: { onAsk: (text: string) => void }) {
  const { setAskSink } = useWorkspace();
  useEffect(() => {
    setAskSink(onAsk);
    return () => setAskSink(null);
  }, [setAskSink, onAsk]);
  return null;
}

// The list flows through the (fake) data provider; the action RPCs go straight to
// callRpc → fetch. Stub fetch to capture RPCs and follow-up chat stream deliveries.
let rpcCalls: Array<{ method: string; params: Record<string, unknown> }>;
let chatCalls: Array<{ message: string; sessionKey: string }>;

interface CapturedBody {
  method?: string;
  params?: Record<string, unknown>;
  message?: string;
  sessionKey?: string;
}

function sseResponse(body = 'event: done\ndata: {"text":"ok"}\n\n'): Response {
  const enc = new TextEncoder();
  const stream = new ReadableStream<Uint8Array>({
    start(controller) {
      controller.enqueue(enc.encode(body));
      controller.close();
    },
  });
  return new Response(stream, { status: 200, headers: { "Content-Type": "text/event-stream" } });
}

beforeEach(() => {
  rpcCalls = [];
  chatCalls = [];
  if (!globalThis.crypto?.randomUUID) vi.stubGlobal("crypto", { randomUUID: () => "test-uuid" });
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) => {
      const body = JSON.parse(String(init?.body ?? "{}")) as CapturedBody;
      const params = body.params ?? {};
      if (String(url).includes("/chat/stream")) {
        chatCalls.push({ message: String(body.message ?? ""), sessionKey: String(body.sessionKey ?? "") });
        return sseResponse();
      }
      rpcCalls.push({ method: String(body.method ?? ""), params });
      const payload =
        body.method === "miniapp.workfeed.answer"
          ? { ok: true, sessionKey: "client:main", prompt: params.answer, removeFromFeed: true }
          : body.method === "miniapp.workfeed.action.run"
            ? params.actionId === "kbinterview:start"
              ? // Periodic-task card (kb-interview): no asking session, prompt only.
                {
                  ok: true,
                  sessionKey: "",
                  prompt: "지식 인터뷰: '단가' 도메인을 인터뷰로 정리하자.",
                  removeFromFeed: true,
                }
              : { ok: true, sessionKey: "client:main", prompt: "후속 액션 실행", removeFromFeed: true }
            : { ok: true };
      return new Response(JSON.stringify({ ok: true, payload }), {
        status: 200,
        headers: { "Content-Type": "application/json" },
      });
    }),
  );
});
afterEach(() => {
  localStorage.clear();
  vi.unstubAllGlobals();
});

describe("WorkfeedPane", () => {
  it("answers a question item via workfeed.answer and delivers the returned prompt", async () => {
    const dataProvider = fakeProvider({
      workfeed: [{ id: "w1", source: "deal_question", title: "검토 요청", body: "승인 여부를 알려주세요." }],
    });
    renderWithProviders(<WorkfeedPane />, { connected: true, dataProvider });

    await userEvent.click(await screen.findByText("검토 요청"));
    const detail = screen.getByLabelText("피드 상세");
    const box = within(detail).getByPlaceholderText("답변 입력…");
    await userEvent.type(box, "승인합니다");
    await userEvent.click(within(detail).getByRole("button", { name: "답변" }));

    await waitFor(() => expect(rpcCalls.some((c) => c.method === "miniapp.workfeed.answer")).toBe(true));
    const answer = rpcCalls.find((c) => c.method === "miniapp.workfeed.answer");
    expect(answer?.params).toMatchObject({ itemId: "w1", answer: "승인합니다" });
    await waitFor(() => expect(chatCalls).toHaveLength(1));
    expect(chatCalls[0]).toMatchObject({ message: "승인합니다", sessionKey: "client:main" });
  });

  it("routes a session-less action prompt into the AI panel instead of dropping it", async () => {
    // Regression: the kb-interview 인터뷰 시작 chip (periodic-task card, no asking
    // session) settled the card but the returned prompt was silently discarded —
    // "인터뷰 시작을 눌러도 아무것도 안 일어남".
    const asked: string[] = [];
    const dataProvider = fakeProvider({
      workfeed: [
        {
          id: "kb1",
          source: "kb-interview-suggest",
          title: "지식 인터뷰 제안: 단가/마진 인텔리전스",
          body: "인터뷰로 정리할까요?",
          question: true,
          actions: [{ id: "kbinterview:start", kind: "answer", label: "인터뷰 시작" }],
        },
      ],
    });
    renderWithProviders(
      <>
        <AskSinkProbe onAsk={(text) => asked.push(text)} />
        <WorkfeedPane />
      </>,
      { connected: true, dataProvider },
    );

    await userEvent.click(await screen.findByText("지식 인터뷰 제안: 단가/마진 인텔리전스"));
    const detail = screen.getByLabelText("피드 상세");
    await userEvent.click(within(detail).getByRole("button", { name: "인터뷰 시작" }));

    await waitFor(() => expect(rpcCalls.some((c) => c.method === "miniapp.workfeed.action.run")).toBe(true));
    expect(rpcCalls.find((c) => c.method === "miniapp.workfeed.action.run")?.params).toMatchObject({
      itemId: "kb1",
      actionId: "kbinterview:start",
    });
    // The prompt lands in the AI panel's conversation, not a background stream.
    await waitFor(() => expect(asked).toEqual(["지식 인터뷰: '단가' 도메인을 인터뷰로 정리하자."]));
    expect(chatCalls).toHaveLength(0);
  });

  it("preserves generic action chips hidden on a non-question card (wide body)", async () => {
    // A non-question card's quick-action chips stay hidden — the desktop detail keeps
    // the body wide (unlike the phone's action sheet). Answer chips only appear on a
    // question card (see the boundary test's action.run coverage), matching native.
    const dataProvider = fakeProvider({
      workfeed: [{ id: "w2", source: "followup", title: "미답장 메일 3건", actions: [{ id: "reply", label: "답장" }] }],
    });
    renderWithProviders(<WorkfeedPane />, { connected: true, dataProvider });

    await userEvent.click(await screen.findByText("미답장 메일 3건"));
    const detail = screen.getByLabelText("피드 상세");

    // 일반(비질문) 카드의 빠른-액션 칩은 숨김 — 본문을 와이드하게. action.run 도 없다.
    expect(within(detail).queryByText("액션")).not.toBeInTheDocument();
    expect(within(detail).queryByRole("button", { name: "답장" })).not.toBeInTheDocument();
    expect(rpcCalls.some((c) => c.method === "miniapp.workfeed.action.run")).toBe(false);

    // 정정은 본문 하단에 그대로 남는다.
    expect(within(detail).getByPlaceholderText("정정·피드백 입력…")).toBeInTheDocument();
  });

  it("corrects and rewrites any workfeed card through the bridge RPCs", async () => {
    const dataProvider = fakeProvider({
      workfeed: [{ id: "w2", source: "followup", title: "미답장 메일 3건" }],
    });
    renderWithProviders(<WorkfeedPane />, { connected: true, dataProvider });

    expect(await screen.findByText("미답장 메일 3건")).toBeInTheDocument();
    expect(screen.queryByPlaceholderText("답변 입력…")).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "다시 작성" })).not.toBeInTheDocument();

    await userEvent.click(screen.getByText("미답장 메일 3건"));
    const detail = screen.getByLabelText("피드 상세");
    await userEvent.click(within(detail).getByRole("button", { name: "다시 작성" }));
    await waitFor(() => expect(rpcCalls.some((c) => c.method === "miniapp.workfeed.rewrite")).toBe(true));
    expect(rpcCalls.find((c) => c.method === "miniapp.workfeed.rewrite")?.params).toMatchObject({ itemId: "w2" });

    await userEvent.type(within(detail).getByPlaceholderText("정정·피드백 입력…"), "이 메일은 이미 처리됐습니다");
    await userEvent.click(within(detail).getByRole("button", { name: "정정" }));

    await waitFor(() => expect(rpcCalls.some((c) => c.method === "miniapp.workfeed.feedback")).toBe(true));
    expect(rpcCalls.find((c) => c.method === "miniapp.workfeed.feedback")?.params).toMatchObject({
      itemId: "w2",
      feedback: "이 메일은 이미 처리됐습니다",
    });
  });

  it("opens and closes a detail panel, processing the selected item from there", async () => {
    const dataProvider = fakeProvider({
      workfeed: [{ id: "w3", source: "alert", title: "일정 충돌 감지", body: "오전 회의가 겹칩니다." }],
    });
    renderWithProviders(<WorkfeedPane />, { connected: true, dataProvider });

    await userEvent.click(await screen.findByText("일정 충돌 감지"));
    const detail = screen.getByLabelText("피드 상세");
    expect(within(detail).getByText("오전 회의가 겹칩니다.")).toBeInTheDocument();

    await userEvent.click(within(detail).getByRole("button", { name: "처리" }));

    await waitFor(() => expect(rpcCalls.some((c) => c.method === "miniapp.workfeed.ack")).toBe(true));
    expect(rpcCalls.find((c) => c.method === "miniapp.workfeed.ack")?.params).toMatchObject({ id: "w3" });
    await waitFor(() => expect(screen.queryByLabelText("피드 상세")).not.toBeInTheDocument());
  });

  it("reuses the assistant renderer for tables and deneb-ui charts in the detail body", async () => {
    const body = [
      "| 항목 | 값 |",
      "| --- | ---: |",
      "| 입찰가 | 120 |",
      "",
      "```deneb-ui",
      '{"type":"chart","label":"입찰 비교","labels":["A","B"],"values":[120,95]}',
      "```",
    ].join("\n");
    const dataProvider = fakeProvider({
      workfeed: [{ id: "w4", source: "alert", title: "도표 확인", body }],
    });
    renderWithProviders(<WorkfeedPane />, { connected: true, dataProvider });

    await screen.findByText("도표 확인");

    await userEvent.click(screen.getByText("도표 확인"));
    const detail = screen.getByLabelText("피드 상세");

    expect(within(detail).getByRole("table")).toBeInTheDocument();
    expect(within(detail).getByText("입찰가")).toBeInTheDocument();
    expect(within(detail).getByText("입찰 비교")).toBeInTheDocument();
    expect(within(detail).getByText("A")).toBeInTheDocument();
    expect(within(detail).getAllByText("120")).toHaveLength(2);
  });

  it("when lands on today and flips to the previous / next day", async () => {
    // Anchor the fixtures to the real clock so the component's "today" matches.
    const t = new Date();
    const at = (daysAgo: number, hour: number) =>
      new Date(t.getFullYear(), t.getMonth(), t.getDate() - daysAgo, hour).getTime();
    const dataProvider = fakeProvider({
      workfeed: [
        { id: "y", source: "followup", title: "어제 항목", createdAtMs: at(1, 14) },
        { id: "t", source: "alert", title: "오늘 항목", createdAtMs: at(0, 9) },
      ],
    });
    renderWithProviders(<WorkfeedPane />, { connected: true, dataProvider });

    // Lands on today.
    expect(await screen.findByText("오늘 항목")).toBeInTheDocument();
    expect(screen.queryByText("어제 항목")).not.toBeInTheDocument();
    expect(screen.getByText("오늘")).toBeInTheDocument();

    // Flip to the previous day → 어제.
    await userEvent.click(screen.getByRole("button", { name: "이전 날" }));
    expect(await screen.findByText("어제 항목")).toBeInTheDocument();
    expect(screen.queryByText("오늘 항목")).not.toBeInTheDocument();
    expect(screen.getByText("어제")).toBeInTheDocument();

    // 'next' returns to today; nothing is newer than today, so it then disables.
    await userEvent.click(screen.getByRole("button", { name: "다음 날" }));
    expect(await screen.findByText("오늘 항목")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "다음 날" })).toBeDisabled();
  });

  it("fetches the selected day's server-side range and refetches when the day changes", async () => {
    // The old flat default fetch (limit 20, unordered) dropped a busy day's later cards
    // past position 20. Each day now fetches its own [sinceMs, beforeMs) window at the
    // gateway max (100); this asserts the params AND that changing the day refetches.
    const ranges: Array<Record<string, unknown>> = [];
    const t = new Date();
    const midnight = (daysAgo: number) => new Date(t.getFullYear(), t.getMonth(), t.getDate() - daysAgo).getTime();
    const at = (daysAgo: number, hour: number) =>
      new Date(t.getFullYear(), t.getMonth(), t.getDate() - daysAgo, hour).getTime();
    // Return both days regardless of range; the client buckets to the selected day.
    const dayFixtures = [
      { id: "y", source: "followup", title: "어제 항목", createdAtMs: at(1, 14) },
      { id: "t", source: "alert", title: "오늘 항목", createdAtMs: at(0, 9) },
    ];
    const recordingProvider: DataProvider = {
      getApiUrl: () => "http://test",
      getList: async ({ meta }) => {
        ranges.push((meta as { rpcParams?: Record<string, unknown> } | undefined)?.rpcParams ?? {});
        return { data: dayFixtures as never[], total: dayFixtures.length };
      },
      getOne: async ({ id }) => ({ data: { id } as never }),
      create: async ({ variables }) => ({ data: { id: "new", ...(variables as object) } as never }),
      update: async ({ id, variables }) => ({ data: { id, ...(variables as object) } as never }),
      deleteOne: async ({ id }) => ({ data: { id } as never }),
    };
    renderWithProviders(<WorkfeedPane />, { connected: true, dataProvider: recordingProvider });

    // Today's fetch ranges [today 00:00, tomorrow 00:00) at the gateway max page.
    await screen.findByText("오늘 항목");
    await waitFor(() => expect(ranges.some((r) => r.sinceMs === startOfDay())).toBe(true));
    expect(ranges.find((r) => r.sinceMs === startOfDay())).toMatchObject({
      limit: 100,
      sinceMs: midnight(0),
      beforeMs: midnight(-1),
    });

    // Stepping a day back issues a fresh fetch for yesterday's window.
    await userEvent.click(screen.getByRole("button", { name: "이전 날" }));
    await screen.findByText("어제 항목");
    await waitFor(() => expect(ranges.some((r) => r.sinceMs === midnight(1))).toBe(true));
    expect(ranges.find((r) => r.sinceMs === midnight(1))).toMatchObject({ beforeMs: midnight(0) });
  });

  it("marks an item read on open and de-emphasizes its row", async () => {
    const dataProvider = fakeProvider({
      workfeed: [{ id: "w1", source: "alert", title: "읽을 항목", body: "본문" }],
    });
    renderWithProviders(<WorkfeedPane />, { connected: true, dataProvider });

    // The row title node is reused across re-render, so its className reflects the update.
    const title = await screen.findByText("읽을 항목");
    expect(title.className).not.toContain("workfeed-row-read");

    await userEvent.click(title);

    // Opening fires the read RPC with the item id…
    await waitFor(() => expect(rpcCalls.some((c) => c.method === "miniapp.workfeed.read")).toBe(true));
    expect(rpcCalls.find((c) => c.method === "miniapp.workfeed.read")?.params).toMatchObject({ itemId: "w1" });
    // …and the row is now de-emphasized (read).
    await waitFor(() => expect(title.className).toContain("workfeed-row-read"));
  });

  it("labels open question rows as 질문 대기 and filters them across days", async () => {
    const t = new Date();
    const at = (daysAgo: number, hour: number) =>
      new Date(t.getFullYear(), t.getMonth(), t.getDate() - daysAgo, hour).getTime();
    const dataProvider = fakeProvider({
      workfeed: [
        { id: "q-today", source: "proactive", title: "오늘 선제 질문", question: true, createdAtMs: at(0, 10) },
        {
          id: "q-acked",
          source: "deal_question",
          title: "이미 답한 질문",
          question: true,
          ackedAtMs: 1,
          createdAtMs: at(0, 9),
        },
        { id: "plain", source: "alert", title: "일반 알림", createdAtMs: at(0, 8) },
        { id: "q-yesterday", source: "deal_question", title: "어제 미답 질문", question: true, createdAtMs: at(1, 14) },
      ],
    });
    renderWithProviders(<WorkfeedPane />, { connected: true, dataProvider });

    // List badge: only unsettled question cards — not source="deal_question" alone, not acked.
    expect(await screen.findByText("오늘 선제 질문")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "질문 대기 1" })).toBeInTheDocument();
    // Row kind badge (exact "질문 대기") vs filter toggle ("질문 대기 1").
    expect(screen.getByText("질문 대기")).toBeInTheDocument();
    expect(screen.getByText("알림")).toBeInTheDocument(); // plain row keeps source label
    expect(screen.getByText("질문")).toBeInTheDocument(); // acked deal_question keeps source label
    expect(screen.queryByText("어제 미답 질문")).not.toBeInTheDocument();

    await userEvent.click(screen.getByRole("button", { name: "질문 대기 1" }));
    expect(await screen.findByText("오늘 선제 질문")).toBeInTheDocument();
    expect(screen.getByText("어제 미답 질문")).toBeInTheDocument();
    expect(screen.queryByText("일반 알림")).not.toBeInTheDocument();
    expect(screen.queryByText("이미 답한 질문")).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "이전 날" })).not.toBeInTheDocument();
    // Cross-day inbox: both open questions → count on the toggle updates to 2.
    expect(screen.getByRole("button", { name: "질문 대기 2" })).toHaveAttribute("aria-pressed", "true");
  });

  it("opens 질문 대기 inbox from a pane target query", async () => {
    function TargetSetter() {
      const { openPane } = useWorkspace();
      return <button onClick={() => openPane("workfeed", { query: "questions" })}>open questions</button>;
    }
    const t = new Date();
    const at = (daysAgo: number, hour: number) =>
      new Date(t.getFullYear(), t.getMonth(), t.getDate() - daysAgo, hour).getTime();
    const dataProvider = fakeProvider({
      workfeed: [
        { id: "q1", source: "proactive", title: "대기 질문", question: true, createdAtMs: at(1, 11) },
        { id: "a1", source: "alert", title: "오늘 알림", createdAtMs: at(0, 9) },
      ],
    });
    renderWithProviders(
      <>
        <TargetSetter />
        <WorkfeedPane />
      </>,
      { connected: true, dataProvider },
    );

    expect(await screen.findByText("오늘 알림")).toBeInTheDocument();
    expect(screen.queryByText("대기 질문")).not.toBeInTheDocument();

    await userEvent.click(screen.getByRole("button", { name: "open questions" }));
    expect(await screen.findByText("대기 질문")).toBeInTheDocument();
    expect(screen.queryByText("오늘 알림")).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: /질문 대기/ })).toHaveAttribute("aria-pressed", "true");
  });
});
