// Shared stub factories for the two workspace contexts (navigation Ctx + pane→AI
// FeedCtx). Component tests that don't want the real WorkspaceProvider build a
// value here and override just what they exercise — one place to extend when the
// context grows a field.
import type { ComponentProps, ReactNode } from "react";
import { vi } from "vitest";
import { Ctx, FeedCtx, FeedWriteCtx } from "@/workspaceContext";

export type WorkspaceValue = NonNullable<ComponentProps<typeof Ctx.Provider>["value"]>;
export type FeedValue = NonNullable<ComponentProps<typeof FeedCtx.Provider>["value"]>;
export type FeedWriters = NonNullable<ComponentProps<typeof FeedWriteCtx.Provider>["value"]>;

export const testCfg = { url: "http://gateway.test", token: "secret" };

export function workspaceValue(overrides: Partial<WorkspaceValue> = {}): WorkspaceValue {
  return {
    connected: true,
    cfg: testCfg,
    setCfg: vi.fn(),
    view: "today",
    setView: vi.fn(),
    tiles: ["today"],
    splitPane: vi.fn(),
    closePane: vi.fn(),
    applyLayout: vi.fn(),
    layouts: [],
    saveLayout: vi.fn(),
    deleteLayout: vi.fn(),
    runCommand: vi.fn(),
    spotlight: null,
    paletteOpen: false,
    setPaletteOpen: vi.fn(),
    // 앱 기본값과 동일하게 접힘 — 열림을 검증하는 테스트가 명시적으로 푼다.
    aiCollapsed: true,
    setAiCollapsed: vi.fn(),
    askDeneb: vi.fn(() => true),
    setAskSink: vi.fn(),
    publishAnswer: vi.fn(),
    setAnswerSink: vi.fn(),
    paneTarget: null,
    openPane: vi.fn(),
    consumePaneTarget: vi.fn(),
    wikiTarget: null,
    openWiki: vi.fn(),
    splitWiki: vi.fn(),
    consumeWikiTarget: vi.fn(),
    followMode: false,
    setFollowMode: vi.fn(),
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

export function feedValue(overrides: Partial<FeedValue> = {}): FeedValue {
  return {
    aiText: "",
    activeResource: undefined,
    ...overrides,
  };
}

export function feedWriters(overrides: Partial<FeedWriters> = {}): FeedWriters {
  return {
    registerPane: vi.fn(),
    unregisterPane: vi.fn(),
    ...overrides,
  };
}

export function WorkspaceStub({
  value,
  feed,
  writers,
  children,
}: {
  value?: WorkspaceValue;
  feed?: FeedValue;
  writers?: FeedWriters;
  children: ReactNode;
}) {
  return (
    <Ctx.Provider value={value ?? workspaceValue()}>
      <FeedWriteCtx.Provider value={writers ?? feedWriters()}>
        <FeedCtx.Provider value={feed ?? feedValue()}>{children}</FeedCtx.Provider>
      </FeedWriteCtx.Provider>
    </Ctx.Provider>
  );
}
