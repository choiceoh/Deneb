import { parseTimeoutMsWithFallback } from "../cli/parse-timeout.js";
import { withProgress } from "../cli/progress.js";
import { readBestEffortConfig, resolveGatewayPort } from "../config/config.js";
import { probeGateway } from "../gateway/monitoring/probe.js";
import type { RuntimeEnv } from "../runtime.js";
import { colorize, isRich, theme } from "../terminal/theme.js";
import {
  buildNetworkHints,
  extractConfigSummary,
  isProbeReachable,
  isScopeLimitedProbeFailure,
  type GatewayStatusTarget,
  pickGatewaySelfPresence,
  renderProbeSummaryLine,
  renderTargetHeader,
  resolveAuthForTarget,
  resolveProbeBudgetMs,
  resolveTargets,
  sanitizeSshTarget,
} from "./gateway-status/helpers.js";

let sshConfigModulePromise: Promise<typeof import("../infra/ssh-config.js")> | undefined;
let sshTunnelModulePromise: Promise<typeof import("../infra/ssh-tunnel.js")> | undefined;

function loadSshConfigModule() {
  sshConfigModulePromise ??= import("../infra/ssh-config.js");
  return sshConfigModulePromise;
}

function loadSshTunnelModule() {
  sshTunnelModulePromise ??= import("../infra/ssh-tunnel.js");
  return sshTunnelModulePromise;
}

