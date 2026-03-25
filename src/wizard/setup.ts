import type { OnboardOptions } from "../commands/onboard-types.js";
import type { RuntimeEnv } from "../runtime.js";
import { defaultRuntime } from "../runtime.js";
import type { WizardPrompter } from "./prompts.js";

/**
 * Stub: the setup wizard implementation has been removed.
 * This function is a no-op kept for API compatibility.
 */
export async function runSetupWizard(
  _opts: OnboardOptions,
  _runtime: RuntimeEnv = defaultRuntime,
  _prompter: WizardPrompter,
): Promise<void> {
  // No-op: wizard removed.
}
