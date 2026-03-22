import crypto from "node:crypto";
import fs from "node:fs";
import path from "node:path";
import { CONFIG_DIR } from "../utils.js";
import type {
  AttentionSignal,
  AutonomousState,
  AutonomousStoreFile,
  CycleOutcome,
  Goal,
  GoalPriority,
  Observation,
  ObservationRelevance,
  Plan,
  SocialEntry,
} from "./types.js";
import {
  isFiniteNonNegative,
  safeTimestamp,
  sanitizeText,
  sanitizeId,
  validateStringArray,
} from "./validation.js";

/** Return a valid timestamp, or undefined if the value is invalid (instead of falling back to 0). */
function validTimestampOrUndefined(value: unknown): number | undefined {
  const ts = safeTimestamp(value, -1);
  return ts >= 0 ? ts : undefined;
}

export const DEFAULT_AUTONOMOUS_DIR = path.join(CONFIG_DIR, "autonomous");
export const DEFAULT_AUTONOMOUS_STORE_PATH = path.join(DEFAULT_AUTONOMOUS_DIR, "state.json");

const MAX_OBSERVATIONS = 100;
const MAX_COMPLETED_GOALS = 50;
const MAX_SOCIAL_ENTRIES = 200;
const MAX_COMPLETED_PLANS = 50;
const MAX_PENDING_SIGNALS = 50;
/** Auto-expire unprocessed observations older than 7 days. */
const OBSERVATION_TTL_MS = 7 * 24 * 3_600_000;

const VALID_GOAL_PRIORITIES = new Set<GoalPriority>(["high", "medium", "low"]);
const VALID_GOAL_STATUSES = new Set(["active", "paused", "completed"]);
const VALID_OBSERVATION_RELEVANCES = new Set(["high", "medium", "low"]);
const VALID_PLAN_STATUSES = new Set(["active", "blocked", "completed"]);

