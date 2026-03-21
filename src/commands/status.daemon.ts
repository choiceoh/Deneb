import { resolveGatewayService } from "../daemon/service.js";
import { formatDaemonRuntimeShort } from "./status.format.js";
import { readServiceStatusSummary } from "./status.service-summary.js";

type DaemonStatusSummary = {
  label: string;
  installed: boolean | null;
  managedByDeneb: boolean;
  externallyManaged: boolean;
  loadedText: string;
  runtimeShort: string | null;
};

async function buildDaemonStatusSummary(_serviceLabel: "gateway"): Promise<DaemonStatusSummary> {
  const service = resolveGatewayService();
  const summary = await readServiceStatusSummary(service, "Daemon");
  return {
    label: summary.label,
    installed: summary.installed,
    managedByDeneb: summary.managedByDeneb,
    externallyManaged: summary.externallyManaged,
    loadedText: summary.loadedText,
    runtimeShort: formatDaemonRuntimeShort(summary.runtime),
  };
}

export async function getGatewayDaemonStatus(): Promise<DaemonStatusSummary> {
  return buildDaemonStatusSummary("gateway");
}

/** @deprecated Use getGatewayDaemonStatus instead. */
export const getDaemonStatusSummary = getGatewayDaemonStatus;
