import type { OnboardOptions } from "../commands/onboard-types.js";
import type { DenebConfig } from "../config/config.js";
import type { RuntimeEnv } from "../runtime.js";
import type { WizardPrompter } from "./prompts.js";
import type { GatewayWizardSettings, WizardFlow } from "./setup.types.js";

type FinalizeOnboardingOptions = {
  flow: WizardFlow;
  opts: OnboardOptions;
  baseConfig: DenebConfig;
  nextConfig: DenebConfig;
  workspaceDir: string;
  settings: GatewayWizardSettings;
  prompter: WizardPrompter;
  runtime: RuntimeEnv;
};

/**
 * Stub: the setup finalization wizard has been removed.
 * This function is a no-op kept for API compatibility.
 */
export async function finalizeSetupWizard(_options: FinalizeOnboardingOptions): Promise<void> {
  // No-op: wizard removed.
}
