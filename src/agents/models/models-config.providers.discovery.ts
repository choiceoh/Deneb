import type { DenebConfig } from "../../config/config.js";
import type { ModelDefinitionConfig } from "../../config/types.models.js";
import { createSubsystemLogger } from "../../logging/subsystem.js";
import {
  SELF_HOSTED_DEFAULT_CONTEXT_WINDOW,
  SELF_HOSTED_DEFAULT_COST,
  SELF_HOSTED_DEFAULT_MAX_TOKENS,
} from "../self-hosted-provider-defaults.js";

// Provider catalog re-exports removed — extensions no longer bundled in-tree.

type ModelsConfig = NonNullable<DenebConfig["models"]>;
type _ProviderConfig = NonNullable<ModelsConfig["providers"]>[string];

const log = createSubsystemLogger("agents/model-providers");

type OpenAICompatModelsResponse = {
  data?: Array<{
    id?: string;
  }>;
};

function isReasoningModelHeuristic(modelId: string): boolean {
  return /r1|reasoning|think|reason/i.test(modelId);
}

export async function discoverOpenAICompatibleLocalModels(params: {
  baseUrl: string;
  apiKey?: string;
  label: string;
  contextWindow?: number;
  maxTokens?: number;
}): Promise<ModelDefinitionConfig[]> {
  if (process.env.VITEST || process.env.NODE_ENV === "test") {
    return [];
  }

  const trimmedBaseUrl = params.baseUrl.trim().replace(/\/+$/, "");
  const url = `${trimmedBaseUrl}/models`;

  try {
    const trimmedApiKey = params.apiKey?.trim();
    const response = await fetch(url, {
      headers: trimmedApiKey ? { Authorization: `Bearer ${trimmedApiKey}` } : undefined,
      signal: AbortSignal.timeout(5000),
    });
    if (!response.ok) {
      log.warn(`Failed to discover ${params.label} models: ${response.status}`);
      return [];
    }
    const data = (await response.json()) as OpenAICompatModelsResponse;
    const models = data.data ?? [];
    if (models.length === 0) {
      log.warn(`No ${params.label} models found on local instance`);
      return [];
    }

    return models
      .map((model) => ({ id: typeof model.id === "string" ? model.id.trim() : "" }))
      .filter((model) => Boolean(model.id))
      .map((model) => {
        const modelId = model.id;
        return {
          id: modelId,
          name: modelId,
          reasoning: isReasoningModelHeuristic(modelId),
          input: ["text"],
          cost: SELF_HOSTED_DEFAULT_COST,
          contextWindow: params.contextWindow ?? SELF_HOSTED_DEFAULT_CONTEXT_WINDOW,
          maxTokens: params.maxTokens ?? SELF_HOSTED_DEFAULT_MAX_TOKENS,
        } satisfies ModelDefinitionConfig;
      });
  } catch (error) {
    log.warn(`Failed to discover ${params.label} models: ${String(error)}`);
    return [];
  }
}
