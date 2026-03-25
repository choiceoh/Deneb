import type { DenebConfig } from "../config/config.js";
import type { SecretInput } from "../config/types.secrets.js";
import { isSecureWebSocketUrl } from "../gateway/net.js";
import type { WizardPrompter } from "../wizard/prompts.js";
import {
  promptSecretRefForSetup,
  resolveSecretInputModeForEnvSelection,
} from "./auth-choice.apply-helpers.js";
import type { SecretInputMode } from "./onboard-types.js";

const DEFAULT_GATEWAY_URL = "ws://127.0.0.1:18789";

function ensureWsUrl(value: string): string {
  const trimmed = value.trim();
  if (!trimmed) {
    return DEFAULT_GATEWAY_URL;
  }
  return trimmed;
}

function validateGatewayWebSocketUrl(value: string): string | undefined {
  const trimmed = value.trim();
  if (!trimmed.startsWith("ws://") && !trimmed.startsWith("wss://")) {
    return "URL must start with ws:// or wss://";
  }
  if (
    !isSecureWebSocketUrl(trimmed, {
      allowPrivateWs: process.env.DENEB_ALLOW_INSECURE_PRIVATE_WS === "1",
    })
  ) {
    return (
      "Use wss:// for remote hosts, or ws://127.0.0.1/localhost via SSH tunnel. " +
      "Break-glass: DENEB_ALLOW_INSECURE_PRIVATE_WS=1 for trusted private networks."
    );
  }
  return undefined;
}

export async function promptRemoteGatewayConfig(
  cfg: DenebConfig,
  prompter: WizardPrompter,
  options?: { secretInputMode?: SecretInputMode },
): Promise<DenebConfig> {
  const suggestedUrl = cfg.gateway?.remote?.url ?? DEFAULT_GATEWAY_URL;

  const urlInput = await prompter.text({
    message: "Gateway WebSocket URL",
    initialValue: suggestedUrl,
    validate: (value) => validateGatewayWebSocketUrl(String(value)),
  });
  const url = ensureWsUrl(String(urlInput));

  const authChoice = await prompter.select({
    message: "Gateway auth",
    options: [
      { value: "token", label: "Token (recommended)" },
      { value: "password", label: "Password" },
      { value: "off", label: "No auth" },
    ],
  });

  let token: SecretInput | undefined = cfg.gateway?.remote?.token;
  let password: SecretInput | undefined = cfg.gateway?.remote?.password;
  if (authChoice === "token") {
    const selectedMode = await resolveSecretInputModeForEnvSelection({
      prompter,
      explicitMode: options?.secretInputMode,
      copy: {
        modeMessage: "How do you want to provide this gateway token?",
        plaintextLabel: "Enter token now",
        plaintextHint: "Stores the token directly in Deneb config",
      },
    });
    if (selectedMode === "ref") {
      const resolved = await promptSecretRefForSetup({
        provider: "gateway-remote-token",
        config: cfg,
        prompter,
        preferredEnvVar: "DENEB_GATEWAY_TOKEN",
        copy: {
          sourceMessage: "Where is this gateway token stored?",
          envVarPlaceholder: "DENEB_GATEWAY_TOKEN",
        },
      });
      token = resolved.ref;
    } else {
      token = String(
        await prompter.text({
          message: "Gateway token",
          initialValue: typeof token === "string" ? token : undefined,
          validate: (value) => (value?.trim() ? undefined : "Required"),
        }),
      ).trim();
    }
    password = undefined;
  } else if (authChoice === "password") {
    const selectedMode = await resolveSecretInputModeForEnvSelection({
      prompter,
      explicitMode: options?.secretInputMode,
      copy: {
        modeMessage: "How do you want to provide this gateway password?",
        plaintextLabel: "Enter password now",
        plaintextHint: "Stores the password directly in Deneb config",
      },
    });
    if (selectedMode === "ref") {
      const resolved = await promptSecretRefForSetup({
        provider: "gateway-remote-password",
        config: cfg,
        prompter,
        preferredEnvVar: "DENEB_GATEWAY_PASSWORD",
        copy: {
          sourceMessage: "Where is this gateway password stored?",
          envVarPlaceholder: "DENEB_GATEWAY_PASSWORD",
        },
      });
      password = resolved.ref;
    } else {
      password = String(
        await prompter.text({
          message: "Gateway password",
          initialValue: typeof password === "string" ? password : undefined,
          validate: (value) => (value?.trim() ? undefined : "Required"),
        }),
      ).trim();
    }
    token = undefined;
  } else {
    token = undefined;
    password = undefined;
  }

  return {
    ...cfg,
    gateway: {
      ...cfg.gateway,
      mode: "remote",
      remote: {
        url,
        ...(token !== undefined ? { token } : {}),
        ...(password !== undefined ? { password } : {}),
      },
    },
  };
}
