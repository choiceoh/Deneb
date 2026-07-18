import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { useEffect } from "react";
import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { App } from "./App";
import { AIPanel } from "./components/AIPanel";
import { Workstation } from "./components/Workstation";
import { fakeProvider, renderWithProviders } from "./test/util";
import { useWorkspace } from "./workspaceContext";

beforeEach(() => {
  localStorage.clear();
  // Sidebar pings the gateway when connected; keep tests offline & deterministic.
  vi.stubGlobal(
    "fetch",
    vi.fn(() => Promise.reject(new Error("offline test"))),
  );
});
afterEach(() => {
  vi.unstubAllGlobals();
});

function sseResponse(body = ""): Response {
  const enc = new TextEncoder();
  const stream = new ReadableStream<Uint8Array>({
    start(controller) {
      if (body) controller.enqueue(enc.encode(body));
      controller.close();
    },
  });
  return new Response(stream, { status: 200, headers: { "Content-Type": "text/event-stream" } });
}

describe("App (disconnected)", () => {
  it("renders the workstation shell with registry-driven nav", () => {
    renderWithProviders(<App />);
    expect(screen.getByRole("navigation")).toBeInTheDocument();
    for (const label of ["채팅", "할일", "노트북", "메일", "일정"]) {
      expect(screen.getByRole("button", { name: new RegExp(label) })).toBeInTheDocument();
    }
    expect(screen.getByText(/미연결/)).toBeInTheDocument();
  });
});

