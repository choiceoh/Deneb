import { type ReactNode } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";

import type { ChatState, ChatTurn } from "@/hooks";

const mocks = vi.hoisted(() => ({
  useChat: vi.fn(),
  useModels: vi.fn(),
  useComposerBehavior: vi.fn(),
  useAttachPipeline: vi.fn(),
  useSessions: vi.fn(),
  useStickyScroll: vi.fn(),
  useFileDrop: vi.fn(),
  proactivePanel: vi.fn(),
}));

vi.mock("@/hooks", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@/hooks")>();
  return { ...actual, useChat: mocks.useChat };
});

vi.mock("@/useChatSurface", () => ({
  useModels: mocks.useModels,
  useComposerBehavior: mocks.useComposerBehavior,
  useAttachPipeline: mocks.useAttachPipeline,
}));

vi.mock("@/useSessions", () => ({ useSessions: mocks.useSessions }));
vi.mock("@/useStickyScroll", () => ({ useStickyScroll: mocks.useStickyScroll }));
vi.mock("@/useFileDrop", () => ({ useFileDrop: mocks.useFileDrop }));
vi.mock("./ProactivePanel", () => ({
  ProactivePanel: (props: unknown) => {
    mocks.proactivePanel(props);
    return <div data-testid="proactive-panel" />;
  },
}));

import { AIPanel } from "./AIPanel";
import { Ctx, FeedCtx } from "@/workspaceContext";
import { feedValue, testCfg, workspaceValue, type WorkspaceValue } from "@/test/workspace";

const cfg = testCfg;

const workspace = workspaceValue;

// AI 피드는 별도 컨텍스트(FeedCtx) — 이 스위트의 기본 피드는 위키 설계 메모를 흉내낸다.
function wrapper(value: WorkspaceValue, children: ReactNode) {
  return (
    <Ctx.Provider value={value}>
      <FeedCtx.Provider value={feedValue({ aiText: "현재 프로젝트의 설계 메모", activeResource: "wiki" })}>
        {children}
      </FeedCtx.Provider>
    </Ctx.Provider>
  );
}

const assistantTurn: ChatTurn = {
  id: "assistant-1",
  role: "assistant",
  text: "설계를 검토했습니다.",
  parts: [{ kind: "text", text: "설계를 검토했습니다." }],
  status: "done",
};

const userTurn: ChatTurn = {
  id: "user-1",
  role: "user",
  text: "이 설계를 봐줘",
  status: "done",
};

function chatState(overrides: Partial<ChatState> = {}): ChatState {
  return {
    thinking: "",
    busy: false,
    stoppable: false,
    turns: [],
    send: vi.fn(async () => {}),
    capture: vi.fn(async () => {}),
    stop: vi.fn(),
    regenerate: vi.fn(),
    editResend: vi.fn(),
    variants: null,
    selectVariant: vi.fn(),
    clear: vi.fn(),
    setTurns: vi.fn(),
    ...overrides,
  };
}

const sessionActions = {
  sessions: [],
  sessionKey: "client:main:panel",
  sessionsOpen: false,
  sessionErr: "",
  toggleSessions: vi.fn(),
  selectSession: vi.fn(),
  removeSession: vi.fn(),
  newChat: vi.fn(),
};

beforeEach(() => {
  vi.clearAllMocks();
  mocks.useChat.mockReturnValue(chatState());
  mocks.useModels.mockReturnValue({
    models: {
      current: "provider/smart",
      roles: [],
      sections: [{ title: "General", models: [{ id: "provider/smart", label: "Smart" }] }],
    },
    model: "provider/smart",
    setModel: vi.fn(),
  });
  mocks.useAttachPipeline.mockReturnValue({
    attachNote: "",
    attachingRef: { current: false },
    attachFiles: vi.fn(async () => {}),
    onPick: vi.fn(),
  });
  mocks.useSessions.mockReturnValue({ ...sessionActions });
  mocks.useStickyScroll.mockReturnValue({
    ref: { current: null },
    onScroll: vi.fn(),
    pin: vi.fn(),
    atBottom: true,
    scrollToBottom: vi.fn(),
  });
  mocks.useFileDrop.mockReturnValue({ over: false, dropProps: {} });
});

