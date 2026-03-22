import type { AttentionSignal, AutonomousState } from "./types.js";
import { isFinitePositive, safeIsoString } from "./validation.js";

const MAX_GOALS_IN_PROMPT = 20;
const MAX_OBSERVATIONS_IN_PROMPT = 15;
const MAX_SIGNALS_IN_PROMPT = 10;
const MAX_SOCIAL_IN_PROMPT = 15;
const MAX_PLANS_IN_PROMPT = 10;

const FALLBACK_PROMPT = `You are an autonomous agent. Your state could not be fully loaded.
Please use the autonomous tool to check your current state and take appropriate action.`;

function formatLastCycleOutcome(state: AutonomousState): string {
  const outcome = state.lastCycleOutcome;
  if (!outcome) {
    return "No previous cycle data available.";
  }
  const duration =
    isFiniteNonNeg(outcome.finishedAt) && isFiniteNonNeg(outcome.startedAt)
      ? `${Math.round((outcome.finishedAt - outcome.startedAt) / 1000)}s`
      : "unknown";
  const actions = outcome.actionsTaken.length > 0 ? outcome.actionsTaken.join(", ") : "none";
  const status = outcome.error ? `Error: ${outcome.error}` : "Completed successfully";
  const age = isFiniteNonNeg(outcome.finishedAt) ? formatAge(outcome.finishedAt) : "unknown";
  return `Cycle #${outcome.cycleNumber} (${age} ago, duration: ${duration}) — ${status}. Actions: ${actions}.`;
}

function formatGoals(state: AutonomousState): string {
  const goals = state.goals ?? [];
  const active = goals.filter((g) => g.status === "active").slice(0, MAX_GOALS_IN_PROMPT);
  if (active.length === 0) {
    return "No active goals.";
  }
  return active
    .map((g) => {
      const due = g.dueAt ? ` (due: ${safeIsoString(g.dueAt) ?? "unknown"})` : "";
      const progress = g.progress ? ` — Progress: ${g.progress}` : "";
      const desc = g.description ?? "untitled goal";
      const id = g.id ?? "unknown";
      return `- [${g.priority ?? "medium"}] ${desc}${due}${progress} (id: ${id})`;
    })
    .join("\n");
}

function formatObservations(state: AutonomousState): string {
  const observations = state.observations ?? [];
  const unprocessed = observations
    .filter((o) => !o.processed)
    .toSorted((a, b) => (b.observedAt ?? 0) - (a.observedAt ?? 0))
    .slice(0, MAX_OBSERVATIONS_IN_PROMPT);
  if (unprocessed.length === 0) {
    return "No unprocessed observations.";
  }
  return unprocessed
    .map((o) => {
      const age = formatAge(o.observedAt);
      const relevance = o.relevance ?? "unknown";
      const source = o.source ?? "unknown";
      const content = o.content ?? "";
      return `- [${relevance}] (${source}, ${age} ago) ${content}`;
    })
    .join("\n");
}

function formatSignals(signals: AttentionSignal[]): string {
  const top = (signals ?? []).slice(0, MAX_SIGNALS_IN_PROMPT);
  if (top.length === 0) {
    return "No attention signals.";
  }
  return top
    .map((s) => {
      const age = formatAge(s.timestamp);
      const urgency = isFinitePositive(s.urgency) ? s.urgency.toFixed(1) : "0.0";
      const type = s.type ?? "unknown";
      const source = s.source ?? "unknown";
      const content = s.content ?? "";
      return `- [urgency=${urgency}] (${type}, ${source}, ${age} ago) ${content}`;
    })
    .join("\n");
}

function formatSocialContext(state: AutonomousState): string {
  const social = state.socialContext ?? [];
  const entries = social
    .toSorted((a, b) => (b.lastInteraction ?? 0) - (a.lastInteraction ?? 0))
    .slice(0, MAX_SOCIAL_IN_PROMPT);
  if (entries.length === 0) {
    return "No social context tracked.";
  }
  return entries
    .map((e) => {
      const lastAge = formatAge(e.lastInteraction);
      let followUp = "";
      if (e.followUpAt != null) {
        if (e.followUpAt <= Date.now()) {
          followUp = " ⚡ FOLLOW-UP DUE";
        } else {
          followUp = ` (follow-up in ${formatAge(Date.now(), e.followUpAt)})`;
        }
      }
      const peerId = e.peerId ?? "unknown";
      const channel = e.channel ?? "unknown";
      const context = e.context ?? "";
      return `- ${peerId} (${channel}, last: ${lastAge} ago${followUp}): ${context}`;
    })
    .join("\n");
}