describe("Workstation (connected, fixtures)", () => {
  const dataProvider = fakeProvider({
    todo: [{ id: 1, title: "세금 신고", done: false }],
    mail: [{ id: "m1", subject: "분기 보고서", from: "lead@corp.com" }],
  });

  it("switches to todo pane when rail selects 할일 after dashboard landing", async () => {
    renderWithProviders(<Workstation cfg={{ url: "http://test", token: "tok" }} />, {
      connected: true,
      dataProvider,
    });
    // The dashboard is the landing view and aggregates several resources at once.
    expect(await screen.findByText("세금 신고")).toBeInTheDocument();
    expect(screen.getByText(/분기 보고서/)).toBeInTheDocument();

    // The dashboard has no add-todo control; the 할일 pane's "+ 새 할일" button does — proves the switch.
    // Scope the nav click to the sidebar (the dashboard also has a 할일 card button).
    expect(screen.queryByRole("button", { name: /새 할일/ })).not.toBeInTheDocument();
    const nav = screen.getByRole("navigation");
    await userEvent.click(within(nav).getByRole("button", { name: /할일/ }));
    expect(await screen.findByRole("button", { name: /새 할일/ })).toBeInTheDocument();
  });

  it("hides work pane when panel expands and restores it when narrowed", async () => {
    renderWithProviders(<Workstation cfg={{ url: "http://test", token: "tok" }} />, {
      connected: true,
      dataProvider,
    });

    // Move to the 할일 pane so its "+ 새 할일" control proves the work pane is mounted.
    const nav = screen.getByRole("navigation");
    await userEvent.click(within(nav).getByRole("button", { name: /할일/ }));
    expect(await screen.findByRole("button", { name: /새 할일/ })).toBeInTheDocument();

    // 패널은 기본 접힘 — 우측 탭으로 먼저 연다.
    await userEvent.click(screen.getByRole("button", { name: "Deneb 패널 열기" }));

    // Widen the Deneb panel → the center work pane is unmounted.
    await userEvent.click(screen.getByRole("button", { name: "패널 넓히기" }));
    expect(screen.queryByRole("button", { name: /새 할일/ })).not.toBeInTheDocument();

    // Collapse back → the work pane returns.
    await userEvent.click(screen.getByRole("button", { name: "패널 좁히기" }));
    expect(await screen.findByRole("button", { name: /새 할일/ })).toBeInTheDocument();
  });

  it("hides panel when collapsed and restores it when edge tab opens", async () => {
    renderWithProviders(<Workstation cfg={{ url: "http://test", token: "tok" }} />, {
      connected: true,
      dataProvider,
    });
    const nav = screen.getByRole("navigation");
    await userEvent.click(within(nav).getByRole("button", { name: /할일/ }));

    // 패널은 기본 접힘 — 우측 탭으로 연 뒤 접기·재열기 동작을 검증한다.
    await userEvent.click(await screen.findByRole("button", { name: "Deneb 패널 열기" }));

    // The side panel is now visible → collapse it; its expand toggle disappears and a
    // reopen tab takes its place.
    await userEvent.click(await screen.findByRole("button", { name: "Deneb 패널 접기" }));
    expect(screen.queryByRole("button", { name: "패널 넓히기" })).not.toBeInTheDocument();

    // Reopen from the edge tab → the panel (its expand toggle) returns.
    await userEvent.click(screen.getByRole("button", { name: "Deneb 패널 열기" }));
    expect(await screen.findByRole("button", { name: "패널 넓히기" })).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Deneb 패널 열기" })).not.toBeInTheDocument();
  });

  it("preserves dashboard view when Ctrl+C pressed without switching panes", async () => {
    renderWithProviders(<Workstation cfg={{ url: "http://test", token: "tok" }} />, {
      connected: true,
      dataProvider,
    });
    expect(await screen.findByText("세금 신고")).toBeInTheDocument();

    // Ctrl+C(복사)는 화면 전환으로 가로채지 않는다 — 대시보드가 그대로 남는다.
    fireEvent.keyDown(window, { key: "c", ctrlKey: true });
    expect(screen.getByText("세금 신고")).toBeInTheDocument();

    // 대조군: 편집 계열이 아닌 pane 단축키(⌘/Ctrl+9 = 피드)는 정상적으로 화면을 전환한다.
    fireEvent.keyDown(window, { key: "9", ctrlKey: true });
    await waitFor(() => expect(screen.queryByText("세금 신고")).not.toBeInTheDocument());
  });

  it("reveals center chat when rail selects 채팅 tab", async () => {
    renderWithProviders(<Workstation cfg={{ url: "http://test", token: "tok" }} />, {
      connected: true,
      dataProvider,
    });
    // chat tab is always mounted (its conversation persists) but hidden until selected
    expect(screen.getByText(/^선택님, /)).not.toBeVisible();
    const nav = screen.getByRole("navigation");
    await userEvent.click(within(nav).getByRole("button", { name: /채팅/ }));
    // selecting the rail tab reveals the center chat column
    expect(screen.getByText(/^선택님, /)).toBeVisible();
  });

  it("opens mail pane with body when dashboard row is clicked", async () => {
    renderWithProviders(<Workstation cfg={{ url: "http://test", token: "tok" }} />, {
      connected: true,
      dataProvider: fakeProvider({
        mail: [{ id: "m1", subject: "분기 보고서", from: "lead@corp.com", body: "본문까지 바로 열립니다." }],
      }),
    });

    await userEvent.click(await screen.findByRole("button", { name: /분기 보고서/ }));

    expect(await screen.findByRole("heading", { name: "메일" })).toBeInTheDocument();
    const detail = await screen.findByLabelText("메일 상세");
    // The body lives behind the 본문 tab now (분석 is the default view).
    await userEvent.click(within(detail).getByRole("button", { name: "본문" }));
    expect(await within(detail).findByText("본문까지 바로 열립니다.")).toBeInTheDocument();
  });

  it("preserves multiline input on Shift+Enter and sends on plain Enter", async () => {
    const user = userEvent.setup();
    const fetchMock = vi.fn(async (input: RequestInfo | URL) => {
      const url = String(input);
      if (url.includes("/api/v1/miniapp/chat/stream")) {
        return sseResponse('event: delta\ndata: {"delta":"완료"}\n\nevent: done\ndata: {"text":"완료"}\n\n');
      }
      return sseResponse();
    });
    vi.stubGlobal("fetch", fetchMock);

    renderWithProviders(<AIPanel cfg={{ url: "http://test", token: "tok" }} />, {
      connected: true,
    });

    const composer = screen.getByRole("textbox", { name: "Deneb에게 메시지" });
    await user.type(composer, "첫 줄");
    await user.keyboard("{Shift>}{Enter}{/Shift}");
    await user.type(composer, "둘째 줄");

    expect(composer).toHaveValue("첫 줄\n둘째 줄");
    expect(fetchMock.mock.calls.filter(([url]) => String(url).includes("/api/v1/miniapp/chat/stream"))).toHaveLength(0);

    await user.keyboard("{Enter}");

    await waitFor(() =>
      expect(fetchMock.mock.calls.filter(([url]) => String(url).includes("/api/v1/miniapp/chat/stream"))).toHaveLength(
        1,
      ),
    );
    expect(composer).toHaveValue("");
    expect(screen.getByText(/첫 줄/)).toBeInTheDocument();
    expect(screen.getByText(/둘째 줄/)).toBeInTheDocument();
    expect(await screen.findByText("완료")).toBeInTheDocument();
  });

  it("renders the assistant reply as Markdown and tool calls as chips", async () => {
    const user = userEvent.setup();
    const fetchMock = vi.fn(async (input: RequestInfo | URL) => {
      const url = String(input);
      if (url.includes("/api/v1/miniapp/chat/stream")) {
        return sseResponse(
          'event: delta\ndata: {"delta":"**완료**했습니다."}\n\n' +
            'event: tool\ndata: {"state":"started","tool":"gmail.list_recent","toolUseId":"tu1"}\n\n' +
            'event: tool\ndata: {"state":"completed","tool":"gmail.list_recent","toolUseId":"tu1","detail":"메일 3건"}\n\n' +
            'event: done\ndata: {"text":"**완료**했습니다."}\n\n',
        );
      }
      return sseResponse();
    });
    vi.stubGlobal("fetch", fetchMock);

    renderWithProviders(<AIPanel cfg={{ url: "http://test", token: "tok" }} />, { connected: true });

    const composer = screen.getByRole("textbox", { name: "Deneb에게 메시지" });
    await user.type(composer, "메일 정리해줘");
    await user.keyboard("{Enter}");

    // Markdown: the reply's **완료** becomes a <strong>, not literal asterisks.
    const bold = await screen.findByText("완료");
    expect(bold.tagName).toBe("STRONG");
    // Tool chip: the gateway's tool frame renders as a labelled chip with its detail.
    expect(screen.getByText("gmail list recent")).toBeInTheDocument();
    expect(screen.getByText("메일 3건")).toBeInTheDocument();
  });

  it("attaches file with caption to client:main when work panel upload completes", async () => {
    const rpcCalls: Array<{ method: string; params: Record<string, unknown> }> = [];
    vi.stubGlobal(
      "fetch",
      vi.fn(async (_url: string, init?: RequestInit) => {
        const body = JSON.parse(String(init?.body ?? "{}")) as {
          method?: string;
          params?: Record<string, unknown>;
        };
        const method = String(body.method ?? "");
        rpcCalls.push({ method, params: body.params ?? {} });
        const payload =
          method === "miniapp.models.list"
            ? { current: "", sections: [] }
            : method === "miniapp.sessions.recent"
              ? { sessions: [], count: 0 }
              : method === "miniapp.capture.image"
                ? { text: "견적 금액은 1,200만원" }
                : {};
        return new Response(JSON.stringify({ ok: true, payload }), {
          status: 200,
          headers: { "Content-Type": "application/json" },
        });
      }),
    );

    const user = userEvent.setup();
    renderWithProviders(<AIPanel cfg={{ url: "http://test", token: "tok" }} />, { connected: true });

    // The work panel offers the same attach affordance as the chat tab.
    expect(screen.getByRole("button", { name: "파일 첨부" })).toBeInTheDocument();

    const composer = screen.getByRole("textbox", { name: "Deneb에게 메시지" });
    await user.type(composer, "이 견적서에서 금액만 찾아줘");
    const input = document.querySelector('input[type="file"]') as HTMLInputElement;
    await user.upload(input, new File(["fake image"], "quote.png", { type: "image/png" }));

    // 스테이징 칩으로 대기 → 전송 버튼이 배치를 나른다 (즉시 업로드 아님).
    expect(await screen.findByRole("group", { name: "첨부 대기 파일" })).toBeInTheDocument();
    expect(rpcCalls.some((c) => c.method === "miniapp.capture.image")).toBe(false);
    await user.click(screen.getByRole("button", { name: "전송" }));

    await waitFor(() => expect(rpcCalls.some((c) => c.method === "miniapp.capture.image")).toBe(true));
    const captureCall = rpcCalls.find((c) => c.method === "miniapp.capture.image");
    // Lands in the panel's own client:main session (not the chat tab's chat:*).
    expect(captureCall?.params).toMatchObject({
      mimeType: "image/png",
      sessionKey: "client:main",
      caption: "이 견적서에서 금액만 찾아줘",
    });
    expect(composer).toHaveValue("");
    const result = await screen.findByRole("group", { name: "첨부 분석 결과" });
    expect(within(result).getByText("quote.png")).toBeInTheDocument();
    expect(within(result).getByText("견적 금액은 1,200만원")).toBeInTheDocument();
  });

  it("shows drop ring when drag enters and attaches via capture when dropped", async () => {
    const rpcCalls: Array<{ method: string; params: Record<string, unknown> }> = [];
    vi.stubGlobal(
      "fetch",
      vi.fn(async (_url: string, init?: RequestInit) => {
        const body = JSON.parse(String(init?.body ?? "{}")) as {
          method?: string;
          params?: Record<string, unknown>;
        };
        const method = String(body.method ?? "");
        rpcCalls.push({ method, params: body.params ?? {} });
        const payload =
          method === "miniapp.models.list"
            ? { current: "", sections: [] }
            : method === "miniapp.sessions.recent"
              ? { sessions: [], count: 0 }
              : method === "miniapp.capture.image"
                ? { text: "현장 사진 분석" }
                : {};
        return new Response(JSON.stringify({ ok: true, payload }), {
          status: 200,
          headers: { "Content-Type": "application/json" },
        });
      }),
    );

    renderWithProviders(<AIPanel cfg={{ url: "http://test", token: "tok" }} />, { connected: true });

    // The whole aside is the drop zone; at rest it shows no drop chrome at all.
    const panel = screen.getByRole("complementary");
    const dt = { files: [new File(["fake image"], "site.png", { type: "image/png" })], types: ["Files"] };
    expect(panel).not.toHaveClass("drop-over");

    // The subtle ring appears only while a file drag is over the zone…
    fireEvent.dragEnter(panel, { dataTransfer: dt });
    expect(panel).toHaveClass("drop-over");
    fireEvent.dragLeave(panel, { dataTransfer: dt });
    expect(panel).not.toHaveClass("drop-over");

    // …and dropping attaches through the same capture path as the clip button.
    fireEvent.dragEnter(panel, { dataTransfer: dt });
    fireEvent.drop(panel, { dataTransfer: dt });
    expect(panel).not.toHaveClass("drop-over");

    // 드롭은 스테이징까지 — 전송 버튼이 capture를 발사한다.
    await screen.findByRole("group", { name: "첨부 대기 파일" });
    fireEvent.click(screen.getByRole("button", { name: "전송" }));

    await waitFor(() => expect(rpcCalls.some((c) => c.method === "miniapp.capture.image")).toBe(true));
    expect(rpcCalls.find((c) => c.method === "miniapp.capture.image")?.params).toMatchObject({
      mimeType: "image/png",
      sessionKey: "client:main",
    });
    expect(await screen.findByRole("group", { name: "첨부 분석 결과" })).toBeInTheDocument();
  });

  it("attaches clipboard image via capture when paste contains files", async () => {
    const rpcCalls: Array<{ method: string; params: Record<string, unknown> }> = [];
    vi.stubGlobal(
      "fetch",
      vi.fn(async (_url: string, init?: RequestInit) => {
        const body = JSON.parse(String(init?.body ?? "{}")) as {
          method?: string;
          params?: Record<string, unknown>;
        };
        const method = String(body.method ?? "");
        rpcCalls.push({ method, params: body.params ?? {} });
        const payload =
          method === "miniapp.models.list"
            ? { current: "", sections: [] }
            : method === "miniapp.sessions.recent"
              ? { sessions: [], count: 0 }
              : method === "miniapp.capture.image"
                ? { text: "붙여넣기 분석" }
                : {};
        return new Response(JSON.stringify({ ok: true, payload }), {
          status: 200,
          headers: { "Content-Type": "application/json" },
        });
      }),
    );

    renderWithProviders(<AIPanel cfg={{ url: "http://test", token: "tok" }} />, { connected: true });

    const composer = screen.getByRole("textbox", { name: "Deneb에게 메시지" });
    fireEvent.paste(composer, {
      clipboardData: { files: [new File(["img"], "screenshot.png", { type: "image/png" })] },
    });

    // 붙여넣기도 스테이징까지 — 전송이 capture를 발사한다.
    await screen.findByRole("group", { name: "첨부 대기 파일" });
    fireEvent.click(screen.getByRole("button", { name: "전송" }));

    await waitFor(() => expect(rpcCalls.some((c) => c.method === "miniapp.capture.image")).toBe(true));
    expect(rpcCalls.find((c) => c.method === "miniapp.capture.image")?.params).toMatchObject({
      mimeType: "image/png",
      sessionKey: "client:main",
    });
  });

  it("attaches files in order with caption on first and rejects unsupported with notice", async () => {
    const rpcCalls: Array<{ method: string; params: Record<string, unknown> }> = [];
    vi.stubGlobal(
      "fetch",
      vi.fn(async (_url: string, init?: RequestInit) => {
        const body = JSON.parse(String(init?.body ?? "{}")) as {
          method?: string;
          params?: Record<string, unknown>;
        };
        const method = String(body.method ?? "");
        rpcCalls.push({ method, params: body.params ?? {} });
        const payload =
          method === "miniapp.models.list"
            ? { current: "", sections: [] }
            : method === "miniapp.sessions.recent"
              ? { sessions: [], count: 0 }
              : method === "miniapp.capture.image"
                ? { text: "이미지 ok" }
                : method === "miniapp.capture.document"
                  ? { text: "문서 ok" }
                  : {};
        return new Response(JSON.stringify({ ok: true, payload }), {
          status: 200,
          headers: { "Content-Type": "application/json" },
        });
      }),
    );

    const user = userEvent.setup();
    renderWithProviders(<AIPanel cfg={{ url: "http://test", token: "tok" }} />, { connected: true });

    const composer = screen.getByRole("textbox", { name: "Deneb에게 메시지" });
    await user.type(composer, "둘 다 검토해줘");
    const panel = screen.getByRole("complementary");
    const files = [
      new File(["i"], "quote.png", { type: "image/png" }),
      new File(["p"], "contract.pdf", { type: "application/pdf" }),
      new File(["v"], "clip.mp4", { type: "video/mp4" }),
    ];
    fireEvent.drop(panel, { dataTransfer: { files, types: ["Files"] } });

    // the unsupported file is skipped with a transient notice at staging time
    expect(await screen.findByRole("status")).toHaveTextContent("clip.mp4");

    // 지원 파일 2개는 칩으로 대기 — 전송 버튼이 배치를 나른다.
    const chips = await screen.findByRole("group", { name: "첨부 대기 파일" });
    expect(within(chips).getByText("quote.png")).toBeInTheDocument();
    expect(within(chips).getByText("contract.pdf")).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "전송" }));

    await waitFor(() => expect(rpcCalls.filter((c) => c.method.startsWith("miniapp.capture.")).length).toBe(2));
    const captures = rpcCalls.filter((c) => c.method.startsWith("miniapp.capture."));
    expect(captures.map((c) => c.method)).toEqual(["miniapp.capture.image", "miniapp.capture.document"]);
    // the typed text rides as the caption of the first attachable file only
    expect(captures[0].params).toMatchObject({ caption: "둘 다 검토해줘" });
    expect(captures[1].params).not.toHaveProperty("caption");
    expect(composer).toHaveValue("");
  });

  it("returns focus to the composer once the reply finishes", async () => {
    const user = userEvent.setup();
    const fetchMock = vi.fn(async (input: RequestInfo | URL) => {
      const url = String(input);
      if (url.includes("/api/v1/miniapp/chat/stream")) {
        return sseResponse('event: delta\ndata: {"delta":"완료"}\n\nevent: done\ndata: {"text":"완료"}\n\n');
      }
      return sseResponse();
    });
    vi.stubGlobal("fetch", fetchMock);

    renderWithProviders(<AIPanel cfg={{ url: "http://test", token: "tok" }} />, { connected: true });

    const composer = screen.getByRole("textbox", { name: "Deneb에게 메시지" });
    await user.type(composer, "안녕");
    await user.keyboard("{Enter}");

    // busy 동안 disabled로 포커스를 잃지만, 턴이 끝나면 자동 복구되어 바로 이어서 칠 수 있다
    await screen.findByText("완료");
    await waitFor(() => expect(composer).toHaveFocus());
    expect(composer).not.toBeDisabled();
  });

  it("preserves user focus when history button selected during attachment turn", async () => {
    let release: () => void = () => {};
    const gate = new Promise<void>((r) => {
      release = r;
    });
    vi.stubGlobal(
      "fetch",
      vi.fn(async (_url: string, init?: RequestInit) => {
        const body = JSON.parse(String(init?.body ?? "{}")) as { method?: string };
        const method = String(body.method ?? "");
        if (method === "miniapp.capture.image") {
          await gate; // hold the turn open until the test moves focus
          return new Response(JSON.stringify({ ok: true, payload: { text: "분석 완료" } }), {
            status: 200,
            headers: { "Content-Type": "application/json" },
          });
        }
        const payload =
          method === "miniapp.models.list"
            ? { current: "", sections: [] }
            : method === "miniapp.sessions.recent"
              ? { sessions: [], count: 0 }
              : {};
        return new Response(JSON.stringify({ ok: true, payload }), {
          status: 200,
          headers: { "Content-Type": "application/json" },
        });
      }),
    );

    renderWithProviders(<AIPanel cfg={{ url: "http://test", token: "tok" }} />, { connected: true });

    const panel = screen.getByRole("complementary");
    fireEvent.drop(panel, {
      dataTransfer: { files: [new File(["i"], "a.png", { type: "image/png" })], types: ["Files"] },
    });
    await screen.findByRole("group", { name: "첨부 대기 파일" });
    fireEvent.click(screen.getByRole("button", { name: "전송" }));
    // busy: the non-stop attach state replaces the send button
    await screen.findByRole("button", { name: "첨부 분석 중" });

    // the user moves focus elsewhere mid-turn…
    const historyBtn = screen.getByRole("button", { name: "대화 기록" });
    historyBtn.focus();
    expect(historyBtn).toHaveFocus();

    // …then the turn finishes: focus must STAY where the user put it.
    release();
    await screen.findByRole("group", { name: "첨부 분석 결과" });
    const composer = screen.getByRole("textbox", { name: "Deneb에게 메시지" });
    await waitFor(() => expect(composer).not.toBeDisabled());
    expect(historyBtn).toHaveFocus();
    expect(composer).not.toHaveFocus();
  });

  it("marks 노트로 저장됨 only when the notebook sink succeeds — a failure stays retryable", async () => {
    const user = userEvent.setup();
    const fetchMock = vi.fn(async (input: RequestInfo | URL) => {
      const url = String(input);
      if (url.includes("/api/v1/miniapp/chat/stream")) {
        return sseResponse('event: delta\ndata: {"delta":"요약 완료"}\n\nevent: done\ndata: {"text":"요약 완료"}\n\n');
      }
      return sseResponse();
    });
    vi.stubGlobal("fetch", fetchMock);

    // The notebook's sink, first failing (RPC error) then succeeding on retry.
    const sink = vi.fn<(text: string) => Promise<boolean>>().mockResolvedValueOnce(false).mockResolvedValue(true);
    function SinkSetter() {
      const { setNoteSink } = useWorkspace();
      useEffect(() => {
        setNoteSink(sink);
        return () => setNoteSink(null);
        // eslint-disable-next-line react-hooks/exhaustive-deps
      }, []);
      return null;
    }

    renderWithProviders(
      <>
        <AIPanel cfg={{ url: "http://test", token: "tok" }} />
        <SinkSetter />
      </>,
      { connected: true },
    );

    const composer = screen.getByRole("textbox", { name: "Deneb에게 메시지" });
    await user.type(composer, "요약해줘");
    await user.keyboard("{Enter}");
    await screen.findByText("요약 완료");

    // Save into the notebook → the sink fails → NOT marked 저장됨; the button
    // reports the failure and stays clickable for a retry.
    await user.click(await screen.findByRole("button", { name: /노트에 저장/ }));
    const retryBtn = await screen.findByRole("button", { name: /저장 실패/ });
    expect(retryBtn).toBeEnabled();
    expect(screen.queryByRole("button", { name: /노트로 저장됨/ })).not.toBeInTheDocument();
    expect(sink).toHaveBeenCalledWith("요약 완료");

    // Retry → success → the done state locks out double-pinning.
    await user.click(retryBtn);
    expect(await screen.findByRole("button", { name: /노트로 저장됨/ })).toBeDisabled();
    expect(sink).toHaveBeenCalledTimes(2);
  });

  it("shows a non-stop state while an attachment is being analyzed (capture is not abortable)", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async (_url: string, init?: RequestInit) => {
        const body = JSON.parse(String(init?.body ?? "{}")) as { method?: string };
        const method = String(body.method ?? "");
        if (method === "miniapp.capture.image") return new Promise<Response>(() => {}); // still in flight
        const payload =
          method === "miniapp.models.list"
            ? { current: "", sections: [] }
            : method === "miniapp.sessions.recent"
              ? { sessions: [], count: 0 }
              : {};
        return new Response(JSON.stringify({ ok: true, payload }), {
          status: 200,
          headers: { "Content-Type": "application/json" },
        });
      }),
    );

    renderWithProviders(<AIPanel cfg={{ url: "http://test", token: "tok" }} />, { connected: true });

    const panel = screen.getByRole("complementary");
    fireEvent.drop(panel, {
      dataTransfer: { files: [new File(["i"], "a.png", { type: "image/png" })], types: ["Files"] },
    });
    await screen.findByRole("group", { name: "첨부 대기 파일" });
    fireEvent.click(screen.getByRole("button", { name: "전송" }));

    const pending = await screen.findByRole("button", { name: "첨부 분석 중" });
    expect(pending).toBeDisabled();
    expect(screen.queryByRole("button", { name: "중단" })).not.toBeInTheDocument();
  });
});
