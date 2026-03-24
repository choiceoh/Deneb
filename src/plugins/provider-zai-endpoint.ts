import { fetchWithTimeout } from "../utils/fetch-timeout.js";
import {
  ZAI_CN_BASE_URL,
  ZAI_CODING_CN_BASE_URL,
  ZAI_CODING_GLOBAL_BASE_URL,
  ZAI_GLOBAL_BASE_URL,
} from "./provider-model-definitions.js";

export type ZaiEndpointId = "global" | "cn" | "coding-global" | "coding-cn";

export type ZaiDetectedEndpoint = {
  endpoint: ZaiEndpointId;
  /** Provider baseUrl to store in config. */
  baseUrl: string;
  /** Recommended default model id for that endpoint. */
  modelId: string;
  /** Human-readable note explaining the choice. */
  note: string;
};

/** Detailed detection result that includes failure diagnostics. */
export type ZaiDetectionResult =
  | { ok: true; detected: ZaiDetectedEndpoint }
  | { ok: false; detected: null; failures: ZaiProbeFailure[] };

export type ZaiProbeFailure = {
  endpoint: ZaiEndpointId;
  modelId: string;
  status?: number;
  errorCode?: string;
  errorMessage?: string;
};

type ProbeResult =
  | { ok: true }
  | {
      ok: false;
      status?: number;
      errorCode?: string;
      errorMessage?: string;
    };

async function probeZaiChatCompletions(params: {
  baseUrl: string;
  apiKey: string;
  modelId: string;
  timeoutMs: number;
  fetchFn?: typeof fetch;
}): Promise<ProbeResult> {
  try {
    const res = await fetchWithTimeout(
      `${params.baseUrl}/chat/completions`,
      {
        method: "POST",
        headers: {
          authorization: `Bearer ${params.apiKey}`,
          "content-type": "application/json",
        },
        body: JSON.stringify({
          model: params.modelId,
          stream: false,
          max_tokens: 1,
          messages: [{ role: "user", content: "ping" }],
        }),
      },
      params.timeoutMs,
      params.fetchFn,
    );

    if (res.ok) {
      return { ok: true };
    }

    let errorCode: string | undefined;
    let errorMessage: string | undefined;
    try {
      const json = (await res.json()) as {
        error?: { code?: unknown; message?: unknown };
        msg?: unknown;
        message?: unknown;
      };
      const code = json?.error?.code;
      const msg = json?.error?.message ?? json?.msg ?? json?.message;
      if (typeof code === "string") {
        errorCode = code;
      } else if (typeof code === "number") {
        errorCode = String(code);
      }
      if (typeof msg === "string") {
        errorMessage = msg;
      }
    } catch {
      // ignore
    }

    return { ok: false, status: res.status, errorCode, errorMessage };
  } catch {
    return { ok: false };
  }
}

type ProbeCandidate = {
  endpoint: ZaiEndpointId;
  baseUrl: string;
  modelId: string;
  note: string;
};

function buildProbeCandidates(endpoint?: ZaiEndpointId): ProbeCandidate[] {
  const general: ProbeCandidate[] = [
    {
      endpoint: "global",
      baseUrl: ZAI_GLOBAL_BASE_URL,
      modelId: "glm-5",
      note: "Verified GLM-5 on global endpoint.",
    },
    {
      endpoint: "cn",
      baseUrl: ZAI_CN_BASE_URL,
      modelId: "glm-5",
      note: "Verified GLM-5 on cn endpoint.",
    },
  ];
  const codingGlm5: ProbeCandidate[] = [
    {
      endpoint: "coding-global",
      baseUrl: ZAI_CODING_GLOBAL_BASE_URL,
      modelId: "glm-5",
      note: "Verified GLM-5 on coding-global endpoint.",
    },
    {
      endpoint: "coding-cn",
      baseUrl: ZAI_CODING_CN_BASE_URL,
      modelId: "glm-5",
      note: "Verified GLM-5 on coding-cn endpoint.",
    },
  ];
  const codingFallback: ProbeCandidate[] = [
    {
      endpoint: "coding-global",
      baseUrl: ZAI_CODING_GLOBAL_BASE_URL,
      modelId: "glm-4.7",
      note: "Coding Plan endpoint verified, but this key/plan does not expose GLM-5 there. Defaulting to GLM-4.7.",
    },
    {
      endpoint: "coding-cn",
      baseUrl: ZAI_CODING_CN_BASE_URL,
      modelId: "glm-4.7",
      note: "Coding Plan CN endpoint verified, but this key/plan does not expose GLM-5 there. Defaulting to GLM-4.7.",
    },
  ];

  switch (endpoint) {
    case "global":
      return general.filter((c) => c.endpoint === "global");
    case "cn":
      return general.filter((c) => c.endpoint === "cn");
    case "coding-global":
      return [
        ...codingGlm5.filter((c) => c.endpoint === "coding-global"),
        ...codingFallback.filter((c) => c.endpoint === "coding-global"),
      ];
    case "coding-cn":
      return [
        ...codingGlm5.filter((c) => c.endpoint === "coding-cn"),
        ...codingFallback.filter((c) => c.endpoint === "coding-cn"),
      ];
    default:
      return [...general, ...codingGlm5, ...codingFallback];
  }
}

