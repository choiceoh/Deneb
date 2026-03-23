import { hasPotentialConfiguredChannels } from "../channels/config-presence.js";
import { formatCliCommand } from "../cli/command-format.js";
import type { DenebConfig } from "../config/config.js";
import { resolveConfigPath, resolveStateDir } from "../config/paths.js";
import { collectBrowserControlFindings } from "./audit-browser.js";
import {
  collectElevatedFindings,
  collectExecRuntimeFindings,
  collectLoggingFindings,
} from "./audit-exec.js";
import { collectFilesystemFindings } from "./audit-filesystem.js";
import { collectGatewayConfigFindings } from "./audit-gateway.js";
import { countBySeverity } from "./audit.helpers.js";
import type {
  AuditExecutionContext,
  SecurityAuditFinding,
  SecurityAuditOptions,
  SecurityAuditReport,
  SecurityAuditSummary,
  SecurityAuditSeverity,
} from "./audit.types.js";

export type {
  SecurityAuditSeverity,
  SecurityAuditFinding,
  SecurityAuditSummary,
  SecurityAuditReport,
  SecurityAuditOptions,
};

type ProbeGatewayFn = typeof import("../gateway/monitoring/probe.js").probeGateway;

let channelPluginsModulePromise: Promise<typeof import("../channels/plugins/index.js")> | undefined;
let auditNonDeepModulePromise: Promise<typeof import("./audit.nondeep.runtime.js")> | undefined;
let auditDeepModulePromise: Promise<typeof import("./audit.deep.runtime.js")> | undefined;
let auditChannelModulePromise: Promise<typeof import("./audit-channel.js")> | undefined;
let gatewayProbeDepsPromise:
  | Promise<{
      buildGatewayConnectionDetails: typeof import("../gateway/call.js").buildGatewayConnectionDetails;
      resolveGatewayProbeAuthSafe: typeof import("../gateway/auth/probe-auth.js").resolveGatewayProbeAuthSafe;
      probeGateway: typeof import("../gateway/monitoring/probe.js").probeGateway;
    }>
  | undefined;

async function loadChannelPlugins() {
  channelPluginsModulePromise ??= import("../channels/plugins/index.js");
  return await channelPluginsModulePromise;
}

async function loadAuditNonDeepModule() {
  auditNonDeepModulePromise ??= import("./audit.nondeep.runtime.js");
  return await auditNonDeepModulePromise;
}

async function loadAuditDeepModule() {
  auditDeepModulePromise ??= import("./audit.deep.runtime.js");
  return await auditDeepModulePromise;
}

async function loadAuditChannelModule() {
  auditChannelModulePromise ??= import("./audit-channel.js");
  return await auditChannelModulePromise;
}

async function loadGatewayProbeDeps() {
  gatewayProbeDepsPromise ??= Promise.all([
    import("../gateway/call.js"),
    import("../gateway/auth/probe-auth.js"),
    import("../gateway/monitoring/probe.js"),
  ]).then(([callModule, probeAuthModule, probeModule]) => ({
    buildGatewayConnectionDetails: callModule.buildGatewayConnectionDetails,
    resolveGatewayProbeAuthSafe: probeAuthModule.resolveGatewayProbeAuthSafe,
    probeGateway: probeModule.probeGateway,
  }));
  return await gatewayProbeDepsPromise;
}

