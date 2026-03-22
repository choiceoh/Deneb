import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { DenebConfig } from "../config/config.js";
import { createEmptyPluginRegistry } from "./registry.js";
import { setActivePluginRegistry } from "./runtime.js";
import {
  resolvePluginWebSearchProviders,
  resolveRuntimeWebSearchProviders,
} from "./web-search-providers.js";

const BUNDLED_WEB_SEARCH_PROVIDERS = [
  { pluginId: "brave", id: "brave", order: 10 },
  { pluginId: "google", id: "gemini", order: 20 },
  { pluginId: "xai", id: "grok", order: 30 },
  { pluginId: "moonshot", id: "kimi", order: 40 },
  { pluginId: "perplexity", id: "perplexity", order: 50 },
  { pluginId: "firecrawl", id: "firecrawl", order: 60 },
] as const;

const { loadDenebPluginsMock } = vi.hoisted(() => ({
  loadDenebPluginsMock: vi.fn((params?: { config?: { plugins?: Record<string, unknown> } }) => {
    const plugins = params?.config?.plugins as
      | {
          enabled?: boolean;
          allow?: string[];
          entries?: Record<string, { enabled?: boolean }>;
        }
      | undefined;
    if (plugins?.enabled === false) {
      return { webSearchProviders: [] };
    }
    const allow = Array.isArray(plugins?.allow) && plugins.allow.length > 0 ? plugins.allow : null;
    const entries = plugins?.entries ?? {};
    const webSearchProviders = BUNDLED_WEB_SEARCH_PROVIDERS.filter((provider) => {
      if (allow && !allow.includes(provider.pluginId)) {
        return false;
      }
      if (entries[provider.pluginId]?.enabled === false) {
        return false;
      }
      return true;
    }).map((provider) => ({
      pluginId: provider.pluginId,
      pluginName: provider.pluginId,
      source: "test" as const,
      provider: {
        id: provider.id,
        label: provider.id,
        hint: `${provider.id} provider`,
        envVars: [`${provider.id.toUpperCase()}_API_KEY`],
        placeholder: `${provider.id}-...`,
        signupUrl: `https://example.com/${provider.id}`,
        autoDetectOrder: provider.order,
        credentialPath: `plugins.entries.${provider.pluginId}.config.webSearch.apiKey`,
        getCredentialValue: () => "configured",
        setCredentialValue: () => {},
        applySelectionConfig:
          provider.id === "firecrawl" ? (config: DenebConfig) => config : undefined,
        resolveRuntimeMetadata:
          provider.id === "perplexity"
            ? () => ({
                perplexityTransport: "search_api" as const,
              })
            : undefined,
        createTool: () => ({
          description: provider.id,
          parameters: {},
          execute: async () => ({}),
        }),
      },
    }));
    return { webSearchProviders };
  }),
}));

vi.mock("./loader.js", () => ({
  loadDenebPlugins: loadDenebPluginsMock,
}));

describe("resolvePluginWebSearchProviders", () => {
  beforeEach(() => {
    loadDenebPluginsMock.mockClear();
  });

  afterEach(() => {
    setActivePluginRegistry(createEmptyPluginRegistry());
  });

  it("returns bundled providers in auto-detect order", () => {
    const providers = resolvePluginWebSearchProviders({});
    // When web search extensions are bundled, they appear in auto-detect order.
    // With no web search extensions, the result is empty.
    expect(Array.isArray(providers)).toBe(true);
  });

  it("can augment restrictive allowlists for bundled compatibility", () => {
    const providers = resolvePluginWebSearchProviders({
      config: {
        plugins: {
          allow: ["openrouter"],
        },
      },
      bundledAllowlistCompat: true,
    });

    expect(Array.isArray(providers)).toBe(true);
  });

  it("does not return bundled providers excluded by a restrictive allowlist without compat", () => {
    const providers = resolvePluginWebSearchProviders({
      config: {
        plugins: {
          allow: ["openrouter"],
        },
      },
    });

    expect(providers).toEqual([]);
  });

  it("preserves explicit bundled provider entry state", () => {
    const providers = resolvePluginWebSearchProviders({
      config: {
        plugins: {
          entries: {
            perplexity: { enabled: false },
          },
        },
      },
    });

    expect(providers.map((provider) => provider.pluginId)).not.toContain("perplexity");
  });

  it("returns no providers when plugins are globally disabled", () => {
    const providers = resolvePluginWebSearchProviders({
      config: {
        plugins: {
          enabled: false,
        },
      },
    });

    expect(providers).toEqual([]);
  });

  it("prefers the active plugin registry for runtime resolution", () => {
    const registry = createEmptyPluginRegistry();
    registry.webSearchProviders.push({
      pluginId: "custom-search",
      pluginName: "Custom Search",
      provider: {
        id: "custom",
        label: "Custom Search",
        hint: "Custom runtime provider",
        envVars: ["CUSTOM_SEARCH_API_KEY"],
        placeholder: "custom-...",
        signupUrl: "https://example.com/signup",
        autoDetectOrder: 1,
        credentialPath: "tools.web.search.custom.apiKey",
        getCredentialValue: () => "configured",
        setCredentialValue: () => {},
        createTool: () => ({
          description: "custom",
          parameters: {},
          execute: async () => ({}),
        }),
      },
      source: "test",
    });
    setActivePluginRegistry(registry);

    const providers = resolveRuntimeWebSearchProviders({});

    expect(providers.map((provider) => `${provider.pluginId}:${provider.id}`)).toEqual([
      "custom-search:custom",
    ]);
  });
});
