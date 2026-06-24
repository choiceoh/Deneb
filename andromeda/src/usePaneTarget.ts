// Deep-link target hook. When another pane asks to open a specific target here
// (via `openPane(view, { id, dayKey, ... })`), this runs `apply` and then consumes
// the target (clears the pending one) — so a pane doesn't re-implement the
// "view matches → act → consume" effect each time.
//
// `apply` receives `(id, target)`: most panes only need the id (a one-arg callback
// is fine — extra args are ignored), while panes targeted by more than an id (e.g.
// the calendar's `dayKey`) read the rest off `target`. It re-runs whenever it/its
// closure changes (so a pane that finds a row by id sees the latest list) but is a
// no-op once `paneTarget` no longer matches this view. `isLoading` (default false)
// gates consumption: panes backed by a still-loading query pass it so the target
// isn't consumed before the matching row exists.
import { useEffect } from "react";
import { useWorkspace, type PaneTarget } from "./workspaceContext";
import type { View } from "./types";

export function usePaneTarget(
  view: View,
  apply: (id: string | number | undefined, target: PaneTarget) => void,
  isLoading = false,
): void {
  const { paneTarget, consumePaneTarget } = useWorkspace();
  useEffect(() => {
    if (paneTarget?.view !== view) return;
    apply(paneTarget.id, paneTarget);
    if (!isLoading) consumePaneTarget();
  }, [apply, consumePaneTarget, paneTarget, view, isLoading]);
}
