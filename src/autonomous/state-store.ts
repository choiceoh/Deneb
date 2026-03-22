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
import { safeTimestamp, sanitizeText, sanitizeId, validateStringArray } from "./validation.js";

export const DEFAULT_AUTONOMOUS_DIR = path.join(CONFIG_DIR, "autonomous");
export const DEFAULT_AUTONOMOUS_STORE_PATH = path.join(DEFAULT_AUTONOMOUS_DIR, "state.json");

const MAX_OBSERVATIONS = 100;
const MAX_COMPLETED_GOALS = 50;
const MAX_SOCIAL_ENTRIES = 200;

const VALID_GOAL_PRIORITIES = new Set<GoalPriority>(["high", "medium", "low"]);
const VALID_GOAL_STATUSES = new Set(["active", "paused", "completed"]);
const VALID_OBSERVATION_RELEVANCES = new Set(["high", "medium", "low"]);
const VALID_PLAN_STATUSES = new Set(["active", "blocked", "completed"]);

/** Simple in-flight flag to prevent concurrent saves from corrupting state. */
let saveInFlight = false;

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

// -- Validation helpers for loaded entries --

function isValidGoal(entry: unknown): entry is Goal {
  if (!entry || typeof entry !== "object") {
    return false;
  }
  const g = entry as Record<string, unknown>;
  if (typeof g.id !== "string" || g.id.length === 0) {
    return false;
  }
  if (typeof g.description !== "string") {
    return false;
  }
  if (!VALID_GOAL_STATUSES.has(g.status as string)) {
    return false;
  }
  if (!VALID_GOAL_PRIORITIES.has(g.priority as GoalPriority)) {
    return false;
  }
  if (typeof g.createdAt !== "number" || !Number.isFinite(g.createdAt) || g.createdAt < 0) {
    return false;
  }
  if (
    g.dueAt !== undefined &&
    (typeof g.dueAt !== "number" || !Number.isFinite(g.dueAt) || g.dueAt < 0)
  ) {
    return false;
  }
  return true;
}

function sanitizeGoal(g: Goal): Goal {
  return {
    id: sanitizeId(g.id),
    description: sanitizeText(g.description),
    priority: g.priority,
    status: g.status,
    createdAt: safeTimestamp(g.createdAt),
    progress: g.progress !== undefined ? sanitizeText(g.progress) : undefined,
    dueAt: g.dueAt !== undefined ? safeTimestamp(g.dueAt) : undefined,
  };
}

function isValidObservation(entry: unknown): entry is Observation {
  if (!entry || typeof entry !== "object") {
    return false;
  }
  const o = entry as Record<string, unknown>;
  if (typeof o.id !== "string" || o.id.length === 0) {
    return false;
  }
  if (typeof o.source !== "string") {
    return false;
  }
  if (typeof o.content !== "string") {
    return false;
  }
  if (typeof o.observedAt !== "number" || !Number.isFinite(o.observedAt) || o.observedAt < 0) {
    return false;
  }
  if (o.relevance !== undefined && !VALID_OBSERVATION_RELEVANCES.has(o.relevance as string)) {
    return false;
  }
  return true;
}

function sanitizeObservation(o: Observation): Observation {
  return {
    id: sanitizeId(o.id),
    source: sanitizeText(o.source),
    content: sanitizeText(o.content),
    observedAt: safeTimestamp(o.observedAt),
    processed: Boolean(o.processed),
    relevance: o.relevance,
  };
}

function isValidPlan(entry: unknown): entry is Plan {
  if (!entry || typeof entry !== "object") {
    return false;
  }
  const p = entry as Record<string, unknown>;
  if (typeof p.id !== "string" || p.id.length === 0) {
    return false;
  }
  if (!Array.isArray(p.steps)) {
    return false;
  }
  if (!VALID_PLAN_STATUSES.has(p.status as string)) {
    return false;
  }
  if (typeof p.currentStep !== "number" || !Number.isFinite(p.currentStep) || p.currentStep < 0) {
    return false;
  }
  if (p.goalId !== undefined && typeof p.goalId !== "string") {
    return false;
  }
  return true;
}

