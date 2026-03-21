import { Type } from "@sinclair/typebox";
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
} from "../../autonomous/state-store.js";
import type { GoalPriority, ObservationRelevance } from "../../autonomous/types.js";
import {
  clampDelay,
  MAX_TIMEOUT_MS,
  safeDateParse,
  safeIsoString,
  sanitizeText,
  validateStringArray,
} from "../../autonomous/validation.js";
import { optionalStringEnum, stringEnum } from "../schema/typebox.js";
import { type AnyAgentTool, jsonResult, readStringParam } from "./common.js";

const ACTIONS = [
  "get-state",
  "add-goal",
  "update-goal",
  "add-observation",
  "add-plan",
  "update-plan",
  "add-social",
  "update-social",
  "set-next-cycle",
  "mark-observations-processed",
] as const;

const PRIORITIES = ["high", "medium", "low"] as const;
const GOAL_STATUSES = ["active", "paused", "completed"] as const;
const PLAN_STATUSES = ["active", "blocked", "completed"] as const;
const RELEVANCES = ["high", "medium", "low"] as const;

const VALID_PRIORITIES = new Set<string>(PRIORITIES);
const VALID_GOAL_STATUSES = new Set<string>(GOAL_STATUSES);
const VALID_PLAN_STATUSES = new Set<string>(PLAN_STATUSES);
const VALID_RELEVANCES = new Set<string>(RELEVANCES);

const AutonomousToolSchema = Type.Object(
  {
    action: stringEnum(ACTIONS),
    description: Type.Optional(Type.String()),
    priority: optionalStringEnum(PRIORITIES),
    status: optionalStringEnum([...GOAL_STATUSES, ...PLAN_STATUSES]),
    progress: Type.Optional(Type.String()),
    dueAt: Type.Optional(Type.String()),
    goalId: Type.Optional(Type.String()),
    source: Type.Optional(Type.String()),
    content: Type.Optional(Type.String()),
    relevance: optionalStringEnum(RELEVANCES),
    steps: Type.Optional(Type.Array(Type.String())),
    currentStep: Type.Optional(Type.Number()),
    planId: Type.Optional(Type.String()),
    channel: Type.Optional(Type.String()),
    peerId: Type.Optional(Type.String()),
    context: Type.Optional(Type.String()),
    followUpAt: Type.Optional(Type.String()),
    entryId: Type.Optional(Type.String()),
    delayMs: Type.Optional(Type.Number()),
    observationIds: Type.Optional(Type.Array(Type.String())),
  },
  { additionalProperties: true },
);

type AutonomousToolOptions = {
  storePath?: string;
};

export function createAutonomousTool(opts?: AutonomousToolOptions): AnyAgentTool {
  return {
    label: "Autonomous",
    name: "autonomous",
    ownerOnly: true,
    description: `Manage the autonomous agent's internal state: goals, plans, observations, social context, and cycle timing.

ACTIONS:
- get-state: View current autonomous state (goals, plans, observations, social context)
- add-goal: Add a new goal (requires description, optional priority/dueAt)
- update-goal: Update a goal (requires goalId, optional status/progress/priority/description)
- add-observation: Record an observation (requires source + content, optional relevance)
- add-plan: Create a plan (requires steps array, optional goalId)
- update-plan: Update a plan (requires planId, optional currentStep/status/steps)
- add-social: Track a social interaction (requires channel + peerId + context, optional followUpAt)
- update-social: Update social entry (requires entryId, optional context/followUpAt)
- set-next-cycle: Set when next autonomous cycle should run (requires delayMs in milliseconds)
- mark-observations-processed: Mark observations as processed (requires observationIds array)`,
    parameters: AutonomousToolSchema,
    async execute(_toolCallId: string, input: Record<string, unknown>) {
      const action = readStringParam(input, "action", { required: true });
      const storePath = opts?.storePath;

      // Wrap the entire execution in try/catch to never crash the gateway.
      try {
        return await executeAction(action, input, storePath);
      } catch (err) {
        const msg = err instanceof Error ? err.message : String(err);
        return jsonResult({ ok: false, error: `Internal error: ${msg}` });
      }
    },
  } as AnyAgentTool;
}

