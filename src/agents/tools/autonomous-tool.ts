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

const AutonomousToolSchema = Type.Object(
  {
    action: stringEnum(ACTIONS),
    // For add-goal / update-goal.
    description: Type.Optional(Type.String()),
    priority: optionalStringEnum(PRIORITIES),
    status: optionalStringEnum([...GOAL_STATUSES, ...PLAN_STATUSES]),
    progress: Type.Optional(Type.String()),
    dueAt: Type.Optional(Type.String()),
    goalId: Type.Optional(Type.String()),
    // For add-observation.
    source: Type.Optional(Type.String()),
    content: Type.Optional(Type.String()),
    relevance: optionalStringEnum(RELEVANCES),
    // For add-plan / update-plan.
    steps: Type.Optional(Type.Array(Type.String())),
    currentStep: Type.Optional(Type.Number()),
    planId: Type.Optional(Type.String()),
    // For add-social / update-social.
    channel: Type.Optional(Type.String()),
    peerId: Type.Optional(Type.String()),
    context: Type.Optional(Type.String()),
    followUpAt: Type.Optional(Type.String()),
    entryId: Type.Optional(Type.String()),
    // For set-next-cycle.
    delayMs: Type.Optional(Type.Number()),
    // For mark-observations-processed.
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

      const state = await loadAutonomousState(storePath);

      switch (action) {
        case "get-state": {
          const activeGoals = state.goals.filter((g) => g.status === "active");
          const activePlans = state.plans.filter((p) => p.status === "active");
          const unprocessedObs = state.observations.filter((o) => !o.processed);
          return jsonResult({
            cycleCount: state.cycleCount,
            lastCycleAt: state.lastCycleAt ? new Date(state.lastCycleAt).toISOString() : null,
            nextCycleAt: state.nextCycleAt ? new Date(state.nextCycleAt).toISOString() : null,
            goals: { active: activeGoals.length, total: state.goals.length, items: activeGoals },
            plans: { active: activePlans.length, total: state.plans.length, items: activePlans },
            observations: { unprocessed: unprocessedObs.length, total: state.observations.length },
            socialContext: { count: state.socialContext.length },
          });
        }

        case "add-goal": {
          const description = readStringParam(input, "description", { required: true });
          const priority = readStringParam(input, "priority") ?? "medium";
          const dueAtStr = readStringParam(input, "dueAt");
          const dueAt = dueAtStr ? new Date(dueAtStr).getTime() : undefined;
          const goal = addGoal(state, description, priority as GoalPriority, dueAt);
          await saveAutonomousState(state, storePath);
          return jsonResult({ ok: true, goal });
        }

        case "update-goal": {
          const goalId = readStringParam(input, "goalId", { required: true });
          const patch: Record<string, unknown> = {};
          const status = readStringParam(input, "status");
          if (status) {
            patch.status = status;
          }
          const progress = readStringParam(input, "progress");
          if (progress) {
            patch.progress = progress;
          }
          const priority = readStringParam(input, "priority");
          if (priority) {
            patch.priority = priority;
          }
          const description = readStringParam(input, "description");
          if (description) {
            patch.description = description;
          }
          const dueAtStr = readStringParam(input, "dueAt");
          if (dueAtStr) {
            patch.dueAt = new Date(dueAtStr).getTime();
          }
          const goal = updateGoal(state, goalId, patch);
          if (!goal) {
            return jsonResult({ ok: false, error: `Goal ${goalId} not found` });
          }
          await saveAutonomousState(state, storePath);
          return jsonResult({ ok: true, goal });
        }

        case "add-observation": {
          const source = readStringParam(input, "source", { required: true });
          const content = readStringParam(input, "content", { required: true });
          const relevance = readStringParam(input, "relevance") as ObservationRelevance | undefined;
          const obs = addObservation(state, source, content, relevance);
          await saveAutonomousState(state, storePath);
          return jsonResult({ ok: true, observation: obs });
        }

        case "add-plan": {
          const steps = input.steps;
          if (!Array.isArray(steps) || steps.length === 0) {
            return jsonResult({ ok: false, error: "steps array is required" });
          }
          const goalId = readStringParam(input, "goalId");
          const plan = addPlan(state, steps as string[], goalId ?? undefined);
          await saveAutonomousState(state, storePath);
          return jsonResult({ ok: true, plan });
        }

        case "update-plan": {
          const planId = readStringParam(input, "planId", { required: true });
          const patch: Record<string, unknown> = {};
          const currentStep = input.currentStep;
          if (typeof currentStep === "number") {
            patch.currentStep = currentStep;
          }
          const planStatus = readStringParam(input, "status");
          if (planStatus) {
            patch.status = planStatus;
          }
          const steps = input.steps;
          if (Array.isArray(steps)) {
            patch.steps = steps;
          }
          const plan = updatePlan(state, planId, patch);
          if (!plan) {
            return jsonResult({ ok: false, error: `Plan ${planId} not found` });
          }
          await saveAutonomousState(state, storePath);
          return jsonResult({ ok: true, plan });
        }

        case "add-social": {
          const channel = readStringParam(input, "channel", { required: true });
          const peerId = readStringParam(input, "peerId", { required: true });
          const context = readStringParam(input, "context", { required: true });
          const followUpStr = readStringParam(input, "followUpAt");
          const followUpAt = followUpStr ? new Date(followUpStr).getTime() : undefined;
          const entry = addSocialEntry(state, channel, peerId, context, followUpAt);
          await saveAutonomousState(state, storePath);
          return jsonResult({ ok: true, entry });
        }

        case "update-social": {
          const entryId = readStringParam(input, "entryId", { required: true });
          const patch: Record<string, unknown> = {};
          const context = readStringParam(input, "context");
          if (context) {
            patch.context = context;
          }
          const followUpStr = readStringParam(input, "followUpAt");
          if (followUpStr) {
            patch.followUpAt = new Date(followUpStr).getTime();
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
          const delayMs = input.delayMs;
          if (typeof delayMs !== "number" || delayMs < 0) {
            return jsonResult({ ok: false, error: "delayMs must be a non-negative number" });
          }
          state.nextCycleAt = Date.now() + delayMs;
          await saveAutonomousState(state, storePath);
          return jsonResult({
            ok: true,
            nextCycleAt: new Date(state.nextCycleAt).toISOString(),
            delayMs,
          });
        }

        case "mark-observations-processed": {
          const ids = input.observationIds;
          if (!Array.isArray(ids)) {
            return jsonResult({ ok: false, error: "observationIds array is required" });
          }
          const idSet = new Set(ids as string[]);
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
    },
  } as AnyAgentTool;
}