function sanitizePlan(p: Plan): Plan {
  const steps = validateStringArray(p.steps).map((s) => sanitizeText(s));
  return {
    id: sanitizeId(p.id),
    goalId: p.goalId !== undefined ? sanitizeId(p.goalId) : undefined,
    steps,
    currentStep: Math.min(Math.max(0, Math.floor(p.currentStep)), Math.max(0, steps.length - 1)),
    status: p.status,
  };
}

function isValidSocialEntry(entry: unknown): entry is SocialEntry {
  if (!entry || typeof entry !== "object") {
    return false;
  }
  const e = entry as Record<string, unknown>;
  if (typeof e.id !== "string" || e.id.length === 0) {
    return false;
  }
  if (typeof e.channel !== "string") {
    return false;
  }
  if (typeof e.peerId !== "string") {
    return false;
  }
  if (
    typeof e.lastInteraction !== "number" ||
    !Number.isFinite(e.lastInteraction) ||
    e.lastInteraction < 0
  ) {
    return false;
  }
  if (typeof e.context !== "string") {
    return false;
  }
  if (
    e.followUpAt !== undefined &&
    (typeof e.followUpAt !== "number" || !Number.isFinite(e.followUpAt) || e.followUpAt < 0)
  ) {
    return false;
  }
  return true;
}

function sanitizeSocialEntry(e: SocialEntry): SocialEntry {
  return {
    id: sanitizeId(e.id),
    channel: sanitizeText(e.channel),
    peerId: sanitizeText(e.peerId),
    lastInteraction: safeTimestamp(e.lastInteraction),
    context: sanitizeText(e.context),
    followUpAt: e.followUpAt !== undefined ? safeTimestamp(e.followUpAt) : undefined,
  };
}

export async function loadAutonomousState(storePath?: string): Promise<AutonomousState> {
  const resolved = storePath ?? DEFAULT_AUTONOMOUS_STORE_PATH;
  try {
    const raw = await fs.promises.readFile(resolved, "utf-8");

    let parsed: unknown;
    try {
      parsed = JSON.parse(raw);
    } catch {
      // JSON is corrupted: keep a .corrupt backup and return empty state.
      console.warn(`[autonomous] Corrupted state file at ${resolved}, backing up and resetting.`);
      const backupPath = `${resolved}.corrupt`;
      await fs.promises.copyFile(resolved, backupPath).catch(() => undefined);
      return createEmptyState();
    }

    if (!parsed || typeof parsed !== "object" || Array.isArray(parsed)) {
      return createEmptyState();
    }
    const record = parsed as Record<string, unknown>;
    const state = record.state as Record<string, unknown> | undefined;
    if (!state || typeof state !== "object") {
      return createEmptyState();
    }

    // Validate and filter array contents, sanitize all entries.
    const rawGoals = Array.isArray(state.goals) ? state.goals : [];
    const rawObservations = Array.isArray(state.observations) ? state.observations : [];
    const rawPlans = Array.isArray(state.plans) ? state.plans : [];
    const rawSocial = Array.isArray(state.socialContext) ? state.socialContext : [];

    return {
      version: 1,
      goals: rawGoals.filter(isValidGoal).map(sanitizeGoal),
      observations: rawObservations.filter(isValidObservation).map(sanitizeObservation),
      plans: rawPlans.filter(isValidPlan).map(sanitizePlan),
      socialContext: rawSocial.filter(isValidSocialEntry).map(sanitizeSocialEntry),
      lastCycleAt: safeTimestamp(state.lastCycleAt),
      nextCycleAt: safeTimestamp(state.nextCycleAt),
      cycleCount: Math.max(0, Math.floor(Number(state.cycleCount) || 0)),
    };
  } catch (err) {
    if ((err as { code?: unknown })?.code === "ENOENT") {
      return createEmptyState();
    }
    // For any other I/O error, log and return empty state instead of crashing.
    console.warn(`[autonomous] Failed to load state from ${resolved}:`, err);
    return createEmptyState();
  }
}

