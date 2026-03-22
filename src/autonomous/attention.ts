import type { AttentionSignal, AutonomousState } from "./types.js";
import { isFiniteNonNegative } from "./validation.js";

const MAX_SIGNALS = 50;
const URGENCY_HIGH = 0.9;
const URGENCY_MEDIUM = 0.6;
const URGENCY_LOW = 0.3;

/** Boost urgency by 0.1 per ignored cycle, capping at 1.0. */
const URGENCY_BOOST_PER_IGNORE = 0.1;
/** After this many ignores, signals are marked as critical in the prompt. */
export const CRITICAL_IGNORE_THRESHOLD = 3;
/** Active goals with no progress update for this many cycles are flagged stale. */
const STALE_GOAL_CYCLE_THRESHOLD = 5;

/** Clamp a value to the 0-1 range, defaulting to 0 for non-finite inputs. */
function clampUrgency(value: number): number {
  if (!Number.isFinite(value)) {
    return 0;
  }
  return Math.max(0, Math.min(1, value));
}

/** Safe numeric comparator that treats NaN/non-finite as 0. */
function safeDesc(a: number, b: number): number {
  const sa = Number.isFinite(a) ? a : 0;
  const sb = Number.isFinite(b) ? b : 0;
  return sb - sa;
}

/**
 * Attention signal collector. Accumulates signals from various
 * sources (channel messages, system events, goal deadlines) and provides
 * prioritized access for the autonomous decision cycle.
 *
 * Signals are restored from persisted state on startup and drained back
 * to state on save, so they survive gateway restarts.
 */
export class AttentionManager {
  private signals: AttentionSignal[] = [];
  /** Signals that were presented to the agent in the last getTopSignals() call. */
  private lastPresented: AttentionSignal[] = [];

  /** Restore persisted signals from state (called on cycle start). */
  restoreFromState(state: AutonomousState): void {
    for (const signal of state.pendingSignals) {
      this.addSignal(signal);
    }
    // Clear from state to avoid double-loading on next call.
    state.pendingSignals = [];
  }

  /** Drain unconsumed signals back into state for persistence. */
  drainToState(state: AutonomousState): void {
    state.pendingSignals = [...this.signals];
  }

  /**
   * Compare pre-cycle state snapshot with post-cycle state to detect
   * which presented signals were actually addressed. Re-queue ignored
   * signals with boosted urgency and incremented ignoredCount.
   */
  reEscalateIgnoredSignals(preCycleState: AutonomousState, postCycleState: AutonomousState): void {
    if (this.lastPresented.length === 0) {
      return;
    }

    // Build sets of things that changed during the cycle.
    const changedGoalIds = new Set<string>();
    const changedSocialIds = new Set<string>();
    const processedObsIds = new Set<string>();

    // Detect goal progress/status changes.
    const preGoalMap = new Map(preCycleState.goals.map((g) => [g.id, g]));
    for (const goal of postCycleState.goals) {
      const pre = preGoalMap.get(goal.id);
      if (!pre) {
        changedGoalIds.add(goal.id);
      } else if (pre.status !== goal.status || pre.progress !== goal.progress) {
        changedGoalIds.add(goal.id);
      }
    }

    // Detect social context changes.
    const preSocialMap = new Map(preCycleState.socialContext.map((s) => [s.id, s]));
    for (const entry of postCycleState.socialContext) {
      const pre = preSocialMap.get(entry.id);
      if (!pre) {
        changedSocialIds.add(entry.id);
      } else if (pre.context !== entry.context || pre.followUpAt !== entry.followUpAt) {
        changedSocialIds.add(entry.id);
      }
    }

    // Detect newly processed observations.
    const preObsMap = new Map(preCycleState.observations.map((o) => [o.id, o]));
    for (const obs of postCycleState.observations) {
      const pre = preObsMap.get(obs.id);
      if (obs.processed && (!pre || !pre.processed)) {
        processedObsIds.add(obs.id);
      }
    }

    // Check each presented signal against changes.
    for (const signal of this.lastPresented) {
      if (wasSignalAddressed(signal, changedGoalIds, changedSocialIds, processedObsIds)) {
        continue;
      }
      // Signal was ignored — re-queue with boosted urgency.
      const count = (signal.ignoredCount ?? 0) + 1;
      const boostedUrgency = Math.min(1.0, signal.urgency + URGENCY_BOOST_PER_IGNORE);
      this.addSignal({
        ...signal,
        urgency: boostedUrgency,
        ignoredCount: count,
      });
    }

    this.lastPresented = [];
  }