async function maybeProbeGateway(params: {
  cfg: DenebConfig;
  env: NodeJS.ProcessEnv;
  timeoutMs: number;
  probe: ProbeGatewayFn;
  explicitAuth?: { token?: string; password?: string };
}): Promise<{
  deep: SecurityAuditReport["deep"];
  authWarning?: string;
}> {
  const { buildGatewayConnectionDetails, resolveGatewayProbeAuthSafe } =
    await loadGatewayProbeDeps();
  const connection = buildGatewayConnectionDetails({ config: params.cfg });
  const url = connection.url;
  const isRemoteMode = params.cfg.gateway?.mode === "remote";
  const remoteUrlRaw =
    typeof params.cfg.gateway?.remote?.url === "string" ? params.cfg.gateway.remote.url.trim() : "";
  const remoteUrlMissing = isRemoteMode && !remoteUrlRaw;

  const authResolution =
    !isRemoteMode || remoteUrlMissing
      ? resolveGatewayProbeAuthSafe({
          cfg: params.cfg,
          env: params.env,
          mode: "local",
          explicitAuth: params.explicitAuth,
        })
      : resolveGatewayProbeAuthSafe({
          cfg: params.cfg,
          env: params.env,
          mode: "remote",
          explicitAuth: params.explicitAuth,
        });
  const res = await params
    .probe({ url, auth: authResolution.auth, timeoutMs: params.timeoutMs })
    .catch((err) => ({
      ok: false,
      url,
      connectLatencyMs: null,
      error: String(err),
      close: null,
      health: null,
      status: null,
      presence: null,
      configSnapshot: null,
    }));

  if (authResolution.warning && !res.ok) {
    res.error = res.error ? `${res.error}; ${authResolution.warning}` : authResolution.warning;
  }

  return {
    deep: {
      gateway: {
        attempted: true,
        url,
        ok: res.ok,
        error: res.ok ? null : res.error,
        close: res.close ? { code: res.close.code, reason: res.close.reason } : null,
      },
    },
    authWarning: authResolution.warning,
  };
}

async function createAuditExecutionContext(
  opts: SecurityAuditOptions,
): Promise<AuditExecutionContext> {
  const cfg = opts.config;
  const sourceConfig = opts.sourceConfig ?? opts.config;
  const env = opts.env ?? process.env;
  const platform = opts.platform ?? process.platform;
  const includeFilesystem = opts.includeFilesystem !== false;
  const includeChannelSecurity = opts.includeChannelSecurity !== false;
  const deep = opts.deep === true;
  const deepTimeoutMs = Math.max(250, opts.deepTimeoutMs ?? 5000);
  const stateDir = opts.stateDir ?? resolveStateDir(env);
  const configPath = opts.configPath ?? resolveConfigPath(env, stateDir);
  const { readConfigSnapshotForAudit } = await loadAuditNonDeepModule();
  const configSnapshot = includeFilesystem
    ? opts.configSnapshot !== undefined
      ? opts.configSnapshot
      : await readConfigSnapshotForAudit({ env, configPath }).catch(() => null)
    : null;
  return {
    cfg,
    sourceConfig,
    env,
    platform,
    includeFilesystem,
    includeChannelSecurity,
    deep,
    deepTimeoutMs,
    stateDir,
    configPath,
    execDockerRawFn: opts.execDockerRawFn,
    probeGatewayFn: opts.probeGatewayFn,
    plugins: opts.plugins,
    configSnapshot,
    codeSafetySummaryCache: opts.codeSafetySummaryCache ?? new Map<string, Promise<unknown>>(),
    deepProbeAuth: opts.deepProbeAuth,
  };
}