export async function saveAutonomousState(
  state: AutonomousState,
  storePath?: string,
): Promise<void> {
  // Mutex-like guard: wait for any in-flight save to complete.
  if (saveInFlight) {
    // Spin-wait with yielding (prevents corruption from concurrent saves).
    let waited = 0;
    while (saveInFlight && waited < 5000) {
      await new Promise<void>((resolve) => setTimeout(resolve, 50));
      waited += 50;
    }
    if (saveInFlight) {
      console.warn("[autonomous] Save still in-flight after 5s, proceeding anyway.");
    }
  }

  saveInFlight = true;
  const resolved = storePath ?? DEFAULT_AUTONOMOUS_STORE_PATH;
  const tmp = `${resolved}.${process.pid}.${crypto.randomBytes(8).toString("hex")}.tmp`;

  try {
    const storeDir = path.dirname(resolved);
    await fs.promises.mkdir(storeDir, { recursive: true, mode: 0o700 });

    const pruned = pruneState(state);
    const store: AutonomousStoreFile = { version: 1, state: pruned };
    const json = JSON.stringify(store, null, 2);

    await fs.promises.writeFile(tmp, json, { encoding: "utf-8", mode: 0o600 });

    try {
      await fs.promises.rename(tmp, resolved);
    } catch {
      try {
        await fs.promises.copyFile(tmp, resolved);
      } finally {
        // Always clean up temp file even if copy fails.
        await fs.promises.unlink(tmp).catch(() => undefined);
      }
    }
  } finally {
    saveInFlight = false;
  }
}

/** Safe numeric comparator that treats NaN/non-finite as 0. */
function safeNumericCompare(a: number, b: number): number {
  const sa = Number.isFinite(a) ? a : 0;
  const sb = Number.isFinite(b) ? b : 0;
  return sb - sa;
}

