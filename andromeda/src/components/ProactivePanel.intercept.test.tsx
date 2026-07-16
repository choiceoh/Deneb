// The workspace-command side of ProactivePanel: gateway `workspace` pushes must
// execute through the command bus and surface as a visible "화면 조정" nudge;
// malformed control frames are swallowed; ordinary nudges pass through untouched.
import { beforeEach, describe, expect, it, vi } from "vitest";
import { render } from "@testing-library/react";

const mocks = vi.hoisted(() => ({ useEvents: vi.fn() }));
vi.mock("@/hooks", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@/hooks")>();
  return { ...actual, useEvents: mocks.useEvents };
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