  /** Get the signals that were last presented (for prompt formatting). */
  getLastPresented(): ReadonlyArray<AttentionSignal> {
    return this.lastPresented;
  }

  addSignal(signal: AttentionSignal): void {
    // Validate signal inputs.
    if (typeof signal.source !== "string" || typeof signal.content !== "string") {
      return;
    }
    if (!isFiniteNonNegative(signal.timestamp)) {
      return;
    }
    if (!Number.isFinite(signal.urgency)) {
      return;
    }

    // Clamp urgency to 0-1 range.
    const clamped: AttentionSignal = {
      ...signal,
      urgency: clampUrgency(signal.urgency),
    };

    this.signals.push(clamped);
    if (this.signals.length > MAX_SIGNALS) {
      // Drop lowest urgency signals when buffer is full.
      this.signals = this.signals
        .toSorted((a, b) => safeDesc(a.urgency, b.urgency))
        .slice(0, MAX_SIGNALS);
    }
  }

  addMessage(source: string, content: string, urgency?: number): void {
    this.addSignal({
      source,
      type: "message",
      content,
      urgency: urgency !== undefined ? clampUrgency(urgency) : URGENCY_MEDIUM,
      timestamp: Date.now(),
    });
  }

  addEvent(source: string, content: string, urgency?: number): void {
    this.addSignal({
      source,
      type: "event",
      content,
      urgency: urgency !== undefined ? clampUrgency(urgency) : URGENCY_LOW,
      timestamp: Date.now(),
    });
  }

  /**
   * Generate attention signals from the autonomous state itself:
   * goal deadlines approaching, social follow-ups due, etc.
   */
  deriveSignalsFromState(state: AutonomousState): void {
    const now = Date.now();

    // Goal deadlines approaching.
    for (const goal of state.goals) {
      if (goal.status !== "active" || goal.dueAt == null) {
        continue;
      }
      if (!isFiniteNonNegative(goal.dueAt)) {
        continue;
      }
      const remaining = goal.dueAt - now;
      if (remaining <= 0) {
        this.addSignal({
          source: `goal:${goal.id}`,
          type: "goal-deadline",
          content: `Goal overdue: "${goal.description}"`,
          urgency: URGENCY_HIGH,
          timestamp: now,
        });
      } else if (remaining < 3_600_000) {
        // Less than 1 hour.
        this.addSignal({
          source: `goal:${goal.id}`,
          type: "goal-deadline",
          content: `Goal due soon (${Math.round(remaining / 60_000)}min): "${goal.description}"`,
          urgency: URGENCY_MEDIUM,
          timestamp: now,
        });
      }
    }

    // Social follow-ups due.
    for (const entry of state.socialContext) {
      if (entry.followUpAt == null) {
        continue;
      }
      if (!isFiniteNonNegative(entry.followUpAt)) {
        continue;
      }
      if (entry.followUpAt <= now) {
        this.addSignal({
          source: `social:${entry.channel}:${entry.peerId}`,
          type: "followup",
          content: `Follow-up due with ${entry.peerId} on ${entry.channel}: ${entry.context}`,
          urgency: URGENCY_MEDIUM,
          timestamp: now,
        });
      }
    }

    // Stale goals: active goals with no progress update for many cycles.
    for (const goal of state.goals) {
      if (goal.status !== "active") {
        continue;
      }
      // Determine how many cycles have passed since last progress update.
      const lastProgress = goal.lastProgressAt ?? goal.createdAt;
      if (!isFiniteNonNegative(lastProgress)) {
        continue;
      }
      const cyclesSinceProgress =
        state.cycleCount > 0 ? estimateCyclesSince(lastProgress, state) : 0;
      if (cyclesSinceProgress >= STALE_GOAL_CYCLE_THRESHOLD) {
        const staleness = Math.min(cyclesSinceProgress, 20);
        // Urgency escalates from MEDIUM toward HIGH as staleness increases.
        const urgency = Math.min(
          1.0,
          URGENCY_MEDIUM + (staleness - STALE_GOAL_CYCLE_THRESHOLD) * 0.05,
        );
        this.addSignal({
          source: `goal:${goal.id}`,
          type: "goal-deadline",
          content: `Stale goal (no progress for ~${staleness} cycles): "${goal.description}"`,
          urgency,
          timestamp: now,
          ignoredCount: Math.max(0, staleness - STALE_GOAL_CYCLE_THRESHOLD),
        });
      }
    }

    // Unprocessed high-relevance observations.
    for (const obs of state.observations) {
      if (obs.processed) {
        continue;
      }
      if (obs.relevance === "high") {
        this.addSignal({
          source: obs.source,
          type: "event",
          content: `Unprocessed observation: ${obs.content}`,
          urgency: URGENCY_MEDIUM,
          timestamp: isFiniteNonNegative(obs.observedAt) ? obs.observedAt : now,
        });
      }
    }
  }

