export const MODELS_AUTH_HELP: Record<string, string> = {
  models:
    "Model catalog root for provider definitions, merge/replace behavior, and optional Bedrock discovery integration. Keep provider definitions explicit and validated before relying on production failover paths.",
  "models.mode":
    'Controls provider catalog behavior: "merge" keeps built-ins and overlays your custom providers, while "replace" uses only your configured providers. In "merge", matching provider IDs preserve non-empty agent models.json baseUrl values, while apiKey values are preserved only when the provider is not SecretRef-managed in current config/auth-profile context; SecretRef-managed providers refresh apiKey from current source markers, and matching model contextWindow/maxTokens use the higher value between explicit and implicit entries.',
  "models.providers":
    "Provider map keyed by provider ID containing connection/auth settings and concrete model definitions. Use stable provider keys so references from agents and tooling remain portable across environments.",
  "models.providers.*.baseUrl":
    "Base URL for the provider endpoint used to serve model requests for that provider entry. Use HTTPS endpoints and keep URLs environment-specific through config templating where needed.",
  "models.providers.*.apiKey":
    "Provider credential used for API-key based authentication when the provider requires direct key auth. Use secret/env substitution and avoid storing real keys in committed config files.",
  "models.providers.*.auth":
    'Selects provider auth style: "api-key" for API key auth, "token" for bearer token auth, "oauth" for OAuth credentials, and "aws-sdk" for AWS credential resolution. Match this to your provider requirements.',
  "models.providers.*.api":
    "Provider API adapter selection controlling request/response compatibility handling for model calls. Use the adapter that matches your upstream provider protocol to avoid feature mismatch.",
  "models.providers.*.injectNumCtxForOpenAICompat":
    "Controls whether Deneb injects `options.num_ctx` for Ollama providers configured with the OpenAI-compatible adapter (`openai-completions`). Default is true. Set false only if your proxy/upstream rejects unknown `options` payload fields.",
  "models.providers.*.headers":
    "Static HTTP headers merged into provider requests for tenant routing, proxy auth, or custom gateway requirements. Use this sparingly and keep sensitive header values in secrets.",
  "models.providers.*.authHeader":
    "When true, credentials are sent via the HTTP Authorization header even if alternate auth is possible. Use this only when your provider or proxy explicitly requires Authorization forwarding.",
  "models.providers.*.models":
    "Declared model list for a provider including identifiers, metadata, and optional compatibility/cost hints. Keep IDs exact to provider catalog values so selection and fallback resolve correctly.",
  "models.bedrockDiscovery":
    "Automatic AWS Bedrock model discovery settings used to synthesize provider model entries from account visibility. Keep discovery scoped and refresh intervals conservative to reduce API churn.",
  "models.bedrockDiscovery.enabled":
    "Enables periodic Bedrock model discovery and catalog refresh for Bedrock-backed providers. Keep disabled unless Bedrock is actively used and IAM permissions are correctly configured.",
  "models.bedrockDiscovery.region":
    "AWS region used for Bedrock discovery calls when discovery is enabled for your deployment. Use the region where your Bedrock models are provisioned to avoid empty discovery results.",
  "models.bedrockDiscovery.providerFilter":
    "Optional provider allowlist filter for Bedrock discovery so only selected providers are refreshed. Use this to limit discovery scope in multi-provider environments.",
  "models.bedrockDiscovery.refreshInterval":
    "Refresh cadence for Bedrock discovery polling in seconds to detect newly available models over time. Use longer intervals in production to reduce API cost and control-plane noise.",
  "models.bedrockDiscovery.defaultContextWindow":
    "Fallback context-window value applied to discovered models when provider metadata lacks explicit limits. Use realistic defaults to avoid oversized prompts that exceed true provider constraints.",
  "models.bedrockDiscovery.defaultMaxTokens":
    "Fallback max-token value applied to discovered models without explicit output token limits. Use conservative defaults to reduce truncation surprises and unexpected token spend.",
  auth: "Authentication profile root used for multi-profile provider credentials and cooldown-based failover ordering. Keep profiles minimal and explicit so automatic failover behavior stays auditable.",
  "auth.profiles": "Named auth profiles (provider + mode + optional email).",
  "auth.order": "Ordered auth profile IDs per provider (used for automatic failover).",
  "auth.cooldowns":
    "Cooldown/backoff controls for temporary profile suppression after billing-related failures and retry windows. Use these to prevent rapid re-selection of profiles that are still blocked.",
  "auth.cooldowns.billingBackoffHours":
    "Base backoff (hours) when a profile fails due to billing/insufficient credits (default: 5).",
  "auth.cooldowns.billingBackoffHoursByProvider":
    "Optional per-provider overrides for billing backoff (hours).",
  "auth.cooldowns.billingMaxHours": "Cap (hours) for billing backoff (default: 24).",
  "auth.cooldowns.failureWindowHours": "Failure window (hours) for backoff counters (default: 24).",
};
