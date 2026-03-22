import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import {
  addGoal,
  addObservation,
  addPlan,
  addSocialEntry,
  loadAutonomousState,
  saveAutonomousState,
  updateGoal,
  updatePlan,
  updateSocialEntry,
} from "./state-store.js";
import type { AutonomousState } from "./types.js";

let tmpDir: string;
let storePath: string;

beforeEach(async () => {
  tmpDir = await fs.promises.mkdtemp(path.join(os.tmpdir(), "autonomous-test-"));
  storePath = path.join(tmpDir, "state.json");
});

afterEach(async () => {
  await fs.promises.rm(tmpDir, { recursive: true, force: true }).catch(() => undefined);
});

describe("loadAutonomousState", () => {
  it("returns empty state when file does not exist", async () => {
    const state = await loadAutonomousState(storePath);
    expect(state.version).toBe(1);
    expect(state.goals).toEqual([]);
    expect(state.observations).toEqual([]);
    expect(state.cycleCount).toBe(0);
  });

  it("loads saved state", async () => {
    const state = await loadAutonomousState(storePath);
    addGoal(state, "Test goal", "high");
    state.cycleCount = 5;
    await saveAutonomousState(state, storePath);

    const loaded = await loadAutonomousState(storePath);
    expect(loaded.goals.length).toBe(1);
    expect(loaded.goals[0]?.description).toBe("Test goal");
    expect(loaded.cycleCount).toBe(5);
  });

  it("recovers from corrupted JSON", async () => {
    await fs.promises.mkdir(path.dirname(storePath), { recursive: true });
    await fs.promises.writeFile(storePath, "not valid json {{{");

    const state = await loadAutonomousState(storePath);
    expect(state.version).toBe(1);
    expect(state.goals).toEqual([]);
  });

  it("recovers from empty file", async () => {
    await fs.promises.mkdir(path.dirname(storePath), { recursive: true });
    await fs.promises.writeFile(storePath, "");

    const state = await loadAutonomousState(storePath);
    expect(state.version).toBe(1);
  });

  it("handles NaN timestamps in stored data", async () => {
    await fs.promises.mkdir(path.dirname(storePath), { recursive: true });
    const badState = {
      version: 1,
      state: {
        version: 1,
        goals: [],
        observations: [],
        plans: [],
        socialContext: [],
        lastCycleAt: "not-a-number",
        nextCycleAt: null,
        cycleCount: -5,
      },
    };
    await fs.promises.writeFile(storePath, JSON.stringify(badState));

    const state = await loadAutonomousState(storePath);
    expect(state.lastCycleAt).toBe(0);
    expect(state.nextCycleAt).toBe(0);
    expect(state.cycleCount).toBe(0);
  });
});

describe("saveAutonomousState", () => {
  it("creates directory if missing", async () => {
    const deepPath = path.join(tmpDir, "deep", "nested", "state.json");
    const state = await loadAutonomousState(deepPath);
    await saveAutonomousState(state, deepPath);

    const exists = await fs.promises
      .access(deepPath)
      .then(() => true)
      .catch(() => false);
    expect(exists).toBe(true);
  });

  it("writes valid JSON", async () => {
    const state = await loadAutonomousState(storePath);
    addGoal(state, "Goal 1", "medium");
    await saveAutonomousState(state, storePath);

    const raw = await fs.promises.readFile(storePath, "utf-8");
    const parsed = JSON.parse(raw);
    expect(parsed.version).toBe(1);
    expect(parsed.state.goals.length).toBe(1);
  });
});

describe("addGoal", () => {
  it("creates a goal with valid fields", () => {
    const state = createEmptyState();
    const goal = addGoal(state, "Learn TypeScript", "high", Date.now() + 86400000);
    expect(goal.id).toBeTypeOf("string");
    expect(goal.id.length).toBeGreaterThan(0);
    expect(goal.description).toBe("Learn TypeScript");
    expect(goal.priority).toBe("high");
    expect(goal.status).toBe("active");
    expect(state.goals.length).toBe(1);
  });

  it("sanitizes description", () => {
    const state = createEmptyState();
    const goal = addGoal(state, "  Hello\0World  ", "medium");
    expect(goal.description).toBe("HelloWorld");
  });

  it("rejects invalid priority", () => {
    const state = createEmptyState();
    // @ts-expect-error testing invalid input
    expect(() => addGoal(state, "Test", "invalid-priority")).toThrow("Invalid goal priority");
  });
});

describe("addObservation", () => {
  it("creates an observation", () => {
    const state = createEmptyState();
    const obs = addObservation(state, "discord:general", "Something happened");
    expect(obs.id).toBeTypeOf("string");
    expect(obs.source).toBe("discord:general");
    expect(obs.processed).toBe(false);
    expect(state.observations.length).toBe(1);
  });
});

