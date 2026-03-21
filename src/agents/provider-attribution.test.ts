import { describe, expect, it } from "vitest";
import {
  listProviderAttributionPolicies,
  resolveProviderAttributionHeaders,
  resolveProviderAttributionIdentity,
  resolveProviderAttributionPolicy,
} from "./provider-attribution.js";

describe("provider attribution", () => {
  it("resolves the canonical Deneb product and runtime version", () => {
    const identity = resolveProviderAttributionIdentity({
      DENEB_VERSION: "2026.3.99",
    });

    expect(identity).toEqual({
      product: "Deneb",
      version: "2026.3.99",
    });
  });

  it("returns a documented OpenRouter attribution policy", () => {
    const policy = resolveProviderAttributionPolicy("openrouter", {
      DENEB_VERSION: "2026.3.14",
    });

    expect(policy).toEqual({
      provider: "openrouter",
      enabledByDefault: true,
      verification: "vendor-documented",
      hook: "request-headers",
      docsUrl: "https://openrouter.ai/docs/app-attribution",
      reviewNote: "Documented app attribution headers. Verified in Deneb runtime wrapper.",
      product: "Deneb",
      version: "2026.3.14",
      headers: {
        "HTTP-Referer": "https://deneb.ai",
        "X-OpenRouter-Title": "Deneb",
        "X-OpenRouter-Categories": "cli-agent",
      },
    });
  });

  it("normalizes aliases when resolving provider headers", () => {
    expect(
      resolveProviderAttributionHeaders("OpenRouter", {
        DENEB_VERSION: "2026.3.14",
      }),
    ).toEqual({
      "HTTP-Referer": "https://deneb.ai",
      "X-OpenRouter-Title": "Deneb",
      "X-OpenRouter-Categories": "cli-agent",
    });
  });

  it("returns a hidden-spec OpenAI attribution policy", () => {
    expect(resolveProviderAttributionPolicy("openai", { DENEB_VERSION: "2026.3.14" })).toEqual({
      provider: "openai",
      enabledByDefault: true,
      verification: "vendor-hidden-api-spec",
      hook: "request-headers",
      reviewNote:
        "OpenAI native traffic supports hidden originator/User-Agent attribution. Verified against the Codex wire contract.",
      product: "Deneb",
      version: "2026.3.14",
      headers: {
        originator: "deneb",
        "User-Agent": "deneb/2026.3.14",
      },
    });
    expect(resolveProviderAttributionHeaders("openai", { DENEB_VERSION: "2026.3.14" })).toEqual({
      originator: "deneb",
      "User-Agent": "deneb/2026.3.14",
    });
  });

  it("returns a hidden-spec OpenAI Codex attribution policy", () => {
    expect(
      resolveProviderAttributionPolicy("openai-codex", { DENEB_VERSION: "2026.3.14" }),
    ).toEqual({
      provider: "openai-codex",
      enabledByDefault: true,
      verification: "vendor-hidden-api-spec",
      hook: "request-headers",
      reviewNote:
        "OpenAI Codex ChatGPT-backed traffic supports the same hidden originator/User-Agent attribution contract.",
      product: "Deneb",
      version: "2026.3.14",
      headers: {
        originator: "deneb",
        "User-Agent": "deneb/2026.3.14",
      },
    });
  });

  it("lists the current attribution support matrix", () => {
    expect(
      listProviderAttributionPolicies({ DENEB_VERSION: "2026.3.14" }).map((policy) => [
        policy.provider,
        policy.enabledByDefault,
        policy.verification,
        policy.hook,
      ]),
    ).toEqual([
      ["openrouter", true, "vendor-documented", "request-headers"],
      ["openai", true, "vendor-hidden-api-spec", "request-headers"],
      ["openai-codex", true, "vendor-hidden-api-spec", "request-headers"],
      ["anthropic", false, "vendor-sdk-hook-only", "default-headers"],
      ["google", false, "vendor-sdk-hook-only", "user-agent-extra"],
      ["groq", false, "vendor-sdk-hook-only", "default-headers"],
      ["mistral", false, "vendor-sdk-hook-only", "custom-user-agent"],
      ["together", false, "vendor-sdk-hook-only", "default-headers"],
    ]);
  });
});
