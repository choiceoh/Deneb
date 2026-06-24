// Deep-link target hook. When another pane asks to open a specific target here
// (via `openPane(view, { id, dayKey, ... })`), this runs `apply(target)` and then
// consumes the pending target — so a pane doesn't re-implement the
// "view matches → act → consume" effect each time.
//
// `apply` receives the whole target (read `id`, `dayKey`, etc. off it). Return
// `false` to signal "can't handle this yet" — e.g. the target lacks the id this
// pane needs, or the matching row hasn't loaded — and the target stays PENDING so
// a later render (after data loads) can retry. Any other return (a value or void)
// counts as handled and consumes the target. `apply` re-runs whenever it/its
// closure changes, so memoize it (useCallback) over the data it reads.
import { useEffect } from "react";
import { useWorkspace, type PaneTarget } from "./workspaceContext";
import type { View } from "./types";

export function usePaneTarget(view: View, apply: (target: PaneTarget) => boolean | void): void {
  const { paneTarget, consumePaneTarget } = useWorkspace();
  useEffect(() => {
    if (paneTarget?.view !== view) return;
    if (apply(paneTarget) !== false) consumePaneTarget();
  }, [apply, consumePaneTarget, paneTarget, view]);
}