describe("addPlan", () => {
  it("creates a plan with steps", () => {
    const state = createEmptyState();
    const plan = addPlan(state, ["Step 1", "Step 2", "Step 3"]);
    expect(plan.steps.length).toBe(3);
    expect(plan.currentStep).toBe(0);
    expect(plan.status).toBe("active");
  });
});

describe("addSocialEntry", () => {
  it("creates a social entry", () => {
    const state = createEmptyState();
    const entry = addSocialEntry(state, "discord", "user123", "Discussing AI");
    expect(entry.channel).toBe("discord");
    expect(entry.peerId).toBe("user123");
    expect(entry.context).toBe("Discussing AI");
  });
});

describe("updateGoal", () => {
  it("updates an existing goal", () => {
    const state = createEmptyState();
    const goal = addGoal(state, "Original", "low");
    const updated = updateGoal(state, goal.id, { status: "completed", progress: "Done!" });
    expect(updated?.status).toBe("completed");
    expect(updated?.progress).toBe("Done!");
  });

  it("sets lastProgressAt and lastProgressCycleCount when progress is updated", () => {
    const state = createEmptyState();
    state.cycleCount = 7;
    const goal = addGoal(state, "Track progress", "medium");
    expect(goal.lastProgressAt).toBeUndefined();
    expect(goal.lastProgressCycleCount).toBeUndefined();

    const before = Date.now();
    const updated = updateGoal(state, goal.id, { progress: "50% done" });
    const after = Date.now();

    expect(updated?.lastProgressAt).toBeTypeOf("number");
    expect(updated?.lastProgressAt).toBeGreaterThanOrEqual(before);
    expect(updated?.lastProgressAt).toBeLessThanOrEqual(after);
    expect(updated?.lastProgressCycleCount).toBe(7);
  });

  it("does not set lastProgressAt when only status changes", () => {
    const state = createEmptyState();
    const goal = addGoal(state, "Status only", "medium");
    const updated = updateGoal(state, goal.id, { status: "paused" });

    expect(updated?.lastProgressAt).toBeUndefined();
  });

  it("returns null for missing goal", () => {
    const state = createEmptyState();
    expect(updateGoal(state, "nonexistent", { status: "completed" })).toBe(null);
  });
});

describe("updatePlan", () => {
  it("updates an existing plan", () => {
    const state = createEmptyState();
    const plan = addPlan(state, ["A", "B"]);
    const updated = updatePlan(state, plan.id, { currentStep: 1 });
    expect(updated?.currentStep).toBe(1);
  });

  it("returns null for missing plan", () => {
    const state = createEmptyState();
    expect(updatePlan(state, "nonexistent", {})).toBe(null);
  });
});

describe("updateSocialEntry", () => {
  it("updates an existing entry", () => {
    const state = createEmptyState();
    const entry = addSocialEntry(state, "slack", "bob", "Work stuff");
    const updated = updateSocialEntry(state, entry.id, { context: "Updated context" });
    expect(updated?.context).toBe("Updated context");
  });

  it("returns null for missing entry", () => {
    const state = createEmptyState();
    expect(updateSocialEntry(state, "nonexistent", {})).toBe(null);
  });
});

describe("pendingSignals persistence", () => {
  it("saves and loads pending signals", async () => {
    const state = await loadAutonomousState(storePath);
    state.pendingSignals = [
      {
        source: "discord:general",
        type: "message",
        content: "Hello world",
        urgency: 0.7,
        timestamp: Date.now(),
      },
    ];
    await saveAutonomousState(state, storePath);

    const loaded = await loadAutonomousState(storePath);
    expect(loaded.pendingSignals.length).toBe(1);
    expect(loaded.pendingSignals[0]?.source).toBe("discord:general");
    expect(loaded.pendingSignals[0]?.urgency).toBe(0.7);
  });

  it("defaults to empty array when pendingSignals missing", async () => {
    await fs.promises.mkdir(path.dirname(storePath), { recursive: true });
    const stateWithoutSignals = {
      version: 1,
      state: {
        version: 1,
        goals: [],
        observations: [],
        plans: [],
        socialContext: [],
        lastCycleAt: 0,
        nextCycleAt: 0,
        cycleCount: 0,
      },
    };
    await fs.promises.writeFile(storePath, JSON.stringify(stateWithoutSignals));

    const loaded = await loadAutonomousState(storePath);
    expect(loaded.pendingSignals).toEqual([]);
  });

  it("filters invalid signals on load", async () => {
    await fs.promises.mkdir(path.dirname(storePath), { recursive: true });
    const stateWithBadSignals = {
      version: 1,
      state: {
        version: 1,
        goals: [],
        observations: [],
        plans: [],
        socialContext: [],
        pendingSignals: [
          {
            source: "good",
            type: "message",
            content: "valid",
            urgency: 0.5,
            timestamp: Date.now(),
          },
          {
            source: 123,
            type: "message",
            content: "bad source",
            urgency: 0.5,
            timestamp: Date.now(),
          },
          {
            source: "bad",
            type: "invalid-type",
            content: "bad type",
            urgency: 0.5,
            timestamp: Date.now(),
          },
          {
            source: "bad",
            type: "message",
            content: "nan urgency",
            urgency: NaN,
            timestamp: Date.now(),
          },
        ],
        lastCycleAt: 0,
        nextCycleAt: 0,
        cycleCount: 0,
      },
    };
    await fs.promises.writeFile(storePath, JSON.stringify(stateWithBadSignals));

    const loaded = await loadAutonomousState(storePath);
    expect(loaded.pendingSignals.length).toBe(1);
    expect(loaded.pendingSignals[0]?.source).toBe("good");
  });
});