function pruneState(state: AutonomousState): AutonomousState {
  const observations =
    state.observations.length === 0
      ? []
      : state.observations
          .toSorted((a, b) => safeNumericCompare(a.observedAt, b.observedAt))
          .slice(0, MAX_OBSERVATIONS);

  const activeGoals = state.goals.filter((g) => g.status !== "completed");
  const completedGoals =
    state.goals.length === 0
      ? []
      : state.goals
          .filter((g) => g.status === "completed")
          .toSorted((a, b) => safeNumericCompare(a.createdAt, b.createdAt))
          .slice(0, MAX_COMPLETED_GOALS);

  const socialContext =
    state.socialContext.length === 0
      ? []
      : state.socialContext
          .toSorted((a, b) => safeNumericCompare(a.lastInteraction, b.lastInteraction))
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
  const sanitizedDesc = sanitizeText(description);
  if (sanitizedDesc.length === 0) {
    throw new Error("Goal description must be a non-empty string.");
  }
  if (!VALID_GOAL_PRIORITIES.has(priority)) {
    throw new Error(`Invalid goal priority: ${String(priority)}`);
  }

  const goal: Goal = {
    id: crypto.randomUUID(),
    description: sanitizedDesc,
    priority,
    status: "active",
    createdAt: Date.now(),
    dueAt: dueAt !== undefined ? safeTimestamp(dueAt) : undefined,
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
  const sanitizedSource = sanitizeText(source);
  const sanitizedContent = sanitizeText(content);
  if (sanitizedSource.length === 0) {
    throw new Error("Observation source must be a non-empty string.");
  }
  if (sanitizedContent.length === 0) {
    throw new Error("Observation content must be a non-empty string.");
  }
  if (relevance !== undefined && !VALID_OBSERVATION_RELEVANCES.has(relevance)) {
    throw new Error(`Invalid observation relevance: ${String(relevance)}`);
  }

  const obs: Observation = {
    id: crypto.randomUUID(),
    source: sanitizedSource,
    content: sanitizedContent,
    observedAt: Date.now(),
    processed: false,
    relevance,
  };
  state.observations.push(obs);
  return obs;
}

export function addPlan(state: AutonomousState, steps: string[], goalId?: string): Plan {
  const sanitizedSteps = validateStringArray(steps)
    .map((s) => sanitizeText(s))
    .filter((s) => s.length > 0);
  if (sanitizedSteps.length === 0) {
    throw new Error("Plan must have at least one non-empty step.");
  }
  const sanitizedGoalId = goalId !== undefined ? sanitizeId(goalId) : undefined;

  const plan: Plan = {
    id: crypto.randomUUID(),
    goalId: sanitizedGoalId,
    steps: sanitizedSteps,
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
  const sanitizedChannel = sanitizeText(channel);
  const sanitizedPeerId = sanitizeText(peerId);
  const sanitizedContext = sanitizeText(context);
  if (sanitizedChannel.length === 0) {
    throw new Error("Social entry channel must be a non-empty string.");
  }
  if (sanitizedPeerId.length === 0) {
    throw new Error("Social entry peerId must be a non-empty string.");
  }

  const entry: SocialEntry = {
    id: crypto.randomUUID(),
    channel: sanitizedChannel,
    peerId: sanitizedPeerId,
    lastInteraction: Date.now(),
    context: sanitizedContext,
    followUpAt: followUpAt !== undefined ? safeTimestamp(followUpAt) : undefined,
  };
  state.socialContext.push(entry);
  return entry;
}

export function updateGoal(
  state: AutonomousState,
  goalId: string,
  patch: Partial<Pick<Goal, "status" | "progress" | "priority" | "description" | "dueAt">>,
): Goal | null {
  const id = sanitizeId(goalId);
  const goal = state.goals.find((g) => g.id === id);
  if (!goal) {
    return null;
  }
  const safePatch: Record<string, unknown> = {};
  if (patch.status !== undefined) {
    if (VALID_GOAL_STATUSES.has(patch.status)) {
      safePatch.status = patch.status;
    }
  }
  if (patch.priority !== undefined) {
    if (VALID_GOAL_PRIORITIES.has(patch.priority)) {
      safePatch.priority = patch.priority;
    }
  }
  if (patch.description !== undefined) {
    const desc = sanitizeText(patch.description);
    if (desc.length > 0) {
      safePatch.description = desc;
    }
  }
  if (patch.progress !== undefined) {
    safePatch.progress = sanitizeText(patch.progress);
  }
  if (patch.dueAt !== undefined) {
    safePatch.dueAt = safeTimestamp(patch.dueAt);
  }
  Object.assign(goal, safePatch);
  return goal;
}

export function updatePlan(
  state: AutonomousState,
  planId: string,
  patch: Partial<Pick<Plan, "currentStep" | "status" | "steps">>,
): Plan | null {
  const id = sanitizeId(planId);
  const plan = state.plans.find((p) => p.id === id);
  if (!plan) {
    return null;
  }
  const safePatch: Record<string, unknown> = {};
  if (patch.status !== undefined) {
    if (VALID_PLAN_STATUSES.has(patch.status)) {
      safePatch.status = patch.status;
    }
  }
  if (patch.currentStep !== undefined) {
    if (
      typeof patch.currentStep === "number" &&
      Number.isFinite(patch.currentStep) &&
      patch.currentStep >= 0
    ) {
      safePatch.currentStep = Math.floor(patch.currentStep);
    }
  }
  if (patch.steps !== undefined) {
    const steps = validateStringArray(patch.steps)
      .map((s) => sanitizeText(s))
      .filter((s) => s.length > 0);
    if (steps.length > 0) {
      safePatch.steps = steps;
    }
  }
  Object.assign(plan, safePatch);
  // Clamp currentStep to valid range after both steps and currentStep may have changed.
  const maxStep = Math.max(0, plan.steps.length - 1);
  if (plan.currentStep > maxStep) {
    plan.currentStep = maxStep;
  }
  return plan;
}

export function updateSocialEntry(
  state: AutonomousState,
  entryId: string,
  patch: Partial<Pick<SocialEntry, "context" | "followUpAt" | "lastInteraction">>,
): SocialEntry | null {
  const id = sanitizeId(entryId);
  const entry = state.socialContext.find((e) => e.id === id);
  if (!entry) {
    return null;
  }
  const safePatch: Record<string, unknown> = {};
  if (patch.context !== undefined) {
    safePatch.context = sanitizeText(patch.context);
  }
  if (patch.followUpAt !== undefined) {
    safePatch.followUpAt = safeTimestamp(patch.followUpAt);
  }
  if (patch.lastInteraction !== undefined) {
    safePatch.lastInteraction = safeTimestamp(patch.lastInteraction);
  }
  Object.assign(entry, safePatch);
  return entry;
}
