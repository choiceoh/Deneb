// Delivers critical agent alerts to the operator's messaging channel.
// Used for severe failures (repeated LLM errors, delivery failures, stuck runs)
// that need immediate human attention rather than just file logging.

import type { DenebConfig } from "../config/config.js";
import type { SessionEntry } from "../config/sessions.js";
import { createSubsystemLogger } from "../logging/subsystem.js";
import { isDeliverableMessageChannel, normalizeMessageChannel } from "../utils/message-channel.js";
import { buildOutboundSessionContext } from "./outbound/session-context.js";
import { resolveSessionDeliveryTarget } from "./outbound/targets.js";
import { enqueueSystemEvent } from "./system-events.js";

const log = createSubsystemLogger("agent-critical-alert");
let deliverRuntimePromise: Promise<typeof import("./outbound/deliver-runtime.js")> | null = null;

function loadDeliverRuntime() {
  deliverRuntimePromise ??= import("./outbound/deliver-runtime.js");
  return deliverRuntimePromise;
}

export type CriticalAlertSeverity = "error" | "fatal";

export type CriticalAlertParams = {
  cfg: DenebConfig;
  sessionKey: string;
  entry: SessionEntry | undefined;
  severity: CriticalAlertSeverity;
  title: string;
  details: string;
  runId?: string;
  agentId?: string;
  provider?: string;
  model?: string;
};

// Dedup window: avoid spamming the same alert within this window.
const DEDUP_WINDOW_MS = 5 * 60_000; // 5 minutes
const recentAlerts = new Map<string, number>();

// Periodic cleanup of stale dedup entries.
const CLEANUP_INTERVAL_MS = 10 * 60_000;
let cleanupTimer: ReturnType<typeof setInterval> | null = null;

function ensureCleanup() {
  if (cleanupTimer) {
    return;
  }
  cleanupTimer = setInterval(() => {
    const cutoff = Date.now() - DEDUP_WINDOW_MS;
    for (const [key, ts] of recentAlerts) {
      if (ts < cutoff) {
        recentAlerts.delete(key);
      }
    }
    if (recentAlerts.size === 0 && cleanupTimer) {
      clearInterval(cleanupTimer);
      cleanupTimer = null;
    }
  }, CLEANUP_INTERVAL_MS);
  cleanupTimer.unref?.();
}

function buildDedupKey(params: CriticalAlertParams): string {
  return [params.sessionKey, params.title, params.provider ?? "", params.model ?? ""].join("|");
}

function shouldSendAlert(): boolean {
  return !process.env.VITEST && process.env.NODE_ENV !== "test";
}

function formatAlertText(params: CriticalAlertParams): string {
  const icon = params.severity === "fatal" ? "🚨" : "⚠️";
  const parts = [`${icon} **Agent Alert: ${params.title}**`];
  if (params.agentId) {
    parts.push(`Agent: \`${params.agentId}\``);
  }
  if (params.runId) {
    parts.push(`Run: \`${params.runId}\``);
  }
  if (params.provider || params.model) {
    const modelInfo = [params.provider, params.model].filter(Boolean).join("/");
    parts.push(`Model: \`${modelInfo}\``);
  }
  parts.push(`\n${params.details}`);
  return parts.join("\n");
}

export async function deliverCriticalAlert(params: CriticalAlertParams): Promise<boolean> {
  if (!shouldSendAlert()) {
    return false;
  }

  const dedupKey = buildDedupKey(params);
  const lastSent = recentAlerts.get(dedupKey);
  if (lastSent && Date.now() - lastSent < DEDUP_WINDOW_MS) {
    log.debug(`Skipping duplicate critical alert: ${params.title}`);
    return false;
  }
  recentAlerts.set(dedupKey, Date.now());
  ensureCleanup();

  const text = formatAlertText(params);

  // Always log the alert at error/fatal level for file logs.
  const logMethod = params.severity === "fatal" ? log.fatal : log.error;
  logMethod(`Critical alert: ${params.title}`, {
    event: "critical_alert",
    sessionKey: params.sessionKey,
    runId: params.runId,
    agentId: params.agentId,
    provider: params.provider,
    model: params.model,
    details: params.details,
  });

  if (!params.entry) {
    enqueueSystemEvent(text, { sessionKey: params.sessionKey });
    return true;
  }

  const target = resolveSessionDeliveryTarget({
    entry: params.entry,
    requestedChannel: "last",
  });

  if (!target.channel || !target.to) {
    enqueueSystemEvent(text, { sessionKey: params.sessionKey });
    return true;
  }

  const channel = normalizeMessageChannel(target.channel) ?? target.channel;
  if (!isDeliverableMessageChannel(channel)) {
    enqueueSystemEvent(text, { sessionKey: params.sessionKey });
    return true;
  }

  try {
    const { deliverOutboundPayloads } = await loadDeliverRuntime();
    const outboundSession = buildOutboundSessionContext({
      cfg: params.cfg,
      sessionKey: params.sessionKey,
    });
    await deliverOutboundPayloads({
      cfg: params.cfg,
      channel,
      to: target.to,
      accountId: target.accountId,
      threadId: target.threadId,
      payloads: [{ text }],
      session: outboundSession,
    });
    return true;
  } catch (err) {
    log.warn(`Failed to deliver critical alert via channel: ${String(err)}`);
    enqueueSystemEvent(text, { sessionKey: params.sessionKey });
    return true;
  }
}

export function resetCriticalAlertStateForTest() {
  recentAlerts.clear();
  if (cleanupTimer) {
    clearInterval(cleanupTimer);
    cleanupTimer = null;
  }
}