async function executeAction(
  action: string,
  input: Record<string, unknown>,
  storePath: string | undefined,
) {
  const state = await loadAutonomousState(storePath);

  switch (action) {
    case "get-state": {
      const activeGoals = state.goals.filter((g) => g.status === "active");
      const activePlans = state.plans.filter((p) => p.status === "active");
      const unprocessedObs = state.observations.filter((o) => !o.processed);
      return jsonResult({
        cycleCount: state.cycleCount,
        lastCycleAt: safeIsoString(state.lastCycleAt),
        nextCycleAt: safeIsoString(state.nextCycleAt),
        goals: { active: activeGoals.length, total: state.goals.length, items: activeGoals },
        plans: { active: activePlans.length, total: state.plans.length, items: activePlans },
        observations: { unprocessed: unprocessedObs.length, total: state.observations.length },
        socialContext: { count: state.socialContext.length },
      });
    }

    case "add-goal": {
      const description = sanitizeText(readStringParam(input, "description", { required: true }));
      if (description.length === 0) {
        return jsonResult({ ok: false, error: "description is required and must be non-empty" });
      }
      const rawPriority = readStringParam(input, "priority") ?? "medium";
      const priority: GoalPriority = VALID_PRIORITIES.has(rawPriority)
        ? (rawPriority as GoalPriority)
        : "medium";
      const dueAt = safeDateParse(readStringParam(input, "dueAt"));
      const goal = addGoal(state, description, priority, dueAt);
      await saveAutonomousState(state, storePath);
      return jsonResult({ ok: true, goal });
    }

    case "update-goal": {
      const goalId = readStringParam(input, "goalId", { required: true });
      if (!goalId) {
        return jsonResult({ ok: false, error: "goalId is required" });
      }
      const patch: Record<string, unknown> = {};
      const status = readStringParam(input, "status");
      if (status && VALID_GOAL_STATUSES.has(status)) {
        patch.status = status;
      }
      const progress = readStringParam(input, "progress");
      if (progress) {
        patch.progress = sanitizeText(progress);
      }
      const priority = readStringParam(input, "priority");
      if (priority && VALID_PRIORITIES.has(priority)) {
        patch.priority = priority;
      }
      const description = readStringParam(input, "description");
      if (description) {
        patch.description = sanitizeText(description);
      }
      const dueAt = safeDateParse(readStringParam(input, "dueAt"));
      if (dueAt !== undefined) {
        patch.dueAt = dueAt;
      }
      const goal = updateGoal(state, goalId, patch);
      if (!goal) {
        return jsonResult({ ok: false, error: `Goal ${goalId} not found` });
      }
      await saveAutonomousState(state, storePath);
      return jsonResult({ ok: true, goal });
    }

    case "add-observation": {
      const source = sanitizeText(readStringParam(input, "source", { required: true }), 500);
      const content = sanitizeText(readStringParam(input, "content", { required: true }));
      if (source.length === 0 || content.length === 0) {
        return jsonResult({ ok: false, error: "source and content are required" });
      }
      const rawRelevance = readStringParam(input, "relevance");
      const relevance: ObservationRelevance | undefined =
        rawRelevance && VALID_RELEVANCES.has(rawRelevance)
          ? (rawRelevance as ObservationRelevance)
          : undefined;
      const obs = addObservation(state, source, content, relevance);
      await saveAutonomousState(state, storePath);
      return jsonResult({ ok: true, observation: obs });
    }

    case "add-plan": {
      const rawSteps = input.steps;
      const steps = validateStringArray(rawSteps).filter((s) => s.trim().length > 0);
      if (steps.length === 0) {
        return jsonResult({
          ok: false,
          error: "steps array with at least one non-empty string is required",
        });
      }
      const goalId = readStringParam(input, "goalId") || undefined;
      const plan = addPlan(state, steps, goalId);
      await saveAutonomousState(state, storePath);
      return jsonResult({ ok: true, plan });
    }

    case "update-plan": {
      const planId = readStringParam(input, "planId", { required: true });
      if (!planId) {
        return jsonResult({ ok: false, error: "planId is required" });
      }
      const patch: Record<string, unknown> = {};
      const currentStep = input.currentStep;
      if (typeof currentStep === "number" && Number.isFinite(currentStep) && currentStep >= 0) {
        patch.currentStep = Math.floor(currentStep);
      }
      const planStatus = readStringParam(input, "status");
      if (planStatus && VALID_PLAN_STATUSES.has(planStatus)) {
        patch.status = planStatus;
      }
      const rawSteps = input.steps;
      if (Array.isArray(rawSteps)) {
        const steps = validateStringArray(rawSteps);
        if (steps.length > 0) {
          patch.steps = steps;
        }
      }
      const plan = updatePlan(state, planId, patch);
      if (!plan) {
        return jsonResult({ ok: false, error: `Plan ${planId} not found` });
      }
      await saveAutonomousState(state, storePath);
      return jsonResult({ ok: true, plan });
    }

    case "add-social": {
      const channel = sanitizeText(readStringParam(input, "channel", { required: true }), 200);
      const peerId = sanitizeText(readStringParam(input, "peerId", { required: true }), 200);
      const context = sanitizeText(readStringParam(input, "context", { required: true }));
      if (channel.length === 0 || peerId.length === 0 || context.length === 0) {
        return jsonResult({ ok: false, error: "channel, peerId, and context are required" });
      }
      const followUpAt = safeDateParse(readStringParam(input, "followUpAt"));
      const entry = addSocialEntry(state, channel, peerId, context, followUpAt);
      await saveAutonomousState(state, storePath);
      return jsonResult({ ok: true, entry });
    }

    case "update-social": {
      const entryId = readStringParam(input, "entryId", { required: true });
      if (!entryId) {
        return jsonResult({ ok: false, error: "entryId is required" });
      }
      const patch: Record<string, unknown> = {};
      const context = readStringParam(input, "context");
      if (context) {
        patch.context = sanitizeText(context);
      }
      const followUpAt = safeDateParse(readStringParam(input, "followUpAt"));
      if (followUpAt !== undefined) {
        patch.followUpAt = followUpAt;
      }
      patch.lastInteraction = Date.now();
      const entry = updateSocialEntry(state, entryId, patch);
      if (!entry) {
        return jsonResult({ ok: false, error: `Social entry ${entryId} not found` });
      }
      await saveAutonomousState(state, storePath);
      return jsonResult({ ok: true, entry });
    }

    case "set-next-cycle": {
      const rawDelay = input.delayMs;
      if (typeof rawDelay !== "number" || !Number.isFinite(rawDelay) || rawDelay < 0) {
        return jsonResult({ ok: false, error: "delayMs must be a finite non-negative number" });
      }
      const delayMs = clampDelay(rawDelay, 0, MAX_TIMEOUT_MS);
      state.nextCycleAt = Date.now() + delayMs;
      await saveAutonomousState(state, storePath);
      return jsonResult({
        ok: true,
        nextCycleAt: safeIsoString(state.nextCycleAt),
        delayMs,
      });
    }

    case "mark-observations-processed": {
      const rawIds = input.observationIds;
      const ids = validateStringArray(rawIds);
      if (ids.length === 0) {
        return jsonResult({ ok: false, error: "observationIds array with string IDs is required" });
      }
      const idSet = new Set(ids);
      let count = 0;
      for (const obs of state.observations) {
        if (idSet.has(obs.id) && !obs.processed) {
          obs.processed = true;
          count++;
        }
      }
      await saveAutonomousState(state, storePath);
      return jsonResult({ ok: true, processedCount: count });
    }

    default:
      return jsonResult({ ok: false, error: `Unknown action: ${action}` });
  }
}