export async function gatewayStatusCommand(
  opts: {
    url?: string;
    token?: string;
    password?: string;
    timeout?: unknown;
    json?: boolean;
    ssh?: string;
    sshIdentity?: string;
    sshAuto?: boolean;
  },
  runtime: RuntimeEnv,
) {
  const startedAt = Date.now();
  const cfg = await readBestEffortConfig();
  const rich = isRich() && opts.json !== true;
  const overallTimeoutMs = parseTimeoutMsWithFallback(opts.timeout, 3000);

  const baseTargets = resolveTargets(cfg, opts.url);
  const network = buildNetworkHints(cfg);

  let sshTarget = sanitizeSshTarget(opts.ssh) ?? sanitizeSshTarget(cfg.gateway?.remote?.sshTarget);
  let sshIdentity =
    sanitizeSshTarget(opts.sshIdentity) ?? sanitizeSshTarget(cfg.gateway?.remote?.sshIdentity);
  const remotePort = resolveGatewayPort(cfg);

  let sshTunnelError: string | null = null;
  let sshTunnelStarted = false;

  if (!sshTarget) {
    sshTarget = inferSshTargetFromRemoteUrl(cfg.gateway?.remote?.url);
  }

  if (sshTarget) {
    const resolved = await resolveSshTarget(sshTarget, sshIdentity, overallTimeoutMs);
    if (resolved) {
      sshTarget = resolved.target;
      if (!sshIdentity && resolved.identity) {
        sshIdentity = resolved.identity;
      }
    }
  }

  const { probed } = await withProgress(
    {
      label: "Inspecting gateways…",
      indeterminate: true,
      enabled: opts.json !== true,
    },
    async () => {
      const tryStartTunnel = async () => {
        if (!sshTarget) {
          return null;
        }
        try {
          const { startSshPortForward } = await loadSshTunnelModule();
          const tunnel = await startSshPortForward({
            target: sshTarget,
            identity: sshIdentity ?? undefined,
            localPortPreferred: remotePort,
            remotePort,
            timeoutMs: Math.min(1500, overallTimeoutMs),
          });
          sshTunnelStarted = true;
          return tunnel;
        } catch (err) {
          sshTunnelError = err instanceof Error ? err.message : String(err);
          return null;
        }
      };

      const tunnelFirst = sshTarget ? await tryStartTunnel() : null;

      const tunnel =
        tunnelFirst ||
        (sshTarget && !sshTunnelStarted && !sshTunnelError ? await tryStartTunnel() : null);

      const tunnelTarget: GatewayStatusTarget | null = tunnel
        ? {
            id: "sshTunnel",
            kind: "sshTunnel",
            url: `ws://127.0.0.1:${tunnel.localPort}`,
            active: true,
            tunnel: {
              kind: "ssh",
              target: sshTarget ?? "",
              localPort: tunnel.localPort,
              remotePort,
              pid: tunnel.pid,
            },
          }
        : null;

      const targets: GatewayStatusTarget[] = tunnelTarget
        ? [tunnelTarget, ...baseTargets.filter((t) => t.url !== tunnelTarget.url)]
        : baseTargets;

      try {
        const probed = await Promise.all(
          targets.map(async (target) => {
            const authResolution = await resolveAuthForTarget(cfg, target, {
              token: typeof opts.token === "string" ? opts.token : undefined,
              password: typeof opts.password === "string" ? opts.password : undefined,
            });
            const auth = {
              token: authResolution.token,
              password: authResolution.password,
            };
            const timeoutMs = resolveProbeBudgetMs(overallTimeoutMs, target.kind);
            const probe = await probeGateway({
              url: target.url,
              auth,
              timeoutMs,
            });
            const configSummary = probe.configSnapshot
              ? extractConfigSummary(probe.configSnapshot)
              : null;
            const self = pickGatewaySelfPresence(probe.presence);
            return {
              target,
              probe,
              configSummary,
              self,
              authDiagnostics: authResolution.diagnostics ?? [],
            };
          }),
        );

        return { probed };
      } finally {
        if (tunnel) {
          try {
            await tunnel.stop();
          } catch {
            // best-effort
          }
        }
      }
    },
  );

  const reachable = probed.filter((p) => isProbeReachable(p.probe));
  const ok = reachable.length > 0;
  const degradedScopeLimited = probed.filter((p) => isScopeLimitedProbeFailure(p.probe));
  const degraded = degradedScopeLimited.length > 0;
  const multipleGateways = reachable.length > 1;
  const primary =
    reachable.find((p) => p.target.kind === "explicit") ??
    reachable.find((p) => p.target.kind === "sshTunnel") ??
    reachable.find((p) => p.target.kind === "configRemote") ??
    reachable.find((p) => p.target.kind === "localLoopback") ??
    null;

  const warnings: Array<{
    code: string;
    message: string;
    targetIds?: string[];
  }> = [];
  if (sshTarget && !sshTunnelStarted) {
    warnings.push({
      code: "ssh_tunnel_failed",
      message: sshTunnelError
        ? `SSH tunnel failed: ${String(sshTunnelError)}`
        : "SSH tunnel failed to start; falling back to direct probes.",
    });
  }
  if (multipleGateways) {
    warnings.push({
      code: "multiple_gateways",
      message:
        "Unconventional setup: multiple reachable gateways detected. Usually one gateway per network is recommended unless you intentionally run isolated profiles, like a rescue bot (see docs: /gateway#multiple-gateways-same-host).",
      targetIds: reachable.map((p) => p.target.id),
    });
  }
  for (const result of probed) {
    if (result.authDiagnostics.length === 0 || isProbeReachable(result.probe)) {
      continue;
    }
    for (const diagnostic of result.authDiagnostics) {
      warnings.push({
        code: "auth_secretref_unresolved",
        message: diagnostic,
        targetIds: [result.target.id],
      });
    }
  }
  for (const result of degradedScopeLimited) {
    warnings.push({
      code: "probe_scope_limited",
      message:
        "Probe diagnostics are limited by gateway scopes (missing operator.read). Connection succeeded, but status details may be incomplete. Hint: pair device identity or use credentials with operator.read.",
      targetIds: [result.target.id],
    });
  }

  if (opts.json) {
    runtime.log(
      JSON.stringify(
        {
          ok,
          degraded,
          ts: Date.now(),
          durationMs: Date.now() - startedAt,
          timeoutMs: overallTimeoutMs,
          primaryTargetId: primary?.target.id ?? null,
          warnings,
          network,
          targets: probed.map((p) => ({
            id: p.target.id,
            kind: p.target.kind,
            url: p.target.url,
            active: p.target.active,
            tunnel: p.target.tunnel ?? null,
            connect: {
              ok: isProbeReachable(p.probe),
              rpcOk: p.probe.ok,
              scopeLimited: isScopeLimitedProbeFailure(p.probe),
              latencyMs: p.probe.connectLatencyMs,
              error: p.probe.error,
              close: p.probe.close,
            },
            self: p.self,
            config: p.configSummary,
            health: p.probe.health,
            summary: p.probe.status,
            presence: p.probe.presence,
          })),
        },
        null,
        2,
      ),
    );
    if (!ok) {
      runtime.exit(1);
    }
    return;
  }

  runtime.log(colorize(rich, theme.heading, "Gateway Status"));
  runtime.log(
    ok
      ? `${colorize(rich, theme.success, "Reachable")}: yes`
      : `${colorize(rich, theme.error, "Reachable")}: no`,
  );
  runtime.log(colorize(rich, theme.muted, `Probe budget: ${overallTimeoutMs}ms`));

  if (warnings.length > 0) {
    runtime.log("");
    runtime.log(colorize(rich, theme.warn, "Warning:"));
    for (const w of warnings) {
      runtime.log(`- ${w.message}`);
    }
  }

  runtime.log("");
  runtime.log(colorize(rich, theme.heading, "Targets"));
  for (const p of probed) {
    runtime.log(renderTargetHeader(p.target, rich));
    runtime.log(`  ${renderProbeSummaryLine(p.probe, rich)}`);
    if (p.target.tunnel?.kind === "ssh") {
      runtime.log(
        `  ${colorize(rich, theme.muted, "ssh")}: ${colorize(rich, theme.command, p.target.tunnel.target)}`,
      );
    }
    if (p.probe.ok && p.self) {
      const host = p.self.host ?? "unknown";
      const ip = p.self.ip ? ` (${p.self.ip})` : "";
      const platform = p.self.platform ? ` · ${p.self.platform}` : "";
      const version = p.self.version ? ` · app ${p.self.version}` : "";
      runtime.log(`  ${colorize(rich, theme.info, "Gateway")}: ${host}${ip}${platform}${version}`);
    }
    runtime.log("");
  }

  if (!ok) {
    runtime.exit(1);
  }
}

