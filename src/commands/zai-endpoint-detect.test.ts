import { describe, expect, it } from "vitest";
import {
  detectZaiEndpoint,
  detectZaiEndpointDetailed,
  formatZaiDetectionFailures,
} from "./zai-endpoint-detect.js";

type FetchResponse = { status: number; body?: unknown };

function makeFetch(map: Record<string, FetchResponse>) {
  return (async (url: string, init?: RequestInit) => {
    const rawBody = typeof init?.body === "string" ? JSON.parse(init.body) : null;
    const entry = map[`${url}::${rawBody?.model ?? ""}`] ?? map[url];
    if (!entry) {
      throw new Error(`unexpected url: ${url} model=${String(rawBody?.model ?? "")}`);
    }
    const json = entry.body ?? {};
    return new Response(JSON.stringify(json), {
      status: entry.status,
      headers: { "content-type": "application/json" },
    });
  }) as typeof fetch;
}

describe("detectZaiEndpoint", () => {
  it("resolves preferred/fallback endpoints and null when probes fail", async () => {
    const scenarios: Array<{
      endpoint?: "global" | "cn" | "coding-global" | "coding-cn";
      responses: Record<string, { status: number; body?: unknown }>;
      expected: { endpoint: string; modelId: string } | null;
    }> = [
      {
        responses: {
          "https://api.z.ai/api/paas/v4/chat/completions::glm-5": { status: 200 },
        },
        expected: { endpoint: "global", modelId: "glm-5" },
      },
      {
        responses: {
          "https://api.z.ai/api/paas/v4/chat/completions::glm-5": {
            status: 404,
            body: { error: { message: "not found" } },
          },
          "https://open.bigmodel.cn/api/paas/v4/chat/completions::glm-5": { status: 200 },
        },
        expected: { endpoint: "cn", modelId: "glm-5" },
      },
      {
        responses: {
          "https://api.z.ai/api/paas/v4/chat/completions::glm-5": { status: 404 },
          "https://open.bigmodel.cn/api/paas/v4/chat/completions::glm-5": { status: 404 },
          "https://api.z.ai/api/coding/paas/v4/chat/completions::glm-5": { status: 200 },
        },
        expected: { endpoint: "coding-global", modelId: "glm-5" },
      },
      {
        endpoint: "coding-global",
        responses: {
          "https://api.z.ai/api/coding/paas/v4/chat/completions::glm-5": {
            status: 404,
            body: { error: { message: "glm-5 unavailable" } },
          },
          "https://api.z.ai/api/coding/paas/v4/chat/completions::glm-4.7": { status: 200 },
        },
        expected: { endpoint: "coding-global", modelId: "glm-4.7" },
      },
      {
        endpoint: "coding-cn",
        responses: {
          "https://open.bigmodel.cn/api/coding/paas/v4/chat/completions::glm-5": { status: 200 },
        },
        expected: { endpoint: "coding-cn", modelId: "glm-5" },
      },
      {
        endpoint: "coding-cn",
        responses: {
          "https://open.bigmodel.cn/api/coding/paas/v4/chat/completions::glm-5": {
            status: 404,
            body: { error: { message: "glm-5 unavailable" } },
          },
          "https://open.bigmodel.cn/api/coding/paas/v4/chat/completions::glm-4.7": { status: 200 },
        },
        expected: { endpoint: "coding-cn", modelId: "glm-4.7" },
      },
      {
        responses: {
          "https://api.z.ai/api/paas/v4/chat/completions::glm-5": { status: 401 },
          "https://open.bigmodel.cn/api/paas/v4/chat/completions::glm-5": { status: 401 },
          "https://api.z.ai/api/coding/paas/v4/chat/completions::glm-5": { status: 401 },
          "https://api.z.ai/api/coding/paas/v4/chat/completions::glm-4.7": { status: 401 },
          "https://open.bigmodel.cn/api/coding/paas/v4/chat/completions::glm-5": { status: 401 },
          "https://open.bigmodel.cn/api/coding/paas/v4/chat/completions::glm-4.7": { status: 401 },
        },
        expected: null,
      },
    ];

    for (const scenario of scenarios) {
      const detected = await detectZaiEndpoint({
        apiKey: "sk-test", // pragma: allowlist secret
        ...(scenario.endpoint ? { endpoint: scenario.endpoint } : {}),
        fetchFn: makeFetch(scenario.responses),
      });

      if (scenario.expected === null) {
        expect(detected).toBeNull();
      } else {
        expect(detected?.endpoint).toBe(scenario.expected.endpoint);
        expect(detected?.modelId).toBe(scenario.expected.modelId);
      }
    }
  });
});

describe("detectZaiEndpointDetailed", () => {
  it("returns structured failure diagnostics when all probes fail", async () => {
    const result = await detectZaiEndpointDetailed({
      apiKey: "sk-bad-key", // pragma: allowlist secret
      endpoint: "coding-global",
      fetchFn: makeFetch({
        "https://api.z.ai/api/coding/paas/v4/chat/completions::glm-5": {
          status: 401,
          body: { error: { code: "invalid_api_key", message: "Invalid API key" } },
        },
        "https://api.z.ai/api/coding/paas/v4/chat/completions::glm-4.7": {
          status: 401,
          body: { error: { code: "invalid_api_key", message: "Invalid API key" } },
        },
      }),
    });

    expect(result.ok).toBe(false);
    expect(result.detected).toBeNull();
    if (!result.ok) {
      expect(result.failures).toHaveLength(2);
      expect(result.failures[0]).toMatchObject({
        endpoint: "coding-global",
        modelId: "glm-5",
        status: 401,
        errorCode: "invalid_api_key",
        errorMessage: "Invalid API key",
      });
      expect(result.failures[1]).toMatchObject({
        endpoint: "coding-global",
        modelId: "glm-4.7",
        status: 401,
      });
    }
  });

  it("returns success with detected endpoint", async () => {
    const result = await detectZaiEndpointDetailed({
      apiKey: "sk-good-key", // pragma: allowlist secret
      endpoint: "global",
      fetchFn: makeFetch({
        "https://api.z.ai/api/paas/v4/chat/completions::glm-5": { status: 200 },
      }),
    });

    expect(result.ok).toBe(true);
    expect(result.detected).toMatchObject({
      endpoint: "global",
      modelId: "glm-5",
    });
  });
});

describe("formatZaiDetectionFailures", () => {
  it("formats auth failures with actionable message", () => {
    const formatted = formatZaiDetectionFailures([
      { endpoint: "global", modelId: "glm-5", status: 401 },
      { endpoint: "cn", modelId: "glm-5", status: 403 },
    ]);
    expect(formatted).toContain("API key was rejected");
    expect(formatted).toContain("401/403");
  });

  it("formats mixed failures with per-endpoint details", () => {
    const formatted = formatZaiDetectionFailures([
      { endpoint: "global", modelId: "glm-5", status: 404, errorMessage: "model not found" },
      { endpoint: "cn", modelId: "glm-5" },
    ]);
    expect(formatted).toContain("global/glm-5");
    expect(formatted).toContain("HTTP 404");
    expect(formatted).toContain("model not found");
    expect(formatted).toContain("network error or timeout");
  });

  it("handles empty failures list", () => {
    expect(formatZaiDetectionFailures([])).toContain("No endpoints were probed");
  });
});
