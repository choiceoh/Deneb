/**
 * Display utilities for sandbox CLI
 */

import type { SandboxContainerInfo } from "../agents/sandbox.js";
import { formatCliCommand } from "../cli/command-format.js";
import { formatDurationCompact } from "../infra/format-time/format-duration.ts";
import type { RuntimeEnv } from "../runtime.js";
import { formatImageMatch, formatSimpleStatus, formatStatus } from "./sandbox-formatters.js";

type DisplayConfig<T> = {
  emptyMessage: string;
  title: string;
  renderItem: (item: T, runtime: RuntimeEnv) => void;
};

function displayItems<T>(items: T[], config: DisplayConfig<T>, runtime: RuntimeEnv): void {
  if (items.length === 0) {
    runtime.log(config.emptyMessage);
    return;
  }

  runtime.log(`\n${config.title}\n`);
  for (const item of items) {
    config.renderItem(item, runtime);
  }
}

export function displayContainers(containers: SandboxContainerInfo[], runtime: RuntimeEnv): void {
  displayItems(
    containers,
    {
      emptyMessage: "No sandbox runtimes found.",
      title: "📦 Sandbox Runtimes:",
      renderItem: (container, rt) => {
        rt.log(`  ${container.runtimeLabel ?? container.containerName}`);
        rt.log(`    Status:  ${formatStatus(container.running)}`);
        rt.log(
          `    ${container.configLabelKind ?? "Image"}:   ${container.image} ${formatImageMatch(container.imageMatch)}`,
        );
        rt.log(`    Backend: ${container.backendId ?? "docker"}`);
        rt.log(
          `    Age:     ${formatDurationCompact(Date.now() - container.createdAtMs, { spaced: true }) ?? "0s"}`,
        );
        rt.log(
          `    Idle:    ${formatDurationCompact(Date.now() - container.lastUsedAtMs, { spaced: true }) ?? "0s"}`,
        );
        rt.log(`    Session: ${container.sessionKey}`);
        rt.log("");
      },
    },
    runtime,
  );
}

export function displaySummary(containers: SandboxContainerInfo[], runtime: RuntimeEnv): void {
  const totalCount = containers.length;
  const runningCount = containers.filter((c) => c.running).length;
  const mismatchCount = containers.filter((c) => !c.imageMatch).length;

  runtime.log(`Total: ${totalCount} (${runningCount} running)`);

  if (mismatchCount > 0) {
    runtime.log(`\n⚠️  ${mismatchCount} runtime(s) with config mismatch detected.`);
    runtime.log(
      `   Run '${formatCliCommand("deneb sandbox recreate --all")}' to update all runtimes.`,
    );
  }
}

export function displayRecreatePreview(
  containers: SandboxContainerInfo[],
  runtime: RuntimeEnv,
): void {
  runtime.log("\nSandbox runtimes to be recreated:\n");

  if (containers.length > 0) {
    runtime.log("📦 Sandbox Runtimes:");
    for (const container of containers) {
      runtime.log(
        `  - ${container.runtimeLabel ?? container.containerName} [${container.backendId ?? "docker"}] (${formatSimpleStatus(container.running)})`,
      );
    }
  }

  runtime.log(`\nTotal: ${containers.length} runtime(s)`);
}

export function displayRecreateResult(
  result: { successCount: number; failCount: number },
  runtime: RuntimeEnv,
): void {
  runtime.log(`\nDone: ${result.successCount} removed, ${result.failCount} failed`);

  if (result.successCount > 0) {
    runtime.log("\nRuntimes will be automatically recreated when the agent is next used.");
  }
}
