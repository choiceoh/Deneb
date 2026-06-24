import { base, type GatewayConfig, gatewayFetch, TOKEN_HEADER } from "./gateway";

export interface FleetGpu {
  index?: number;
  utilPct?: number | null;
  tempC?: number | null;
}

export interface FleetMemory {
  totalKB?: number;
  availableKB?: number;
}

export interface FleetDisk {
  path?: string;
  totalKB?: number;
  usedKB?: number;
  usePct?: number;
}

export interface FleetServiceHealth {
  name?: string;
  ok?: boolean;
}

export interface FleetNodeMetrics {
  gpus?: FleetGpu[] | null;
  memory?: FleetMemory | null;
  disks?: FleetDisk[] | null;
  services?: FleetServiceHealth[] | null;
}

export interface FleetModel {
  name?: string;
  sizeBytes?: number;
}

export interface FleetNode {
  name: string;
  role?: string;
  reachable?: boolean;
  error?: string | null;
  metrics?: FleetNodeMetrics | null;
  models?: FleetModel[] | null;
}

export interface FleetState {
  nodes?: FleetNode[] | null;
}

export interface FleetVllm {
  gpuMemoryUtilization?: number | null;
  maxModelLen?: number | null;
  maxNumSeqs?: number | null;
}

export interface FleetRecipeStatus {
  running?: boolean;
  weightsPresent?: boolean;
  node?: string;
}

export interface FleetRecipe {
  name: string;
  description?: string;
  node?: string;
  container?: string;
  port?: number;
  vllm?: FleetVllm | null;
  status?: FleetRecipeStatus | null;
}

export interface FleetJob {
  id: string;
  title?: string;
  state?: string;
  log?: string;
  startedAt?: string;
  endedAt?: string;
}

export interface FleetJobIdResponse {
  jobId?: string;
}

export async function fleetState(cfg: GatewayConfig): Promise<FleetState> {
  return fleetGet(cfg, "/api/state");
}

export async function fleetRecipes(cfg: GatewayConfig): Promise<FleetRecipe[]> {
  return fleetGet(cfg, "/api/recipes");
}

export async function fleetJobs(cfg: GatewayConfig): Promise<FleetJob[]> {
  return fleetGet(cfg, "/api/jobs");
}

export async function fleetRecipeAction(
  cfg: GatewayConfig,
  recipe: string,
  action: "launch" | "stop" | "restart" | "pull",
  overrides?: FleetVllm,
): Promise<FleetJobIdResponse> {
  const body: Record<string, unknown> = { recipe, action };
  if (action === "launch" && overrides && hasVllmOverride(overrides)) body.overrides = compactVllm(overrides);
  return fleetPostAction(cfg, "/api/recipes/action", body);
}

async function fleetGet<T>(cfg: GatewayConfig, path: string): Promise<T> {
  const text = await fleetRequestText(cfg, path, { method: "GET" });
  return JSON.parse(text) as T;
}

async function fleetPostAction(
  cfg: GatewayConfig,
  path: string,
  body: Record<string, unknown>,
): Promise<FleetJobIdResponse> {
  const text = await fleetRequestText(cfg, path, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(body),
  });
  if (!text.trim()) return {};
  try {
    return JSON.parse(text) as FleetJobIdResponse;
  } catch {
    return {};
  }
}

async function fleetRequestText(cfg: GatewayConfig, path: string, init: RequestInit): Promise<string> {
  if (!cfg.url || !cfg.token) throw new Error("게이트웨이에 연결되어 있지 않습니다.");
  const headers = new Headers(init.headers);
  headers.set(TOKEN_HEADER, cfg.token);
  const res = await gatewayFetch(`${base(cfg.url)}/api/v1/fleet${path}`, {
    ...init,
    headers,
  });
  const text = await res.text();
  if (!res.ok) throw new Error(errorText(text) || `플릿 요청 실패 (${res.status})`);
  return text;
}

function errorText(text: string): string {
  if (!text.trim()) return "";
  try {
    const parsed = JSON.parse(text) as { error?: unknown; message?: unknown };
    return String(parsed.error ?? parsed.message ?? text);
  } catch {
    return text;
  }
}

function hasVllmOverride(v: FleetVllm): boolean {
  return v.gpuMemoryUtilization != null || v.maxModelLen != null || v.maxNumSeqs != null;
}

function compactVllm(v: FleetVllm): FleetVllm {
  return {
    ...(v.gpuMemoryUtilization != null ? { gpuMemoryUtilization: v.gpuMemoryUtilization } : {}),
    ...(v.maxModelLen != null ? { maxModelLen: v.maxModelLen } : {}),
    ...(v.maxNumSeqs != null ? { maxNumSeqs: v.maxNumSeqs } : {}),
  };
}
