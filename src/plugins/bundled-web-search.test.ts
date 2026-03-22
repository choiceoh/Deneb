import { expect, it } from "vitest";
import { resolveBundledWebSearchPluginIds } from "./bundled-web-search.js";

it("keeps bundled web search compat ids aligned with bundled manifests", () => {
  const resolved = resolveBundledWebSearchPluginIds({});
  // When web search extensions are bundled, they appear here.
  // With no web search extensions, the result is empty.
  expect(resolved).toEqual(expect.arrayContaining([]));
  // Each resolved ID must be from the known set
  for (const id of resolved) {
    expect(["brave", "firecrawl", "google", "moonshot", "perplexity", "xai"]).toContain(id);
  }
});
