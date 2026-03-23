import { type DenebConfig } from "../config/config.js";
import { resolveSecretInputRef } from "../config/types.secrets.js";
import { readGatewayTokenEnv } from "../gateway/auth/credentials.js";

type GatewayInstallTokenOptions = {
  config: DenebConfig;
  env: NodeJS.ProcessEnv;
  explicitToken?: string;
  autoGenerateWhenMissing?: boolean;
  persistGeneratedToken?: boolean;
};

export type GatewayInstallTokenResolution = {
  token?: string;
  tokenRefConfigured: boolean;
  unavailableReason?: string;
  warnings: string[];
};

export async function resolveGatewayInstallToken(
  options: GatewayInstallTokenOptions,
): Promise<GatewayInstallTokenResolution> {
  const cfg = options.config;
  const warnings: string[] = [];
  const tokenRef = resolveSecretInputRef({
    value: cfg.gateway?.auth?.token,
    defaults: cfg.secrets?.defaults,
  }).ref;
  const tokenRefConfigured = Boolean(tokenRef);
  const configToken =
    tokenRef || typeof cfg.gateway?.auth?.token !== "string"
      ? undefined
      : cfg.gateway.auth.token.trim() || undefined;
  const explicitToken = options.explicitToken?.trim() || undefined;
  const envToken = readGatewayTokenEnv(options.env);

  const token: string | undefined =
    explicitToken || configToken || (tokenRef ? undefined : envToken);
  const unavailableReason: string | undefined = undefined;

  return {
    token,
    tokenRefConfigured,
    unavailableReason,
    warnings,
  };
}
