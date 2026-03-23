/**
 * Browser-control security audit findings.
 *
 * Checks CDP URL exposure, auth gaps, and SSRF policy for remote browser
 * control profiles.
 */
import { redactCdpUrl } from "../browser/cdp.helpers.js";
import { resolveBrowserConfig, resolveProfile } from "../browser/config.js";
import { resolveBrowserControlAuth } from "../browser/control-auth.js";
import { formatCliCommand } from "../cli/command-format.js";
import type { DenebConfig } from "../config/config.js";
import { resolveConfigPath } from "../config/paths.js";
import { hasConfiguredSecretInput } from "../config/types.secrets.js";
import { isBlockedHostnameOrIp, isPrivateNetworkAllowedByPolicy } from "../infra/net/ssrf.js";
import { hasNonEmptyString } from "./audit.helpers.js";
import type { SecurityAuditFinding } from "./audit.types.js";

export function collectBrowserControlFindings(
  cfg: DenebConfig,
  env: NodeJS.ProcessEnv,
): SecurityAuditFinding[] {
  const findings: SecurityAuditFinding[] = [];

  let resolved: ReturnType<typeof resolveBrowserConfig>;
  try {
    resolved = resolveBrowserConfig(cfg.browser, cfg);
  } catch (err) {
    findings.push({
      checkId: "browser.control_invalid_config",
      severity: "warn",
      title: "Browser control config looks invalid",
      detail: String(err),
      remediation: `Fix browser.cdpUrl in ${resolveConfigPath()} and re-run "${formatCliCommand("deneb security audit --deep")}".`,
    });
    return findings;
  }

  if (!resolved.enabled) {
    return findings;
  }

  const browserAuth = resolveBrowserControlAuth(cfg, env);
  const explicitAuthMode = cfg.gateway?.auth?.mode;
  const tokenConfigured =
    Boolean(browserAuth.token) ||
    hasNonEmptyString(env.DENEB_GATEWAY_TOKEN) ||
    hasNonEmptyString(env.CLAWDBOT_GATEWAY_TOKEN) ||
    hasConfiguredSecretInput(cfg.gateway?.auth?.token, cfg.secrets?.defaults);
  const passwordCanWin =
    explicitAuthMode === "password" ||
    (explicitAuthMode !== "token" &&
      explicitAuthMode !== "none" &&
      explicitAuthMode !== "trusted-proxy" &&
      !tokenConfigured);
  const passwordConfigured =
    Boolean(browserAuth.password) ||
    (passwordCanWin &&
      (hasNonEmptyString(env.DENEB_GATEWAY_PASSWORD) ||
        hasNonEmptyString(env.CLAWDBOT_GATEWAY_PASSWORD) ||
        hasConfiguredSecretInput(cfg.gateway?.auth?.password, cfg.secrets?.defaults)));
  if (!tokenConfigured && !passwordConfigured) {
    findings.push({
      checkId: "browser.control_no_auth",
      severity: "critical",
      title: "Browser control has no auth",
      detail:
        "Browser control HTTP routes are enabled but no gateway.auth token/password is configured. " +
        "Any local process (or SSRF to loopback) can call browser control endpoints.",
      remediation:
        "Set gateway.auth.token (recommended) or gateway.auth.password so browser control HTTP routes require authentication. Restarting the gateway will auto-generate gateway.auth.token when browser control is enabled.",
    });
  }

  for (const name of Object.keys(resolved.profiles)) {
    const profile = resolveProfile(resolved, name);
    if (!profile || profile.cdpIsLoopback) {
      continue;
    }
    let url: URL;
    try {
      url = new URL(profile.cdpUrl);
    } catch {
      continue;
    }
    const redactedCdpUrl = redactCdpUrl(profile.cdpUrl) ?? profile.cdpUrl;
    if (url.protocol === "http:") {
      findings.push({
        checkId: "browser.remote_cdp_http",
        severity: "warn",
        title: "Remote CDP uses HTTP",
        detail: `browser profile "${name}" uses http CDP (${redactedCdpUrl}); this is OK only if it's tailnet-only or behind an encrypted tunnel.`,
        remediation: `Prefer HTTPS/TLS or a tailnet-only endpoint for remote CDP.`,
      });
    }
    if (
      isPrivateNetworkAllowedByPolicy(resolved.ssrfPolicy) &&
      isBlockedHostnameOrIp(url.hostname)
    ) {
      findings.push({
        checkId: "browser.remote_cdp_private_host",
        severity: "warn",
        title: "Remote CDP targets a private/internal host",
        detail:
          `browser profile "${name}" points at a private/internal CDP host (${redactedCdpUrl}). ` +
          "This is expected for LAN/tailnet/WSL-style setups, but treat it as a trusted-network endpoint.",
        remediation:
          "Prefer a tailnet or tunnel for remote CDP. If you want strict blocking, set browser.ssrfPolicy.dangerouslyAllowPrivateNetwork=false and allow only explicit hosts.",
      });
    }
  }

  return findings;
}