describe("AIPanel shell", () => {
  it("renders the collaboration panel and gateway-connected chrome", () => {
    render(wrapper(workspace(), <AIPanel cfg={cfg} />));

    expect(screen.getByText("Deneb AI")).toBeInTheDocument();
    expect(screen.getByRole("log", { name: "Deneb 대화", hidden: true })).toBeInTheDocument();
    expect(screen.getByRole("combobox", { name: "모델 선택" })).toHaveValue("provider/smart");
    expect(screen.getByTestId("proactive-panel")).toBeInTheDocument();
    expect(mocks.proactivePanel).toHaveBeenCalledWith({ cfg });
  });

  it("shows an honest disconnected empty state", () => {
    render(wrapper(workspace({ connected: false }), <AIPanel cfg={cfg} />));
    expect(screen.getByText("게이트웨이 연결 대기 중")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "전송" })).toBeDisabled();
    expect(screen.getByRole("button", { name: "파일 첨부" })).toBeDisabled();
  });

  it("pitches the screen-aware suggestions on a connected empty transcript", () => {
    render(wrapper(workspace(), <AIPanel cfg={cfg} />));
    expect(screen.queryByText("게이트웨이 연결 대기 중")).not.toBeInTheDocument();
    expect(screen.getByText("지금 보고 있는 화면을 함께 봅니다")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "이 화면 요약해줘" })).toBeEnabled();
  });

  it("hides without unmounting when the parent changes views", () => {
    const { container } = render(wrapper(workspace(), <AIPanel cfg={cfg} hidden />));
    expect(container.querySelector("aside")).toHaveStyle({ display: "none" });
    expect(screen.getByRole("log", { name: "Deneb 대화", hidden: true })).toBeInTheDocument();
  });

  it("when uses fixed side-panel sizing by default", () => {
    const { container } = render(wrapper(workspace(), <AIPanel cfg={cfg} />));
    expect(container.querySelector("aside")).toHaveStyle({ width: "var(--ai-w)", flex: "0 0 auto" });
    expect(container.querySelector("aside")).not.toHaveClass("ai-expanded", "ai-bottom");
  });

  it("when expands across the available work area", () => {
    const { container } = render(wrapper(workspace(), <AIPanel cfg={cfg} expanded />));
    expect(container.querySelector("aside")).toHaveClass("ai-expanded");
    expect(container.querySelector("aside")).toHaveStyle({ width: "auto", flex: "1 1 auto" });
  });

  it("when uses docked sizing for notebook placement", () => {
    const { container } = render(wrapper(workspace(), <AIPanel cfg={cfg} placement="bottom" />));
    expect(container.querySelector("aside")).toHaveClass("ai-bottom");
    expect(container.querySelector("aside")).toHaveStyle({ minHeight: "0", padding: "12px 16px" });
    expect(container.querySelector("aside")).not.toHaveStyle({ width: "var(--ai-w)" });
  });

  it("when surfaces active file drag state", () => {
    mocks.useFileDrop.mockReturnValue({ over: true, dropProps: { title: "drop target" } });
    const { container } = render(wrapper(workspace(), <AIPanel cfg={cfg} />));
    expect(container.querySelector("aside")).toHaveClass("drop-over");
    expect(container.querySelector("aside")).toHaveAttribute("title", "drop target");
  });

  it("when invokes expand and collapse shell controls", async () => {
    const onToggleExpand = vi.fn();
    const onCollapse = vi.fn();
    render(wrapper(workspace(), <AIPanel cfg={cfg} onToggleExpand={onToggleExpand} onCollapse={onCollapse} />));

    await userEvent.click(screen.getByRole("button", { name: "패널 넓히기" }));
    await userEvent.click(screen.getByRole("button", { name: "Deneb 패널 접기" }));

    expect(onToggleExpand).toHaveBeenCalledTimes(1);
    expect(onCollapse).toHaveBeenCalledTimes(1);
  });

  it("changes the expand affordance in expanded mode and without collapse", () => {
    render(wrapper(workspace(), <AIPanel cfg={cfg} expanded onToggleExpand={() => {}} onCollapse={() => {}} />));
    expect(screen.getByRole("button", { name: "패널 좁히기" })).toHaveAttribute("aria-pressed", "true");
    expect(screen.queryByRole("button", { name: "Deneb 패널 접기" })).not.toBeInTheDocument();
  });
});

