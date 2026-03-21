import type { AttentionSignal, AutonomousState } from "./types.js";

const MAX_SIGNALS = 50;
const URGENCY_HIGH = 0.9;
const URGENCY_MEDIUM = 0.6;
const URGENCY_LOW = 0.3;

/**
 * In-memory attention signal collector. Accumulates signals from various
 * sources (channel messages, system events, goal deadlines) and provides
 * prioritized access for the autonomous decision cycle.
 */
export class AttentionManager {
  private signals: AttentionSignal[] = [];

  addSignal(signal: AttentionSignal): void {
    this.signals.push(signal);
    if (this.signals.length > MAX_SIGNALS) {
      // Drop lowest urgency signals when buffer is full.
      this.signals = this.signals.toSorted((a, b) => b.urgency - a.urgency).slice(0, MAX_SIGNALS);
    }
  }

  addMessage(source: string, content: string, urgency?: number): void {
    this.addSignal({
      source,
      type: "message",
      content,
      urgency: urgency ?? URGENCY_MEDIUM,
      timestamp: Date.now(),
    });
  }

  addEvent(source: string, content: string, urgency?: number): void {
    this.addSignal({
      source,
      type: "event",
      content,
      urgency: urgency ?? URGENCY_LOW,
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
      if (goal.status !== "active" || !goal.dueAt) {
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
      if (!entry.followUpAt) {
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
          timestamp: obs.observedAt,
        });
      }
    }
  }

  /**
   * Return the top N signals sorted by urgency (desc), then timestamp (desc).
   * Drains the returned signals from the buffer.
   */
  getTopSignals(n: number): AttentionSignal[] {
    const sorted = this.signals.toSorted((a, b) => {
      if (b.urgency !== a.urgency) {
        return b.urgency - a.urgency;
      }
      return b.timestamp - a.timestamp;
    });
    const top = sorted.slice(0, n);
    this.signals = sorted.slice(n);
    return top;
  }

  /** Check if there are any signals with urgency above the threshold. */
  hasUrgentSignals(threshold = 0.8): boolean {
    return this.signals.some((s) => s.urgency >= threshold);
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
