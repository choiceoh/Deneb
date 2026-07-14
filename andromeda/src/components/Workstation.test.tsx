import { type ComponentProps } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";

const mocks = vi.hoisted(() => ({
  nativeSync: vi.fn(),
  aiPanel: vi.fn(),
  chatView: vi.fn(),
  filesPane: vi.fn(),
  sidebar: vi.fn(),
}));

vi.mock("@/sync", () => ({ useNativeSync: mocks.nativeSync }));
vi.mock("./Sidebar", () => ({
  Sidebar: () => {
    mocks.sidebar();
    return <nav aria-label="test sidebar" />;
  },
}));
vi.mock("./ChatView", () => ({
  ChatView: (props: { hidden: boolean }) => {
    mocks.chatView(props);
    return <section data-testid="chat-view" data-hidden={props.hidden} />;
  },
}));
vi.mock("./AIPanel", () => ({
  AIPanel: (props: {
    hidden: boolean;
    placement: string;
    expanded: boolean;
    onToggleExpand?: () => void;
    onCollapse?: () => void;
  }) => {
    mocks.aiPanel(props);
    return (
      <aside
        data-testid="ai-panel"
        data-hidden={props.hidden}
        data-placement={props.placement}
        data-expanded={props.expanded}
      >
        {props.onToggleExpand && <button onClick={props.onToggleExpand}>toggle expand</button>}
        {props.onCollapse && <button onClick={props.onCollapse}>collapse AI</button>}
      </aside>
    );
  },
}));
vi.mock("./panes/FilesPane", () => ({
  FilesPane: (props: { active: boolean }) => {
    mocks.filesPane(props);
    return (
      <div data-testid="files-pane" data-active={props.active}>
        files pane
      </div>
    );
  },
}));
vi.mock("./panes", () => ({
  PANES: [
    { key: "today", label: "오늘", shortcut: "1", Component: () => <div>today pane</div> },
    { key: "wiki", label: "위키", shortcut: "2", Component: () => <div>wiki pane</div> },
    { key: "notebook", label: "노트북", shortcut: "3", Component: () => <div>notebook pane</div> },
    { key: "files", label: "파일", shortcut: "4", Component: () => <div>generic files pane</div> },
    { key: "chat", label: "채팅", shortcut: "5", Component: () => <div>generic chat pane</div> },
    { key: "progress", label: "진행", shortcut: "c", Component: () => <div>progress pane</div> },
  ],
}));

import { Workstation } from "./Workstation";
import { Ctx } from "@/workspaceContext";
import type { View } from "@/types";

type WorkspaceValue = NonNullable<ComponentProps<typeof Ctx.Provider>["value"]>;
const cfg = { url: "http://gateway.test", token: "secret" };

function workspace(overrides: Partial<WorkspaceValue> = {}): WorkspaceValue {
  return {
    connected: true,
    cfg,
    setCfg: vi.fn(),
    view: "today",
    setView: vi.fn(),
    paneTarget: null,
    openPane: vi.fn(),
    consumePaneTarget: vi.fn(),
    aiText: "",
    activeResource: undefined,
    registerPane: vi.fn(),
    wikiTarget: null,
    openWiki: vi.fn(),
    consumeWikiTarget: vi.fn(),
    noteSink: null,
    setNoteSink: vi.fn(),
    hiddenViews: [],
    toggleViewHidden: vi.fn(),
    viewOrder: [],
    setViewOrder: vi.fn(),
    notebookTop: "default",
    setNotebookTop: vi.fn(),
    ...overrides,
  };
}

function renderWorkstation(value = workspace(), gateway = cfg) {
  return render(
    <Ctx.Provider value={value}>
      <Workstation cfg={gateway} />
    </Ctx.Provider>,
  );
}

// 우측 데네브 패널은 기본 접힘 — 열림 상태 동작을 검증하는 테스트는 먼저 우측 탭으로 연다.
async function renderWithPanelOpen(value = workspace()) {
  const result = renderWorkstation(value);
  await userEvent.click(screen.getByRole("button", { name: "Deneb 패널 열기" }));
  return result;
}

beforeEach(() => vi.clearAllMocks());

