import { resolveCommandSecretRefsViaGateway } from "../../cli/command-secret-gateway.js";
import { getModelsCommandSecretTargetIds } from "../../cli/command-secret-targets.js";
import {
  loadConfig,
  readConfigFileSnapshotForWrite,
  setRuntimeConfigSnapshot,
  type DenebConfig,
} from "../../config/config.js";
import type { RuntimeEnv } from "../../runtime.js";

export type LoadedModelsConfig = {
  sourceConfig: DenebConfig;
  resolvedConfig: DenebConfig;
  diagnostics: string[];
};

async function loadSourceConfigSnapshot(fallback: DenebConfig): Promise<DenebConfig> {
  try {
    const { snapshot } = await readConfigFileSnapshotForWrite();
    if (snapshot.valid) {
      return snapshot.resolved;
    }
  } catch {
    // Fall back to runtime-loaded config if source snapshot cannot be read.
  }
  return fallback;
}

export async function loadModelsConfigWithSource(params: {
  commandName: string;
  runtime?: RuntimeEnv;
}): Promise<LoadedModelsConfig> {
  const runtimeConfig = loadConfig();
  const sourceConfig = await loadSourceConfigSnapshot(runtimeConfig);
  const { resolvedConfig, diagnostics } = await resolveCommandSecretRefsViaGateway({
    config: runtimeConfig,
    commandName: params.commandName,
    targetIds: getModelsCommandSecretTargetIds(),
  });
  if (params.runtime) {
    for (const entry of diagnostics) {
      params.runtime.log(`[secrets] ${entry}`);
    }
  }
  setRuntimeConfigSnapshot(resolvedConfig, sourceConfig);
  return {
    sourceConfig,
    resolvedConfig,
    diagnostics,
  };
}

export async function loadModelsConfig(params: {
  commandName: string;
  runtime?: RuntimeEnv;
}): Promise<DenebConfig> {
  return (await loadModelsConfigWithSource(params)).resolvedConfig;
}
