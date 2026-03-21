import type { DenebConfig } from "./config.js";

export function ensurePluginAllowlisted(cfg: DenebConfig, pluginId: string): DenebConfig {
  const allow = cfg.plugins?.allow;
  if (!Array.isArray(allow) || allow.includes(pluginId)) {
    return cfg;
  }
  return {
    ...cfg,
    plugins: {
      ...cfg.plugins,
      allow: [...allow, pluginId],
    },
  };
}
