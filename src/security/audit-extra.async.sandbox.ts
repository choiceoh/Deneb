/**
 * Async security audit: sandbox browser container checks.
 */
import { SANDBOX_BROWSER_SECURITY_HASH_EPOCH } from "../agents/sandbox/constants.js";
import { execDockerRaw, type ExecDockerRawResult } from "../agents/sandbox/docker.js";
import { formatCliCommand } from "../cli/command-format.js";
import type { SecurityAuditFinding } from "./audit-extra-shared.js";

type ExecDockerRawFn = (
  args: string[],
  opts?: { allowFailure?: boolean; input?: Buffer | string; signal?: AbortSignal },
) => Promise<ExecDockerRawResult>;

function normalizeDockerLabelValue(raw: string | undefined): string | null {
  const trimmed = raw?.trim() ?? "";
  if (!trimmed || trimmed === "<no value>") {
    return null;
  }
  return trimmed;
}

async function listSandboxBrowserContainers(
  execDockerRawFn: ExecDockerRawFn,
): Promise<string[] | null> {
  try {
    const result = await execDockerRawFn(
      ["ps", "-a", "--filter", "label=deneb.sandboxBrowser=1", "--format", "{{.Names}}"],
      { allowFailure: true },
    );
    if (result.code !== 0) {
      return null;
    }
    return result.stdout
      .toString("utf8")
      .split(/\r?\n/)
      .map((entry) => entry.trim())
      .filter(Boolean);
  } catch {
    return null;
  }
}

async function readSandboxBrowserHashLabels(params: {
  containerName: string;
  execDockerRawFn: ExecDockerRawFn;
}): Promise<{ configHash: string | null; epoch: string | null } | null> {
  try {
    const result = await params.execDockerRawFn(
      [
        "inspect",
        "-f",
        '{{ index .Config.Labels "deneb.configHash" }}\t{{ index .Config.Labels "deneb.browserConfigEpoch" }}',
        params.containerName,
      ],
      { allowFailure: true },
    );
    if (result.code !== 0) {
      return null;
    }
    const [hashRaw, epochRaw] = result.stdout.toString("utf8").split("\t");
    return {
      configHash: normalizeDockerLabelValue(hashRaw),
      epoch: normalizeDockerLabelValue(epochRaw),
    };
  } catch {
    return null;
  }
}

function parsePublishedHostFromDockerPortLine(line: string): string | null {
  const trimmed = line.trim();
  const rhs = trimmed.includes("->") ? (trimmed.split("->").at(-1)?.trim() ?? "") : trimmed;
  if (!rhs) {
    return null;
  }
  const bracketHost = rhs.match(/^\[([^\]]+)\]:\d+$/);
  if (bracketHost?.[1]) {
    return bracketHost[1];
  }
  const hostPort = rhs.match(/^([^:]+):\d+$/);
  if (hostPort?.[1]) {
    return hostPort[1];
  }
  return null;
}

function isLoopbackPublishHost(host: string): boolean {
  const normalized = host.trim().toLowerCase();
  return normalized === "127.0.0.1" || normalized === "::1" || normalized === "localhost";
}

async function readSandboxBrowserPortMappings(params: {
  containerName: string;
  execDockerRawFn: ExecDockerRawFn;
}): Promise<string[] | null> {
  try {
    const result = await params.execDockerRawFn(["port", params.containerName], {
      allowFailure: true,
    });
    if (result.code !== 0) {
      return null;
    }
    return result.stdout
      .toString("utf8")
      .split(/\r?\n/)
      .map((entry) => entry.trim())
      .filter(Boolean);
  } catch {
    return null;
  }
}

export async function collectSandboxBrowserHashLabelFindings(params?: {
  execDockerRawFn?: ExecDockerRawFn;
}): Promise<SecurityAuditFinding[]> {
  const findings: SecurityAuditFinding[] = [];
  const execFn = params?.execDockerRawFn ?? execDockerRaw;
  const containers = await listSandboxBrowserContainers(execFn);
  if (!containers || containers.length === 0) {
    return findings;
  }

  const missingHash: string[] = [];
  const staleEpoch: string[] = [];
  const nonLoopbackPublished: string[] = [];

  for (const containerName of containers) {
    const labels = await readSandboxBrowserHashLabels({ containerName, execDockerRawFn: execFn });
    if (!labels) {
      continue;
    }
    if (!labels.configHash) {
      missingHash.push(containerName);
    }
    if (labels.epoch !== SANDBOX_BROWSER_SECURITY_HASH_EPOCH) {
      staleEpoch.push(containerName);
    }
    const portMappings = await readSandboxBrowserPortMappings({
      containerName,
      execDockerRawFn: execFn,
    });
    if (!portMappings?.length) {
      continue;
    }
    const exposedMappings = portMappings.filter((line) => {
      const host = parsePublishedHostFromDockerPortLine(line);
      return Boolean(host && !isLoopbackPublishHost(host));
    });
    if (exposedMappings.length > 0) {
      nonLoopbackPublished.push(`${containerName} (${exposedMappings.join("; ")})`);
    }
  }

  if (missingHash.length > 0) {
    findings.push({
      checkId: "sandbox.browser_container.hash_label_missing",
      severity: "warn",
      title: "Sandbox browser container missing config hash label",
      detail:
        `Containers: ${missingHash.join(", ")}. ` +
        "These browser containers predate hash-based drift checks and may miss security remediations until recreated.",
      remediation: `${formatCliCommand("deneb sandbox recreate --browser --all")} (add --force to skip prompt).`,
    });
  }

  if (staleEpoch.length > 0) {
    findings.push({
      checkId: "sandbox.browser_container.hash_epoch_stale",
      severity: "warn",
      title: "Sandbox browser container hash epoch is stale",
      detail:
        `Containers: ${staleEpoch.join(", ")}. ` +
        `Expected deneb.browserConfigEpoch=${SANDBOX_BROWSER_SECURITY_HASH_EPOCH}.`,
      remediation: `${formatCliCommand("deneb sandbox recreate --browser --all")} (add --force to skip prompt).`,
    });
  }

  if (nonLoopbackPublished.length > 0) {
    findings.push({
      checkId: "sandbox.browser_container.non_loopback_publish",
      severity: "critical",
      title: "Sandbox browser container publishes ports on non-loopback interfaces",
      detail:
        `Containers: ${nonLoopbackPublished.join(", ")}. ` +
        "Sandbox browser observer/control ports should stay loopback-only to avoid unintended remote access.",
      remediation:
        `${formatCliCommand("deneb sandbox recreate --browser --all")} (add --force to skip prompt), ` +
        "then verify published ports are bound to 127.0.0.1.",
    });
  }

  return findings;
}
