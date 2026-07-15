import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { useState } from "react";
import type { Mail } from "@/types";
import { renderWithProviders } from "@/test/util";
import { useWorkspace } from "@/workspaceContext";
import { MailDetail } from "./MailDetail";

type RpcCall = { method: string; params: Record<string, unknown> };

const baseMail: Mail = {
  id: "mail-42",
  threadId: "thread-7",
  subject: "계약 조건 확인",
  from: "김리드 <lead@example.com>",
  to: "선택 <choice@example.com>",
  date: "2026-07-11T08:15:00+09:00",
  body: "# 요청\n\n- 계약 조건을 확인해 주세요.\n- **7월 15일**까지 회신 바랍니다.",
  labels: ["INBOX", "중요"],
  isUnread: true,
};

function reply(payload: unknown, ok = true): Response {
  return {
    ok,
    status: ok ? 200 : 500,
    json: async () => (ok ? { ok: true, payload } : { ok: false, error: String(payload) }),
  } as Response;
}

function installGateway(
  calls: RpcCall[],
  handlers: Partial<Record<string, (params: Record<string, unknown>) => Response | Promise<Response>>> = {},
) {
  vi.stubGlobal(
    "fetch",
    vi.fn(async (input: string | URL | Request, init?: RequestInit) => {
      const url = String(input);
      if (url.includes("/gmail/attachment")) {
        const blob = { type: "text/plain", size: 12, text: async () => "attachment body" } as Blob;
        return { ok: true, status: 200, blob: async () => blob } as Response;
      }
      const request = JSON.parse(String(init?.body ?? "{}")) as RpcCall;
      const method = request.method ?? "";
      const params = request.params ?? {};
      calls.push({ method, params });
      if (handlers[method]) return handlers[method]!(params);
      if (method === "miniapp.mail.analysis_cached") return reply(null);
      if (method === "miniapp.mail.sender_context") return reply({ sender: params.sender });
      if (method === "miniapp.mail.analyze") {
        return reply({ id: params.id, analysis: "# 분석\n\n마감 확인이 필요합니다.", cached: false });
      }
      if (method === "miniapp.mail.ask") return reply({ answer: `답변: ${params.question}` });
      return reply({});
    }),
  );
}

function renderMail(
  over: Partial<Mail> = {},
  opts: {
    connected?: boolean;
    busy?: boolean;
    query?: { isLoading: boolean; isError?: boolean; error?: unknown };
    onMarkRead?: () => void;
    onArchive?: () => void;
    onTrash?: () => void;
  } = {},
) {
  return renderWithProviders(
    <MailDetail
      mail={{ ...baseMail, ...over }}
      query={opts.query ?? { isLoading: false }}
      busy={opts.busy ?? false}
      onMarkRead={opts.onMarkRead ?? (() => {})}
      onArchive={opts.onArchive ?? (() => {})}
      onTrash={opts.onTrash ?? (() => {})}
    />,
    {
      connected: opts.connected ?? true,
      cfg: opts.connected === false ? { url: "", token: "" } : { url: "http://test", token: "tok" },
    },
  );
}

function lastCall(calls: RpcCall[], method: string) {
  return calls.filter((call) => call.method === method).at(-1);
}

