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
  FilesPane: () => {
    mocks.filesPane();
    return <div data-testid="files-pane">files pane</div>;
  },
}));
vi.mock("./panes", () => {
  const PANES = [
    { key: "today", label: "오늘", shortcut: "1", Component: () => <div>today pane</div> },
    { key: "wiki", label: "위키", shortcut: "2", Component: () => <div>wiki pane</div> },
    { key: "notebook", label: "노트북", shortcut: "3", Component: () => <div>notebook pane</div> },
    { key: "files", label: "파일", shortcut: "4", Component: () => <div>generic files pane</div> },
    { key: "chat", label: "채팅", shortcut: "5", Component: () => <div>generic chat pane</div> },
  ];
  return {
    PANES,
    paneLabel: (key: string) => PANES.find((p) => p.key === key)?.label ?? key,
    orderedViews: () => PANES.map((p) => p.key),
  };
});
// Workstation renders the palette only when paletteOpen — stub it out so this
// suite stays a shell test (CommandPalette has its own suite).
vi.mock("./CommandPalette", () => ({
  CommandPalette: () => <div data-testid="command-palette" />,
}));

import { Workstation } from "./Workstation";
import { workspaceValue, WorkspaceStub, testCfg as cfg } from "@/test/workspace";
import type { View } from "@/types";

const workspace = workspaceValue;

function renderWorkstation(value = workspace(), gateway = cfg) {
  return render(
    <WorkspaceStub value={value}>
      <Workstation cfg={gateway} />
    </WorkspaceStub>,
  );
}

// 우측 데네브 패널 접힘은 이제 컨텍스트 소유(aiCollapsed) — 열림 동작 검증은 오버라이드로.
function renderWithPanelOpen(value = workspace({ aiCollapsed: false })) {
  return renderWorkstation(value);
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

  it("starts native sync when gateway state renders", () => {
    renderWorkstation(workspace({ connected: false }));
    expect(mocks.nativeSync).toHaveBeenCalledWith(cfg, false);
  });

  it("falls back to the first pane for an unknown view", () => {
    renderWorkstation(workspace({ view: "missing" as View }));
    expect(screen.getByText("today pane")).toBeInTheDocument();
  });

  it("shows center chat and hides side AI when chat view selected", () => {
    renderWorkstation(workspace({ view: "chat" }));
    expect(screen.queryByText("generic chat pane")).not.toBeInTheDocument();
    expect(screen.queryByRole("main")).not.toBeInTheDocument();
    expect(screen.getByTestId("chat-view")).toHaveAttribute("data-hidden", "false");
    expect(screen.getByTestId("ai-panel")).toHaveAttribute("data-hidden", "true");
  });

  it("renders bottom AI placement when notebook view active", () => {
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
  ] as const)("applies notebook top mode class when %s", (notebookTop, className) => {
    const { container } = renderWorkstation(workspace({ view: "notebook", notebookTop }));
    expect(container.querySelector(".workstation-shell")).toHaveClass(className);
  });

  it("renders default notebook without mode classes", () => {
    const { container } = renderWorkstation(workspace({ view: "notebook", notebookTop: "default" }));
    expect(container.querySelector(".workstation-shell")).not.toHaveClass("ws-top-folded", "ws-top-expanded");
  });

  it("renders drag strip with tauri region when frameless", () => {
    const { container } = renderWorkstation();
    expect(container.querySelector(".drag-strip")).toHaveAttribute("data-tauri-drag-region");
  });
});

