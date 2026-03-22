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
    pendingSignals: [],
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

  it("restoreFromState loads persisted signals", () => {
    const mgr = new AttentionManager();
    const state = createEmptyState();
    state.pendingSignals = [
      {
        source: "test",
        type: "message",
        content: "persisted signal",
        urgency: 0.8,
        timestamp: Date.now(),
      },
      {
        source: "test2",
        type: "event",
        content: "another signal",
        urgency: 0.3,
        timestamp: Date.now(),
      },
    ];

    mgr.restoreFromState(state);
    expect(mgr.pendingCount).toBe(2);
    // State should be cleared after restore.
    expect(state.pendingSignals).toEqual([]);
  });

  it("drainToState persists unconsumed signals", () => {
    const mgr = new AttentionManager();
    mgr.addMessage("a", "msg1", 0.5);
    mgr.addEvent("b", "evt1", 0.3);

    const state = createEmptyState();
    mgr.drainToState(state);
    expect(state.pendingSignals.length).toBe(2);
  });

  it("restoreFromState does not duplicate signals when called twice", () => {
    const mgr = new AttentionManager();
    const state = createEmptyState();
    state.pendingSignals = [
      {
        source: "test",
        type: "message",
        content: "signal",
        urgency: 0.5,
        timestamp: Date.now(),
      },
    ];

    mgr.restoreFromState(state);
    // Second call with empty pendingSignals should not add more.
    mgr.restoreFromState(state);
    expect(mgr.pendingCount).toBe(1);
  });

  it("round-trips signals through drain and restore", () => {
    const mgr1 = new AttentionManager();
    mgr1.addMessage("src", "important message", 0.9);
    mgr1.addEvent("src2", "background event", 0.2);

    const state = createEmptyState();
    mgr1.drainToState(state);
    expect(state.pendingSignals.length).toBe(2);

    // Simulate restart: new manager restores from state.
    const mgr2 = new AttentionManager();
    mgr2.restoreFromState(state);
    expect(mgr2.pendingCount).toBe(2);

    const signals = mgr2.getTopSignals(10);
    expect(signals[0]?.urgency).toBe(0.9);
    expect(signals[1]?.urgency).toBe(0.2);
  });

  it("getTopSignals records lastPresented", () => {
    const mgr = new AttentionManager();
    mgr.addMessage("a", "msg1", 0.8);
    mgr.addMessage("b", "msg2", 0.3);

    const signals = mgr.getTopSignals(2);
    expect(signals.length).toBe(2);

    const presented = mgr.getLastPresented();
    expect(presented.length).toBe(2);
    expect(presented[0]?.source).toBe("a");
  });

  it("reEscalateIgnoredSignals boosts urgency for unaddressed goal signals", () => {
    const mgr = new AttentionManager();
    mgr.addSignal({
      source: "goal:g1",
      type: "goal-deadline",
      content: "Goal overdue: test",
      urgency: 0.6,
      timestamp: Date.now(),
    });

    // Present the signal.
    mgr.getTopSignals(10);

    // Pre/post state are identical — agent did nothing.
    const preState = createEmptyState();
    preState.goals.push({
      id: "g1",
      description: "test",
      priority: "high",
      status: "active",
      createdAt: Date.now(),
    });
    const postState = { ...preState, goals: [...preState.goals] };

    mgr.reEscalateIgnoredSignals(preState, postState);

    // Signal should be re-queued with boosted urgency.
    expect(mgr.pendingCount).toBe(1);
    const reQueued = mgr.getTopSignals(1);
    expect(reQueued[0]?.urgency).toBe(0.7); // 0.6 + 0.1 boost
    expect(reQueued[0]?.ignoredCount).toBe(1);
  });

  it("reEscalateIgnoredSignals does not re-queue addressed signals", () => {
    const mgr = new AttentionManager();
    mgr.addSignal({
      source: "goal:g1",
      type: "goal-deadline",
      content: "Goal overdue: test",
      urgency: 0.6,
      timestamp: Date.now(),
    });
    mgr.getTopSignals(10);

    const preState = createEmptyState();
    preState.goals.push({
      id: "g1",
      description: "test",
      priority: "high",
      status: "active",
      createdAt: Date.now(),
    });

    // Post-state: goal progress was updated.
    const postState = createEmptyState();
    postState.goals.push({
      id: "g1",
      description: "test",
      priority: "high",
      status: "active",
      createdAt: Date.now(),
      progress: "50% done",
    });

    mgr.reEscalateIgnoredSignals(preState, postState);

    // Signal was addressed, should NOT be re-queued.
    expect(mgr.pendingCount).toBe(0);
  });

  it("ignoredCount accumulates across multiple cycles", () => {
    const mgr = new AttentionManager();
    mgr.addSignal({
      source: "goal:g1",
      type: "goal-deadline",
      content: "Goal overdue: test",
      urgency: 0.5,
      timestamp: Date.now(),
      ignoredCount: 2,
    });
    mgr.getTopSignals(10);

    const state = createEmptyState();
    state.goals.push({
      id: "g1",
      description: "test",
      priority: "high",
      status: "active",
      createdAt: Date.now(),
    });

    mgr.reEscalateIgnoredSignals(state, state);

    const reQueued = mgr.getTopSignals(1);
    expect(reQueued[0]?.ignoredCount).toBe(3); // 2 + 1
    expect(reQueued[0]?.urgency).toBe(0.6); // 0.5 + 0.1
  });

  it("urgency is capped at 1.0 during re-escalation", () => {
    const mgr = new AttentionManager();
    mgr.addSignal({
      source: "goal:g1",
      type: "goal-deadline",
      content: "test",
      urgency: 0.95,
      timestamp: Date.now(),
    });
    mgr.getTopSignals(10);

    const state = createEmptyState();
    state.goals.push({
      id: "g1",
      description: "test",
      priority: "high",
      status: "active",
      createdAt: Date.now(),
    });

    mgr.reEscalateIgnoredSignals(state, state);

    const reQueued = mgr.getTopSignals(1);
    expect(reQueued[0]?.urgency).toBe(1.0);
  });

  it("derives stale goal signals based on cycle count gap", () => {
    const mgr = new AttentionManager();
    const state = createEmptyState();
    state.cycleCount = 20;
    state.lastCycleAt = Date.now();
    state.goals.push({
      id: "stale-g1",
      description: "Stale goal",
      priority: "medium",
      status: "active",
      createdAt: Date.now() - 86400000,
      lastProgressCycleCount: 5, // Last progress at cycle 5, now at 20 = 15 cycles stale.
    });

    mgr.deriveSignalsFromState(state);

    const signals = mgr.getTopSignals(10);
    const staleSignal = signals.find((s) => s.content.includes("Stale goal"));
    expect(staleSignal).toBeDefined();
    expect(staleSignal?.content).toContain("no progress for ~15 cycles");
  });

  it("does not flag goals with recent progress cycle count as stale", () => {
    const mgr = new AttentionManager();
    const state = createEmptyState();
    state.cycleCount = 10;
    state.lastCycleAt = Date.now();
    state.goals.push({
      id: "fresh-g1",
      description: "Fresh goal",
      priority: "medium",
      status: "active",
      createdAt: Date.now() - 60000,
      lastProgressCycleCount: 8, // Only 2 cycles ago.
    });

    mgr.deriveSignalsFromState(state);

    const signals = mgr.getTopSignals(10);
    const staleSignal = signals.find((s) => s.content.includes("Stale goal"));
    expect(staleSignal).toBeUndefined();
  });

  it("treats goal without lastProgressCycleCount as stale from cycle 0", () => {
    const mgr = new AttentionManager();
    const state = createEmptyState();
    state.cycleCount = 6; // 6 cycles with no progress = stale (threshold = 5).
    state.lastCycleAt = Date.now();
    state.goals.push({
      id: "old-g1",
      description: "Old goal",
      priority: "high",
      status: "active",
      createdAt: Date.now() - 86400000,
      // No lastProgressCycleCount — falls back to 0.
    });

    mgr.deriveSignalsFromState(state);

    const signals = mgr.getTopSignals(10);
    const staleSignal = signals.find((s) => s.content.includes("Stale goal"));
    expect(staleSignal).toBeDefined();
  });

  it("does not re-escalate external message when agent made state changes", () => {
    const mgr = new AttentionManager();
    mgr.addSignal({
      source: "telegram:user123",
      type: "message",
      content: "Hello from user",
      urgency: 0.6,
      timestamp: Date.now(),
    });
    mgr.getTopSignals(10);

    const preState = createEmptyState();
    const postState = createEmptyState();
    // Agent made some state change (processed an observation).
    postState.observations.push({
      id: "obs1",
      source: "test",
      content: "something",
      observedAt: Date.now(),
      processed: true,
    });

    mgr.reEscalateIgnoredSignals(preState, postState);

    // External message should NOT be re-escalated since agent was active.
    expect(mgr.pendingCount).toBe(0);
  });

  it("re-escalates external message when agent made no state changes", () => {
    const mgr = new AttentionManager();
    mgr.addSignal({
      source: "telegram:user123",
      type: "message",
      content: "Hello from user",
      urgency: 0.6,
      timestamp: Date.now(),
    });
    mgr.getTopSignals(10);

    const preState = createEmptyState();
    const postState = createEmptyState();
    // No state changes — agent did nothing.

    mgr.reEscalateIgnoredSignals(preState, postState);

    // External message SHOULD be re-escalated.
    expect(mgr.pendingCount).toBe(1);
    const reQueued = mgr.getTopSignals(1);
    expect(reQueued[0]?.ignoredCount).toBe(1);
  });

  it("clear resets lastPresented", () => {
    const mgr = new AttentionManager();
    mgr.addMessage("a", "test", 0.5);
    mgr.getTopSignals(1);
    expect(mgr.getLastPresented().length).toBe(1);

    mgr.clear();
    expect(mgr.getLastPresented().length).toBe(0);
  });
});