  /**
   * Return the top N signals sorted by urgency (desc), then timestamp (desc).
   * Drains the returned signals from the buffer and records them as "presented"
   * for subsequent ignore detection.
   */
  getTopSignals(n: number): AttentionSignal[] {
    if (this.signals.length === 0 || n <= 0) {
      return [];
    }
    const sorted = this.signals.toSorted((a, b) => {
      const urgencyDiff = safeDesc(a.urgency, b.urgency);
      if (urgencyDiff !== 0) {
        return urgencyDiff;
      }
      return safeDesc(a.timestamp, b.timestamp);
    });
    const top = sorted.slice(0, n);
    this.signals = sorted.slice(n);
    // Record presented signals for ignore detection after the cycle.
    this.lastPresented = [...top];
    return top;
  }

  /** Check if there are any signals with urgency above the threshold. */
  hasUrgentSignals(threshold = 0.8): boolean {
    return this.signals.some((s) => {
      const u = Number.isFinite(s.urgency) ? s.urgency : 0;
      return u >= threshold;
    });
  }

  /** Current number of pending signals. */
  get pendingCount(): number {
    return this.signals.length;
  }

  /** Clear all signals. */
  clear(): void {
    this.signals = [];
    this.lastPresented = [];
  }
}

/**
 * Determine if a presented signal was addressed by checking if any
 * related state entity changed during the cycle.
 */
function wasSignalAddressed(
  signal: AttentionSignal,
  changedGoalIds: Set<string>,
  changedSocialIds: Set<string>,
  processedObsIds: Set<string>,
): boolean {
  const src = signal.source;
  // Goal-related signal: "goal:<id>"
  if (src.startsWith("goal:")) {
    const goalId = src.slice("goal:".length);
    return changedGoalIds.has(goalId);
  }
  // Social follow-up: "social:<channel>:<peerId>"
  if (src.startsWith("social:")) {
    // Check if any social entry was updated.
    return changedSocialIds.size > 0;
  }
  // Observation-based signals: check if any observation was processed.
  if (signal.type === "event" && signal.content.startsWith("Unprocessed observation:")) {
    return processedObsIds.size > 0;
  }
  // Message/event from external sources: assume addressed if agent took any action.
  // (This is conservative — we don't re-escalate external messages unless
  // nothing at all happened, which is handled by the "no actions" path.)
  return false;
}

/**
 * Estimate how many cycles have elapsed since a given timestamp,
 * based on the average cycle interval derived from state.
 */
function estimateCyclesSince(timestamp: number, state: AutonomousState): number {
  if (state.cycleCount <= 0 || !isFiniteNonNegative(state.lastCycleAt)) {
    return 0;
  }
  const elapsed = state.lastCycleAt - timestamp;
  if (elapsed <= 0) {
    return 0;
  }
  // Use lastCycleAt / cycleCount as a rough average interval.
  // This avoids needing to store cycle history.
  const avgInterval = state.lastCycleAt > 0 ? elapsed / state.cycleCount : 300_000;
  if (avgInterval <= 0) {
    return 0;
  }
  return Math.floor(elapsed / avgInterval);
}
