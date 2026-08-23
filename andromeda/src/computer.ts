// Computer use — Deneb operates the host OS (mouse / keyboard / screen) through
// this desktop shell. The gateway's `computer` chat tool pushes a `computer`
// frame over the events SSE; this module parses it, runs it through the Tauri
// side (src-tauri/src/computer.rs: screenshot via the platform, input via
// enigo) and reports the outcome back with miniapp.computer.result so the
// waiting tool call resumes.
//
// Safety posture: OFF by default (설정 › 일반 › 컴퓨터 조종). While off, every
// frame is answered with ok=false so the model tells the user to enable it —
// never executed silently. The web build has no executor and says so. The
// gateway validates verbs/arguments first; this side re-validates because a
// push frame is just JSON.
import { type GatewayConfig, callRpc } from "./gateway";
import { log } from "./log";
import { COMPUTER_RPC } from "./resources";
import { getString, setString } from "./storage";
import { isTauri } from "./tauri";

const cuLog = log.child("computer");

const ENABLED_KEY = "andromeda.computerUse";

export function isComputerUseEnabled(): boolean {
  return getString(ENABLED_KEY) === "on";
}

export function setComputerUseEnabled(on: boolean): void {
  setString(ENABLED_KEY, on ? "on" : "off");
}

// Mirrors the gateway allowlist (hostops/computer.go) and the Rust executor.
export type ComputerCommand =
  | { action: "screenshot"; zoom?: { x: number; y: number; width: number; height: number } }
  | { action: "click"; x: number; y: number; button: "left" | "right" | "middle" }
  | { action: "double_click" | "right_click" | "move"; x: number; y: number }
  | { action: "drag"; x: number; y: number; toX: number; toY: number }
  | { action: "scroll"; x: number; y: number; direction: "up" | "down" | "left" | "right"; amount: number }
  | { action: "type"; text: string }
  | { action: "key"; key: string }
  | { action: "wait"; waitMs: number }
  | { action: "cursor" };

export function isComputerCommandKind(kind: string | undefined): boolean {
  return kind === "computer";
}

const MAX_COORD = 16384;

function num(v: unknown): number | undefined {
  if (typeof v === "number" && Number.isFinite(v)) return Math.round(v);
  if (typeof v === "string" && v.trim() !== "") {
    const n = Number(v);
    return Number.isFinite(n) ? Math.round(n) : undefined;
  }
  return undefined;
}

function coord(v: unknown): number | undefined {
  const n = num(v);
  return n !== undefined && n >= 0 && n <= MAX_COORD ? n : undefined;
}

// Parse a gateway push payload (the frame's `data` map — every value a string)
// into a command, or null when malformed.
export function parseComputerCommand(raw: Record<string, unknown>): ComputerCommand | null {
  const action = typeof raw.action === "string" ? raw.action.trim().toLowerCase() : "";
  const x = coord(raw.x);
  const y = coord(raw.y);
  switch (action) {
    case "screenshot": {
      const w = num(raw.width);
      const h = num(raw.height);
      if (x === undefined && y === undefined && w === undefined && h === undefined) return { action };
      if (x === undefined || y === undefined || w === undefined || h === undefined || w <= 0 || h <= 0) return null;
      return { action, zoom: { x, y, width: w, height: h } };
    }
    case "click": {
      if (x === undefined || y === undefined) return null;
      const b = typeof raw.button === "string" ? raw.button : "left";
      if (b !== "left" && b !== "right" && b !== "middle") return null;
      return { action, x, y, button: b };
    }
    case "double_click":
    case "right_click":
    case "move":
      if (x === undefined || y === undefined) return null;
      return { action, x, y };
    case "drag": {
      const toX = coord(raw.to_x);
      const toY = coord(raw.to_y);
      if (x === undefined || y === undefined || toX === undefined || toY === undefined) return null;
      return { action, x, y, toX, toY };
    }
    case "scroll": {
      if (x === undefined || y === undefined) return null;
      const d = typeof raw.scroll === "string" ? raw.scroll : "down";
      if (d !== "up" && d !== "down" && d !== "left" && d !== "right") return null;
      const amount = num(raw.amount) ?? 3;
      if (amount <= 0 || amount > 50) return null;
      return { action, x, y, direction: d, amount };
    }
    case "type":
      if (typeof raw.text !== "string" || raw.text === "") return null;
      return { action, text: raw.text };
    case "key":
      if (typeof raw.key !== "string" || raw.key.trim() === "") return null;
      return { action, key: raw.key.trim() };
    case "wait": {
      const ms = num(raw.wait_ms) ?? 1000;
      if (ms <= 0 || ms > 10000) return null;
      return { action, waitMs: ms };
    }
    case "cursor":
      return { action };
    default:
      return null;
  }
}

