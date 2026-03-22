import type { AttentionSignal, AutonomousState } from "./types.js";
import { isFiniteNonNegative } from "./validation.js";

const MAX_SIGNALS = 50;
const URGENCY_HIGH = 0.9;
const URGENCY_MEDIUM = 0.6;
const URGENCY_LOW = 0.3;

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
   * Drains the returned signals from the buffer.
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
  }
}
