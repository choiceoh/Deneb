import type { DenebConfig } from "../config/config.js";
/**
 * Gateway security audit collectors.
 *
 * Checks: hooks hardening, HTTP session-key override, HTTP no-auth.
 */
import { resolveGatewayAuth } from "../gateway/auth/auth.js";
import { resolveAllowedAgentIds } from "../gateway/hooks-policy.js";
import type { SecurityAuditFinding } from "./audit-extra-shared.js";
import { isGatewayRemotelyExposed } from "./audit-extra.sync.helpers.js";

export function collectHooksHardeningFindings(
  cfg: DenebConfig,
  env: NodeJS.ProcessEnv = process.env,
): SecurityAuditFinding[] {
  const findings: SecurityAuditFinding[] = [];
  if (cfg.hooks?.enabled !== true) {
    return findings;
  }

  const token = typeof cfg.hooks?.token === "string" ? cfg.hooks.token.trim() : "";
  if (token && token.length < 24) {
    findings.push({
      checkId: "hooks.token_too_short",
      severity: "warn",
      title: "Hooks token looks short",
      detail: `hooks.token is ${token.length} chars; prefer a long random token.`,
    });
  }

  const gatewayAuth = resolveGatewayAuth({
    authConfig: cfg.gateway?.auth,
    tailscaleMode: cfg.gateway?.tailscale?.mode ?? "off",
    env,
  });
  const denebGatewayToken =
    typeof env.DENEB_GATEWAY_TOKEN === "string" && env.DENEB_GATEWAY_TOKEN.trim()
      ? env.DENEB_GATEWAY_TOKEN.trim()
      : null;
  const gatewayToken =
    gatewayAuth.mode === "token" &&
    typeof gatewayAuth.token === "string" &&
    gatewayAuth.token.trim()
      ? gatewayAuth.token.trim()
      : denebGatewayToken
        ? denebGatewayToken
        : null;
  if (token && gatewayToken && token === gatewayToken) {
    findings.push({
      checkId: "hooks.token_reuse_gateway_token",
      severity: "critical",
      title: "Hooks token reuses the Gateway token",
      detail:
        "hooks.token matches gateway.auth token; compromise of hooks expands blast radius to the Gateway API.",
      remediation: "Use a separate hooks.token dedicated to hook ingress.",
    });
  }

  const rawPath = typeof cfg.hooks?.path === "string" ? cfg.hooks.path.trim() : "";
  if (rawPath === "/") {
    findings.push({
      checkId: "hooks.path_root",
      severity: "critical",
      title: "Hooks base path is '/'",
      detail: "hooks.path='/' would shadow other HTTP endpoints and is unsafe.",
      remediation: "Use a dedicated path like '/hooks'.",
    });
  }

  const allowRequestSessionKey = cfg.hooks?.allowRequestSessionKey === true;
  const defaultSessionKey =
    typeof cfg.hooks?.defaultSessionKey === "string" ? cfg.hooks.defaultSessionKey.trim() : "";
  const allowedAgentIds = resolveAllowedAgentIds(cfg.hooks?.allowedAgentIds);
  const allowedPrefixes = Array.isArray(cfg.hooks?.allowedSessionKeyPrefixes)
    ? cfg.hooks.allowedSessionKeyPrefixes
        .map((prefix) => prefix.trim())
        .filter((prefix) => prefix.length > 0)
    : [];
  const remoteExposure = isGatewayRemotelyExposed(cfg);

  if (!defaultSessionKey) {
    findings.push({
      checkId: "hooks.default_session_key_unset",
      severity: "warn",
      title: "hooks.defaultSessionKey is not configured",
      detail:
        "Hook agent runs without explicit sessionKey use generated per-request keys. Set hooks.defaultSessionKey to keep hook ingress scoped to a known session.",
      remediation: 'Set hooks.defaultSessionKey (for example, "hook:ingress").',
    });
  }

  if (allowedAgentIds === undefined) {
    findings.push({
      checkId: "hooks.allowed_agent_ids_unrestricted",
      severity: remoteExposure ? "critical" : "warn",
      title: "Hook agent routing allows any configured agent",
      detail:
        "hooks.allowedAgentIds is unset or includes '*', so authenticated hook callers may route to any configured agent id.",
      remediation:
        'Set hooks.allowedAgentIds to an explicit allowlist (for example, ["hooks", "main"]) or [] to deny explicit agent routing.',
    });
  }

  if (allowRequestSessionKey) {
    findings.push({
      checkId: "hooks.request_session_key_enabled",
      severity: remoteExposure ? "critical" : "warn",
      title: "External hook payloads may override sessionKey",
      detail:
        "hooks.allowRequestSessionKey=true allows `/hooks/agent` callers to choose the session key. Treat hook token holders as full-trust unless you also restrict prefixes.",
      remediation:
        "Set hooks.allowRequestSessionKey=false (recommended) or constrain hooks.allowedSessionKeyPrefixes.",
    });
  }

  if (allowRequestSessionKey && allowedPrefixes.length === 0) {
    findings.push({
      checkId: "hooks.request_session_key_prefixes_missing",
      severity: remoteExposure ? "critical" : "warn",
      title: "Request sessionKey override is enabled without prefix restrictions",
      detail:
        "hooks.allowRequestSessionKey=true and hooks.allowedSessionKeyPrefixes is unset/empty, so request payloads can target arbitrary session key shapes.",
      remediation:
        'Set hooks.allowedSessionKeyPrefixes (for example, ["hook:"]) or disable request overrides.',
    });
  }

  return findings;
}

