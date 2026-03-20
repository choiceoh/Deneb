// Provider catalog re-exports removed — extensions no longer bundled in-tree.
// Consumers must load provider catalogs at runtime via the plugin SDK.

// Stub: static provider catalog is now empty; runtime discovery handles providers.
export function getStaticProviderCatalog(): readonly [] {
  return [] as const;
}
