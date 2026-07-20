import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { DataProvider } from "@/crud";
import type { WorkItem } from "@/types";
import { fakeProvider, renderWithProviders } from "@/test/util";
import { useAiFeed, useWorkspace } from "@/workspaceContext";
import { WorkfeedPane } from "./WorkfeedPane";

type RpcCall = { method: string; params: Record<string, unknown> };
type ChatCall = { message: string; sessionKey: string };

const NOW = new Date("2026-07-11T15:30:00+09:00");
const at = (dayDelta: number, hour: number, minute = 0) =>
  new Date(2026, 6, 11 + dayDelta, hour, minute, 0, 0).getTime();

const todayItems: WorkItem[] = [
  {
    id: "question",
    source: "deal_question",
    title: "계약 승인 여부",
    body: "위험을 검토하고 승인 여부를 알려주세요.",
    refId: "deal-42",
    createdAtMs: at(0, 14, 20),
  },
  {
    id: "alert",
    source: "alert",
    title: "일정 충돌",
    body: "오전 일정 두 건이 겹칩니다.",
    createdAtMs: at(0, 10),
  },
  {
    id: "followup",
    source: "follow_up-needed",
    title: "미답장 메일",
    createdAtMs: at(0, 9),
  },
  {
    id: "missing-time",
    title: "시각 없는 항목",
    body: "생성 시각이 누락됐습니다.",
  },
];

const approvalItem: WorkItem = {
  id: "approval-7",
  source: "groupware-approval",
  title: "모듈 구매 품의",
  body: "공급가액과 납기를 확인하세요.",
  refId: "DOC-99178",
  question: true,
  actions: [
    { id: "approval:approve", kind: "ack", label: "승인" },
    { id: "approval:reject", kind: "ack", label: "반려" },
  ],
  createdAtMs: at(0, 13),
};

function rpcReply(payload: unknown, ok = true): Response {
  return new Response(JSON.stringify(ok ? { ok: true, payload } : { ok: false, error: String(payload) }), {
    status: ok ? 200 : 500,
    headers: { "Content-Type": "application/json" },
  });
}

function streamReply(body = 'event: done\ndata: {"text":"ok"}\n\n'): Response {
  return new Response(body, { status: 200, headers: { "Content-Type": "text/event-stream" } });
}

function installGateway(
  rpc: RpcCall[],
  chats: ChatCall[],
  handlers: Partial<Record<string, (params: Record<string, unknown>) => Response | Promise<Response>>> = {},
  stream?: (call: ChatCall) => Response | Promise<Response>,
) {
  vi.stubGlobal(
    "fetch",
    vi.fn(async (input: string | URL | Request, init?: RequestInit) => {
      const request = JSON.parse(String(init?.body ?? "{}")) as {
        method?: string;
        params?: Record<string, unknown>;
        message?: string;
        sessionKey?: string;
      };
      if (String(input).includes("/chat/stream")) {
        const call = { message: String(request.message ?? ""), sessionKey: String(request.sessionKey ?? "") };
        chats.push(call);
        return stream ? stream(call) : streamReply();
      }
      const method = String(request.method ?? "");
      const params = request.params ?? {};
      rpc.push({ method, params });
      if (handlers[method]) return handlers[method]!(params);
      if (method === "miniapp.workfeed.answer") {
        return rpcReply({ sessionKey: "client:main", prompt: String(params.answer), removeFromFeed: true });
      }
      return rpcReply({ ok: true });
    }),
  );
}

function renderFeed(items: WorkItem[] = todayItems, connected = true, provider?: DataProvider) {
  return renderWithProviders(<WorkfeedPane />, {
    connected,
    cfg: connected ? { url: "http://test", token: "tok" } : { url: "", token: "" },
    dataProvider: provider ?? fakeProvider({ workfeed: items }),
  });
}

function lastRpc(calls: RpcCall[], method: string) {
  return calls.filter((call) => call.method === method).at(-1);
}

