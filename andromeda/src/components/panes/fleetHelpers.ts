import type { FleetJob, FleetNode } from "@/fleet";

// FleetPane's shared types and pure display helpers — split from FleetPane.tsx
// (calendarHelpers/fileHelpers pattern) so the pane file stays within the repo's
// size guideline and non-component exports don't trip react-refresh.

export type FleetView = "overview" | "nodes" | "models" | "services" | "jobs";
export type JobFilter = "all" | "running" | "done" | "failed";
export type ServiceFilter = "all" | "healthy" | "down";

export const FLEET_VIEWS: { key: FleetView; label: string }[] = [
  { key: "overview", label: "개요" },
  { key: "nodes", label: "노드" },
  { key: "models", label: "모델" },
  { key: "services", label: "서비스" },
  { key: "jobs", label: "작업" },
];

export interface FleetIssue {
  key: string;
  title: string;
  detail: string;
  view: FleetView;
  tone: "bad" | "warn";
}

export interface FleetModelRow {
  key: string;
  name: string;
  sizeBytes?: number;
  nodeName: string;
  nodeRole?: string;
  nodeReachable: boolean;
}

export interface FleetServiceRow {
  key: string;
  name: string;
  ok: boolean;
  nodeName: string;
  nodeRole?: string;
  nodeReachable: boolean;
}

export function asArray<T>(value: T[] | null | undefined): T[] {
  return Array.isArray(value) ? value : [];
}

export function gpuText(node: FleetNode): string {
  const gpus = asArray(node.metrics?.gpus);
  if (gpus.length === 0) return "";
  return gpus
    .map((g) => {
      const name = `GPU${g.index ?? 0}`;
      const util = g.utilPct != null ? `${g.utilPct}%` : "";
      const temp = g.tempC != null ? `${g.tempC}°C` : "";
      return [name, util, temp].filter(Boolean).join(" ");
    })
    .join(", ");
}

export function memoryText(node: FleetNode): string {
  const mem = node.metrics?.memory;
  if (!mem?.totalKB) return "";
  const used = Math.max(0, mem.totalKB - (mem.availableKB ?? 0));
  return `${percent(used, mem.totalKB)}% · ${bytes(used * 1024)}/${bytes(mem.totalKB * 1024)}`;
}

export function jobState(job: FleetJob): string {
  return (job.state || "").toLowerCase() || "unknown";
}

export function jobStateLabel(state: string): string {
  if (state === "running") return "진행";
  if (state === "done") return "완료";
  if (state === "failed") return "실패";
  return state;
}

export function jobFilterLabel(filter: JobFilter): string {
  if (filter === "running") return "진행";
  if (filter === "done") return "완료";
  if (filter === "failed") return "실패";
  return "전체";
}

export function serviceFilterLabel(filter: ServiceFilter): string {
  if (filter === "healthy") return "정상";
  if (filter === "down") return "다운";
  return "전체";
}

export function nodeHasIssue(node: FleetNode): boolean {
  if (node.reachable === false || Boolean(node.error)) return true;
  const memory = node.metrics?.memory;
  if (memory?.totalKB) {
    const used = Math.max(0, memory.totalKB - (memory.availableKB ?? 0));
    if (percent(used, memory.totalKB) >= 90) return true;
  }
  if (asArray(node.metrics?.disks).some((disk) => (disk.usePct ?? 0) >= 90)) return true;
  if (asArray(node.metrics?.gpus).some((gpu) => (gpu.tempC ?? 0) >= 85)) return true;
  return asArray(node.metrics?.services).some((service) => service.ok === false);
}

export function nodeIssueText(node: FleetNode): string {
  if (node.reachable === false) return "연결 안 됨";
  if (node.error) return node.error;
  const downServices = asArray(node.metrics?.services)
    .filter((service) => service.ok === false)
    .map((service) => service.name)
    .filter(Boolean);
  if (downServices.length) return `서비스 다운: ${downServices.join(", ")}`;
  const hotGpu = asArray(node.metrics?.gpus).find((gpu) => (gpu.tempC ?? 0) >= 85);
  if (hotGpu) return `GPU${hotGpu.index ?? 0} ${hotGpu.tempC}°C`;
  const disk = asArray(node.metrics?.disks).find((item) => (item.usePct ?? 0) >= 90);
  if (disk) return `디스크 ${disk.usePct}% · ${disk.path || "/"}`;
  const memory = node.metrics?.memory;
  if (memory?.totalKB) {
    const used = Math.max(0, memory.totalKB - (memory.availableKB ?? 0));
    const pct = percent(used, memory.totalKB);
    if (pct >= 90) return `메모리 ${pct}%`;
  }
  return "상태 확인 필요";
}

export function nodeIssueView(node: FleetNode): FleetView {
  if (node.reachable === false || node.error) return "nodes";
  if (asArray(node.metrics?.services).some((service) => service.ok === false)) return "services";
  return "nodes";
}

export function percent(used: number, total: number): number {
  if (!total) return 0;
  return Math.round((used / total) * 100);
}

export function bytes(n: number): string {
  if (!Number.isFinite(n) || n <= 0) return "0 B";
  const units = ["B", "KB", "MB", "GB", "TB"];
  let value = n;
  let unit = 0;
  while (value >= 1024 && unit < units.length - 1) {
    value /= 1024;
    unit += 1;
  }
  return `${value >= 10 ? value.toFixed(0) : value.toFixed(1)} ${units[unit]}`;
}

export function oneLine(text: string): string {
  return text.replace(/\s+/g, " ").trim().slice(0, 140);
}

export function clamp(n: number): number {
  return Math.max(0, Math.min(100, Number.isFinite(n) ? n : 0));
}