describe("lastCycleOutcome persistence", () => {
  it("saves and loads cycle outcome", async () => {
    const state = await loadAutonomousState(storePath);
    state.lastCycleOutcome = {
      cycleNumber: 5,
      startedAt: Date.now() - 10000,
      finishedAt: Date.now() - 5000,
      actionsTaken: ["agent-turn"],
      error: undefined,
    };
    await saveAutonomousState(state, storePath);

    const loaded = await loadAutonomousState(storePath);
    expect(loaded.lastCycleOutcome).toBeDefined();
    expect(loaded.lastCycleOutcome?.cycleNumber).toBe(5);
    expect(loaded.lastCycleOutcome?.actionsTaken).toEqual(["agent-turn"]);
  });

  it("loads cycle outcome with error", async () => {
    const state = await loadAutonomousState(storePath);
    state.lastCycleOutcome = {
      cycleNumber: 3,
      startedAt: Date.now() - 5000,
      finishedAt: Date.now(),
      actionsTaken: [],
      error: "timeout: something went wrong",
    };
    await saveAutonomousState(state, storePath);

    const loaded = await loadAutonomousState(storePath);
    expect(loaded.lastCycleOutcome?.error).toBe("timeout: something went wrong");
  });

  it("handles missing lastCycleOutcome gracefully", async () => {
    const state = await loadAutonomousState(storePath);
    expect(state.lastCycleOutcome).toBeUndefined();
  });
});

describe("observation TTL auto-expiry", () => {
  it("removes old unprocessed observations on save", async () => {
    const state = createEmptyState();
    const eightDaysAgo = Date.now() - 8 * 24 * 3_600_000;
    const oneDayAgo = Date.now() - 1 * 24 * 3_600_000;
    const oldObs = addObservation(state, "old-src", "old observation");
    const recentObs = addObservation(state, "recent-src", "recent observation");

    // Manually set timestamps.
    oldObs.observedAt = eightDaysAgo;
    recentObs.observedAt = oneDayAgo;

    await saveAutonomousState(state, storePath);

    const loaded = await loadAutonomousState(storePath);
    // Old unprocessed observation should be expired.
    expect(loaded.observations.length).toBe(1);
    expect(loaded.observations[0]?.source).toBe("recent-src");
  });

  it("keeps old processed observations", async () => {
    const state = createEmptyState();
    const eightDaysAgo = Date.now() - 8 * 24 * 3_600_000;
    const obs = addObservation(state, "old-processed", "old but processed");
    obs.observedAt = eightDaysAgo;
    obs.processed = true;

    await saveAutonomousState(state, storePath);

    const loaded = await loadAutonomousState(storePath);
    expect(loaded.observations.length).toBe(1);
    expect(loaded.observations[0]?.source).toBe("old-processed");
  });
});

describe("concurrent saves (async mutex)", () => {
  it("serializes concurrent saves without corruption", async () => {
    const state = await loadAutonomousState(storePath);
    addGoal(state, "Goal A", "high");

    // Fire multiple concurrent saves.
    const saves = Array.from({ length: 5 }, (_, i) => {
      state.cycleCount = i + 1;
      return saveAutonomousState(state, storePath);
    });
    await Promise.all(saves);

    // File should be valid JSON with last value.
    const loaded = await loadAutonomousState(storePath);
    expect(loaded.goals.length).toBe(1);
    expect(loaded.cycleCount).toBe(5);
  });
});

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
