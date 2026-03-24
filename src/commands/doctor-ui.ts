import {
  resolveControlUiDistIndexHealth,
  resolveControlUiDistIndexPathForRoot,
} from "../infra/control-ui-assets.js";
import { resolveDenebPackageRoot } from "../infra/deneb-root.js";
import type { RuntimeEnv } from "../runtime.js";
import { note } from "../terminal/note.js";
import type { DoctorPrompter } from "./doctor-prompter.js";

export async function maybeRepairUiProtocolFreshness(
  _runtime: RuntimeEnv,
  _prompter: DoctorPrompter,
) {
  const root = await resolveDenebPackageRoot({
    moduleUrl: import.meta.url,
    argv1: process.argv[1],
    cwd: process.cwd(),
  });

  if (!root) {
    return;
  }

  // The legacy Lit-based UI (ui/) has been removed. The Control UI is now
  // served from pre-built dist assets only. If those are missing, note it
  // but there is nothing to rebuild from source.
  const uiHealth = await resolveControlUiDistIndexHealth({
    root,
    argv1: process.argv[1],
  });
  const uiIndexPath = uiHealth.indexPath ?? resolveControlUiDistIndexPathForRoot(root);

  try {
    const { stat } = await import("node:fs/promises");
    const uiStats = await stat(uiIndexPath).catch(() => null);
    if (!uiStats) {
      note("Control UI dist assets are missing. Re-install or rebuild to restore.", "UI");
    }
  } catch {
    // Silently skip if stat fails.
  }
}
