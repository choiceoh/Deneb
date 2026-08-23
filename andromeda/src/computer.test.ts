// Computer-use frame handling: the gateway's `computer` push must parse into a
// narrow command (anything else is dropped), stay OFF by default, and always
// report back — ok=false with a Korean reason when it cannot run.
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const mocks = vi.hoisted(() => ({ callRpc: vi.fn() }));
vi.mock("./gateway", async (importOriginal) => {
  const actual = await importOriginal<typeof import("./gateway")>();
  return { ...actual, callRpc: mocks.callRpc };
});

import {
  describeComputerCommand,
  executeComputerCommand,
  handleComputerFrame,
  isComputerCommandKind,
  isComputerUseEnabled,
  parseComputerCommand,
  setComputerUseEnabled,
  toInvokeArgs,
} from "./computer";

const cfg = { url: "http://gateway.test", token: "secret" };

beforeEach(() => {
  localStorage.clear();
  mocks.callRpc.mockReset();
  mocks.callRpc.mockResolvedValue({ resolved: true });
});
afterEach(() => localStorage.clear());

describe("parseComputerCommand", () => {
  it("parses the gateway's string-valued data map for every verb", () => {
    expect(parseComputerCommand({ action: "screenshot" })).toEqual({ action: "screenshot" });
    expect(parseComputerCommand({ action: "screenshot", x: "10", y: "20", width: "300", height: "200" })).toEqual({
      action: "screenshot",
      zoom: { x: 10, y: 20, width: 300, height: 200 },
    });
    expect(parseComputerCommand({ action: "click", x: "5", y: "7", button: "left" })).toEqual({
      action: "click",
      x: 5,
      y: 7,
      button: "left",
    });
    expect(parseComputerCommand({ action: "drag", x: "1", y: "2", to_x: "3", to_y: "4" })).toEqual({
      action: "drag",
      x: 1,
      y: 2,
      toX: 3,
      toY: 4,
    });
    expect(parseComputerCommand({ action: "scroll", x: "1", y: "2" })).toEqual({
      action: "scroll",
      x: 1,
      y: 2,
      direction: "down",
      amount: 3,
    });
    expect(parseComputerCommand({ action: "type", text: "안녕" })).toEqual({ action: "type", text: "안녕" });
    expect(parseComputerCommand({ action: "key", key: " ctrl+c " })).toEqual({ action: "key", key: "ctrl+c" });
    expect(parseComputerCommand({ action: "wait", wait_ms: "250" })).toEqual({ action: "wait", waitMs: 250 });
    expect(parseComputerCommand({ action: "cursor" })).toEqual({ action: "cursor" });
  });

  it("drops malformed frames instead of guessing", () => {
    expect(parseComputerCommand({ action: "format_disk" })).toBeNull();
    expect(parseComputerCommand({ action: "click", x: "5" })).toBeNull();
    expect(parseComputerCommand({ action: "click", x: "-1", y: "2" })).toBeNull();
    expect(parseComputerCommand({ action: "click", x: "1", y: "2", button: "back" })).toBeNull();
    expect(parseComputerCommand({ action: "screenshot", x: "1", y: "2" })).toBeNull();
    expect(parseComputerCommand({ action: "scroll", x: "1", y: "2", scroll: "diagonal" })).toBeNull();
    expect(parseComputerCommand({ action: "type", text: "" })).toBeNull();
    expect(parseComputerCommand({ action: "wait", wait_ms: "99999" })).toBeNull();
    expect(parseComputerCommand({})).toBeNull();
  });

  it("recognises only the computer frame kind", () => {
    expect(isComputerCommandKind("computer")).toBe(true);
    expect(isComputerCommandKind("workspace")).toBe(false);
    expect(isComputerCommandKind(undefined)).toBe(false);
  });
});

describe("describe + invoke shape", () => {
  it("renders Korean one-liners and a flat snake_case invoke payload", () => {
    expect(describeComputerCommand({ action: "click", x: 1, y: 2, button: "left" })).toBe("클릭 (1,2)");
    expect(describeComputerCommand({ action: "click", x: 1, y: 2, button: "right" })).toBe("right 클릭 (1,2)");
    expect(describeComputerCommand({ action: "drag", x: 1, y: 2, toX: 3, toY: 4 })).toBe("드래그 (1,2)→(3,4)");
    expect(describeComputerCommand({ action: "type", text: "x".repeat(50) })).toMatch(/…"$/);
    expect(toInvokeArgs({ action: "drag", x: 1, y: 2, toX: 3, toY: 4 })).toEqual({
      action: "drag",
      x: 1,
      y: 2,
      to_x: 3,
      to_y: 4,
    });
    expect(toInvokeArgs({ action: "screenshot", zoom: { x: 1, y: 2, width: 3, height: 4 } })).toEqual({
      action: "screenshot",
      x: 1,
      y: 2,
      width: 3,
      height: 4,
    });
    expect(toInvokeArgs({ action: "wait", waitMs: 5 })).toEqual({ action: "wait", wait_ms: 5 });
  });
});

describe("execution gate", () => {
  it("is off by default and refuses with a reason the model can relay", async () => {
    expect(isComputerUseEnabled()).toBe(false);
    const out = await executeComputerCommand({ action: "screenshot" });
    expect(out.ok).toBe(false);
    expect(out.error).toContain("꺼져");
  });

  it("when enabled off-desktop, reports the web-build limitation (no executor)", async () => {
    setComputerUseEnabled(true);
    expect(isComputerUseEnabled()).toBe(true);
    const out = await executeComputerCommand({ action: "cursor" });
    expect(out.ok).toBe(false);
    expect(out.error).toContain("웹 빌드");
  });
});

describe("handleComputerFrame", () => {
  it("reports the executor outcome to miniapp.computer.result with the frame id", async () => {
    const out = await handleComputerFrame(cfg, "cu-1", { action: "screenshot" }, async () => ({
      ok: true,
      image: "QUJD",
      width: 10,
      height: 5,
    }));
    expect(out.ok).toBe(true);
    expect(mocks.callRpc).toHaveBeenCalledWith(cfg, "miniapp.computer.result", {
      id: "cu-1",
      ok: true,
      error: "",
      image: "QUJD",
      width: 10,
      height: 5,
      text: "",
    });
  });

  it("reports failures (ok=false + reason) and survives a failed report", async () => {
    mocks.callRpc.mockRejectedValueOnce(new Error("offline"));
    const out = await handleComputerFrame(cfg, "cu-2", { action: "cursor" }, async () => ({
      ok: false,
      error: "꺼짐",
    }));
    expect(out).toEqual({ ok: false, error: "꺼짐" });
    expect(mocks.callRpc).toHaveBeenCalledWith(
      cfg,
      "miniapp.computer.result",
      expect.objectContaining({ id: "cu-2", ok: false, error: "꺼짐" }),
    );
  });
});
