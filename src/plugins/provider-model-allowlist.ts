import { DEFAULT_PROVIDER } from "../agents/defaults.js";
import { resolveAllowlistModelKey } from "../agents/model-selection.js";
import type { DenebConfig } from "../config/config.js";

export function ensureModelAllowlistEntry(params: {
  cfg: DenebConfig;
  modelRef: string;
  defaultProvider?: string;
}): DenebConfig {
  const rawModelRef = params.modelRef.trim();
  if (!rawModelRef) {
    return params.cfg;
  }

  const models = { ...params.cfg.agents?.defaults?.models };
  const keySet = new Set<string>([rawModelRef]);
  const canonicalKey = resolveAllowlistModelKey(
    rawModelRef,
    params.defaultProvider ?? DEFAULT_PROVIDER,
  );
  if (canonicalKey) {
    keySet.add(canonicalKey);
  }

  for (const key of keySet) {
    models[key] = {
      ...models[key],
    };
  }

  return {
    ...params.cfg,
    agents: {
      ...params.cfg.agents,
      defaults: {
        ...params.cfg.agents?.defaults,
        models,
      },
    },
  };
}
