import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
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

  it("binds a separate Deneb session per active work pane", async () => {
    const user = userEvent.setup();
    const bodies: { sessionKey?: string }[] = [];
    const fetchMock = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input);
      if (url.includes("/api/v1/miniapp/chat/stream")) {
        bodies.push(JSON.parse(String(init?.body ?? "{}")));
        return sseResponse('event: done\ndata: {"text":"네"}\n\n');
      }
      // Any RPC (sessions.recent/transcript, models.list) → an empty-but-ok envelope.
      // `sections: []` keeps ModelPicker happy; sessions/transcript read their own
      // (absent → []) fields, so one shape serves all three.
      return new Response(JSON.stringify({ ok: true, payload: { sections: [] } }), {
        status: 200,
        headers: { "Content-Type": "application/json" },
      });
    });
    vi.stubGlobal("fetch", fetchMock);

    renderWithProviders(<Workstation cfg={{ url: "http://test", token: "tok" }} />, {
      connected: true,
      dataProvider,
    });

    // 오늘(대시보드) = 메인 업무 피드 → client:main (게이트웨이 능동 리포트가 도착하는 곳).
    const composer = screen.getByRole("textbox", { name: "Deneb에게 메시지" });
    await user.type(composer, "안녕");
    await user.keyboard("{Enter}");
    await waitFor(() => expect(bodies.at(-1)?.sessionKey).toBe("client:main"));

    // 메일 화면으로 전환 → 그 화면 전용 세션 client:mail.
    await user.click(within(screen.getByRole("navigation")).getByRole("button", { name: /메일/ }));
    const mailComposer = screen.getByRole("textbox", { name: "Deneb에게 메시지" });
    await waitFor(() => expect(mailComposer).not.toBeDisabled());
    await user.type(mailComposer, "정리해줘");
    await user.keyboard("{Enter}");
    await waitFor(() => expect(bodies.at(-1)?.sessionKey).toBe("client:mail"));
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
    expect(await within(screen.getByLabelText("메일 상세")).findByText("본문까지 바로 열립니다.")).toBeInTheDocument();
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
});