describe("Workstation composition", () => {
  it("renders the active work pane beside the persistent chat surfaces", () => {
    renderWorkstation();

    expect(screen.getByRole("navigation", { name: "test sidebar" })).toBeInTheDocument();
    expect(screen.getByText("today pane")).toBeInTheDocument();
    expect(screen.getByTestId("chat-view")).toHaveAttribute("data-hidden", "true");
    // 우측 데네브 패널은 기본 접힘 — 숨겨진 채 마운트되고 우측 탭으로 연다.
    expect(screen.getByTestId("ai-panel")).toHaveAttribute("data-hidden", "true");
    expect(screen.getByTestId("ai-panel")).toHaveAttribute("data-placement", "side");
    expect(screen.getByRole("button", { name: "Deneb 패널 열기" })).toBeInTheDocument();
  });

  it("starts native catch-up sync with the current gateway state", () => {
    renderWorkstation(workspace({ connected: false }));
    expect(mocks.nativeSync).toHaveBeenCalledWith(cfg, false);
  });

  it("falls back to the first pane for an unknown view", () => {
    renderWorkstation(workspace({ view: "missing" as View }));
    expect(screen.getByText("today pane")).toBeInTheDocument();
  });

  it("displays chat in center and hides side AI when chat tab active", () => {
    renderWorkstation(workspace({ view: "chat" }));
    expect(screen.queryByText("generic chat pane")).not.toBeInTheDocument();
    expect(screen.queryByRole("main")).not.toBeInTheDocument();
    expect(screen.getByTestId("chat-view")).toHaveAttribute("data-hidden", "false");
    expect(screen.getByTestId("ai-panel")).toHaveAttribute("data-hidden", "true");
  });

  it("docks AI below notebook pane when notebook tab active", () => {
    const { container } = renderWorkstation(workspace({ view: "notebook" }));
    expect(screen.getByText("notebook pane")).toBeInTheDocument();
    expect(screen.getByTestId("ai-panel")).toHaveAttribute("data-placement", "bottom");
    expect(screen.getByTestId("ai-panel")).toHaveAttribute("data-expanded", "false");
    expect(screen.queryByRole("button", { name: "toggle expand" })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "collapse AI" })).not.toBeInTheDocument();
    expect(container.querySelector(".workstation-shell")).toHaveClass("ws-bottom-chat");
  });

  it.each([
    ["folded", "ws-top-folded"],
    ["expanded", "ws-top-expanded"],
  ] as const)("applies notebook top mode class %s when mode configured", (notebookTop, className) => {
    const { container } = renderWorkstation(workspace({ view: "notebook", notebookTop }));
    expect(container.querySelector(".workstation-shell")).toHaveClass(className);
  });

  it("preserves default notebook sizing without mode classes when mode unset", () => {
    const { container } = renderWorkstation(workspace({ view: "notebook", notebookTop: "default" }));
    expect(container.querySelector(".workstation-shell")).not.toHaveClass("ws-top-folded", "ws-top-expanded");
  });

  it("marks frameless-window drag strip when platform supports drag region", () => {
    const { container } = renderWorkstation();
    expect(container.querySelector(".drag-strip")).toHaveAttribute("data-tauri-drag-region");
  });
});

describe("Workstation AI panel controls", () => {
  it("expands AI panel and hides work pane when AI expand triggered", async () => {
    await renderWithPanelOpen();
    expect(screen.getByText("today pane")).toBeInTheDocument();

    await userEvent.click(screen.getByRole("button", { name: "toggle expand" }));

    expect(screen.queryByText("today pane")).not.toBeInTheDocument();
    expect(screen.getByTestId("ai-panel")).toHaveAttribute("data-expanded", "true");
  });

  it("restores the work pane after narrowing AI", async () => {
    await renderWithPanelOpen();
    await userEvent.click(screen.getByRole("button", { name: "toggle expand" }));
    await userEvent.click(screen.getByRole("button", { name: "toggle expand" }));
    expect(screen.getByText("today pane")).toBeInTheDocument();
    expect(screen.getByTestId("ai-panel")).toHaveAttribute("data-expanded", "false");
  });

  it("collapses side AI and preserves full-width work pane when AI collapsed", async () => {
    await renderWithPanelOpen();

    await userEvent.click(screen.getByRole("button", { name: "collapse AI" }));

    expect(screen.getByText("today pane")).toBeInTheDocument();
    expect(screen.getByTestId("ai-panel")).toHaveAttribute("data-hidden", "true");
    expect(screen.getByRole("button", { name: "Deneb 패널 열기" })).toBeInTheDocument();
  });

  it("displays collapsed AI panel again when reopen control clicked", async () => {
    await renderWithPanelOpen();
    await userEvent.click(screen.getByRole("button", { name: "collapse AI" }));
    await userEvent.click(screen.getByRole("button", { name: "Deneb 패널 열기" }));
    expect(screen.getByTestId("ai-panel")).toHaveAttribute("data-hidden", "false");
    expect(screen.queryByRole("button", { name: "Deneb 패널 열기" })).not.toBeInTheDocument();
  });

  it("clears expansion state when expanded panel collapsed", async () => {
    await renderWithPanelOpen();
    await userEvent.click(screen.getByRole("button", { name: "toggle expand" }));
    // Expanded AIPanel intentionally has no collapse button; narrow first, then collapse.
    await userEvent.click(screen.getByRole("button", { name: "toggle expand" }));
    await userEvent.click(screen.getByRole("button", { name: "collapse AI" }));
    expect(screen.getByTestId("ai-panel")).toHaveAttribute("data-expanded", "false");
  });
});

