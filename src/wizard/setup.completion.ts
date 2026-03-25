import type { WizardPrompter } from "./prompts.js";
import type { WizardFlow } from "./setup.types.js";

/**
 * Stub: the shell completion wizard setup has been removed.
 * This function is a no-op kept for API compatibility.
 */
export async function setupWizardShellCompletion(_params: {
  flow: WizardFlow;
  prompter: Pick<WizardPrompter, "confirm" | "note">;
  deps?: Partial<Record<string, unknown>>;
}): Promise<void> {
  // No-op: wizard removed.
}
