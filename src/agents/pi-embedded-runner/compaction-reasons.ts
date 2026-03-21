export type CompactionReasonCode =
  | "no_compactable_entries"
  | "below_threshold"
  | "already_compacted_recently"
  | "live_context_still_exceeds_target"
  | "guard_blocked"
  | "summary_failed"
  | "timeout"
  | "provider_error_4xx"
  | "provider_error_5xx"
  | "unknown";

/** Classify a free-text compaction reason into a structured code. */
export function classifyCompactionReason(reason?: string): CompactionReasonCode {
  const text = (reason ?? "").trim().toLowerCase();
  if (!text) {
    return "unknown";
  }
  if (text.includes("nothing to compact")) {
    return "no_compactable_entries";
  }
  if (text.includes("below threshold")) {
    return "below_threshold";
  }
  if (text.includes("already compacted")) {
    return "already_compacted_recently";
  }
  if (text.includes("still exceeds target")) {
    return "live_context_still_exceeds_target";
  }
  if (text.includes("guard")) {
    return "guard_blocked";
  }
  if (text.includes("summary")) {
    return "summary_failed";
  }
  if (text.includes("timed out") || text.includes("timeout")) {
    return "timeout";
  }
  if (
    text.includes("400") ||
    text.includes("401") ||
    text.includes("403") ||
    text.includes("429")
  ) {
    return "provider_error_4xx";
  }
  if (
    text.includes("500") ||
    text.includes("502") ||
    text.includes("503") ||
    text.includes("504")
  ) {
    return "provider_error_5xx";
  }
  return "unknown";
}
