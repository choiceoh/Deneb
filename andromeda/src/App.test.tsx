import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { App } from "./App";
import { AIPanel } from "./components/AIPanel";
import { Workstation } from "./components/Workstation";
import { fakeProvider, renderWithProviders } from "./test/util";

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

  it("lands on the 오늘 dashboard and switches to a resource pane", async () => {
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

  it("expands the Deneb panel over the work pane and collapses back", async () => {
    renderWithProviders(<Workstation cfg={{ url: "http://test", token: "tok" }} />, {
      connected: true,
      dataProvider,
    });

    // Move to the 할일 pane so its "+ 새 할일" control proves the work pane is mounted.
    const nav = screen.getByRole("navigation");
    await userEvent.click(within(nav).getByRole("button", { name: /할일/ }));
    expect(await screen.findByRole("button", { name: /새 할일/ })).toBeInTheDocument();

    // Widen the Deneb panel → the center work pane is unmounted.
    await userEvent.click(screen.getByRole("button", { name: "패널 넓히기" }));
    expect(screen.queryByRole("button", { name: /새 할일/ })).not.toBeInTheDocument();

    // Collapse back → the work pane returns.
    await userEvent.click(screen.getByRole("button", { name: "패널 좁히기" }));
    expect(await screen.findByRole("button", { name: /새 할일/ })).toBeInTheDocument();
  });

  it("collapses the Deneb panel and reopens it from the edge tab", async () => {
    renderWithProviders(<Workstation cfg={{ url: "http://test", token: "tok" }} />, {
      connected: true,
      dataProvider,
    });
    const nav = screen.getByRole("navigation");
    await userEvent.click(within(nav).getByRole("button", { name: /할일/ }));

    // The side panel is visible → collapse it; its expand toggle disappears and a
    // reopen tab takes its place.
    await userEvent.click(await screen.findByRole("button", { name: "Deneb 패널 접기" }));
    expect(screen.queryByRole("button", { name: "패널 넓히기" })).not.toBeInTheDocument();

    // Reopen from the edge tab → the panel (its expand toggle) returns.
    await userEvent.click(screen.getByRole("button", { name: "Deneb 패널 열기" }));
    expect(await screen.findByRole("button", { name: "패널 넓히기" })).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Deneb 패널 열기" })).not.toBeInTheDocument();
  });

  it("keeps Ctrl+C for copy — editing combos never switch panes (code moved to Ctrl+D)", async () => {
    renderWithProviders(<Workstation cfg={{ url: "http://test", token: "tok" }} />, {
      connected: true,
      dataProvider,
    });
    expect(await screen.findByText("세금 신고")).toBeInTheDocument();

    // Ctrl+C(복사)는 화면 전환으로 가로채지 않는다 — 대시보드가 그대로 남는다.
    fireEvent.keyDown(window, { key: "c", ctrlKey: true });
    expect(screen.getByText("세금 신고")).toBeInTheDocument();

    // 코드 pane은 Ctrl+D로 이동했다 — 대시보드가 코드 pane으로 바뀐다.
    fireEvent.keyDown(window, { key: "d", ctrlKey: true });
    await waitFor(() => expect(screen.queryByText("세금 신고")).not.toBeInTheDocument());
  });

  it("opens the 비업무 채팅 탭 from the rail (center chat greets)", async () => {
    renderWithProviders(<Workstation cfg={{ url: "http://test", token: "tok" }} />, {
      connected: true,
      dataProvider,
    });
    // chat tab is always mounted (its conversation persists) but hidden until selected
    expect(screen.getByText("안녕하세요? 무슨 대화를 할까요?")).not.toBeVisible();
    const nav = screen.getByRole("navigation");
    await userEvent.click(within(nav).getByRole("button", { name: /채팅/ }));
    // selecting the rail tab reveals the center chat column
    expect(screen.getByText("안녕하세요? 무슨 대화를 할까요?")).toBeVisible();
  });

  it("opens a dashboard mail row directly in the mail pane", async () => {
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

  it("supports multiline AI prompts while plain Enter sends", async () => {
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

  it("attaches a file from the work panel with typed text as caption (client:main)", async () => {
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

  it("drops a file anywhere on the panel — subtle ring only while a drag is over it", async () => {
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

    await waitFor(() => expect(rpcCalls.some((c) => c.method === "miniapp.capture.image")).toBe(true));
    expect(rpcCalls.find((c) => c.method === "miniapp.capture.image")?.params).toMatchObject({
      mimeType: "image/png",
      sessionKey: "client:main",
    });
    expect(await screen.findByRole("group", { name: "첨부 분석 결과" })).toBeInTheDocument();
  });

  it("pastes a clipboard image into the composer as an attachment", async () => {
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

    await waitFor(() => expect(rpcCalls.some((c) => c.method === "miniapp.capture.image")).toBe(true));
    expect(rpcCalls.find((c) => c.method === "miniapp.capture.image")?.params).toMatchObject({
      mimeType: "image/png",
      sessionKey: "client:main",
    });
  });

  it("attaches multiple dropped files in order — caption on the first, unsupported skipped with a notice", async () => {
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

    // the unsupported file is skipped with a transient notice, not a silent drop
    expect(await screen.findByRole("status")).toHaveTextContent("clip.mp4");

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

    const pending = await screen.findByRole("button", { name: "첨부 분석 중" });
    expect(pending).toBeDisabled();
    expect(screen.queryByRole("button", { name: "중단" })).not.toBeInTheDocument();
  });
});