// Korean one-liner for the proactive nudge — the user must always see what
// Deneb did to their machine.
export function describeComputerCommand(cmd: ComputerCommand): string {
  switch (cmd.action) {
    case "screenshot":
      return cmd.zoom ? `스크린샷 (확대 ${cmd.zoom.width}×${cmd.zoom.height})` : "스크린샷";
    case "click":
      return `${cmd.button === "left" ? "" : cmd.button + " "}클릭 (${cmd.x},${cmd.y})`;
    case "double_click":
      return `더블클릭 (${cmd.x},${cmd.y})`;
    case "right_click":
      return `우클릭 (${cmd.x},${cmd.y})`;
    case "move":
      return `마우스 이동 (${cmd.x},${cmd.y})`;
    case "drag":
      return `드래그 (${cmd.x},${cmd.y})→(${cmd.toX},${cmd.toY})`;
    case "scroll":
      return `스크롤 ${cmd.direction} ×${cmd.amount}`;
    case "type":
      return `입력 "${cmd.text.length > 40 ? cmd.text.slice(0, 40) + "…" : cmd.text}"`;
    case "key":
      return `키 ${cmd.key}`;
    case "wait":
      return `대기 ${cmd.waitMs}ms`;
    case "cursor":
      return "커서 위치";
  }
}

// What the Rust executor returns (src-tauri/src/computer.rs ComputerOutcome).
export interface ComputerOutcome {
  ok: boolean;
  error?: string;
  image?: string; // base64 PNG (screenshot)
  width?: number;
  height?: number;
  text?: string;
}

// Runs one command on this machine. Never throws — every failure becomes
// ok=false with a Korean reason the model can relay.
export async function executeComputerCommand(cmd: ComputerCommand): Promise<ComputerOutcome> {
  if (!isComputerUseEnabled()) {
    return {
      ok: false,
      error: "데스크톱 앱 설정(일반 › 컴퓨터 조종)에서 컴퓨터 조종이 꺼져 있습니다 — 사용자가 켜야 합니다",
    };
  }
  if (!isTauri()) {
    return { ok: false, error: "웹 빌드에서는 컴퓨터 조종을 실행할 수 없습니다 (데스크톱 앱 필요)" };
  }
  try {
    const { invoke } = await import("@tauri-apps/api/core");
    const out = await invoke<ComputerOutcome>("computer_action", { cmd: toInvokeArgs(cmd) });
    return out ?? { ok: false, error: "실행 결과가 비어 있습니다" };
  } catch (e) {
    return { ok: false, error: e instanceof Error ? e.message : String(e) };
  }
}

// Flat snake_case shape the Rust side deserializes (serde, all optional).
export function toInvokeArgs(cmd: ComputerCommand): Record<string, unknown> {
  switch (cmd.action) {
    case "screenshot":
      return cmd.zoom
        ? { action: cmd.action, x: cmd.zoom.x, y: cmd.zoom.y, width: cmd.zoom.width, height: cmd.zoom.height }
        : { action: cmd.action };
    case "click":
      return { action: cmd.action, x: cmd.x, y: cmd.y, button: cmd.button };
    case "double_click":
    case "right_click":
    case "move":
      return { action: cmd.action, x: cmd.x, y: cmd.y };
    case "drag":
      return { action: cmd.action, x: cmd.x, y: cmd.y, to_x: cmd.toX, to_y: cmd.toY };
    case "scroll":
      return { action: cmd.action, x: cmd.x, y: cmd.y, scroll: cmd.direction, amount: cmd.amount };
    case "type":
      return { action: cmd.action, text: cmd.text };
    case "key":
      return { action: cmd.action, key: cmd.key };
    case "wait":
      return { action: cmd.action, wait_ms: cmd.waitMs };
    case "cursor":
      return { action: cmd.action };
  }
}

// Full round trip for one pushed frame: execute, then report to the gateway
// (correlated by the frame's Ref id). Returns the outcome for the caller's
// nudge; reporting failures are logged — the gateway's wait will time out and
// tell the model the desktop did not answer.
export async function handleComputerFrame(
  cfg: GatewayConfig,
  id: string,
  cmd: ComputerCommand,
  run: (cmd: ComputerCommand) => Promise<ComputerOutcome> = executeComputerCommand,
): Promise<ComputerOutcome> {
  const outcome = await run(cmd);
  if (!outcome.ok) cuLog.warn(`computer ${cmd.action} failed`, outcome.error ?? "");
  try {
    await callRpc(cfg, COMPUTER_RPC.result, {
      id,
      ok: outcome.ok,
      error: outcome.error ?? "",
      image: outcome.image ?? "",
      width: outcome.width ?? 0,
      height: outcome.height ?? 0,
      text: outcome.text ?? "",
    });
  } catch (e) {
    cuLog.error("computer result report failed", e instanceof Error ? e.message : String(e));
  }
  return outcome;
}