export async function runSecurityAudit(opts: SecurityAuditOptions): Promise<SecurityAuditReport> {
  const findings: SecurityAuditFinding[] = [];
  const context = await createAuditExecutionContext(opts);
  const { cfg, env, platform, stateDir, configPath } = context;
  const auditNonDeep = await loadAuditNonDeepModule();

  findings.push(...auditNonDeep.collectAttackSurfaceSummaryFindings(cfg));
  findings.push(...auditNonDeep.collectSyncedFolderFindings({ stateDir, configPath }));

  findings.push(...collectGatewayConfigFindings(cfg, context.sourceConfig, env));
  findings.push(...collectBrowserControlFindings(cfg, env));
  findings.push(...collectLoggingFindings(cfg));
  findings.push(...collectElevatedFindings(cfg));
  findings.push(...collectExecRuntimeFindings(cfg));
  findings.push(...auditNonDeep.collectHooksHardeningFindings(cfg, env));
  findings.push(...auditNonDeep.collectGatewayHttpNoAuthFindings(cfg, env));
  findings.push(...auditNonDeep.collectGatewayHttpSessionKeyOverrideFindings(cfg));
  findings.push(...auditNonDeep.collectSandboxDockerNoopFindings(cfg));
  findings.push(...auditNonDeep.collectSandboxDangerousConfigFindings(cfg));
  findings.push(...auditNonDeep.collectNodeDenyCommandPatternFindings(cfg));
  findings.push(...auditNonDeep.collectNodeDangerousAllowCommandFindings(cfg));
  findings.push(...auditNonDeep.collectMinimalProfileOverrideFindings(cfg));
  findings.push(...auditNonDeep.collectSecretsInConfigFindings(cfg));
  findings.push(...auditNonDeep.collectModelHygieneFindings(cfg));
  findings.push(...auditNonDeep.collectSmallModelRiskFindings({ cfg, env }));
  findings.push(...auditNonDeep.collectExposureMatrixFindings(cfg));
  findings.push(...auditNonDeep.collectLikelyMultiUserSetupFindings(cfg));

  if (context.includeFilesystem) {
    findings.push(
      ...(await collectFilesystemFindings({
        stateDir,
        configPath,
      })),
    );
    if (context.configSnapshot) {
      findings.push(
        ...(await auditNonDeep.collectIncludeFilePermFindings({
          configSnapshot: context.configSnapshot,
          env,
          platform,
        })),
      );
    }
    findings.push(
      ...(await auditNonDeep.collectStateDeepFilesystemFindings({
        cfg,
        env,
        stateDir,
      })),
    );
    findings.push(...(await auditNonDeep.collectWorkspaceSkillSymlinkEscapeFindings({ cfg })));
    findings.push(
      ...(await auditNonDeep.collectSandboxBrowserHashLabelFindings({
        execDockerRawFn: context.execDockerRawFn,
      })),
    );
    findings.push(...(await auditNonDeep.collectPluginsTrustFindings({ cfg, stateDir })));
    if (context.deep) {
      const auditDeep = await loadAuditDeepModule();
      findings.push(
        ...(await auditDeep.collectPluginsCodeSafetyFindings({
          stateDir,
          summaryCache: context.codeSafetySummaryCache,
        })),
      );
      findings.push(
        ...(await auditDeep.collectInstalledSkillsCodeSafetyFindings({
          cfg,
          stateDir,
          summaryCache: context.codeSafetySummaryCache,
        })),
      );
    }
  }

  const shouldAuditChannelSecurity =
    context.includeChannelSecurity &&
    (context.plugins !== undefined || hasPotentialConfiguredChannels(cfg, env));
  if (shouldAuditChannelSecurity) {
    const channelPlugins = context.plugins ?? (await loadChannelPlugins()).listChannelPlugins();
    const { collectChannelSecurityFindings } = await loadAuditChannelModule();
    findings.push(
      ...(await collectChannelSecurityFindings({
        cfg,
        sourceConfig: context.sourceConfig,
        plugins: channelPlugins,
      })),
    );
  }

  const deepProbeResult = context.deep
    ? await maybeProbeGateway({
        cfg,
        env,
        timeoutMs: context.deepTimeoutMs,
        probe: context.probeGatewayFn ?? (await loadGatewayProbeDeps()).probeGateway,
        explicitAuth: context.deepProbeAuth,
      })
    : undefined;
  const deep = deepProbeResult?.deep;

  if (deep?.gateway?.attempted && !deep.gateway.ok) {
    findings.push({
      checkId: "gateway.probe_failed",
      severity: "warn",
      title: "Gateway probe failed (deep)",
      detail: deep.gateway.error ?? "gateway unreachable",
      remediation: `Run "${formatCliCommand("deneb status --all")}" to debug connectivity/auth, then re-run "${formatCliCommand("deneb security audit --deep")}".`,
    });
  }
  if (deepProbeResult?.authWarning) {
    findings.push({
      checkId: "gateway.probe_auth_secretref_unavailable",
      severity: "warn",
      title: "Gateway probe auth SecretRef is unavailable",
      detail: deepProbeResult.authWarning,
      remediation: `Set DENEB_GATEWAY_TOKEN/DENEB_GATEWAY_PASSWORD in this shell or resolve the external secret provider, then re-run "${formatCliCommand("deneb security audit --deep")}".`,
    });
  }

  const summary = countBySeverity(findings);
  return { ts: Date.now(), summary, findings, deep };
}