export function collectGatewayHttpSessionKeyOverrideFindings(
  cfg: DenebConfig,
): SecurityAuditFinding[] {
  const findings: SecurityAuditFinding[] = [];
  const chatCompletionsEnabled = cfg.gateway?.http?.endpoints?.chatCompletions?.enabled === true;
  const responsesEnabled = cfg.gateway?.http?.endpoints?.responses?.enabled === true;
  if (!chatCompletionsEnabled && !responsesEnabled) {
    return findings;
  }

  const enabledEndpoints = [
    chatCompletionsEnabled ? "/v1/chat/completions" : null,
    responsesEnabled ? "/v1/responses" : null,
  ].filter((entry): entry is string => Boolean(entry));

  findings.push({
    checkId: "gateway.http.session_key_override_enabled",
    severity: "info",
    title: "HTTP API session-key override is enabled",
    detail:
      `${enabledEndpoints.join(", ")} accept x-deneb-session-key for per-request session routing. ` +
      "Treat API credential holders as trusted principals.",
  });

  return findings;
}

export function collectGatewayHttpNoAuthFindings(
  cfg: DenebConfig,
  env: NodeJS.ProcessEnv,
): SecurityAuditFinding[] {
  const findings: SecurityAuditFinding[] = [];
  const tailscaleMode = cfg.gateway?.tailscale?.mode ?? "off";
  const auth = resolveGatewayAuth({ authConfig: cfg.gateway?.auth, tailscaleMode, env });
  if (auth.mode !== "none") {
    return findings;
  }

  const chatCompletionsEnabled = cfg.gateway?.http?.endpoints?.chatCompletions?.enabled === true;
  const responsesEnabled = cfg.gateway?.http?.endpoints?.responses?.enabled === true;
  const enabledEndpoints = [
    "/tools/invoke",
    chatCompletionsEnabled ? "/v1/chat/completions" : null,
    responsesEnabled ? "/v1/responses" : null,
  ].filter((entry): entry is string => Boolean(entry));

  const remoteExposure = isGatewayRemotelyExposed(cfg);
  findings.push({
    checkId: "gateway.http.no_auth",
    severity: remoteExposure ? "critical" : "warn",
    title: "Gateway HTTP APIs are reachable without auth",
    detail:
      `gateway.auth.mode="none" leaves ${enabledEndpoints.join(", ")} callable without a shared secret. ` +
      "Treat this as trusted-local only and avoid exposing the gateway beyond loopback.",
    remediation:
      "Set gateway.auth.mode to token/password (recommended). If you intentionally keep mode=none, keep gateway.bind=loopback and disable optional HTTP endpoints.",
  });

  return findings;
}
