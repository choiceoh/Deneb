import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { fakeProvider, renderWithProviders } from "@/test/util";
import { WorkfeedPane } from "./WorkfeedPane";

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
            ? { ok: true, sessionKey: "client:main", prompt: "후속 액션 실행", removeFromFeed: true }
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
    const detail = screen.getByLabelText("작업피드 상세");
    const box = within(detail).getByPlaceholderText("답변 입력…");
    await userEvent.type(box, "승인합니다");
    await userEvent.click(within(detail).getByRole("button", { name: "답변" }));

    await waitFor(() => expect(rpcCalls.some((c) => c.method === "miniapp.workfeed.answer")).toBe(true));
    const answer = rpcCalls.find((c) => c.method === "miniapp.workfeed.answer");
    expect(answer?.params).toMatchObject({ itemId: "w1", answer: "승인합니다" });
    await waitFor(() => expect(chatCalls).toHaveLength(1));
    expect(chatCalls[0]).toMatchObject({ message: "승인합니다", sessionKey: "client:main" });
  });

  it("runs fixed action chips via action.run and delivers the returned prompt", async () => {
    const dataProvider = fakeProvider({
      workfeed: [{ id: "w2", source: "followup", title: "미답장 메일 3건", actions: [{ id: "reply", label: "답장" }] }],
    });
    renderWithProviders(<WorkfeedPane />, { connected: true, dataProvider });

    expect(await screen.findByText("미답장 메일 3건")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "답장" })).not.toBeInTheDocument();
    await userEvent.click(screen.getByText("미답장 메일 3건"));
    const detail = screen.getByLabelText("작업피드 상세");
    await userEvent.click(within(detail).getByRole("button", { name: "답장" }));

    await waitFor(() => expect(rpcCalls.some((c) => c.method === "miniapp.workfeed.action.run")).toBe(true));
    const action = rpcCalls.find((c) => c.method === "miniapp.workfeed.action.run");
    expect(action?.params).toMatchObject({ itemId: "w2", actionId: "reply" });
    await waitFor(() => expect(chatCalls[0]).toMatchObject({ message: "후속 액션 실행", sessionKey: "client:main" }));
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
    const detail = screen.getByLabelText("작업피드 상세");
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
    const detail = screen.getByLabelText("작업피드 상세");
    expect(within(detail).getByText("오전 회의가 겹칩니다.")).toBeInTheDocument();

    await userEvent.click(within(detail).getByRole("button", { name: "처리" }));

    await waitFor(() => expect(rpcCalls.some((c) => c.method === "miniapp.workfeed.ack")).toBe(true));
    expect(rpcCalls.find((c) => c.method === "miniapp.workfeed.ack")?.params).toMatchObject({ id: "w3" });
    await waitFor(() => expect(screen.queryByLabelText("작업피드 상세")).not.toBeInTheDocument());
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
    expect(screen.getByText("표/도표 포함")).toBeInTheDocument();

    await userEvent.click(screen.getByText("도표 확인"));
    const detail = screen.getByLabelText("작업피드 상세");

    expect(within(detail).getByRole("table")).toBeInTheDocument();
    expect(within(detail).getByText("입찰가")).toBeInTheDocument();
    expect(within(detail).getByText("입찰 비교")).toBeInTheDocument();
    expect(within(detail).getByText("A")).toBeInTheDocument();
    expect(within(detail).getAllByText("120")).toHaveLength(2);
  });
});