function formatPlans(state: AutonomousState): string {
  const plans = state.plans ?? [];
  const active = plans.filter((p) => p.status === "active").slice(0, MAX_PLANS_IN_PROMPT);
  if (active.length === 0) {
    return "No active plans.";
  }
  return active
    .map((p) => {
      const goal = p.goalId ? ` (for goal: ${p.goalId})` : "";
      const steps = p.steps ?? [];
      const currentStep = isFiniteNonNeg(p.currentStep) ? p.currentStep : 0;
      const step = `step ${currentStep + 1}/${steps.length}`;
      const currentStepText =
        currentStep >= 0 && currentStep < steps.length ? (steps[currentStep] ?? "done") : "done";
      const id = p.id ?? "unknown";
      return `- Plan ${id}${goal}: ${step} — current: "${currentStepText}"`;
    })
    .join("\n");
}

function isFiniteNonNeg(value: unknown): value is number {
  return typeof value === "number" && Number.isFinite(value) && value >= 0;
}

function formatAge(from: number, to?: number): string {
  if (!Number.isFinite(from)) {
    return "unknown";
  }
  const now = to ?? Date.now();
  if (!Number.isFinite(now)) {
    return "unknown";
  }
  const diff = now - from;
  // Negative diff means the timestamp is in the future or clock skew.
  if (!Number.isFinite(diff) || diff < 0) {
    return "unknown";
  }
  if (diff < 60_000) {
    return `${Math.round(diff / 1000)}s`;
  }
  if (diff < 3_600_000) {
    return `${Math.round(diff / 60_000)}m`;
  }
  if (diff < 86_400_000) {
    return `${(diff / 3_600_000).toFixed(1)}h`;
  }
  return `${(diff / 86_400_000).toFixed(1)}d`;
}

export function buildCyclePrompt(
  state: AutonomousState,
  signals: AttentionSignal[],
  opts?: {
    defaultChannel?: string;
    defaultTarget?: string;
    dryRun?: boolean;
  },
): string {
  try {
    const now = new Date();
    let isoNow: string;
    try {
      isoNow = now.toISOString();
    } catch {
      isoNow = "unknown";
    }
    let localeNow: string;
    try {
      localeNow = now.toLocaleString();
    } catch {
      localeNow = "unknown";
    }

    const cycleNote =
      state.cycleCount > 0 && isFinitePositive(state.lastCycleAt)
        ? `This is cycle #${state.cycleCount + 1}. Last cycle ran ${formatAge(state.lastCycleAt)} ago.`
        : "This is your first cycle.";

    const dryRunNote = opts?.dryRun
      ? "\n⚠️ DRY RUN MODE: Do not send any actual messages or make external changes. Only update your internal state."
      : "";

    const channelNote = opts?.defaultChannel
      ? `\nDefault channel: ${opts.defaultChannel}${opts.defaultTarget ? ` (target: ${opts.defaultTarget})` : ""}`
      : "";

    const lastOutcome = formatLastCycleOutcome(state);

    return `You are an autonomous agent operating continuously, like a real person who is always present and engaged with the world. You think, plan, observe, and act on your own initiative.

${cycleNote}${dryRunNote}${channelNote}

Current time: ${isoNow} (${localeNow})

## Last Cycle Outcome
${lastOutcome}

## Active Goals
${formatGoals(state)}

## Active Plans
${formatPlans(state)}

## Attention Signals (things demanding your attention right now)
${formatSignals(signals ?? [])}

## Recent Unprocessed Observations
${formatObservations(state)}

## Social Context (people and conversations you're tracking)
${formatSocialContext(state)}

## Instructions

Think about what a thoughtful, proactive person would do right now. Consider:

1. **Urgent items first**: Are there any signals or deadlines that need immediate attention?
2. **Goal progress**: Can you make progress on any active goals? Take concrete steps.
3. **Social engagement**: Should you reach out to someone, follow up on a conversation, or share something interesting?
4. **World monitoring**: Is there anything you should check on (news, web, APIs)?
5. **Planning**: Do you need to create or update plans for your goals?
6. **Waiting**: If there's nothing actionable right now, that's fine — use the autonomous tool to set when you'd like your next cycle.

You have access to tools for:
- **Sending messages** to channels and people
- **Browsing the web** and searching for information
- **Managing your state** (goals, plans, observations, social context) via the \`autonomous\` tool
- **Scheduling** via the cron tool
- **Memory** for long-term knowledge storage

After taking actions, use the \`autonomous\` tool to:
- Update goal progress
- Mark observations as processed
- Record new observations
- Update social context
- Set when your next cycle should run (use \`set-next-cycle\` action)

Be natural, thoughtful, and proactive. Don't just wait — initiate.`;
  } catch (err) {
    // If anything fails during prompt construction, return a minimal fallback
    // so the agent cycle does not crash entirely.
    const msg = err instanceof Error ? err.message : String(err);
    return `${FALLBACK_PROMPT}\n\n(Prompt build error: ${msg})`;
  }
}