describe("AIPanel conversations", () => {
  it("renders user and assistant turns with stable labels", () => {
    mocks.useChat.mockReturnValue(chatState({ turns: [userTurn, assistantTurn] }));
    render(wrapper(workspace(), <AIPanel cfg={cfg} />));

    expect(screen.getByText("나")).toBeInTheDocument();
    expect(screen.getByText("이 설계를 봐줘")).toBeInTheDocument();
    expect(screen.getByText("Deneb")).toBeInTheDocument();
    expect(screen.getByText("설계를 검토했습니다.")).toBeInTheDocument();
  });

  it("submits trimmed input with workspace, resource, model, and session context", async () => {
    const send = vi.fn(async () => {});
    mocks.useChat.mockReturnValue(chatState({ send }));
    const sticky = mocks.useStickyScroll();
    render(wrapper(workspace(), <AIPanel cfg={cfg} />));
    const editor = screen.getByRole("textbox", { name: "Deneb에게 메시지" });

    await userEvent.type(editor, "  검토해줘  ");
    await userEvent.click(screen.getByRole("button", { name: "전송" }));

    expect(send).toHaveBeenCalledWith("검토해줘", {
      workspaceContext: "현재 프로젝트의 설계 메모",
      activeResource: "wiki",
      model: "provider/smart",
      sessionKey: "client:main:panel",
    });
    expect(sticky.pin).toHaveBeenCalledTimes(1);
    expect(editor).toHaveValue("");
  });

  it.each([
    [false, false],
    [true, true],
  ])("does not submit when connected=%s busy=%s", async (connected, busy) => {
    const send = vi.fn(async () => {});
    mocks.useChat.mockReturnValue(chatState({ send, busy }));
    render(wrapper(workspace({ connected }), <AIPanel cfg={cfg} />));
    const editor = screen.getByRole("textbox", { name: "Deneb에게 메시지" });
    if (!busy) await userEvent.type(editor, "질문");
    expect(screen.getByRole("button", { name: busy ? "첨부 분석 중" : "전송" })).toBeDisabled();
    expect(send).not.toHaveBeenCalled();
  });

  it("routes stop and regenerate actions to chat state", async () => {
    const stop = vi.fn();
    const regenerate = vi.fn();
    mocks.useChat.mockReturnValue(
      chatState({
        busy: true,
        stoppable: true,
        stop,
        regenerate,
        turns: [assistantTurn],
      }),
    );
    const { rerender } = render(wrapper(workspace(), <AIPanel cfg={cfg} />));
    await userEvent.click(screen.getByRole("button", { name: "중단" }));
    expect(stop).toHaveBeenCalledTimes(1);

    mocks.useChat.mockReturnValue(chatState({ regenerate, turns: [assistantTurn] }));
    rerender(wrapper(workspace(), <AIPanel cfg={cfg} />));
    await userEvent.click(screen.getByRole("button", { name: /다시 생성/ }));
    expect(regenerate).toHaveBeenCalledTimes(1);
  });

  it("shows thinking after content has started streaming", () => {
    mocks.useChat.mockReturnValue(
      chatState({
        thinking: "달력 확인 중",
        busy: true,
        turns: [
          {
            ...assistantTurn,
            status: "streaming",
            text: "부분",
            parts: [{ kind: "text", text: "부분" }],
          },
        ],
      }),
    );
    render(wrapper(workspace(), <AIPanel cfg={cfg} />));
    expect(screen.getByText("달력 확인 중…")).toHaveClass("ai-thinking");
  });

  it("when opens and routes the session drawer", async () => {
    const toggleSessions = vi.fn();
    const selectSession = vi.fn();
    const removeSession = vi.fn();
    const newChat = vi.fn();
    mocks.useSessions.mockReturnValue({
      ...sessionActions,
      sessionsOpen: true,
      sessions: [{ key: "client:main:old", label: "지난 대화" }],
      toggleSessions,
      selectSession,
      removeSession,
      newChat,
    });
    render(wrapper(workspace(), <AIPanel cfg={cfg} />));

    expect(screen.getByRole("button", { name: "대화 기록" })).toHaveClass("active");
    await userEvent.click(screen.getByRole("button", { name: "대화 기록" }));
    await userEvent.click(screen.getByText("지난 대화").closest("button")!);
    await userEvent.click(screen.getByRole("button", { name: "대화 삭제: 지난 대화" }));
    await userEvent.click(screen.getByRole("button", { name: /새 대화/ }));

    expect(toggleSessions).toHaveBeenCalledTimes(1);
    expect(selectSession).toHaveBeenCalledWith("client:main:old");
    expect(removeSession).toHaveBeenCalledWith("client:main:old");
    expect(newChat).toHaveBeenCalledTimes(1);
  });

  it("when locks session actions while chat work is busy", () => {
    mocks.useChat.mockReturnValue(chatState({ busy: true }));
    mocks.useSessions.mockReturnValue({
      ...sessionActions,
      sessionsOpen: true,
      sessions: [{ key: "client:main:old", label: "지난 대화" }],
    });
    render(wrapper(workspace(), <AIPanel cfg={cfg} />));
    for (const button of screen.getByRole("group", { name: "대화 기록" }).querySelectorAll("button")) {
      expect(button).toBeDisabled();
    }
  });

  it("shows the shared scroll-to-bottom action when unpinned", async () => {
    const scrollToBottom = vi.fn();
    mocks.useStickyScroll.mockReturnValue({
      ref: { current: null },
      onScroll: vi.fn(),
      pin: vi.fn(),
      atBottom: false,
      scrollToBottom,
    });
    mocks.useChat.mockReturnValue(chatState({ turns: [assistantTurn] }));
    render(wrapper(workspace(), <AIPanel cfg={cfg} />));

    await userEvent.click(screen.getByRole("button", { name: "맨 아래로" }));
    expect(scrollToBottom).toHaveBeenCalledTimes(1);
  });
});

