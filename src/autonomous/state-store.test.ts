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
