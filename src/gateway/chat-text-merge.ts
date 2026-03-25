/**
 * Helpers for merging streamed assistant text segments during chat delivery.
 *
 * Extracted from server-chat.ts so that the merge logic—especially the
 * tool-boundary retention fix—can be unit-tested in isolation.
 *
 * Preserve pre-tool-call text when a fresh
 * post-tool segment arrives that does not overlap with the existing buffer.
 */

/**
 * Appends `suffix` to `base` while de-duplicating any trailing overlap
 * between the two strings.
 */
export function appendUniqueSuffix(base: string, suffix: string): string {
  if (!suffix) {
    return base;
  }
  if (!base) {
    return suffix;
  }
  if (base.endsWith(suffix)) {
    return base;
  }
  const maxOverlap = Math.min(base.length, suffix.length);
  for (let overlap = maxOverlap; overlap > 0; overlap -= 1) {
    if (base.slice(-overlap) === suffix.slice(0, overlap)) {
      return base + suffix.slice(overlap);
    }
  }
  return base + suffix;
}

/**
 * Merge incoming assistant text/delta into the running per-run buffer.
 *
 * `nextText` is the full accumulated text for the current segment (may
 * reset to a fresh string after a tool call).  `nextDelta` is the
 * incremental chunk for the current event.
 *
 * The tool-boundary fix: when `nextText` is completely disjoint from
 * `previousText` (neither is a prefix of the other) and no delta is
 * provided, the previous implementation would silently discard
 * `previousText`.  We now detect this case and concatenate the two
 * segments so pre-tool text is retained in the buffer.
 */
export function resolveMergedAssistantText(params: {
  previousText: string;
  nextText: string;
  nextDelta: string;
}): string {
  const { previousText, nextText, nextDelta } = params;

  if (nextText && previousText) {
    // nextText is a continuation that already includes previousText.
    if (nextText.startsWith(previousText)) {
      return nextText;
    }
    // Stale / duplicate segment shorter than what we already have.
    if (previousText.startsWith(nextText) && !nextDelta) {
      return previousText;
    }
    // Tool-boundary retention: nextText is a fresh
    // post-tool segment that does not overlap with the pre-tool buffer.
    // Concatenate so the earlier text is not lost.
    if (nextDelta) {
      return appendUniqueSuffix(previousText, nextDelta);
    }
    return previousText + "\n\n" + nextText;
  }

  if (nextDelta) {
    return appendUniqueSuffix(previousText, nextDelta);
  }
  if (nextText) {
    return nextText;
  }
  return previousText;
}