describe("AIPanel notebook output loop", () => {
  it("when offers completed assistant text to the active notebook", () => {
    const noteSink = vi.fn(async () => true);
    mocks.useChat.mockReturnValue(chatState({ turns: [assistantTurn] }));
    render(wrapper(workspace({ noteSink }), <AIPanel cfg={cfg} />));
    expect(screen.getByRole("button", { name: /노트에 저장/ })).toBeEnabled();
  });

  it.each([
    [{ ...assistantTurn, status: "streaming" as const }, "streaming"],
    [{ ...assistantTurn, text: "   " }, "blank"],
    [userTurn, "user"],
  ])("does not offer note save for %s turns", (turn) => {
    mocks.useChat.mockReturnValue(chatState({ turns: [turn] }));
    render(wrapper(workspace({ noteSink: vi.fn(async () => true) }), <AIPanel cfg={cfg} />));
    expect(screen.queryByRole("button", { name: /노트/ })).not.toBeInTheDocument();
  });

  it("marks a note saved only after the sink confirms success", async () => {
    let resolve!: (ok: boolean) => void;
    const noteSink = vi.fn(() => new Promise<boolean>((done) => (resolve = done)));
    mocks.useChat.mockReturnValue(chatState({ turns: [assistantTurn] }));
    render(wrapper(workspace({ noteSink }), <AIPanel cfg={cfg} />));

    await userEvent.click(screen.getByRole("button", { name: /노트에 저장/ }));
    expect(screen.getByRole("button", { name: /저장 중/ })).toBeDisabled();
    expect(noteSink).toHaveBeenCalledWith("설계를 검토했습니다.");
    resolve(true);

    await waitFor(() => expect(screen.getByRole("button", { name: /노트로 저장됨/ })).toBeDisabled());
  });

  it("keeps a failed save available for retry", async () => {
    const noteSink = vi.fn().mockResolvedValueOnce(false).mockResolvedValueOnce(true);
    mocks.useChat.mockReturnValue(chatState({ turns: [assistantTurn] }));
    render(wrapper(workspace({ noteSink }), <AIPanel cfg={cfg} />));

    await userEvent.click(screen.getByRole("button", { name: /노트에 저장/ }));
    const retry = await screen.findByRole("button", { name: /저장 실패/ });
    expect(retry).toBeEnabled();
    await userEvent.click(retry);

    await waitFor(() => expect(screen.getByRole("button", { name: /노트로 저장됨/ })).toBeDisabled());
    expect(noteSink).toHaveBeenCalledTimes(2);
  });

  it("turns a rejected sink into retry state", async () => {
    const noteSink = vi.fn(async () => {
      throw new Error("notebook offline");
    });
    mocks.useChat.mockReturnValue(chatState({ turns: [assistantTurn] }));
    render(wrapper(workspace({ noteSink }), <AIPanel cfg={cfg} />));

    await userEvent.click(screen.getByRole("button", { name: /노트에 저장/ }));

    expect(await screen.findByRole("button", { name: /저장 실패/ })).toBeEnabled();
  });

  it("resets saved marks when the target notebook changes", async () => {
    const firstSink = vi.fn(async () => true);
    const secondSink = vi.fn(async () => true);
    mocks.useChat.mockReturnValue(chatState({ turns: [assistantTurn] }));
    const { rerender } = render(wrapper(workspace({ noteSink: firstSink }), <AIPanel cfg={cfg} />));
    await userEvent.click(screen.getByRole("button", { name: /노트에 저장/ }));
    await screen.findByRole("button", { name: /노트로 저장됨/ });

    rerender(wrapper(workspace({ noteSink: secondSink }), <AIPanel cfg={cfg} />));

    expect(screen.getByRole("button", { name: /노트에 저장/ })).toBeEnabled();
  });
});