describe("Workstation shortcuts", () => {
  it.each([
    ["1", "today"],
    ["2", "wiki"],
    ["3", "notebook"],
    ["4", "files"],
    ["5", "chat"],
  ])("routes Ctrl+%s to %s when shortcut pressed", (key, view) => {
    const setView = vi.fn();
    renderWorkstation(workspace({ setView }));
    const event = new KeyboardEvent("keydown", { key, ctrlKey: true, cancelable: true });

    window.dispatchEvent(event);

    expect(event.defaultPrevented).toBe(true);
    expect(setView).toHaveBeenCalledWith(view);
  });

  it("allows platform Meta modifier when shortcut includes Meta key", () => {
    const setView = vi.fn();
    renderWorkstation(workspace({ setView }));
    const event = new KeyboardEvent("keydown", { key: "2", metaKey: true, cancelable: true });
    window.dispatchEvent(event);
    expect(setView).toHaveBeenCalledWith("wiki");
    expect(event.defaultPrevented).toBe(true);
  });

  it.each(["a", "c", "v", "x", "y", "z", "A", "C"])("denies shortcut steal when editing combo %s pressed in input", (key) => {
    const setView = vi.fn();
    renderWorkstation(workspace({ setView }));
    const event = new KeyboardEvent("keydown", { key, ctrlKey: true, cancelable: true });
    window.dispatchEvent(event);
    expect(setView).not.toHaveBeenCalled();
    expect(event.defaultPrevented).toBe(false);
  });

  it("ignores shortcuts without Ctrl or Meta", () => {
    const setView = vi.fn();
    renderWorkstation(workspace({ setView }));
    const event = new KeyboardEvent("keydown", { key: "2", cancelable: true });
    window.dispatchEvent(event);
    expect(setView).not.toHaveBeenCalled();
    expect(event.defaultPrevented).toBe(false);
  });

  it("ignores unregistered modified keys", () => {
    const setView = vi.fn();
    renderWorkstation(workspace({ setView }));
    const event = new KeyboardEvent("keydown", { key: "9", ctrlKey: true, cancelable: true });
    window.dispatchEvent(event);
    expect(setView).not.toHaveBeenCalled();
    expect(event.defaultPrevented).toBe(false);
  });

  it("removes global shortcut listener when component unmounts", () => {
    const setView = vi.fn();
    const view = renderWorkstation(workspace({ setView }));
    view.unmount();
    window.dispatchEvent(new KeyboardEvent("keydown", { key: "2", ctrlKey: true, cancelable: true }));
    expect(setView).not.toHaveBeenCalled();
  });
});

describe("Workstation persistent files pane", () => {
  it("denies files pane mount before first visit when tab never opened", () => {
    renderWorkstation(workspace({ view: "today" }));
    expect(screen.queryByTestId("files-pane")).not.toBeInTheDocument();
    expect(mocks.filesPane).not.toHaveBeenCalled();
  });

  it("mounts dedicated files pane when files tab visited first time", () => {
    renderWorkstation(workspace({ view: "files" }));
    expect(screen.getByTestId("files-pane")).toHaveAttribute("data-active", "true");
    expect(screen.queryByText("generic files pane")).not.toBeInTheDocument();
  });

  it("preserves files pane mount but marks inactive when other pane selected", () => {
    const initial = workspace({ view: "files" });
    const { rerender } = renderWorkstation(initial);
    expect(screen.getByTestId("files-pane")).toHaveAttribute("data-active", "true");

    rerender(
      <Ctx.Provider value={workspace({ view: "wiki" })}>
        <Workstation cfg={cfg} />
      </Ctx.Provider>,
    );

    expect(screen.getByText("wiki pane")).toBeInTheDocument();
    expect(screen.getByTestId("files-pane")).toHaveAttribute("data-active", "false");
    expect(screen.getByTestId("files-pane").closest("main")).toHaveStyle({ display: "none" });
  });

  it("hides files while AI is expanded without unmounting it", async () => {
    await renderWithPanelOpen(workspace({ view: "files" }));
    await userEvent.click(screen.getByRole("button", { name: "toggle expand" }));
    expect(screen.getByTestId("files-pane")).toBeInTheDocument();
    expect(screen.getByTestId("files-pane").closest("main")).toHaveStyle({ display: "none" });
  });

  it("recreates the files pane container when gateway identity changes", () => {
    const { container, rerender } = renderWorkstation(workspace({ view: "files" }), cfg);
    const first = screen.getByTestId("files-pane").closest("main");
    first?.setAttribute("data-instance", "old");

    const nextCfg = { url: "http://second.test", token: "second" };
    rerender(
      <Ctx.Provider value={workspace({ view: "files", cfg: nextCfg })}>
        <Workstation cfg={nextCfg} />
      </Ctx.Provider>,
    );

    const second = screen.getByTestId("files-pane").closest("main");
    expect(second).not.toBe(first);
    expect(container.querySelector('[data-instance="old"]')).toBeNull();
  });
});
