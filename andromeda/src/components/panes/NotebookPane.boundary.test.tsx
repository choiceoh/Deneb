import { useEffect } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { renderWithProviders } from "@/test/util";
import { useWorkspace } from "@/workspaceContext";
import { NotebookPane } from "./NotebookPane";

type RpcCall = { method: string; params: Record<string, unknown> };
type NotebookRow = {
  id: string;
  name: string;
  description?: string;
  dealRef?: string;
  sourceCount: number;
  updated: number;
};
type Source = { cite: string; kind: string; title?: string; text?: string; ref?: string };

const initialRows: NotebookRow[] = [
  { id: "older", name: "이전 노트북", description: "이전 거래", sourceCount: 0, updated: 100 },
  {
    id: "latest",
    name: "최신 노트북",
    description: "현재 거래 검토",
    dealRef: "프로젝트/최신.md",
    sourceCount: 4,
    updated: 200,
  },
];

const initialSources: Record<string, Source[]> = {
  older: [],
  latest: [
    { cite: "S1", kind: "note", title: "직접 메모", text: "마감은 7월 30일입니다." },
    { cite: "S2", kind: "wiki", title: "프로젝트 위키", ref: "프로젝트/최신.md", text: "위키 요약" },
    { cite: "S3", kind: "file", title: "계약서", ref: "contracts/latest.pdf" },
    { cite: "S4", kind: "mail", title: "협상 메일", ref: "mail-42" },
  ],
};

function reply(payload: unknown, ok = true): Response {
  return {
    ok,
    status: ok ? 200 : 500,
    json: async () => (ok ? { ok: true, payload } : { ok: false, error: String(payload) }),
  } as Response;
}

function installNotebookGateway(
  calls: RpcCall[],
  handlers: Partial<Record<string, (params: Record<string, unknown>) => Response | Promise<Response>>> = {},
) {
  let rows = initialRows.map((row) => ({ ...row }));
  const sources = Object.fromEntries(
    Object.entries(initialSources).map(([id, value]) => [id, value.map((s) => ({ ...s }))]),
  );
  let sequence = 10;
  vi.stubGlobal(
    "fetch",
    vi.fn(async (_url: string, init?: RequestInit) => {
      const request = JSON.parse(String(init?.body ?? "{}")) as RpcCall;
      const method = request.method ?? "";
      const params = request.params ?? {};
      calls.push({ method, params });
      if (handlers[method]) return handlers[method]!(params);
      switch (method) {
        case "miniapp.notebook.list":
          return reply({ notebooks: rows });
        case "miniapp.notebook.get": {
          const id = String(params.id);
          const row = rows.find((candidate) => candidate.id === id);
          return reply({ ...row, sources: sources[id] ?? [] });
        }
        case "miniapp.notebook.create": {
          const row: NotebookRow = {
            id: `new-${sequence++}`,
            name: String(params.name),
            description: String(params.description ?? ""),
            sourceCount: 0,
            updated: 1_000 + sequence,
          };
          rows = [row, ...rows];
          sources[row.id] = [];
          return reply(row);
        }
        case "miniapp.notebook.add_source": {
          const id = String(params.id);
          const source: Source = {
            cite: `S${(sources[id]?.length ?? 0) + 1}`,
            kind: String(params.kind ?? "note"),
            title: String(params.title ?? ""),
            text: String(params.text ?? ""),
            ref: String(params.ref ?? ""),
          };
          sources[id] = [...(sources[id] ?? []), source];
          return reply(source);
        }
        case "miniapp.notebook.add_ref": {
          // The gateway would fetch/read the ref into text; the stub pins a source
          // whose text stands in for the ingested content.
          const id = String(params.id);
          const source: Source = {
            cite: `S${(sources[id]?.length ?? 0) + 1}`,
            kind: String(params.kind ?? "url"),
            title: String(params.title || params.ref || ""),
            text: `읽어온 본문: ${String(params.ref ?? "")}`,
            ref: String(params.ref ?? ""),
          };
          sources[id] = [...(sources[id] ?? []), source];
          return reply(source);
        }
        case "miniapp.notebook.edit_source": {
          const id = String(params.id);
          const cite = String(params.cite);
          const src = (sources[id] ?? []).find((s) => s.cite === cite);
          if (src) src.title = String(params.title ?? "");
          return reply(src ?? {});
        }
        case "miniapp.notebook.remove_source": {
          const id = String(params.id);
          sources[id] = (sources[id] ?? []).filter((source) => source.cite !== params.cite);
          return reply({ ...rows.find((row) => row.id === id), sources: sources[id] });
        }
        case "miniapp.notebook.delete":
          rows = rows.filter((row) => row.id !== params.id);
          return reply({ deleted: true });
        default:
          return reply({});
      }
    }),
  );
}