/** Promise-based mutex to prevent concurrent saves from corrupting state. */
let saveMutex: Promise<void> = Promise.resolve();

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
  if (
    g.lastProgressAt !== undefined &&
    (typeof g.lastProgressAt !== "number" ||
      !Number.isFinite(g.lastProgressAt) ||
      g.lastProgressAt < 0)
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
    lastProgressAt: g.lastProgressAt !== undefined ? safeTimestamp(g.lastProgressAt) : undefined,
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
  if (!Array.isArray(p.steps) || p.steps.length === 0) {
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

// -- Validation for AttentionSignal --

const VALID_SIGNAL_TYPES = new Set(["message", "event", "schedule", "followup", "goal-deadline"]);

function isValidSignal(entry: unknown): entry is AttentionSignal {
  if (!entry || typeof entry !== "object") {
    return false;
  }
  const s = entry as Record<string, unknown>;
  if (typeof s.source !== "string" || typeof s.content !== "string") {
    return false;
  }
  if (typeof s.type !== "string" || !VALID_SIGNAL_TYPES.has(s.type)) {
    return false;
  }
  if (!isFiniteNonNegative(s.timestamp)) {
    return false;
  }
  if (typeof s.urgency !== "number" || !Number.isFinite(s.urgency)) {
    return false;
  }
  return true;
}

function sanitizeSignal(s: AttentionSignal): AttentionSignal {
  return {
    source: sanitizeText(s.source, 500),
    type: s.type,
    content: sanitizeText(s.content),
    urgency: Math.max(0, Math.min(1, s.urgency)),
    timestamp: safeTimestamp(s.timestamp),
  };
}

// -- Validation for CycleOutcome --

function loadCycleOutcome(raw: unknown): CycleOutcome | undefined {
  if (!raw || typeof raw !== "object") {
    return undefined;
  }
  const o = raw as Record<string, unknown>;
  if (!isFiniteNonNegative(o.cycleNumber)) {
    return undefined;
  }
  if (!isFiniteNonNegative(o.startedAt)) {
    return undefined;
  }
  if (!isFiniteNonNegative(o.finishedAt)) {
    return undefined;
  }
  return {
    cycleNumber: o.cycleNumber,
    startedAt: safeTimestamp(o.startedAt),
    finishedAt: safeTimestamp(o.finishedAt),
    actionsTaken: Array.isArray(o.actionsTaken) ? validateStringArray(o.actionsTaken) : [],
    error: typeof o.error === "string" ? sanitizeText(o.error, 1000) : undefined,
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
    const rawSignals = Array.isArray(state.pendingSignals) ? state.pendingSignals : [];

    return {
      version: 1,
      goals: rawGoals.filter(isValidGoal).map(sanitizeGoal),
      observations: rawObservations.filter(isValidObservation).map(sanitizeObservation),
      plans: rawPlans.filter(isValidPlan).map(sanitizePlan),
      socialContext: rawSocial.filter(isValidSocialEntry).map(sanitizeSocialEntry),
      pendingSignals: rawSignals.filter(isValidSignal).map(sanitizeSignal),
      lastCycleOutcome: loadCycleOutcome(state.lastCycleOutcome),
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
  // Chain on the mutex so concurrent saves serialize properly without spin-waiting.
  const previous = saveMutex;
  let releaseMutex: () => void;
  saveMutex = new Promise<void>((resolve) => {
    releaseMutex = resolve;
  });

  // Wait for any in-flight save to finish.
  await previous;

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
        await fs.promises.unlink(tmp).catch(() => undefined);
      }
    }
  } finally {
    releaseMutex!();
  }
}

/** Safe numeric comparator that treats NaN/non-finite as 0. */
function safeNumericCompare(a: number, b: number): number {
  const sa = Number.isFinite(a) ? a : 0;
  const sb = Number.isFinite(b) ? b : 0;
  return sb - sa;
}

function pruneState(state: AutonomousState): AutonomousState {
  const now = Date.now();

  // Auto-expire old unprocessed observations (TTL-based cleanup).
  const nonExpiredObservations = state.observations.filter((o) => {
    if (o.processed) {
      return true;
    }
    const age = now - (isFiniteNonNegative(o.observedAt) ? o.observedAt : 0);
    return age < OBSERVATION_TTL_MS;
  });

  const observations =
    nonExpiredObservations.length === 0
      ? []
      : nonExpiredObservations
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

  const activePlans = state.plans.filter((p) => p.status === "active");
  const completedPlans = state.plans
    .filter((p) => p.status !== "active")
    .slice(0, MAX_COMPLETED_PLANS);

  // Keep only the most urgent pending signals, capped at MAX_PENDING_SIGNALS.
  const pendingSignals =
    state.pendingSignals.length === 0
      ? []
      : state.pendingSignals
          .toSorted((a, b) => safeNumericCompare(a.urgency, b.urgency))
          .slice(0, MAX_PENDING_SIGNALS);

  return {
    ...state,
    goals: [...activeGoals, ...completedGoals],
    observations,
    plans: [...activePlans, ...completedPlans],
    socialContext,
    pendingSignals,
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
    dueAt: dueAt !== undefined ? validTimestampOrUndefined(dueAt) : undefined,
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
    followUpAt: followUpAt !== undefined ? validTimestampOrUndefined(followUpAt) : undefined,
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
    safePatch.lastProgressAt = Date.now();
  }
  if (patch.dueAt !== undefined) {
    if (patch.dueAt === null) {
      safePatch.dueAt = undefined;
    } else {
      const ts = validTimestampOrUndefined(patch.dueAt);
      if (ts !== undefined) {
        safePatch.dueAt = ts;
      }
    }
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
    if (patch.followUpAt === null) {
      safePatch.followUpAt = undefined;
    } else {
      const ts = validTimestampOrUndefined(patch.followUpAt);
      if (ts !== undefined) {
        safePatch.followUpAt = ts;
      }
    }
  }
  if (patch.lastInteraction !== undefined) {
    const ts = validTimestampOrUndefined(patch.lastInteraction);
    if (ts !== undefined) {
      safePatch.lastInteraction = ts;
    }
  }
  Object.assign(entry, safePatch);
  return entry;
}
