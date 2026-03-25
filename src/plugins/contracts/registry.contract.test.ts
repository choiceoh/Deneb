import { describe, expect, it } from "vitest";
import { resolveBundledWebSearchPluginIds } from "../bundled-web-search.js";
import { loadPluginManifestRegistry } from "../manifest-registry.js";
import {
  imageGenerationProviderContractRegistry,
  mediaUnderstandingProviderContractRegistry,
  pluginRegistrationContractRegistry,
  providerContractLoadError,
  providerContractPluginIds,
  providerContractRegistry,
  webSearchProviderContractRegistry,
} from "./registry.js";

describe("plugin contract registry", () => {
  it("loads bundled non-provider capability registries without import-time failure", () => {
    expect(providerContractLoadError).toBeUndefined();
  });

  it("does not duplicate bundled provider ids", () => {
    const ids = providerContractRegistry.map((entry) => entry.provider.id);
    expect(ids).toEqual([...new Set(ids)]);
  });

  it("does not duplicate bundled web search provider ids", () => {
    const ids = webSearchProviderContractRegistry.map((entry) => entry.provider.id);
    expect(ids).toEqual([...new Set(ids)]);
  });

  it("does not duplicate bundled media provider ids", () => {
    const ids = mediaUnderstandingProviderContractRegistry.map((entry) => entry.provider.id);
    expect(ids).toEqual([...new Set(ids)]);
  });

  it("covers every bundled provider plugin discovered from manifests", () => {
    const bundledProviderPluginIds = loadPluginManifestRegistry({})
      .plugins.filter((plugin) => plugin.origin === "bundled" && plugin.providers.length > 0)
      .map((plugin) => plugin.id)
      .toSorted((left, right) => left.localeCompare(right));

    expect(providerContractPluginIds).toEqual(bundledProviderPluginIds);
  });

  it("covers every bundled web search plugin from the shared resolver", () => {
    const bundledWebSearchPluginIds = resolveBundledWebSearchPluginIds({});

    expect(
      [...new Set(webSearchProviderContractRegistry.map((entry) => entry.pluginId))].toSorted(
        (left, right) => left.localeCompare(right),
      ),
    ).toEqual(bundledWebSearchPluginIds);
  });

  it("does not duplicate bundled image-generation provider ids", () => {
    const ids = imageGenerationProviderContractRegistry.map((entry) => entry.provider.id);
    expect(ids).toEqual([...new Set(ids)]);
  });

  it("tracks every provider, media, image, or web search plugin in the registration registry", () => {
    const expectedPluginIds = [
      ...new Set([
        ...providerContractRegistry.map((entry) => entry.pluginId),
        ...mediaUnderstandingProviderContractRegistry.map((entry) => entry.pluginId),
        ...imageGenerationProviderContractRegistry.map((entry) => entry.pluginId),
        ...webSearchProviderContractRegistry.map((entry) => entry.pluginId),
      ]),
    ].toSorted((left, right) => left.localeCompare(right));

    expect(
      pluginRegistrationContractRegistry
        .map((entry) => entry.pluginId)
        .toSorted((left, right) => left.localeCompare(right)),
    ).toEqual(expectedPluginIds);
  });
});