function renderNotebook(connected = true) {
  return renderWithProviders(<NotebookPane />, {
    connected,
    cfg: connected ? { url: "http://test", token: "tok" } : { url: "", token: "" },
  });
}

function lastCall(calls: RpcCall[], method: string) {
  return calls.filter((call) => call.method === method).at(-1);
}

describe("NotebookPane boundary behavior", () => {
  let calls: RpcCall[];

  beforeEach(() => {
    calls = [];
    localStorage.clear();
    installNotebookGateway(calls);
    if (!globalThis.crypto?.randomUUID) vi.stubGlobal("crypto", { randomUUID: () => "notebook-test" });
  });

  afterEach(() => {
    localStorage.clear();
    vi.unstubAllGlobals();
  });

  describe("loading and selection", () => {
    it("renders a disconnected state without issuing notebook RPCs", () => {
      renderNotebook(false);
      expect(screen.getByText("게이트웨이에 연결하세요.")).toBeInTheDocument();
      expect(screen.getByRole("button", { name: "새 노트북" })).toBeDisabled();
      expect(calls).toHaveLength(0);
    });

    it("auto-opens the most recently updated notebook regardless of list order", async () => {
      renderNotebook();
      expect(await screen.findByRole("heading", { name: "최신 노트북" })).toBeInTheDocument();
      expect(lastCall(calls, "miniapp.notebook.get")?.params).toEqual({ id: "latest" });
    });

    it("renders notebook description and source count in the selector", async () => {
      renderNotebook();
      await screen.findByRole("heading", { name: "최신 노트북" });
      const select = screen.getByRole("combobox", { name: "노트북 선택" });
      expect(within(select).getByRole("option", { name: /최신 노트북 · 자료 4/ })).toBeInTheDocument();
    });

    it("switches notebooks by stable id and clears the old source preview", async () => {
      renderNotebook();
      await screen.findByRole("heading", { name: "최신 노트북" });
      await userEvent.click(screen.getByRole("button", { name: /직접 메모/ }));
      expect(screen.getByRole("group", { name: "자료 내용" })).toBeInTheDocument();
      await userEvent.selectOptions(screen.getByRole("combobox", { name: "노트북 선택" }), "older");
      expect(await screen.findByRole("heading", { name: "이전 노트북" })).toBeInTheDocument();
      expect(screen.queryByRole("group", { name: "자료 내용" })).not.toBeInTheDocument();
      expect(screen.getByText(/아직 자료가 없습니다/)).toBeInTheDocument();
    });

    it("shows an empty catalog distinctly from a connection failure", async () => {
      calls = [];
      installNotebookGateway(calls, { "miniapp.notebook.list": () => reply({ notebooks: [] }) });
      renderNotebook();
      expect(await screen.findByText("노트북이 없습니다. “＋ 새 노트북”으로 만드세요.")).toBeInTheDocument();
      expect(screen.queryByText("미연결")).not.toBeInTheDocument();
    });

    it("surfaces list failure without offering stale notebook content", async () => {
      calls = [];
      installNotebookGateway(calls, { "miniapp.notebook.list": () => reply("list unavailable", false) });
      renderNotebook();
      expect(await screen.findByText(/HTTP 500/)).toBeInTheDocument();
      expect(screen.queryByRole("heading", { name: "최신 노트북" })).not.toBeInTheDocument();
    });

    it("surfaces detail failure without mixing sources from the previous notebook", async () => {
      calls = [];
      installNotebookGateway(calls, { "miniapp.notebook.get": () => reply("detail unavailable", false) });
      renderNotebook();
      expect(await screen.findByText(/HTTP 500/)).toBeInTheDocument();
      expect(screen.queryByText("직접 메모")).not.toBeInTheDocument();
    });

    it("when opens a linked project wiki from the active notebook", async () => {
      function WikiProbe() {
        const { wikiTarget } = useWorkspace();
        return <output data-testid="wiki-target">{wikiTarget}</output>;
      }
      renderWithProviders(
        <>
          <NotebookPane />
          <WikiProbe />
        </>,
        { connected: true, cfg: { url: "http://test", token: "tok" } },
      );
      await screen.findByRole("heading", { name: "최신 노트북" });
      await userEvent.click(screen.getByTitle("딜 페이지 열기"));
      expect(screen.getByTestId("wiki-target")).toHaveTextContent("프로젝트/최신.md");
    });
  });

  describe("source presentation", () => {
    it.each([
      ["직접 메모", "노트"],
      ["프로젝트 위키", "위키"],
      ["계약서", "파일"],
      ["협상 메일", "메일"],
    ])("when labels %s as %s", async (title, kind) => {
      renderNotebook();
      await screen.findByRole("heading", { name: "최신 노트북" });
      const chip = screen.getByRole("button", { name: new RegExp(title) });
      expect(chip).toHaveTextContent(kind);
    });

    it("expands source text and reference without losing cite identity", async () => {
      renderNotebook();
      await screen.findByRole("heading", { name: "최신 노트북" });
      await userEvent.click(screen.getByRole("button", { name: /프로젝트 위키/ }));
      const preview = screen.getByRole("group", { name: "자료 내용" });
      expect(preview).toHaveTextContent("S2");
      expect(preview).toHaveTextContent("위키 요약");
    });

    it("uses note text as a safe title fallback when title is blank", async () => {
      calls = [];
      installNotebookGateway(calls, {
        "miniapp.notebook.get": (params) =>
          reply({
            ...initialRows.find((row) => row.id === params.id),
            sources: [{ cite: "S1", kind: "note", text: "첫 줄 요약\n둘째 줄" }],
          }),
      });
      renderNotebook();
      expect(await screen.findByRole("button", { name: /첫 줄 요약/ })).toBeInTheDocument();
    });

    it("keeps cite identity when an otherwise unnamed source uses the empty-title fallback", async () => {
      calls = [];
      installNotebookGateway(calls, {
        "miniapp.notebook.get": (params) =>
          reply({ ...initialRows.find((row) => row.id === params.id), sources: [{ cite: "S99", kind: "file" }] }),
      });
      renderNotebook();
      const chip = await screen.findByRole("button", { name: /\(제목 없음\)/ });
      expect(chip.closest('[role="listitem"]')).toHaveTextContent("S99");
    });

    it("offers 원본 열기 for a file source whose original was archived (path ref)", async () => {
      calls = [];
      installNotebookGateway(calls, {
        "miniapp.notebook.get": (params) =>
          reply({
            ...initialRows.find((row) => row.id === params.id),
            sources: [
              { cite: "S1", kind: "file", title: "계약서.pdf", ref: "노트북/최신/계약서.pdf", text: "추출 본문" },
              { cite: "S2", kind: "file", title: "메모.txt", ref: "메모.txt", text: "본문" },
            ],
          }),
      });
      renderNotebook();

      // Archived original (ref is a file-store path) → the preview links to it.
      await userEvent.click(await screen.findByRole("button", { name: /계약서\.pdf/ }));
      const link = await screen.findByRole("link", { name: "원본 열기" });
      expect(link).toHaveAttribute("href", expect.stringContaining("/api/v1/files/download"));
      expect(link).toHaveAttribute("href", expect.stringContaining(encodeURIComponent("노트북/최신/계약서.pdf")));

      // A bare-filename ref (no archived original) shows no 원본 열기.
      await userEvent.click(screen.getByRole("button", { name: /메모\.txt/ }));
      expect(screen.queryByRole("link", { name: "원본 열기" })).not.toBeInTheDocument();
    });

    it("filters the source chips by a title/text query", async () => {
      renderNotebook();
      await screen.findByRole("heading", { name: "최신 노트북" });
      // All four chips present initially (>3 → the search box shows).
      expect(screen.getByRole("button", { name: /직접 메모/ })).toBeInTheDocument();
      expect(screen.getByRole("button", { name: /협상 메일/ })).toBeInTheDocument();

      await userEvent.type(screen.getByLabelText("자료 검색"), "메일");
      // Only the 협상 메일 chip survives the filter; 직접 메모 is hidden.
      expect(screen.getByRole("button", { name: /협상 메일/ })).toBeInTheDocument();
      expect(screen.queryByRole("button", { name: /직접 메모/ })).not.toBeInTheDocument();

      await userEvent.clear(screen.getByLabelText("자료 검색"));
      expect(screen.getByRole("button", { name: /직접 메모/ })).toBeInTheDocument();
    });

    it("renames a source through the preview, keeping its cite", async () => {
      renderNotebook();
      await screen.findByRole("heading", { name: "최신 노트북" });
      await userEvent.click(screen.getByRole("button", { name: /직접 메모/ }));
      await userEvent.click(screen.getByRole("button", { name: "이름 변경" }));

      const dialog = screen.getByRole("dialog", { name: "자료 이름 변경" });
      const field = within(dialog).getByLabelText("제목");
      await userEvent.clear(field);
      await userEvent.type(field, "마감 일정");
      await userEvent.click(within(dialog).getByRole("button", { name: "저장" }));

      await waitFor(() => expect(lastCall(calls, "miniapp.notebook.edit_source")).toBeDefined());
      expect(lastCall(calls, "miniapp.notebook.edit_source")?.params).toMatchObject({
        id: "latest",
        cite: "S1",
        title: "마감 일정",
      });
      expect((await screen.findAllByText("마감 일정")).length).toBeGreaterThan(0);
    });

    it("브리핑 asks the docked chat for a grounded briefing over the notebook", async () => {
      const asked: string[] = [];
      function AskProbe() {
        const { setAskSink } = useWorkspace();
        useEffect(() => {
          setAskSink((t) => asked.push(t));
          return () => setAskSink(null);
        }, [setAskSink]);
        return null;
      }
      renderWithProviders(
        <>
          <NotebookPane />
          <AskProbe />
        </>,
        { connected: true, cfg: { url: "http://test", token: "tok" } },
      );
      await screen.findByRole("heading", { name: "최신 노트북" });
      await userEvent.click(screen.getByRole("button", { name: "브리핑" }));
      expect(asked).toHaveLength(1);
      expect(asked[0]).toContain("최신");
      expect(asked[0]).toContain("[S번호]");
    });

    it("lights up the chips an answer cited ([S1]…)", async () => {
      function AnswerProbe() {
        const { publishAnswer } = useWorkspace();
        return <button onClick={() => publishAnswer("잔금은 [S1]에 있고 계약서는 [S3] 참고.")}>emit</button>;
      }
      renderWithProviders(
        <>
          <NotebookPane />
          <AnswerProbe />
        </>,
        { connected: true, cfg: { url: "http://test", token: "tok" } },
      );
      await screen.findByRole("heading", { name: "최신 노트북" });
      // No highlight before any answer.
      expect(screen.queryAllByTitle("최근 답변이 인용한 자료")).toHaveLength(0);

      await userEvent.click(screen.getByRole("button", { name: "emit" }));
      // S1 (직접 메모) and S3 (계약서) chips light up; S2/S4 stay dim.
      await waitFor(() => expect(screen.getAllByTitle("최근 답변이 인용한 자료")).toHaveLength(2));
    });

    it("offers 딜 페이지 담기 for a deal notebook whose page isn't pinned yet", async () => {
      calls = [];
      installNotebookGateway(calls, {
        "miniapp.notebook.list": () =>
          reply({ notebooks: [{ id: "d", name: "새 딜", dealRef: "프로젝트/새딜.md", sourceCount: 0, updated: 9 }] }),
        "miniapp.notebook.get": (params) =>
          reply({ id: params.id, name: "새 딜", dealRef: "프로젝트/새딜.md", sources: [] }),
      });
      renderNotebook();
      await screen.findByRole("heading", { name: "새 딜" });

      await userEvent.click(screen.getByRole("button", { name: "딜 페이지 담기" }));
      await waitFor(() => expect(lastCall(calls, "miniapp.notebook.add_source")).toBeDefined());
      // One click pins the deal's own wiki page as a source.
      expect(lastCall(calls, "miniapp.notebook.add_source")?.params).toMatchObject({
        id: "d",
        kind: "wiki",
        ref: "프로젝트/새딜.md",
        title: "딜 페이지",
      });
    });

    it("hides 딜 페이지 담기 once the deal page is already a source", async () => {
      // The default 최신 notebook's S2 IS its deal page (kind=wiki, ref=dealRef).
      renderNotebook();
      await screen.findByRole("heading", { name: "최신 노트북" });
      expect(screen.queryByRole("button", { name: "딜 페이지 담기" })).not.toBeInTheDocument();
    });

    it("toggles the same source closed when its chip is clicked twice", async () => {
      renderNotebook();
      await screen.findByRole("heading", { name: "최신 노트북" });
      const chip = screen.getByRole("button", { name: /직접 메모/ });
      await userEvent.click(chip);
      expect(screen.getByRole("group", { name: "자료 내용" })).toBeInTheDocument();
      await userEvent.click(chip);
      expect(screen.queryByRole("group", { name: "자료 내용" })).not.toBeInTheDocument();
    });

    it("closes a source preview through its labelled close control", async () => {
      renderNotebook();
      await screen.findByRole("heading", { name: "최신 노트북" });
      await userEvent.click(screen.getByRole("button", { name: /계약서/ }));
      const preview = screen.getByRole("group", { name: "자료 내용" });
      await userEvent.click(within(preview).getByRole("button", { name: "미리보기 닫기" }));
      expect(screen.queryByRole("group", { name: "자료 내용" })).not.toBeInTheDocument();
    });
  });

  describe("create and source validation", () => {
    it("when requires a nonblank notebook name", async () => {
      renderNotebook();
      await screen.findByRole("heading", { name: "최신 노트북" });
      await userEvent.click(screen.getByRole("button", { name: "새 노트북" }));
      const dialog = screen.getByRole("dialog", { name: "새 노트북" });
      expect(within(dialog).getByRole("button", { name: "생성" })).toBeDisabled();
      await userEvent.type(within(dialog).getByLabelText("이름"), "   ");
      expect(within(dialog).getByRole("button", { name: "생성" })).toBeDisabled();
      expect(calls.filter((call) => call.method === "miniapp.notebook.create")).toHaveLength(0);
    });

    it("trims create fields before sending them", async () => {
      renderNotebook();
      await screen.findByRole("heading", { name: "최신 노트북" });
      await userEvent.click(screen.getByRole("button", { name: "새 노트북" }));
      const dialog = screen.getByRole("dialog", { name: "새 노트북" });
      fireEvent.change(within(dialog).getByLabelText("이름"), { target: { value: "  신규 거래  " } });
      fireEvent.change(within(dialog).getByLabelText("설명 (선택)"), { target: { value: "  검토 메모  " } });
      await userEvent.click(within(dialog).getByRole("button", { name: "생성" }));
      await waitFor(() =>
        expect(lastCall(calls, "miniapp.notebook.create")?.params).toEqual({
          name: "신규 거래",
          description: "검토 메모",
        }),
      );
      expect(await screen.findByRole("heading", { name: "신규 거래" })).toBeInTheDocument();
    });

    it("cancels create without mutating the catalog", async () => {
      renderNotebook();
      await screen.findByRole("heading", { name: "최신 노트북" });
      await userEvent.click(screen.getByRole("button", { name: "새 노트북" }));
      const dialog = screen.getByRole("dialog", { name: "새 노트북" });
      await userEvent.type(within(dialog).getByLabelText("이름"), "버릴 노트북");
      await userEvent.click(within(dialog).getByRole("button", { name: "취소" }));
      expect(screen.queryByRole("dialog", { name: "새 노트북" })).not.toBeInTheDocument();
      expect(calls.filter((call) => call.method === "miniapp.notebook.create")).toHaveLength(0);
    });

    it("when requires text for note sources", async () => {
      renderNotebook();
      await screen.findByRole("heading", { name: "최신 노트북" });
      await userEvent.click(screen.getByRole("button", { name: "자료 추가" }));
      const dialog = screen.getByRole("dialog", { name: /인용자료 추가/ });
      expect(within(dialog).getByRole("button", { name: "추가" })).toBeDisabled();
      await userEvent.type(within(dialog).getByLabelText("내용"), "   ");
      expect(within(dialog).getByRole("button", { name: "추가" })).toBeDisabled();
    });

    // 파일 no longer rides add_source-by-ref — it uses a picker + add_file (covered
    // in NotebookPane.test.tsx). wiki still pins by a typed ref via add_source (its
    // page is read live at brief time, so it needs no ingestion).
    it("when adds a wiki source by canonical ref via add_source", async () => {
      renderNotebook();
      await screen.findByRole("heading", { name: "최신 노트북" });
      await userEvent.click(screen.getByRole("button", { name: "자료 추가" }));
      const dialog = screen.getByRole("dialog", { name: /인용자료 추가/ });
      await userEvent.click(within(dialog).getByRole("button", { name: "위키" }));
      fireEvent.change(within(dialog).getByLabelText("제목 (선택)"), { target: { value: "  근거 자료  " } });
      fireEvent.change(within(dialog).getByLabelText("위키 경로"), { target: { value: "  프로젝트/계약.md  " } });
      await userEvent.click(within(dialog).getByRole("button", { name: "추가" }));
      await waitFor(() => expect(lastCall(calls, "miniapp.notebook.add_source")).toBeDefined());
      expect(lastCall(calls, "miniapp.notebook.add_source")?.params).toMatchObject({
        id: "latest",
        kind: "wiki",
        title: "근거 자료",
        ref: "프로젝트/계약.md",
      });
    });

    // url/mail/diary carry only a ref the user typed; the gateway reads it into text,
    // so they route to add_ref (not add_source, which would reject them for no text).
    it.each([
      ["메일", "메일 ID", "message-42", "mail"],
      ["URL", "URL", "https://example.com/source", "url"],
      ["일기", "일기 날짜/ID", "2026-06-24", "diary"],
    ])("when ingests a %s source by ref via add_ref", async (tab, field, ref, kind) => {
      renderNotebook();
      await screen.findByRole("heading", { name: "최신 노트북" });
      await userEvent.click(screen.getByRole("button", { name: "자료 추가" }));
      const dialog = screen.getByRole("dialog", { name: /인용자료 추가/ });
      await userEvent.click(within(dialog).getByRole("button", { name: tab }));
      fireEvent.change(within(dialog).getByLabelText("제목 (선택)"), { target: { value: "  근거 자료  " } });
      fireEvent.change(within(dialog).getByLabelText(field), { target: { value: `  ${ref}  ` } });
      await userEvent.click(within(dialog).getByRole("button", { name: "추가" }));
      await waitFor(() => expect(lastCall(calls, "miniapp.notebook.add_ref")).toBeDefined());
      expect(lastCall(calls, "miniapp.notebook.add_ref")?.params).toMatchObject({
        id: "latest",
        kind,
        title: "근거 자료",
        ref,
      });
      // add_source must NOT be used for these ingested kinds.
      expect(lastCall(calls, "miniapp.notebook.add_source")).toBeUndefined();
    });

    it("preserves add-source input after a gateway failure", async () => {
      calls = [];
      installNotebookGateway(calls, { "miniapp.notebook.add_source": () => reply("write denied", false) });
      renderNotebook();
      await screen.findByRole("heading", { name: "최신 노트북" });
      await userEvent.click(screen.getByRole("button", { name: "자료 추가" }));
      const dialog = screen.getByRole("dialog", { name: /인용자료 추가/ });
      await userEvent.type(within(dialog).getByLabelText("내용"), "보존할 근거");
      await userEvent.click(within(dialog).getByRole("button", { name: "추가" }));
      expect(await screen.findByText(/HTTP 500/)).toBeInTheDocument();
      expect(within(dialog).getByLabelText("내용")).toHaveValue("보존할 근거");
    });
  });

  describe("note sink and destructive changes", () => {
    it("when registers a note sink only while a notebook is open", async () => {
      function SinkProbe() {
        const { noteSink } = useWorkspace();
        return <output data-testid="sink">{noteSink ? "registered" : "none"}</output>;
      }
      renderWithProviders(
        <>
          <NotebookPane />
          <SinkProbe />
        </>,
        { connected: true, cfg: { url: "http://test", token: "tok" } },
      );
      await screen.findByRole("heading", { name: "최신 노트북" });
      await waitFor(() => expect(screen.getByTestId("sink")).toHaveTextContent("registered"));
    });

    it("rejects a whitespace-only AI answer without a gateway mutation", async () => {
      let sink: ((text: string) => Promise<boolean>) | null = null;
      function SinkProbe() {
        sink = useWorkspace().noteSink;
        return null;
      }
      renderWithProviders(
        <>
          <NotebookPane />
          <SinkProbe />
        </>,
        { connected: true, cfg: { url: "http://test", token: "tok" } },
      );
      await screen.findByRole("heading", { name: "최신 노트북" });
      await waitFor(() => expect(sink).not.toBeNull());
      await expect(sink!(" \n\t ")).resolves.toBe(false);
      expect(calls.filter((call) => call.method === "miniapp.notebook.add_source")).toHaveLength(0);
    });

    it("returns false from the AI sink when persistence fails", async () => {
      calls = [];
      installNotebookGateway(calls, { "miniapp.notebook.add_source": () => reply("sink write failed", false) });
      let sink: ((text: string) => Promise<boolean>) | null = null;
      function SinkProbe() {
        sink = useWorkspace().noteSink;
        return null;
      }
      renderWithProviders(
        <>
          <NotebookPane />
          <SinkProbe />
        </>,
        { connected: true, cfg: { url: "http://test", token: "tok" } },
      );
      await screen.findByRole("heading", { name: "최신 노트북" });
      await waitFor(() => expect(sink).not.toBeNull());
      await expect(sink!("저장 실패할 답변")).resolves.toBe(false);
      expect(await screen.findByText(/HTTP 500/)).toBeInTheDocument();
    });

    it("when requires confirmation before removing a cited source", async () => {
      renderNotebook();
      await screen.findByRole("heading", { name: "최신 노트북" });
      await userEvent.click(screen.getByRole("button", { name: "인용자료 삭제 S2" }));
      const dialog = screen.getByRole("dialog", { name: "인용자료 삭제" });
      expect(dialog).toHaveTextContent("프로젝트 위키");
      expect(calls.filter((call) => call.method === "miniapp.notebook.remove_source")).toHaveLength(0);
      await userEvent.click(within(dialog).getByRole("button", { name: "삭제" }));
      await waitFor(() =>
        expect(lastCall(calls, "miniapp.notebook.remove_source")?.params).toEqual({ id: "latest", cite: "S2" }),
      );
      expect(screen.queryByText("프로젝트 위키")).not.toBeInTheDocument();
    });

    it("keeps source confirmation open when removal fails", async () => {
      calls = [];
      installNotebookGateway(calls, { "miniapp.notebook.remove_source": () => reply("remove denied", false) });
      renderNotebook();
      await screen.findByRole("heading", { name: "최신 노트북" });
      await userEvent.click(screen.getByRole("button", { name: "인용자료 삭제 S1" }));
      const dialog = screen.getByRole("dialog", { name: "인용자료 삭제" });
      await userEvent.click(within(dialog).getByRole("button", { name: "삭제" }));
      expect(await screen.findByText(/HTTP 500/)).toBeInTheDocument();
      expect(dialog).toBeInTheDocument();
      expect(screen.getByText("직접 메모")).toBeInTheDocument();
    });

    it("when requires confirmation before deleting the whole notebook", async () => {
      renderNotebook();
      await screen.findByRole("heading", { name: "최신 노트북" });
      await userEvent.click(screen.getByRole("button", { name: "노트북 삭제" }));
      const dialog = screen.getByRole("dialog", { name: "노트북 삭제" });
      expect(dialog).toHaveTextContent("최신 노트북");
      expect(calls.filter((call) => call.method === "miniapp.notebook.delete")).toHaveLength(0);
      await userEvent.click(within(dialog).getByRole("button", { name: "삭제" }));
      await waitFor(() => expect(lastCall(calls, "miniapp.notebook.delete")?.params).toEqual({ id: "latest" }));
      expect(await screen.findByRole("heading", { name: "이전 노트북" })).toBeInTheDocument();
    });

    it("keeps notebook confirmation open and content intact when deletion fails", async () => {
      calls = [];
      installNotebookGateway(calls, { "miniapp.notebook.delete": () => reply("delete denied", false) });
      renderNotebook();
      await screen.findByRole("heading", { name: "최신 노트북" });
      await userEvent.click(screen.getByRole("button", { name: "노트북 삭제" }));
      const dialog = screen.getByRole("dialog", { name: "노트북 삭제" });
      await userEvent.click(within(dialog).getByRole("button", { name: "삭제" }));
      expect(await screen.findByText(/HTTP 500/)).toBeInTheDocument();
      expect(dialog).toBeInTheDocument();
      expect(screen.getByRole("heading", { name: "최신 노트북" })).toBeInTheDocument();
    });
  });
});
