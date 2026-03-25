import type { SecretInputMode } from "../commands/onboard-types.js";
import type { DenebConfig } from "../config/config.js";
import type { RuntimeEnv } from "../runtime.js";
import type { WizardPrompter } from "./prompts.js";
import type {
  GatewayWizardSettings,
  QuickstartGatewayDefaults,
  WizardFlow,
} from "./setup.types.js";

type ConfigureGatewayOptions = {
  flow: WizardFlow;
  baseConfig: DenebConfig;
  nextConfig: DenebConfig;
  localPort: number;
  quickstartGateway: QuickstartGatewayDefaults;
  secretInputMode?: SecretInputMode;
  prompter: WizardPrompter;
  runtime: RuntimeEnv;
};

type ConfigureGatewayResult = {
  nextConfig: DenebConfig;
  settings: GatewayWizardSettings;
};

/**
 * Stub: the gateway setup wizard implementation has been removed.
 * Returns defaults as a no-op.
 */
export async function configureGatewayForSetup(
  opts: ConfigureGatewayOptions,
): Promise<ConfigureGatewayResult> {
  return {
    nextConfig: opts.nextConfig,
    settings: {
      port: opts.localPort,
      bind: opts.quickstartGateway.bind,
      authMode: opts.quickstartGateway.authMode,
      tailscaleMode: opts.quickstartGateway.tailscaleMode,
      tailscaleResetOnExit: opts.quickstartGateway.tailscaleResetOnExit,
    },
  };
}
