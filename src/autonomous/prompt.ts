import type { AttentionSignal, AutonomousState } from "./types.js";

const MAX_GOALS_IN_PROMPT = 20;
const MAX_OBSERVATIONS_IN_PROMPT = 15;
const MAX_SIGNALS_IN_PROMPT = 10;
const MAX_SOCIAL_IN_PROMPT = 15;
const MAX_PLANS_IN_PROMPT = 10;

function formatGoals(state: AutonomousState): string {
  const active = state.goals.filter((g) => g.status === "active").slice(0, MAX_GOALS_IN_PROMPT);
  if (active.length === 0) {
    return "No active goals.";
  }
  return active
    .map((g) => {
      const due = g.dueAt ? ` (due: ${new Date(g.dueAt).toISOString()})` : "";
      const progress = g.progress ? ` — Progress: ${g.progress}` : "";
      return `- [${g.priority}] ${g.description}${due}${progress} (id: ${g.id})`;
    })
    .join("\n");
}

function formatObservations(state: AutonomousState): string {
  const unprocessed = state.observations
    .filter((o) => !o.processed)
    .toSorted((a, b) => b.observedAt - a.observedAt)
    .slice(0, MAX_OBSERVATIONS_IN_PROMPT);
  if (unprocessed.length === 0) {
    return "No unprocessed observations.";
  }
  return unprocessed
    .map((o) => {
      const age = formatAge(o.observedAt);
      return `- [${o.relevance ?? "unknown"}] (${o.source}, ${age} ago) ${o.content}`;
    })
    .join("\n");
}

function formatSignals(signals: AttentionSignal[]): string {
  const top = signals.slice(0, MAX_SIGNALS_IN_PROMPT);
  if (top.length === 0) {
    return "No attention signals.";
  }
  return top
    .map((s) => {
      const age = formatAge(s.timestamp);
      return `- [urgency=${s.urgency.toFixed(1)}] (${s.type}, ${s.source}, ${age} ago) ${s.content}`;
    })
    .join("\n");
}

function formatSocialContext(state: AutonomousState): string {
  const entries = state.socialContext
    .toSorted((a, b) => b.lastInteraction - a.lastInteraction)
    .slice(0, MAX_SOCIAL_IN_PROMPT);
  if (entries.length === 0) {
    return "No social context tracked.";
  }
  return entries
    .map((e) => {
      const lastAge = formatAge(e.lastInteraction);
      const followUp = e.followUpAt
        ? e.followUpAt <= Date.now()
          ? " ⚡ FOLLOW-UP DUE"
          : ` (follow-up in ${formatAge(Date.now(), e.followUpAt)})`
        : "";
      return `- ${e.peerId} (${e.channel}, last: ${lastAge} ago${followUp}): ${e.context}`;
    })
    .join("\n");
}

function formatPlans(state: AutonomousState): string {
  const active = state.plans.filter((p) => p.status === "active").slice(0, MAX_PLANS_IN_PROMPT);
  if (active.length === 0) {
    return "No active plans.";
  }
  return active
    .map((p) => {
      const goal = p.goalId ? ` (for goal: ${p.goalId})` : "";
      const step = `step ${p.currentStep + 1}/${p.steps.length}`;
      const currentStepText = p.steps[p.currentStep] ?? "done";
      return `- Plan ${p.id}${goal}: ${step} — current: "${currentStepText}"`;
    })
    .join("\n");
}

function formatAge(from: number, to?: number): string {
  const diff = (to ?? Date.now()) - from;
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
  const now = new Date();
  const cycleNote =
    state.cycleCount > 0
      ? `This is cycle #${state.cycleCount + 1}. Last cycle ran ${formatAge(state.lastCycleAt)} ago.`
      : "This is your first cycle.";

  const dryRunNote = opts?.dryRun
    ? "\n⚠️ DRY RUN MODE: Do not send any actual messages or make external changes. Only update your internal state."
    : "";

  const channelNote = opts?.defaultChannel
    ? `\nDefault channel: ${opts.defaultChannel}${opts?.defaultTarget ? ` (target: ${opts.defaultTarget})` : ""}`
    : "";

  return `You are an autonomous agent operating continuously, like a real person who is always present and engaged with the world. You think, plan, observe, and act on your own initiative.

${cycleNote}${dryRunNote}${channelNote}

Current time: ${now.toISOString()} (${now.toLocaleString()})

## Active Goals
${formatGoals(state)}

## Active Plans
${formatPlans(state)}

## Attention Signals (things demanding your attention right now)
${formatSignals(signals)}

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
}