describe("MailDetail boundary behavior", () => {
  let calls: RpcCall[];

  beforeEach(() => {
    localStorage.clear();
    calls = [];
    installGateway(calls);
  });

  afterEach(() => {
    localStorage.clear();
    vi.unstubAllGlobals();
    vi.restoreAllMocks();
  });

  describe("message identity and action controls", () => {
    it("renders nothing for a missing selected message", () => {
      const { container } = renderWithProviders(
        <MailDetail
          query={{ isLoading: false }}
          busy={false}
          onMarkRead={() => {}}
          onArchive={() => {}}
          onTrash={() => {}}
        />,
        { connected: true },
      );
      expect(container).toBeEmptyDOMElement();
      expect(calls).toHaveLength(0);
    });

    it("renders subject, normalized sender, recipient, date and labels", async () => {
      renderMail();
      expect(screen.getByText("계약 조건 확인")).toBeInTheDocument();
      expect(screen.getByText(/김리드 → 선택/)).toBeInTheDocument();
      expect(screen.getByText("INBOX")).toHaveClass("mail-label");
      expect(screen.getByText("중요")).toHaveClass("mail-label");
      await waitFor(() => expect(lastCall(calls, "miniapp.mail.sender_context")).toBeDefined());
      expect(lastCall(calls, "miniapp.mail.sender_context")?.params).toEqual({
        sender: "김리드 <lead@example.com>",
      });
    });

    it("uses safe fallbacks for missing subject and sender", () => {
      renderMail({ subject: undefined, from: undefined, to: undefined, date: undefined });
      expect(screen.getByText("(제목 없음)")).toBeInTheDocument();
      expect(screen.getByText("—")).toBeInTheDocument();
    });

    it("shows both loading and failure status independently of stale mail metadata", () => {
      renderMail({}, { query: { isLoading: true, isError: true, error: new Error("offline") } });
      expect(screen.getByText("본문 불러오는 중…")).toBeInTheDocument();
      expect(screen.getByText("본문 불러오기 실패")).toHaveClass("error");
      expect(screen.getByText("계약 조건 확인")).toBeInTheDocument();
    });

    it("offers mark-read only for unread messages", () => {
      const view = renderMail({ isUnread: true });
      expect(screen.getByRole("button", { name: "읽음" })).toBeInTheDocument();
      view.unmount();
      renderMail({ isUnread: false });
      expect(screen.queryByRole("button", { name: "읽음" })).not.toBeInTheDocument();
    });

    it("dispatches read, archive and trash through their distinct callbacks", async () => {
      const onMarkRead = vi.fn();
      const onArchive = vi.fn();
      const onTrash = vi.fn();
      renderMail({}, { onMarkRead, onArchive, onTrash });
      await userEvent.click(screen.getByRole("button", { name: "읽음" }));
      await userEvent.click(screen.getByRole("button", { name: "보관" }));
      await userEvent.click(screen.getByRole("button", { name: "삭제" }));
      expect(onMarkRead).toHaveBeenCalledOnce();
      expect(onArchive).toHaveBeenCalledOnce();
      expect(onTrash).toHaveBeenCalledOnce();
    });

    it("when disables destructive mail operations while a mutation is busy", () => {
      renderMail({}, { busy: true });
      expect(screen.getByRole("button", { name: "읽음" })).toBeDisabled();
      expect(screen.getByRole("button", { name: "보관" })).toBeDisabled();
      expect(screen.getByRole("button", { name: "삭제" })).toBeDisabled();
      expect(screen.getByRole("button", { name: "인쇄" })).toBeEnabled();
    });

    it("when exposes analysis/body selection as a pressed-state group", async () => {
      renderMail();
      const group = screen.getByRole("group", { name: "메일 보기 방식" });
      expect(within(group).getByRole("button", { name: "분석" })).toHaveAttribute("aria-pressed", "true");
      expect(within(group).getByRole("button", { name: "본문" })).toHaveAttribute("aria-pressed", "false");
      await userEvent.click(within(group).getByRole("button", { name: "본문" }));
      expect(within(group).getByRole("button", { name: "본문" })).toHaveAttribute("aria-pressed", "true");
    });
  });

  describe("body rendering", () => {
    it("renders gateway markdown as structured headings, lists and emphasis", async () => {
      renderMail();
      await userEvent.click(screen.getByRole("button", { name: "본문" }));
      expect(screen.getByRole("heading", { name: "요청" })).toBeInTheDocument();
      expect(screen.getByRole("list")).toBeInTheDocument();
      expect(screen.getByText("7월 15일").tagName).toBe("STRONG");
    });

    it("renders a clear empty state when every body alias is absent", async () => {
      renderMail({ body: undefined, text: undefined, snippet: undefined });
      await userEvent.click(screen.getByRole("button", { name: "본문" }));
      expect(screen.getByText("본문 없음")).toBeInTheDocument();
    });

    it("without keep the raw body mounted behind analysis mode", () => {
      renderMail();
      expect(screen.queryByRole("heading", { name: "요청" })).not.toBeInTheDocument();
      expect(screen.getByRole("button", { name: /이 메일 분석/ })).toBeInTheDocument();
    });
  });

  describe("analysis lifecycle", () => {
    it("when requests cached analysis by stable mail id on open", async () => {
      renderMail();
      await waitFor(() => expect(lastCall(calls, "miniapp.mail.analysis_cached")).toBeDefined());
      expect(lastCall(calls, "miniapp.mail.analysis_cached")?.params).toEqual({ id: "mail-42" });
    });

    it("renders cached markdown, importance, related projects and candidate counts", async () => {
      installGateway(calls, {
        "miniapp.mail.analysis_cached": () =>
          reply({
            id: "mail-42",
            cached: true,
            analysis: "## 핵심\n\n기한 내 회신이 필요합니다.",
            analysisQuality: "긴급",
            relatedProjects: [{ path: "프로젝트/계약", title: "계약 프로젝트", summary: "연결된 프로젝트" }],
            calendarProposalCount: 2,
            todoCount: 3,
          }),
      });
      renderMail();
      expect(await screen.findByRole("heading", { name: "핵심" })).toBeInTheDocument();
      expect(screen.getByText("긴급")).toHaveClass("hot");
      expect(screen.getByRole("button", { name: "계약 프로젝트" })).toBeInTheDocument();
      expect(screen.getByText("일정 제안 2 · 할일 후보 3")).toBeInTheDocument();
    });

    it("without treat a non-hot quality label as urgent", async () => {
      installGateway(calls, {
        "miniapp.mail.analysis_cached": () =>
          reply({ id: "mail-42", cached: true, analysis: "참고 분석", analysisQuality: "normal" }),
      });
      renderMail();
      expect(await screen.findByText("normal")).not.toHaveClass("hot");
    });

    it("runs initial analysis with force=false", async () => {
      renderMail();
      await userEvent.click(await screen.findByRole("button", { name: /이 메일 분석/ }));
      expect(await screen.findByRole("heading", { name: "분석" })).toBeInTheDocument();
      expect(lastCall(calls, "miniapp.mail.analyze")?.params).toEqual({ id: "mail-42", force: false });
    });

    it("reruns an existing analysis with force=true", async () => {
      installGateway(calls, {
        "miniapp.mail.analysis_cached": () =>
          reply({ id: "mail-42", cached: true, analysis: "기존 분석", analysisQuality: "high" }),
      });
      renderMail();
      await userEvent.click(await screen.findByRole("button", { name: "다시 분석" }));
      await waitFor(() => expect(lastCall(calls, "miniapp.mail.analyze")?.params.force).toBe(true));
    });

    it("when disables manual analysis while disconnected", () => {
      renderMail({}, { connected: false });
      expect(screen.getByRole("button", { name: /이 메일 분석/ })).toBeDisabled();
      expect(calls.filter((call) => call.method.startsWith("miniapp.mail.analysis"))).toHaveLength(0);
    });

    it("reports analysis failure and permits retry", async () => {
      installGateway(calls, { "miniapp.mail.analyze": () => reply("model unavailable", false) });
      renderMail();
      await userEvent.click(await screen.findByRole("button", { name: /이 메일 분석/ }));
      expect(await screen.findByText(/HTTP 500/)).toBeInTheDocument();
      expect(screen.getByRole("button", { name: /이 메일 분석/ })).toBeEnabled();
    });

    it("when opens a related project through the workspace wiki channel", async () => {
      installGateway(calls, {
        "miniapp.mail.analysis_cached": () =>
          reply({
            id: "mail-42",
            cached: true,
            analysis: "프로젝트 관련",
            relatedProjects: [{ path: "프로젝트/계약", title: "계약 프로젝트" }],
          }),
      });
      function WikiTarget() {
        return <output data-testid="wiki-target">{useWorkspace().wikiTarget}</output>;
      }
      renderWithProviders(
        <>
          <MailDetail
            mail={baseMail}
            query={{ isLoading: false }}
            busy={false}
            onMarkRead={() => {}}
            onArchive={() => {}}
            onTrash={() => {}}
          />
          <WikiTarget />
        </>,
        { connected: true, cfg: { url: "http://test", token: "tok" } },
      );
      await userEvent.click(await screen.findByRole("button", { name: "계약 프로젝트" }));
      expect(screen.getByTestId("wiki-target")).toHaveTextContent("프로젝트/계약");
    });
  });

  describe("sender context disclosure", () => {
    it("does not render an empty sender card", async () => {
      renderMail();
      await waitFor(() => expect(lastCall(calls, "miniapp.mail.sender_context")).toBeDefined());
      expect(screen.queryByRole("button", { name: /발신자/ })).not.toBeInTheDocument();
    });

    it("shows truncated recent volume in the collapsed teaser", async () => {
      installGateway(calls, {
        "miniapp.mail.sender_context": () =>
          reply({ sender: baseMail.from, recent: { windowDays: 30, count: 100, truncated: true } }),
      });
      renderMail();
      expect(await screen.findByText("최근 30일 100+건")).toBeInTheDocument();
      expect(screen.getByRole("button", { name: /발신자/ })).toHaveAttribute("aria-expanded", "false");
    });

    it("falls back to wiki-hit count when recent volume is unavailable", async () => {
      installGateway(calls, {
        "miniapp.mail.sender_context": () =>
          reply({ sender: baseMail.from, wikiHits: [{ path: "인물/김리드" }, { path: "회사/Example" }] }),
      });
      renderMail();
      expect(await screen.findByText("위키 2건")).toBeInTheDocument();
    });

    it("when expands recent details, wiki facts and curated page chips", async () => {
      installGateway(calls, {
        "miniapp.mail.sender_context": () =>
          reply({
            sender: baseMail.from,
            recent: { windowDays: 14, count: 8, lastReceivedAt: "2026-07-10T09:00:00+09:00" },
            wikiHits: [{ path: "인물/김리드", title: "김리드", summary: "구매팀 리드" }],
            wikiFacts: "의사결정권자 · 구매팀",
          }),
      });
      renderMail();
      await userEvent.click(await screen.findByRole("button", { name: /발신자/ }));
      expect(screen.getByText(/최근 14일 8건 · 마지막/)).toBeInTheDocument();
      expect(screen.getByText("의사결정권자 · 구매팀")).toBeInTheDocument();
      expect(screen.getByRole("button", { name: "김리드" })).toHaveAttribute("title", "구매팀 리드");
    });

    it("toggles expanded sender context closed without refetching", async () => {
      installGateway(calls, {
        "miniapp.mail.sender_context": () => reply({ sender: baseMail.from, recent: { windowDays: 7, count: 2 } }),
      });
      renderMail();
      const disclosure = await screen.findByRole("button", { name: /발신자/ });
      await userEvent.click(disclosure);
      expect(disclosure).toHaveAttribute("aria-expanded", "true");
      await userEvent.click(disclosure);
      expect(disclosure).toHaveAttribute("aria-expanded", "false");
      expect(calls.filter((call) => call.method === "miniapp.mail.sender_context")).toHaveLength(1);
    });
  });

  describe("attachment safety", () => {
    it("shows count-only metadata when attachment details are unavailable", () => {
      renderMail({ attachmentCount: 3, attachments: [] });
      expect(screen.getByText("첨부파일 3개")).toBeInTheDocument();
    });

    it("does not render an attachment section for a trustworthy zero count", () => {
      renderMail({ attachmentCount: 0, attachments: [] });
      expect(screen.queryByText("첨부파일")).not.toBeInTheDocument();
    });

    it("keeps an attachment without id inert and aria-disabled", () => {
      renderMail({ attachments: [{ filename: "missing-id.txt", mimeType: "text/plain", size: 12 }] });
      const item = screen.getByText("missing-id.txt").closest(".mail-attachment")!;
      expect(item).toHaveAttribute("aria-disabled", "true");
      expect(item.tagName).toBe("DIV");
    });

    it("renders non-previewable attachments as safe external download links", () => {
      renderMail({
        attachments: [{ id: "sheet", name: "budget.xlsx", mimeType: "application/vnd.ms-excel", size: 2_048 }],
      });
      const link = screen.getByRole("link", { name: /budget\.xlsx/ });
      expect(link).toHaveAttribute("target", "_blank");
      expect(link).toHaveAttribute("rel", "noreferrer");
      expect(link).toHaveAttribute("href", expect.stringContaining("mail-42"));
      expect(link).toHaveTextContent("application/vnd.ms-excel · 2.0 KB");
    });

    it("opens previewable text attachment in a read-only viewer", async () => {
      renderMail({ attachments: [{ attachmentId: "text", filename: "notes.txt", mimeType: "text/plain", size: 12 }] });
      await userEvent.click(screen.getByRole("button", { name: /notes\.txt/ }));
      const dialog = await screen.findByRole("dialog", { name: "notes.txt" });
      expect(await within(dialog).findByText("attachment body")).toBeInTheDocument();
      expect(within(dialog).queryByRole("button", { name: "저장" })).not.toBeInTheDocument();
      expect(within(dialog).getByRole("link", { name: /다운로드/ })).toBeInTheDocument();
    });
  });

  describe("grounded Q&A", () => {
    it("when disables question input and submit while disconnected", () => {
      renderMail({}, { connected: false });
      expect(screen.getByPlaceholderText("예: 핵심 요청이 뭐야?")).toBeDisabled();
      expect(screen.getByRole("button", { name: "질문" })).toBeDisabled();
    });

    it("when enables submit only for nonblank trimmed questions", () => {
      renderMail();
      const input = screen.getByPlaceholderText("예: 핵심 요청이 뭐야?");
      const submit = screen.getByRole("button", { name: "질문" });
      expect(submit).toBeDisabled();
      fireEvent.change(input, { target: { value: "   " } });
      expect(submit).toBeDisabled();
      fireEvent.change(input, { target: { value: "마감은?" } });
      expect(submit).toBeEnabled();
    });

    it("trims a question, clears input while busy and renders markdown answer", async () => {
      installGateway(calls, {
        "miniapp.mail.ask": (params) => reply({ answer: `**답변**: ${params.question}` }),
      });
      renderMail();
      const input = screen.getByPlaceholderText("예: 핵심 요청이 뭐야?");
      fireEvent.change(input, { target: { value: "  핵심 요청은?  " } });
      await userEvent.click(screen.getByRole("button", { name: "질문" }));
      await waitFor(() =>
        expect(lastCall(calls, "miniapp.mail.ask")?.params).toEqual({
          id: "mail-42",
          question: "핵심 요청은?",
          history: [],
        }),
      );
      expect((await screen.findByText("답변")).tagName).toBe("STRONG");
      expect(screen.getByText("핵심 요청은?")).toHaveClass("mail-qa-q");
      expect(input).toHaveValue("");
    });

    it("when resends prior turns as grounded history on follow-up", async () => {
      renderMail();
      const input = screen.getByPlaceholderText("예: 핵심 요청이 뭐야?");
      fireEvent.change(input, { target: { value: "첫 질문" } });
      await userEvent.click(screen.getByRole("button", { name: "질문" }));
      await screen.findByText("답변: 첫 질문");
      fireEvent.change(input, { target: { value: "후속 질문" } });
      await userEvent.click(screen.getByRole("button", { name: "질문" }));
      await waitFor(() => expect(calls.filter((call) => call.method === "miniapp.mail.ask")).toHaveLength(2));
      expect(lastCall(calls, "miniapp.mail.ask")?.params).toEqual({
        id: "mail-42",
        question: "후속 질문",
        history: [{ q: "첫 질문", a: "답변: 첫 질문" }],
      });
    });

    it("reports an ask failure without fabricating a conversation turn", async () => {
      installGateway(calls, { "miniapp.mail.ask": () => reply("qa unavailable", false) });
      renderMail();
      fireEvent.change(screen.getByPlaceholderText("예: 핵심 요청이 뭐야?"), { target: { value: "질문" } });
      await userEvent.click(screen.getByRole("button", { name: "질문" }));
      expect(await screen.findByText(/HTTP 500/)).toBeInTheDocument();
      expect(screen.queryByText("질문", { selector: ".mail-qa-q" })).not.toBeInTheDocument();
      expect(screen.getByRole("button", { name: "질문" })).toBeDisabled();
    });

    it("starts a fresh Q&A thread when the selected message id changes", async () => {
      function SwitchableMail() {
        const [id, setId] = useState("mail-42");
        return (
          <>
            <button onClick={() => setId("mail-99")}>다른 메일 선택</button>
            <MailDetail
              mail={{ ...baseMail, id, subject: id === "mail-42" ? baseMail.subject : "다른 메일" }}
              query={{ isLoading: false }}
              busy={false}
              onMarkRead={() => {}}
              onArchive={() => {}}
              onTrash={() => {}}
            />
          </>
        );
      }
      renderWithProviders(<SwitchableMail />, { connected: true, cfg: { url: "http://test", token: "tok" } });
      fireEvent.change(screen.getByPlaceholderText("예: 핵심 요청이 뭐야?"), { target: { value: "첫 질문" } });
      await userEvent.click(screen.getByRole("button", { name: "질문" }));
      await screen.findByText("답변: 첫 질문");

      await userEvent.click(screen.getByRole("button", { name: "다른 메일 선택" }));
      expect(screen.queryByText("답변: 첫 질문")).not.toBeInTheDocument();
      const input = screen.getByPlaceholderText("예: 핵심 요청이 뭐야?");
      fireEvent.change(input, { target: { value: "새 질문" } });
      await userEvent.click(screen.getByRole("button", { name: "질문" }));
      await waitFor(() => expect(lastCall(calls, "miniapp.mail.ask")?.params.id).toBe("mail-99"));
      expect(lastCall(calls, "miniapp.mail.ask")?.params.history).toEqual([]);
    });
  });
});