function inferSshTargetFromRemoteUrl(rawUrl?: string | null): string | null {
  if (typeof rawUrl !== "string") {
    return null;
  }
  const trimmed = rawUrl.trim();
  if (!trimmed) {
    return null;
  }
  let host: string | null = null;
  try {
    host = new URL(trimmed).hostname || null;
  } catch {
    return null;
  }
  if (!host) {
    return null;
  }
  const user = process.env.USER?.trim() || "";
  return user ? `${user}@${host}` : host;
}

function buildSshTarget(input: { user?: string; host?: string; port?: number }): string | null {
  const host = input.host?.trim() ?? "";
  if (!host) {
    return null;
  }
  const user = input.user?.trim() ?? "";
  const base = user ? `${user}@${host}` : host;
  const port = input.port ?? 22;
  if (port && port !== 22) {
    return `${base}:${port}`;
  }
  return base;
}

async function resolveSshTarget(
  rawTarget: string,
  identity: string | null,
  overallTimeoutMs: number,
): Promise<{ target: string; identity?: string } | null> {
  const [{ resolveSshConfig }, { parseSshTarget }] = await Promise.all([
    loadSshConfigModule(),
    loadSshTunnelModule(),
  ]);
  const parsed = parseSshTarget(rawTarget);
  if (!parsed) {
    return null;
  }
  const config = await resolveSshConfig(parsed, {
    identity: identity ?? undefined,
    timeoutMs: Math.min(800, overallTimeoutMs),
  });
  if (!config) {
    return { target: rawTarget, identity: identity ?? undefined };
  }
  const target = buildSshTarget({
    user: config.user ?? parsed.user,
    host: config.host ?? parsed.host,
    port: config.port ?? parsed.port,
  });
  if (!target) {
    return { target: rawTarget, identity: identity ?? undefined };
  }
  const identityFile =
    identity ?? config.identityFiles.find((entry) => entry.trim().length > 0)?.trim() ?? undefined;
  return { target, identity: identityFile };
}
