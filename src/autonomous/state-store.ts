import crypto from "node:crypto";
import fs from "node:fs";
import path from "node:path";
import { CONFIG_DIR } from "../utils.js";
import type {
  AutonomousState,
  AutonomousStoreFile,
  Goal,
  GoalPriority,
  Observation,
  ObservationRelevance,
  Plan,
  SocialEntry,
} from "./types.js";

export const DEFAULT_AUTONOMOUS_DIR = path.join(CONFIG_DIR, "autonomous");
export const DEFAULT_AUTONOMOUS_STORE_PATH = path.join(DEFAULT_AUTONOMOUS_DIR, "state.json");

const MAX_OBSERVATIONS = 100;
const MAX_COMPLETED_GOALS = 50;
const MAX_SOCIAL_ENTRIES = 200;

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

export async function loadAutonomousState(storePath?: string): Promise<AutonomousState> {
  const resolved = storePath ?? DEFAULT_AUTONOMOUS_STORE_PATH;
  try {
    const raw = await fs.promises.readFile(resolved, "utf-8");
    const parsed: unknown = JSON.parse(raw);
    if (!parsed || typeof parsed !== "object" || Array.isArray(parsed)) {
      return createEmptyState();
    }
    const record = parsed as Record<string, unknown>;
    const state = record.state as AutonomousState | undefined;
    if (!state || typeof state !== "object") {
      return createEmptyState();
    }
    return {
      version: 1,
      goals: Array.isArray(state.goals) ? state.goals : [],
      observations: Array.isArray(state.observations) ? state.observations : [],
      plans: Array.isArray(state.plans) ? state.plans : [],
      socialContext: Array.isArray(state.socialContext) ? state.socialContext : [],
      lastCycleAt: typeof state.lastCycleAt === "number" ? state.lastCycleAt : 0,
      nextCycleAt: typeof state.nextCycleAt === "number" ? state.nextCycleAt : 0,
      cycleCount: typeof state.cycleCount === "number" ? state.cycleCount : 0,
    };
  } catch (err) {
    if ((err as { code?: unknown })?.code === "ENOENT") {
      return createEmptyState();
    }
    throw err;
  }
}

export async function saveAutonomousState(
  state: AutonomousState,
  storePath?: string,
): Promise<void> {
  const resolved = storePath ?? DEFAULT_AUTONOMOUS_STORE_PATH;
  const storeDir = path.dirname(resolved);
  await fs.promises.mkdir(storeDir, { recursive: true, mode: 0o700 });

  const pruned = pruneState(state);
  const store: AutonomousStoreFile = { version: 1, state: pruned };
  const json = JSON.stringify(store, null, 2);

  const tmp = `${resolved}.${process.pid}.${crypto.randomBytes(8).toString("hex")}.tmp`;
  await fs.promises.writeFile(tmp, json, { encoding: "utf-8", mode: 0o600 });

  try {
    await fs.promises.rename(tmp, resolved);
  } catch {
    await fs.promises.copyFile(tmp, resolved);
    await fs.promises.unlink(tmp).catch(() => undefined);
  }
}

function pruneState(state: AutonomousState): AutonomousState {
  const observations = state.observations
    .toSorted((a, b) => b.observedAt - a.observedAt)
    .slice(0, MAX_OBSERVATIONS);

  const activeGoals = state.goals.filter((g) => g.status !== "completed");
  const completedGoals = state.goals
    .filter((g) => g.status === "completed")
    .toSorted((a, b) => b.createdAt - a.createdAt)
    .slice(0, MAX_COMPLETED_GOALS);

  const socialContext = state.socialContext
    .toSorted((a, b) => b.lastInteraction - a.lastInteraction)
    .slice(0, MAX_SOCIAL_ENTRIES);

  return {
    ...state,
    goals: [...activeGoals, ...completedGoals],
    observations,
    socialContext,
  };
}

// -- Mutation helpers --

export function addGoal(
  state: AutonomousState,
  description: string,
  priority: GoalPriority = "medium",
  dueAt?: number,
): Goal {
  const goal: Goal = {
    id: crypto.randomUUID(),
    description,
    priority,
    status: "active",
    createdAt: Date.now(),
    dueAt,
  };
  state.goals.push(goal);
  return goal;
}

export function addObservation(
  state: AutonomousState,
  source: string,
  content: string,
  relevance?: ObservationRelevance,
): Observation {
  const obs: Observation = {
    id: crypto.randomUUID(),
    source,
    content,
    observedAt: Date.now(),
    processed: false,
    relevance,
  };
  state.observations.push(obs);
  return obs;
}

export function addPlan(state: AutonomousState, steps: string[], goalId?: string): Plan {
  const plan: Plan = {
    id: crypto.randomUUID(),
    goalId,
    steps,
    currentStep: 0,
    status: "active",
  };
  state.plans.push(plan);
  return plan;
}

export function addSocialEntry(
  state: AutonomousState,
  channel: string,
  peerId: string,
  context: string,
  followUpAt?: number,
): SocialEntry {
  const entry: SocialEntry = {
    id: crypto.randomUUID(),
    channel,
    peerId,
    lastInteraction: Date.now(),
    context,
    followUpAt,
  };
  state.socialContext.push(entry);
  return entry;
}

export function updateGoal(
  state: AutonomousState,
  goalId: string,
  patch: Partial<Pick<Goal, "status" | "progress" | "priority" | "description" | "dueAt">>,
): Goal | null {
  const goal = state.goals.find((g) => g.id === goalId);
  if (!goal) {
    return null;
  }
  Object.assign(goal, patch);
  return goal;
}

export function updatePlan(
  state: AutonomousState,
  planId: string,
  patch: Partial<Pick<Plan, "currentStep" | "status" | "steps">>,
): Plan | null {
  const plan = state.plans.find((p) => p.id === planId);
  if (!plan) {
    return null;
  }
  Object.assign(plan, patch);
  return plan;
}

export function updateSocialEntry(
  state: AutonomousState,
  entryId: string,
  patch: Partial<Pick<SocialEntry, "context" | "followUpAt" | "lastInteraction">>,
): SocialEntry | null {
  const entry = state.socialContext.find((e) => e.id === entryId);
  if (!entry) {
    return null;
  }
  Object.assign(entry, patch);
  return entry;
}