describe("Workstation AI panel controls", () => {
  it("hides work pane when AI panel expands", async () => {
    renderWithPanelOpen();
    expect(screen.getByText("today pane")).toBeInTheDocument();

    await userEvent.click(screen.getByRole("button", { name: "toggle expand" }));

    expect(screen.queryByText("today pane")).not.toBeInTheDocument();
    expect(screen.getByTestId("ai-panel")).toHaveAttribute("data-expanded", "true");
  });

  it("restores the work pane after narrowing AI", async () => {
    renderWithPanelOpen();
    await userEvent.click(screen.getByRole("button", { name: "toggle expand" }));
    await userEvent.click(screen.getByRole("button", { name: "toggle expand" }));
    expect(screen.getByText("today pane")).toBeInTheDocument();
    expect(screen.getByTestId("ai-panel")).toHaveAttribute("data-expanded", "false");
  });

  // 접힘 상태는 이제 컨텍스트(aiCollapsed) 소유 — Workstation은 setAiCollapsed를 쏘고,
  // 표시는 스텁 값으로 검증한다(상태 전이는 WorkspaceProvider 테스트의 몫).
  it("requests collapse when the AI collapse button clicks", async () => {
    const setAiCollapsed = vi.fn();
    renderWithPanelOpen(workspace({ aiCollapsed: false, setAiCollapsed }));

    await userEvent.click(screen.getByRole("button", { name: "collapse AI" }));

    expect(setAiCollapsed).toHaveBeenCalledWith(true);
    expect(screen.getByText("today pane")).toBeInTheDocument();
  });

  it("shows the edge tab while collapsed and requests reopen on click", async () => {
    const setAiCollapsed = vi.fn();
    renderWorkstation(workspace({ setAiCollapsed })); // 기본 접힘
    expect(screen.getByTestId("ai-panel")).toHaveAttribute("data-hidden", "true");

    await userEvent.click(screen.getByRole("button", { name: "Deneb 패널 열기" }));

    expect(setAiCollapsed).toHaveBeenCalledWith(false);
  });

  it("resets expansion when collapsing an expanded panel", async () => {
    renderWithPanelOpen();
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
  ])("switches to %s view when Ctrl+%s pressed", (key, view) => {
    const setView = vi.fn();
    renderWorkstation(workspace({ setView }));
    const event = new KeyboardEvent("keydown", { key, ctrlKey: true, cancelable: true });

    window.dispatchEvent(event);

    expect(event.defaultPrevented).toBe(true);
    expect(setView).toHaveBeenCalledWith(view);
  });

  it("switches view when Meta modifier shortcut fires", () => {
    const setView = vi.fn();
    renderWorkstation(workspace({ setView }));
    const event = new KeyboardEvent("keydown", { key: "2", metaKey: true, cancelable: true });
    window.dispatchEvent(event);
    expect(setView).toHaveBeenCalledWith("wiki");
    expect(event.defaultPrevented).toBe(true);
  });

  it.each(["a", "c", "v", "x", "y", "z", "A", "C"])("ignores editing shortcut %s without switching view", (key) => {
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

  it("removes shortcut listener on unmount without leaving handlers", () => {
    const setView = vi.fn();
    const view = renderWorkstation(workspace({ setView }));
    view.unmount();
    window.dispatchEvent(new KeyboardEvent("keydown", { key: "2", ctrlKey: true, cancelable: true }));
    expect(setView).not.toHaveBeenCalled();
  });
});

describe("Workstation persistent files pane", () => {
  it("omits files pane without mounting before first visit", () => {
    renderWorkstation(workspace({ view: "today" }));
    expect(screen.queryByTestId("files-pane")).not.toBeInTheDocument();
    expect(mocks.filesPane).not.toHaveBeenCalled();
  });

  it("renders dedicated files pane when files view first visited", () => {
    renderWorkstation(workspace({ view: "files" }));
    expect(screen.getByTestId("files-pane")).toBeInTheDocument();
    expect(screen.queryByText("generic files pane")).not.toBeInTheDocument();
  });

  it("preserves files mount and hides pane when switching views", () => {
    const initial = workspace({ view: "files" });
    const { rerender } = renderWorkstation(initial);
    expect(screen.getByTestId("files-pane")).toBeInTheDocument();

    rerender(
      <WorkspaceStub value={workspace({ view: "wiki" })}>
        <Workstation cfg={cfg} />
      </WorkspaceStub>,
    );

    expect(screen.getByText("wiki pane")).toBeInTheDocument();
    expect(screen.getByTestId("files-pane").closest("main")).toHaveStyle({ display: "none" });
  });

  it("hides files while AI is expanded without unmounting it", async () => {
    renderWithPanelOpen(workspace({ aiCollapsed: false, view: "files" }));
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
      <WorkspaceStub value={workspace({ view: "files", cfg: nextCfg })}>
        <Workstation cfg={nextCfg} />
      </WorkspaceStub>,
    );

    const second = screen.getByTestId("files-pane").closest("main");
    expect(second).not.toBe(first);
    expect(container.querySelector('[data-instance="old"]')).toBeNull();
  });
});