describe("WorkfeedPane boundary behavior", () => {
  let rpc: RpcCall[];
  let chats: ChatCall[];

  beforeEach(() => {
    localStorage.clear();
    rpc = [];
    chats = [];
    installGateway(rpc, chats);
    vi.useFakeTimers({ toFake: ["Date"] });
    vi.setSystemTime(NOW);
    if (!globalThis.crypto?.randomUUID) vi.stubGlobal("crypto", { randomUUID: () => "workfeed-test" });
  });

  afterEach(() => {
    vi.useRealTimers();
    localStorage.clear();
    vi.unstubAllGlobals();
  });

  describe("daily range and ordering", () => {
    it("without fetch or show day navigation while disconnected", () => {
      renderFeed(todayItems, false);
      expect(screen.getByText("미연결")).toBeInTheDocument();
      expect(screen.queryByRole("button", { name: "이전 날" })).not.toBeInTheDocument();
      expect(screen.queryByRole("button", { name: "다음 날" })).not.toBeInTheDocument();
    });

    it("when requests the complete selected local-day half-open range", async () => {
      const ranges: Record<string, unknown>[] = [];
      const base = fakeProvider({ workfeed: todayItems });
      const provider: DataProvider = {
        ...base,
        getList: async (args) => {
          ranges.push(
            ((args.meta as { rpcParams?: Record<string, unknown> })?.rpcParams ?? {}) as Record<string, unknown>,
          );
          return base.getList(args);
        },
      };
      renderFeed(todayItems, true, provider);
      await screen.findByText("계약 승인 여부");
      expect(ranges[0]).toEqual({ limit: 100, sinceMs: at(0, 0), beforeMs: at(1, 0) });
    });

    it("when sorts a day's entries newest first regardless of provider order", async () => {
      renderFeed([...todayItems].reverse());
      const rows = await screen.findAllByTitle(/상세$/);
      expect(rows.map((row) => row.textContent)).toEqual([
        expect.stringContaining("시각 없는 항목"),
        expect.stringContaining("계약 승인 여부"),
        expect.stringContaining("일정 충돌"),
        expect.stringContaining("미답장 메일"),
      ]);
    });

    it("places a missing createdAt timestamp on today instead of making it unreachable", async () => {
      renderFeed([{ id: "missing", title: "도달 가능한 항목" }]);
      expect(await screen.findByText("도달 가능한 항목")).toBeInTheDocument();
      expect(screen.getByText("1건")).toBeInTheDocument();
    });

    it("shows only the selected day's entries even if a provider returns multiple days", async () => {
      renderFeed([
        { id: "today", title: "오늘 카드", createdAtMs: at(0, 9) },
        { id: "yesterday", title: "어제 카드", createdAtMs: at(-1, 9) },
      ]);
      expect(await screen.findByText("오늘 카드")).toBeInTheDocument();
      expect(screen.queryByText("어제 카드")).not.toBeInTheDocument();
      await userEvent.click(screen.getByRole("button", { name: "이전 날" }));
      expect(await screen.findByText("어제 카드")).toBeInTheDocument();
      expect(screen.queryByText("오늘 카드")).not.toBeInTheDocument();
    });

    it("keeps the pager visible on an empty historical day", async () => {
      renderFeed([]);
      expect(await screen.findByText("이 날짜에는 항목이 없습니다.")).toBeInTheDocument();
      await userEvent.click(screen.getByRole("button", { name: "이전 날" }));
      expect(screen.getByText("이 날짜에는 항목이 없습니다.")).toBeInTheDocument();
      expect(screen.getByRole("button", { name: "이전 날" })).toBeInTheDocument();
      expect(screen.getByRole("button", { name: "다음 날" })).toBeEnabled();
      expect(screen.getByRole("button", { name: "오늘로" })).toBeInTheDocument();
    });

    it("returns directly to today from a historical day", async () => {
      renderFeed([
        { id: "today", title: "오늘 카드", createdAtMs: at(0, 9) },
        { id: "old", title: "과거 카드", createdAtMs: at(-4, 9) },
      ]);
      await screen.findByText("오늘 카드");
      for (let i = 0; i < 4; i++) await userEvent.click(screen.getByRole("button", { name: "이전 날" }));
      expect(await screen.findByText("과거 카드")).toBeInTheDocument();
      await userEvent.click(screen.getByRole("button", { name: "오늘로" }));
      expect(await screen.findByText("오늘 카드")).toBeInTheDocument();
      expect(screen.queryByRole("button", { name: "오늘로" })).not.toBeInTheDocument();
    });

    it("stops forward navigation at today", async () => {
      renderFeed([]);
      await screen.findByText("이 날짜에는 항목이 없습니다.");
      expect(screen.getByRole("button", { name: "다음 날" })).toBeDisabled();
    });

    it("stops backward navigation after the native 31-day lookback", async () => {
      renderFeed([]);
      await screen.findByText("이 날짜에는 항목이 없습니다.");
      for (let i = 0; i < 31; i++) await userEvent.click(screen.getByRole("button", { name: "이전 날" }));
      expect(screen.getByRole("button", { name: "이전 날" })).toBeDisabled();
      expect(screen.getByRole("button", { name: "다음 날" })).toBeEnabled();
    });

    it("extends lookback when the provider contains an older item", async () => {
      renderFeed([{ id: "older", title: "아주 오래된 카드", createdAtMs: at(-40, 9) }]);
      await screen.findByText("이 날짜에는 항목이 없습니다.");
      for (let i = 0; i < 31; i++) await userEvent.click(screen.getByRole("button", { name: "이전 날" }));
      expect(screen.getByRole("button", { name: "이전 날" })).toBeEnabled();
    });

    it("clears an expanded detail when changing days", async () => {
      renderFeed(todayItems);
      await userEvent.click(await screen.findByText("계약 승인 여부"));
      expect(screen.getByRole("region", { name: "피드 상세" })).toBeInTheDocument();
      await userEvent.click(screen.getByRole("button", { name: "이전 날" }));
      expect(screen.queryByRole("region", { name: "피드 상세" })).not.toBeInTheDocument();
    });
  });

  describe("row semantics and read state", () => {
    it.each([
      ["deal_question", "질문"],
      ["alert", "알림"],
      ["followup", "후속"],
      ["proactive", "제안"],
      ["custom_signal", "custom signal"],
      ["custom-signal", "custom signal"],
      ["", "피드"],
    ])("when labels source %s as %s", async (source, label) => {
      renderFeed([{ id: source || "none", source, title: `item-${source}`, createdAtMs: at(0, 9) }]);
      expect(await screen.findByText(label)).toBeInTheDocument();
    });

    it("uses stable fallbacks for a titleless item", async () => {
      renderFeed([{ id: "untitled", source: "alert", createdAtMs: at(0, 9) }]);
      expect(await screen.findByText("(항목)")).toBeInTheDocument();
      expect(screen.getByTitle("(항목) 상세")).toBeInTheDocument();
    });

    it("marks an unread item optimistically before a slow RPC completes", async () => {
      let resolve!: (response: Response) => void;
      installGateway(rpc, chats, {
        "miniapp.workfeed.read": () => new Promise<Response>((done) => (resolve = done)),
      });
      renderFeed([{ id: "slow", title: "느린 읽음", createdAtMs: at(0, 9) }]);
      const title = await screen.findByText("느린 읽음");
      await userEvent.click(title);
      expect(title).toHaveClass("workfeed-row-read");
      resolve(rpcReply({ ok: true }));
    });

    it("does not send read RPC for an item already persisted as read", async () => {
      renderFeed([{ id: "read", title: "이미 읽음", readAtMs: at(0, 8), createdAtMs: at(0, 9) }]);
      const title = await screen.findByText("이미 읽음");
      expect(title).toHaveClass("workfeed-row-read");
      await userEvent.click(title);
      expect(rpc.filter((call) => call.method === "miniapp.workfeed.read")).toHaveLength(0);
    });

    it("does not duplicate read RPC when reopening an optimistically read item", async () => {
      renderFeed([{ id: "once", title: "한 번만 읽음", createdAtMs: at(0, 9) }]);
      const title = await screen.findByText("한 번만 읽음");
      await userEvent.click(title);
      await waitFor(() => expect(rpc.filter((call) => call.method === "miniapp.workfeed.read")).toHaveLength(1));
      await userEvent.click(title);
      await userEvent.click(title);
      expect(rpc.filter((call) => call.method === "miniapp.workfeed.read")).toHaveLength(1);
    });

    it("keeps optimistic read styling when the durable read call fails", async () => {
      installGateway(rpc, chats, {
        "miniapp.workfeed.read": () => Promise.reject(new Error("read offline")),
      });
      renderFeed([{ id: "offline", title: "오프라인 읽음", createdAtMs: at(0, 9) }]);
      const title = await screen.findByText("오프라인 읽음");
      await userEvent.click(title);
      expect(title).toHaveClass("workfeed-row-read");
      expect(screen.queryByText(/read offline/)).not.toBeInTheDocument();
    });
  });

  describe("detail state and actions", () => {
    it("opens with body expanded and toggles analysis visibility", async () => {
      renderFeed(todayItems);
      await userEvent.click(await screen.findByText("일정 충돌"));
      const detail = screen.getByRole("region", { name: "피드 상세" });
      expect(within(detail).getByText("오전 일정 두 건이 겹칩니다.")).toBeInTheDocument();
      const toggle = within(detail).getByRole("button", { name: "분석 접기" });
      expect(toggle).toHaveAttribute("aria-expanded", "true");
      await userEvent.click(toggle);
      expect(within(detail).queryByText("오전 일정 두 건이 겹칩니다.")).not.toBeInTheDocument();
      expect(within(detail).getByRole("button", { name: "분석 펼치기" })).toHaveAttribute("aria-expanded", "false");
    });

    it("renders explicit no-body state without a meaningless analysis toggle", async () => {
      renderFeed([{ id: "empty", title: "본문 없는 카드", source: "alert", createdAtMs: at(0, 9) }]);
      await userEvent.click(await screen.findByText("본문 없는 카드"));
      const detail = screen.getByRole("region", { name: "피드 상세" });
      expect(within(detail).getByText("본문 없음")).toBeInTheDocument();
      expect(within(detail).queryByRole("button", { name: /분석/ })).not.toBeInTheDocument();
    });

    it("when includes source, date and reference id in detail metadata", async () => {
      renderFeed(todayItems);
      await userEvent.click(await screen.findByText("계약 승인 여부"));
      expect(screen.getByRole("region", { name: "피드 상세" })).toHaveTextContent(/질문.*ref deal-42/);
    });

    it("shows answer controls only when source contains question", async () => {
      renderFeed([
        { id: "custom", title: "사용자 질문", source: "customer-question-pending", createdAtMs: at(0, 9) },
        { id: "normal", title: "일반 카드", source: "alert", createdAtMs: at(0, 8) },
      ]);
      await userEvent.click(await screen.findByText("사용자 질문"));
      expect(screen.getByPlaceholderText("답변 입력…")).toBeInTheDocument();
      await userEvent.click(screen.getByText("일반 카드"));
      expect(screen.queryByPlaceholderText("답변 입력…")).not.toBeInTheDocument();
    });

    it("shows the free-text reply when the question flag is set even without a question source", async () => {
      renderFeed([{ id: "flagged", source: "proactive", title: "선제 질문", question: true, createdAtMs: at(0, 9) }]);
      await userEvent.click(await screen.findByText("선제 질문"));
      expect(screen.getByPlaceholderText("답변 입력…")).toBeInTheDocument();
    });

    it("renders answer chips from actions and settles the tapped one via action.run, streaming the returned prompt", async () => {
      installGateway(rpc, chats, {
        "miniapp.workfeed.action.run": (params) =>
          rpcReply({ sessionKey: "client:main:wf-verdict", prompt: `채택: ${params.actionId}`, removeFromFeed: true }),
      });
      renderFeed([
        {
          id: "verdict",
          source: "genesis-meta",
          title: "저신뢰 채택 판정",
          body: "이 스킬 진화를 채택할까요?",
          question: true,
          actions: [
            { id: "meta:adopt", label: "채택" },
            { id: "meta:reject", label: "기각" },
          ],
          createdAtMs: at(0, 9),
        },
      ]);
      await userEvent.click(await screen.findByText("저신뢰 채택 판정"));
      const detail = screen.getByRole("region", { name: "피드 상세" });
      // Chips take priority over the free-text field (native WorkFeedAnswerBlock parity).
      expect(within(detail).queryByPlaceholderText("답변 입력…")).not.toBeInTheDocument();
      expect(within(detail).getByRole("button", { name: "기각" })).toBeInTheDocument();
      await userEvent.click(within(detail).getByRole("button", { name: "채택" }));
      await waitFor(() =>
        expect(lastRpc(rpc, "miniapp.workfeed.action.run")?.params).toEqual({
          itemId: "verdict",
          actionId: "meta:adopt",
        }),
      );
      await waitFor(() =>
        expect(chats).toEqual([{ message: "채택: meta:adopt", sessionKey: "client:main:wf-verdict" }]),
      );
    });

    it("settles an ack-style decision chip (meta:adopt) without spawning a chat turn", async () => {
      // Mirrors the gateway's genesis-meta 채택/기각 card: ActionAck chips settle the
      // card server-side (handleMetaProposalAction) and return a BLANK prompt, so the
      // desktop must run the action but NOT deliver a chat turn (native parity — the
      // adopt/reject decision is applied by the RPC hook, not by a follow-up turn).
      installGateway(rpc, chats, {
        "miniapp.workfeed.action.run": () => rpcReply({ item: { id: "proposal" }, removeFromFeed: true }),
      });
      renderFeed([
        {
          id: "proposal",
          source: "genesis-meta",
          title: "메타 개정 제안",
          body: "채택하거나 기각하세요.",
          question: true,
          actions: [
            { id: "meta:adopt", kind: "ack", label: "채택" },
            { id: "meta:reject", kind: "ack", label: "기각" },
          ],
          createdAtMs: at(0, 9),
        },
      ]);
      await userEvent.click(await screen.findByText("메타 개정 제안"));
      const detail = screen.getByRole("region", { name: "피드 상세" });
      await userEvent.click(within(detail).getByRole("button", { name: "채택" }));
      await waitFor(() =>
        expect(lastRpc(rpc, "miniapp.workfeed.action.run")?.params).toEqual({
          itemId: "proposal",
          actionId: "meta:adopt",
        }),
      );
      // Flush the run's onResult microtasks, then assert the blank prompt produced no chat.
      await new Promise((resolve) => setTimeout(resolve, 0));
      expect(chats).toHaveLength(0);
    });

    // 501-keystroke type()이 느린 머신에서 5s 기본 타임아웃을 넘고, 죽은 뒤에도
    // 좀비 타이핑이 다음 테스트의 textarea를 오염시킨다 — 여유 타임아웃 필수.
    it(
      "opens an accessible reject dialog with document context and a 500-character limit",
      { timeout: 30_000 },
      async () => {
        renderFeed([approvalItem]);
        await userEvent.click(await screen.findByText("모듈 구매 품의"));
        await userEvent.click(screen.getByRole("button", { name: "반려" }));

        const dialog = screen.getByRole("dialog", { name: "결재 반려" });
        expect(dialog).toHaveTextContent("모듈 구매 품의");
        expect(dialog).toHaveTextContent("문서 참조: DOC-99178");
        const comment = within(dialog).getByRole("textbox", { name: "반려 사유 (선택)" });
        expect(comment).toHaveAttribute("maxlength", "500");
        await userEvent.type(comment, "가".repeat(501));
        expect(comment).toHaveValue("가".repeat(500));
        expect(within(dialog).getByText("500/500")).toBeInTheDocument();

        await userEvent.click(within(dialog).getByRole("button", { name: "취소" }));
        expect(screen.queryByRole("dialog", { name: "결재 반려" })).not.toBeInTheDocument();
        expect(rpc.filter((call) => call.method === "miniapp.workfeed.action.run")).toHaveLength(0);
      },
    );

    it("submits a nonblank reject comment and omits comments from approve", async () => {
      renderFeed([approvalItem]);
      await userEvent.click(await screen.findByText("모듈 구매 품의"));

      await userEvent.click(screen.getByRole("button", { name: "반려" }));
      const dialog = screen.getByRole("dialog", { name: "결재 반려" });
      await userEvent.type(within(dialog).getByRole("textbox", { name: "반려 사유 (선택)" }), "  예산 재검토  ");
      await userEvent.click(within(dialog).getByRole("button", { name: "반려" }));
      await waitFor(() =>
        expect(lastRpc(rpc, "miniapp.workfeed.action.run")?.params).toEqual({
          itemId: "approval-7",
          actionId: "approval:reject",
          comment: "예산 재검토",
        }),
      );
      await waitFor(() => expect(screen.queryByRole("dialog", { name: "결재 반려" })).not.toBeInTheDocument());

      await userEvent.click(screen.getByRole("button", { name: "승인" }));
      // App-styled confirm modal (window.confirm 대체) 경유.
      const approveDialog = await screen.findByRole("dialog", { name: "결재 승인" });
      await userEvent.click(within(approveDialog).getByRole("button", { name: "승인" }));
      await waitFor(() =>
        expect(lastRpc(rpc, "miniapp.workfeed.action.run")?.params).toEqual({
          itemId: "approval-7",
          actionId: "approval:approve",
        }),
      );

      await userEvent.click(screen.getByRole("button", { name: "반려" }));
      const blankDialog = screen.getByRole("dialog", { name: "결재 반려" });
      expect(within(blankDialog).getByRole("textbox", { name: "반려 사유 (선택)" })).toHaveValue("");
      await userEvent.click(within(blankDialog).getByRole("button", { name: "반려" }));
      await waitFor(() =>
        expect(lastRpc(rpc, "miniapp.workfeed.action.run")?.params).toEqual({
          itemId: "approval-7",
          actionId: "approval:reject",
        }),
      );
    });

    it("keeps the reject dialog and card open when submission fails", async () => {
      installGateway(rpc, chats, {
        "miniapp.workfeed.action.run": () => rpcReply("reject denied", false),
      });
      renderFeed([approvalItem]);
      await userEvent.click(await screen.findByText("모듈 구매 품의"));
      await userEvent.click(screen.getByRole("button", { name: "반려" }));
      const dialog = screen.getByRole("dialog", { name: "결재 반려" });
      await userEvent.type(within(dialog).getByRole("textbox", { name: "반려 사유 (선택)" }), "사유");
      await userEvent.click(within(dialog).getByRole("button", { name: "반려" }));

      expect(await within(dialog).findByRole("alert")).toHaveTextContent("HTTP 500");
      expect(screen.getByRole("dialog", { name: "결재 반려" })).toBeInTheDocument();
      expect(screen.getByRole("region", { name: "피드 상세" })).toBeInTheDocument();
      expect(screen.getAllByText("모듈 구매 품의").length).toBeGreaterThan(0);
    });

    it("trims answers, clears the field and delivers the returned prompt", async () => {
      renderFeed(todayItems);
      await userEvent.click(await screen.findByText("계약 승인 여부"));
      const detail = screen.getByRole("region", { name: "피드 상세" });
      fireEvent.change(within(detail).getByPlaceholderText("답변 입력…"), { target: { value: "  조건부 승인  " } });
      await userEvent.click(within(detail).getByRole("button", { name: "답변" }));
      await waitFor(() =>
        expect(lastRpc(rpc, "miniapp.workfeed.answer")?.params).toEqual({
          itemId: "question",
          answer: "조건부 승인",
        }),
      );
      expect(within(detail).getByPlaceholderText("답변 입력…")).toHaveValue("");
      await waitFor(() => expect(chats).toEqual([{ message: "조건부 승인", sessionKey: "client:main" }]));
    });

    it("when disables answer until trimmed input is nonblank", async () => {
      renderFeed(todayItems);
      await userEvent.click(await screen.findByText("계약 승인 여부"));
      const detail = screen.getByRole("region", { name: "피드 상세" });
      const input = within(detail).getByPlaceholderText("답변 입력…");
      const button = within(detail).getByRole("button", { name: "답변" });
      expect(button).toBeDisabled();
      fireEvent.change(input, { target: { value: "   " } });
      expect(button).toBeDisabled();
      fireEvent.change(input, { target: { value: "답변" } });
      expect(button).toBeEnabled();
    });

    it("trims feedback and clears it after dispatch", async () => {
      renderFeed(todayItems);
      await userEvent.click(await screen.findByText("일정 충돌"));
      const detail = screen.getByRole("region", { name: "피드 상세" });
      const input = within(detail).getByPlaceholderText("정정·피드백 입력…");
      fireEvent.change(input, { target: { value: "  이미 해결됨  " } });
      await userEvent.click(within(detail).getByRole("button", { name: "정정" }));
      await waitFor(() =>
        expect(lastRpc(rpc, "miniapp.workfeed.feedback")?.params).toEqual({ itemId: "alert", feedback: "이미 해결됨" }),
      );
      expect(input).toHaveValue("");
    });

    it("rewrites the selected item by itemId", async () => {
      renderFeed(todayItems);
      await userEvent.click(await screen.findByText("일정 충돌"));
      await userEvent.click(screen.getByRole("button", { name: "다시 작성" }));
      await waitFor(() => expect(lastRpc(rpc, "miniapp.workfeed.rewrite")?.params).toEqual({ itemId: "alert" }));
    });

    it("closes detail without acknowledging or mutating it", async () => {
      renderFeed(todayItems);
      await userEvent.click(await screen.findByText("일정 충돌"));
      const detail = screen.getByRole("region", { name: "피드 상세" });
      await userEvent.click(within(detail).getByRole("button", { name: "닫기" }));
      expect(screen.queryByRole("region", { name: "피드 상세" })).not.toBeInTheDocument();
      expect(rpc.filter((call) => call.method === "miniapp.workfeed.ack")).toHaveLength(0);
    });

    it("acknowledges by id and closes only on success", async () => {
      renderFeed(todayItems);
      await userEvent.click(await screen.findByText("일정 충돌"));
      await userEvent.click(screen.getByRole("button", { name: "처리" }));
      await waitFor(() => expect(lastRpc(rpc, "miniapp.workfeed.ack")?.params).toEqual({ id: "alert" }));
      await waitFor(() => expect(screen.queryByRole("region", { name: "피드 상세" })).not.toBeInTheDocument());
    });

    it("keeps detail open and reports an acknowledge failure", async () => {
      installGateway(rpc, chats, { "miniapp.workfeed.ack": () => rpcReply("ack denied", false) });
      renderFeed(todayItems);
      await userEvent.click(await screen.findByText("일정 충돌"));
      await userEvent.click(screen.getByRole("button", { name: "처리" }));
      expect(await screen.findByText(/HTTP 500/)).toBeInTheDocument();
      expect(screen.getByRole("region", { name: "피드 상세" })).toBeInTheDocument();
    });

    it("does not start chat delivery when an action has no complete turn", async () => {
      installGateway(rpc, chats, {
        "miniapp.workfeed.rewrite": () => rpcReply({ sessionKey: "client:main", prompt: "  " }),
      });
      renderFeed(todayItems);
      await userEvent.click(await screen.findByText("일정 충돌"));
      await userEvent.click(screen.getByRole("button", { name: "다시 작성" }));
      await waitFor(() => expect(rpc.some((call) => call.method === "miniapp.workfeed.rewrite")).toBe(true));
      expect(chats).toHaveLength(0);
    });

    it("surfaces downstream chat stream errors from an otherwise successful action", async () => {
      installGateway(
        rpc,
        chats,
        { "miniapp.workfeed.rewrite": () => rpcReply({ sessionKey: "client:main", prompt: "후속 실행" }) },
        () => streamReply('event: error\ndata: {"error":"model offline"}\n\n'),
      );
      renderFeed(todayItems);
      await userEvent.click(await screen.findByText("일정 충돌"));
      await userEvent.click(screen.getByRole("button", { name: "다시 작성" }));
      expect(await screen.findByText(/model offline/)).toBeInTheDocument();
      expect(chats).toEqual([{ message: "후속 실행", sessionKey: "client:main" }]);
    });
  });

  describe("pane target and AI projection", () => {
    it("when opens a requested workfeed id once data arrives", async () => {
      function TargetSetter() {
        const { openPane } = useWorkspace();
        return <button onClick={() => openPane("workfeed", { id: "alert" })}>target</button>;
      }
      renderWithProviders(
        <>
          <TargetSetter />
          <WorkfeedPane />
        </>,
        { connected: true, dataProvider: fakeProvider({ workfeed: todayItems }) },
      );
      await userEvent.click(await screen.findByRole("button", { name: "target" }));
      expect(await screen.findByRole("region", { name: "피드 상세" })).toHaveTextContent("일정 충돌");
    });

    it("keeps an id-less target from clearing the current selection", async () => {
      function TargetSetter() {
        const { openPane } = useWorkspace();
        return <button onClick={() => openPane("workfeed")}>empty target</button>;
      }
      renderWithProviders(
        <>
          <TargetSetter />
          <WorkfeedPane />
        </>,
        { connected: true, dataProvider: fakeProvider({ workfeed: todayItems }) },
      );
      await userEvent.click(await screen.findByText("일정 충돌"));
      await userEvent.click(screen.getByRole("button", { name: "empty target" }));
      expect(screen.getByRole("region", { name: "피드 상세" })).toHaveTextContent("일정 충돌");
    });

    it("publishes only the selected day with titles, sources and bodies", async () => {
      function Projection() {
        const { aiText } = useAiFeed();
        return <output data-testid="projection">{aiText}</output>;
      }
      renderWithProviders(
        <>
          <WorkfeedPane />
          <Projection />
        </>,
        {
          connected: true,
          dataProvider: fakeProvider({
            workfeed: [...todayItems, { id: "y", title: "어제 비밀", body: "보이면 안 됨", createdAtMs: at(-1, 9) }],
          }),
        },
      );
      await screen.findByText("계약 승인 여부");
      const projection = screen.getByTestId("projection");
      expect(projection).toHaveTextContent("[피드 · 오늘]");
      expect(projection).toHaveTextContent("계약 승인 여부 [deal_question]");
      expect(projection).toHaveTextContent("위험을 검토하고 승인 여부를 알려주세요.");
      expect(projection).not.toHaveTextContent("어제 비밀");
    });
  });
});
