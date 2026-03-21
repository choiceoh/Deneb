import { describe, expect, it } from "vitest";
import { AttentionManager } from "./attention.js";
import type { AutonomousState } from "./types.js";

function createEmptyState(): AutonomousState {
  return {
    version: 1,
    goals: [],
    observations: [],
    plans: [],
    socialContext: [],
    lastCycleAt: 0,
    nextCycleAt: 0,
    cycleCount: 0,
  };
}

describe("AttentionManager", () => {
  it("adds and retrieves signals sorted by urgency", () => {
    const mgr = new AttentionManager();
    mgr.addMessage("a", "low priority", 0.2);
    mgr.addMessage("b", "high priority", 0.9);
    mgr.addMessage("c", "medium priority", 0.5);

    const signals = mgr.getTopSignals(3);
    expect(signals.length).toBe(3);
    expect(signals[0]?.source).toBe("b");
    expect(signals[1]?.source).toBe("c");
    expect(signals[2]?.source).toBe("a");
  });

  it("drains returned signals from buffer", () => {
    const mgr = new AttentionManager();
    mgr.addMessage("a", "msg1", 0.5);
    mgr.addMessage("b", "msg2", 0.5);

    const first = mgr.getTopSignals(1);
    expect(first.length).toBe(1);
    expect(mgr.pendingCount).toBe(1);

    const second = mgr.getTopSignals(10);
    expect(second.length).toBe(1);
    expect(mgr.pendingCount).toBe(0);
  });

  it("caps signal buffer at MAX_SIGNALS", () => {
    const mgr = new AttentionManager();
    for (let i = 0; i < 100; i++) {
      mgr.addMessage(`src-${i}`, `msg-${i}`, Math.random());
    }
    expect(mgr.pendingCount).toBeLessThanOrEqual(50);
  });

  it("hasUrgentSignals works correctly", () => {
    const mgr = new AttentionManager();
    expect(mgr.hasUrgentSignals()).toBe(false);

    mgr.addMessage("a", "low", 0.3);
    expect(mgr.hasUrgentSignals(0.8)).toBe(false);

    mgr.addMessage("b", "urgent", 0.95);
    expect(mgr.hasUrgentSignals(0.8)).toBe(true);
  });

  it("clear removes all signals", () => {
    const mgr = new AttentionManager();
    mgr.addMessage("a", "test", 0.5);
    mgr.addEvent("b", "event", 0.3);
    expect(mgr.pendingCount).toBe(2);

    mgr.clear();
    expect(mgr.pendingCount).toBe(0);
  });

  it("rejects signals with NaN urgency", () => {
    const mgr = new AttentionManager();
    mgr.addSignal({
      source: "bad",
      type: "event",
      content: "NaN urgency",
      urgency: NaN,
      timestamp: Date.now(),
    });
    mgr.addMessage("good", "valid", 0.5);

    // NaN urgency signal should be rejected, only 1 remains.
    const signals = mgr.getTopSignals(10);
    expect(signals.length).toBe(1);
    expect(signals[0]?.source).toBe("good");
  });

  it("derives signals from state with overdue goals", () => {
    const mgr = new AttentionManager();
    const state = createEmptyState();
    state.goals.push({
      id: "g1",
      description: "Overdue goal",
      priority: "high",
      status: "active",
      createdAt: Date.now() - 86400000,
      dueAt: Date.now() - 1000, // Past due.
    });

    mgr.deriveSignalsFromState(state);
    expect(mgr.pendingCount).toBe(1);
    const signals = mgr.getTopSignals(1);
    expect(signals[0]?.content).toContain("overdue");
  });

  it("derives signals from state with due follow-ups", () => {
    const mgr = new AttentionManager();
    const state = createEmptyState();
    state.socialContext.push({
      id: "s1",
      channel: "discord",
      peerId: "alice",
      lastInteraction: Date.now() - 3600000,
      context: "Check on project",
      followUpAt: Date.now() - 1000, // Past due.
    });

    mgr.deriveSignalsFromState(state);
    expect(mgr.pendingCount).toBe(1);
  });

  it("skips completed goals in deriveSignalsFromState", () => {
    const mgr = new AttentionManager();
    const state = createEmptyState();
    state.goals.push({
      id: "g1",
      description: "Done goal",
      priority: "high",
      status: "completed",
      createdAt: Date.now(),
      dueAt: Date.now() - 1000,
    });

    mgr.deriveSignalsFromState(state);
    expect(mgr.pendingCount).toBe(0);
  });
});