/**
 * Probe all candidates in parallel and return the first success immediately.
 * Remaining in-flight probes are left to settle naturally (no abort) so we
 * collect their failure diagnostics without blocking on them.
 *
 * Candidate priority is preserved: when multiple probes succeed concurrently,
 * the one with the lowest original index wins.
 */
async function probeAllParallel(
  candidates: ProbeCandidate[],
  params: { apiKey: string; timeoutMs: number; fetchFn?: typeof fetch },
): Promise<ZaiDetectionResult> {
  if (candidates.length === 0) {
    return { ok: false, detected: null, failures: [] };
  }

  type Settled = { candidate: ProbeCandidate; result: ProbeResult; index: number };

  const probes = candidates.map(async (candidate, index) => {
    const result = await probeZaiChatCompletions({
      baseUrl: candidate.baseUrl,
      apiKey: params.apiKey,
      modelId: candidate.modelId,
      timeoutMs: params.timeoutMs,
      fetchFn: params.fetchFn,
    });
    return { candidate, result, index };
  });

  // Race: resolve as soon as any probe succeeds, preserving candidate priority.
  const settled = await Promise.allSettled(probes);
  const results: Settled[] = [];
  for (const s of settled) {
    if (s.status === "fulfilled") {
      results.push(s.value);
    }
  }

  // Sort by original candidate index so higher-priority endpoints win ties.
  results.sort((a, b) => a.index - b.index);

  const failures: ZaiProbeFailure[] = [];
  for (const { candidate, result } of results) {
    if (result.ok) {
      return { ok: true as const, detected: candidate };
    }
    failures.push({
      endpoint: candidate.endpoint,
      modelId: candidate.modelId,
      ...(!result.ok
        ? {
            status: result.status,
            errorCode: result.errorCode,
            errorMessage: result.errorMessage,
          }
        : {}),
    });
  }

  return { ok: false, detected: null, failures };
}

/**
 * In-memory cache for successful endpoint detection results.
 * Keyed by `${apiKeyPrefix}:${endpoint ?? "auto"}` to avoid re-probing
 * when the same key and endpoint hint are used repeatedly.
 */
const detectionCache = new Map<string, { result: ZaiDetectionResult; expiresAt: number }>();

/** Cache TTL: 5 minutes. */
const DETECTION_CACHE_TTL_MS = 5 * 60_000;

function cacheKey(apiKey: string, endpoint: ZaiEndpointId | undefined): string {
  // Use a short prefix of the API key for the cache key to avoid storing full secrets.
  const prefix = apiKey.length > 8 ? apiKey.slice(0, 4) + apiKey.slice(-4) : apiKey;
  return `${prefix}:${endpoint ?? "auto"}`;
}

/** Clear the endpoint detection cache (useful for tests and re-onboarding). */
export function clearZaiEndpointCache(): void {
  detectionCache.clear();
}

export async function detectZaiEndpoint(params: {
  apiKey: string;
  endpoint?: ZaiEndpointId;
  timeoutMs?: number;
  fetchFn?: typeof fetch;
}): Promise<ZaiDetectedEndpoint | null> {
  const result = await detectZaiEndpointDetailed(params);
  return result.detected;
}

/**
 * Like `detectZaiEndpoint` but returns structured failure diagnostics so
 * callers can display actionable error messages when detection fails.
 */
export async function detectZaiEndpointDetailed(params: {
  apiKey: string;
  endpoint?: ZaiEndpointId;
  timeoutMs?: number;
  fetchFn?: typeof fetch;
}): Promise<ZaiDetectionResult> {
  // Never auto-probe in vitest; it would create flaky network behavior.
  if (process.env.VITEST && !params.fetchFn) {
    return { ok: false, detected: null, failures: [] };
  }

  // Check the in-memory cache first.
  const key = cacheKey(params.apiKey, params.endpoint);
  const cached = detectionCache.get(key);
  if (cached && cached.expiresAt > Date.now()) {
    return cached.result;
  }

  const timeoutMs = params.timeoutMs ?? 5_000;
  const candidates = buildProbeCandidates(params.endpoint);
  const result = await probeAllParallel(candidates, {
    apiKey: params.apiKey,
    timeoutMs,
    fetchFn: params.fetchFn,
  });

  // Only cache successful detections to allow retries on transient failures.
  if (result.ok) {
    detectionCache.set(key, { result, expiresAt: Date.now() + DETECTION_CACHE_TTL_MS });
  }

  return result;
}

/**
 * Format detection failures into a human-readable diagnostic string.
 */
export function formatZaiDetectionFailures(failures: ZaiProbeFailure[]): string {
  if (failures.length === 0) {
    return "No endpoints were probed.";
  }

  const authFailures = failures.filter((f) => f.status === 401 || f.status === 403);
  if (authFailures.length === failures.length) {
    return "API key was rejected by all endpoints (HTTP 401/403). Verify your Z.AI API key is valid.";
  }

  const lines = failures.map((f) => {
    const parts = [`  ${f.endpoint}/${f.modelId}`];
    if (f.status) {
      parts.push(`HTTP ${f.status}`);
    }
    if (f.errorMessage) {
      parts.push(f.errorMessage);
    } else if (!f.status) {
      parts.push("network error or timeout");
    }
    return parts.join(" — ");
  });
  return `Endpoint detection failed:\n${lines.join("\n")}`;
}
