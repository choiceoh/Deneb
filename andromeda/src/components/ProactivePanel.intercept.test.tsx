// The workspace-command side of ProactivePanel: gateway `workspace` pushes must
// execute through the command bus and surface as a visible "화면 조정" nudge;
// malformed control frames are swallowed; ordinary nudges pass through untouched.
import { beforeEach, describe, expect, it, vi } from "vitest";
import { render } from "@testing-library/react";

const mocks = vi.hoisted(() => ({ useEvents: vi.fn(), handleComputerFrame: vi.fn() }));
vi.mock("@/hooks", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@/hooks")>();
  return { ...actual, useEvents: mocks.useEvents };
});
vi.mock("@/computer", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@/computer")>();
  return { ...actual, handleComputerFrame: mocks.handleComputerFrame };
});

import { ProactivePanel } from "./ProactivePanel";
import type { ProactiveEvent } from "@/events";
import { testCfg, workspaceValue, WorkspaceStub } from "@/test/workspace";

type Intercept = (ev: ProactiveEvent) => ProactiveEvent | null;

function mountAndGrabIntercept(value = workspaceValue()) {
  mocks.useEvents.mockReturnValue({ events: [], status: "", dismiss: vi.fn(), clearAll: vi.fn() });
  render(
    <WorkspaceStub value={value}>
      <ProactivePanel cfg={testCfg} />
    </WorkspaceStub>,
  );
  const intercept = mocks.useEvents.mock.calls.at(-1)?.[2] as Intercept;
  expect(typeof intercept).toBe("function");
  return { value, intercept };
}

beforeEach(() => vi.clearAllMocks());

describe("ProactivePanel workspace-command intercept", () => {
  it("executes a pushed workspace command and rewrites it into a visible nudge", () => {
    const { value, intercept } = mountAndGrabIntercept();
    const nudge = intercept({ id: "e1", kind: "workspace", raw: { action: "split", view: "mail" } });

    expect(value.runCommand).toHaveBeenCalledWith({ kind: "split", view: "mail", ref: undefined });
    expect(nudge?.kind).toBe("workspace");
    expect(nudge?.title).toBe("화면 조정");
    expect(nudge?.body).toContain("메일");
  });

  it("parses the gateway's data-nested command frame (workstation tool wire shape)", () => {
    const { value, intercept } = mountAndGrabIntercept();
    const nudge = intercept({
      id: "e4",
      kind: "workspace",
      title: "화면 조정",
      raw: { title: "화면 조정", body: "layout", data: { action: "layout", views: "mail,calendar" } },
    });

    expect(value.runCommand).toHaveBeenCalledWith({ kind: "layout", views: ["mail", "calendar"] });
    expect(nudge?.body).toContain("메일 · 일정");
  });

  it("swallows malformed control frames without running anything", () => {
    const { value, intercept } = mountAndGrabIntercept();
    const nudge = intercept({ id: "e2", kind: "workspace", raw: { action: "split", view: "settings" } });

    expect(nudge).toBeNull();
    expect(value.runCommand).not.toHaveBeenCalled();
  });

  it("passes ordinary nudges through untouched", () => {
    const { value, intercept } = mountAndGrabIntercept();
    const ev: ProactiveEvent = { id: "e3", kind: "mail", title: "급한 메일", raw: {} };
    expect(intercept(ev)).toBe(ev);
    expect(value.runCommand).not.toHaveBeenCalled();
  });
});

describe("ProactivePanel computer-use intercept", () => {
  it("executes a pushed computer frame through the executor and shows a 컴퓨터 조종 nudge", () => {
    mocks.handleComputerFrame.mockResolvedValue({ ok: true });
    const { value, intercept } = mountAndGrabIntercept();
    const nudge = intercept({
      id: "c1",
      kind: "computer",
      ref: "cu-abc",
      raw: { title: "컴퓨터 조종", body: "클릭", data: { action: "click", x: "10", y: "20", button: "left" } },
    });

    expect(mocks.handleComputerFrame).toHaveBeenCalledWith(testCfg, "cu-abc", {
      action: "click",
      x: 10,
      y: 20,
      button: "left",
    });
    expect(value.runCommand).not.toHaveBeenCalled();
    expect(nudge?.kind).toBe("computer");
    expect(nudge?.title).toBe("컴퓨터 조종");
    expect(nudge?.body).toBe("클릭 (10,20)");
  });

  it("answers a malformed computer frame with a failure report instead of leaving the tool waiting", async () => {
    mocks.handleComputerFrame.mockResolvedValue({ ok: false });
    const { intercept } = mountAndGrabIntercept();
    const nudge = intercept({ id: "c2", kind: "computer", ref: "cu-bad", raw: { data: { action: "click", x: "1" } } });

    expect(nudge).toBeNull();
    expect(mocks.handleComputerFrame).toHaveBeenCalledTimes(1);
    const [, id, cmd, run] = mocks.handleComputerFrame.mock.calls[0] as [
      unknown,
      string,
      unknown,
      () => Promise<{ ok: boolean }>,
    ];
    expect(id).toBe("cu-bad");
    expect(cmd).toEqual({ action: "cursor" });
    await expect(run()).resolves.toMatchObject({ ok: false });
  });
});
