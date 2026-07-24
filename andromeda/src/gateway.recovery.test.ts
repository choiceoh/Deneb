import { describe, expect, it } from "vitest";

import {
  CHAT_RECOVERY_BUDGET_MS,
  CHAT_RECOVERY_POLL_MS,
  type GatewayConfig,
  type TranscriptMsg,
  composeChatMessage,
  effectiveTurnProbe,
  probeTranscriptForTurn,
  recoverTurnAnswer,
} from "./gateway";

const cfg = { url: "http://gateway.test", token: "t" } as GatewayConfig;
const user = (content: string): TranscriptMsg => ({ role: "user", content });
const assistant = (content: string): TranscriptMsg => ({ role: "assistant", content });

// A virtual clock advanced by the injected sleep, so a full poll window resolves
// instantly (Date.now / real timers are never touched).
function virtualClock() {
  let t = 0;
  return {
    now: () => t,
    sleep: async (ms: number) => {
      t += ms;
    },
  };
}

describe("probeTranscriptForTurn", () => {
  it("classifies a user turn with no reply yet as running", () => {
    expect(probeTranscriptForTurn([user("question")], "question")).toEqual({ kind: "running" });
  });

  it("returns the last non-empty assistant row after the user turn as the answer", () => {
    const msgs = [user("question"), assistant(""), assistant("final answer")];
    expect(probeTranscriptForTurn(msgs, "question")).toEqual({ kind: "answered", text: "final answer" });
  });

  it("returns notArrived when the user turn is absent or blank", () => {
    expect(probeTranscriptForTurn([user("other")], "question")).toEqual({ kind: "notArrived" });
    expect(probeTranscriptForTurn([user("question")], "   ")).toEqual({ kind: "notArrived" });
  });

  it("matches the composed user text (work-area prefix included)", () => {
    const sent = composeChatMessage("정리해줘", "표: 매출 100");
    const msgs = [user(sent), assistant("done")];
    expect(probeTranscriptForTurn(msgs, sent)).toEqual({ kind: "answered", text: "done" });
    // A bare message must not match the composed row.
    expect(probeTranscriptForTurn(msgs, "정리해줘")).toEqual({ kind: "notArrived" });
  });
});

describe("recoverTurnAnswer", () => {
  it("polls past the short budget to recover a late answer once the turn is confirmed running", async () => {
    // 90s / 3s = 30 polls is the old fixed budget. Stay "running" well past it,
    // then answer — a tool-heavy turn runs minutes. Under a fixed budget this
    // returned null and froze on the preamble.
    const shortBudgetPolls = CHAT_RECOVERY_BUDGET_MS / CHAT_RECOVERY_POLL_MS;
    const queue = [
      ...Array.from({ length: shortBudgetPolls + 5 }, () => ({ messages: [user("question")], turnRunning: false })),
      { messages: [user("question"), assistant("late answer")], turnRunning: false },
      { messages: [user("question"), assistant("late answer")], turnRunning: false },
    ];
    let calls = 0;
    const { now, sleep } = virtualClock();
    const recovered = await recoverTurnAnswer(cfg, "client:main", "question", {
      now,
      sleep,
      fetchSnapshot: async () => queue[Math.min(calls++, queue.length - 1)],
    });
    expect(recovered).toEqual({ text: "late answer", reasoning: undefined });
    expect(calls).toBeGreaterThan(shortBudgetPolls);
  });

  it("gives up at the short budget when the turn never confirms running (no signal)", async () => {
    // Transcript never reachable: without a still-running confirmation the window
    // stays the short budget so a truly-lost turn can't hang the resend path.
    let calls = 0;
    const { now, sleep } = virtualClock();
    const recovered = await recoverTurnAnswer(cfg, "client:main", "question", {
      now,
      sleep,
      fetchSnapshot: async () => {
        calls++;
        throw new Error("unreachable");
      },
    });
    const shortBudgetPolls = CHAT_RECOVERY_BUDGET_MS / CHAT_RECOVERY_POLL_MS;
    expect(recovered).toBeNull();
    expect(calls).toBeLessThanOrEqual(shortBudgetPolls + 1);
  });

  it("returns null after two confirmed notArrived polls", async () => {
    let calls = 0;
    const { now, sleep } = virtualClock();
    const recovered = await recoverTurnAnswer(cfg, "client:main", "question", {
      now,
      sleep,
      fetchSnapshot: async () => {
        calls++;
        return { messages: [user("different")], turnRunning: false };
      },
    });
    expect(recovered).toBeNull();
    expect(calls).toBe(2);
  });

  it("requires the same answered tail twice before accepting", async () => {
    const queue = [
      { messages: [user("question"), assistant("first")], turnRunning: false },
      { messages: [user("question"), assistant("second")], turnRunning: false },
      { messages: [user("question"), assistant("second")], turnRunning: false },
    ];
    let calls = 0;
    const { now, sleep } = virtualClock();
    const recovered = await recoverTurnAnswer(cfg, "client:main", "question", {
      now,
      sleep,
      fetchSnapshot: async () => queue[Math.min(calls++, queue.length - 1)],
    });
    expect(recovered).toEqual({ text: "second", reasoning: undefined });
    expect(calls).toBe(3);
  });

  it("treats a stable preamble as still running while turnRunning is true", () => {
    expect(
      effectiveTurnProbe(
        probeTranscriptForTurn([user("question"), assistant("확인하고 답할게요")], "question"),
        true,
      ),
    ).toEqual({ kind: "running" });
  });

  it("polls past a stable preamble once the server run finishes", async () => {
    const preamble = [user("question"), assistant("확인하고 답할게요")];
    const finalAnswer = [user("question"), assistant("확인하고 답할게요"), assistant("final answer")];
    const queue = [
      { messages: preamble, turnRunning: true },
      { messages: preamble, turnRunning: true },
      { messages: finalAnswer, turnRunning: false },
      { messages: finalAnswer, turnRunning: false },
    ];
    let calls = 0;
    const { now, sleep } = virtualClock();
    const recovered = await recoverTurnAnswer(cfg, "client:main", "question", {
      now,
      sleep,
      fetchSnapshot: async () => queue[Math.min(calls++, queue.length - 1)],
    });
    expect(recovered).toEqual({ text: "final answer", reasoning: undefined });
    expect(calls).toBe(4);
  });

  it("aborts immediately when the signal is already aborted", async () => {
    const controller = new AbortController();
    controller.abort();
    let calls = 0;
    const recovered = await recoverTurnAnswer(cfg, "client:main", "question", {
      signal: controller.signal,
      fetchSnapshot: async () => {
        calls++;
        return { messages: [user("question")], turnRunning: false };
      },
    });
    expect(recovered).toBeNull();
    expect(calls).toBe(0);
  });
});
