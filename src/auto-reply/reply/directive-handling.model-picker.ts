import { type ModelRef, normalizeProviderId } from "../../agents/models/model-selection.js";
import type { DenebConfig } from "../../config/config.js";

export type ModelPickerCatalogEntry = {
  provider: string;
  id: string;
  name?: string;
};

export type ModelPickerItem = ModelRef;

export function resolveProviderEndpointLabel(
  provider: string,
  cfg: DenebConfig,
): { endpoint?: string; api?: string } {
  const normalized = normalizeProviderId(provider);
  const providers = (cfg.models?.providers ?? {}) as Record<
    string,
    { baseUrl?: string; api?: string } | undefined
  >;
  const entry = providers[normalized];
  const endpoint = entry?.baseUrl?.trim();
  const api = entry?.api?.trim();
  return {
    endpoint: endpoint || undefined,
    api: api || undefined,
  };
}
