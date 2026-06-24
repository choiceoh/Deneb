import { useCallback, useEffect, useMemo, useRef, useState } from "react";

import { serializeList } from "@/aiText";
import {
  type FleetJob,
  type FleetNode,
  type FleetRecipe,
  fleetJobs,
  fleetRecipeAction,
  fleetRecipes,
  fleetState,
} from "@/fleet";
import { errText, fmtDate } from "@/format";
import { useRegisterPane, useWorkspace } from "@/workspaceContext";
import { Column, Grid, RowBtn } from "@/components/Grid";
import { Detail, Modal, ModalFooter } from "@/components/Modal";

type FleetTab = "nodes" | "recipes" | "jobs";
type RecipeAction = "launch" | "stop" | "restart";

const TABS: Array<{ key: FleetTab; label: string }> = [
  { key: "nodes", label: "노드" },
  { key: "recipes", label: "레시피" },
  { key: "jobs", label: "작업" },
];

export function FleetPane() {
  const { connected, cfg } = useWorkspace();
  const [tab, setTab] = useState<FleetTab>("nodes");
  const [nodes, setNodes] = useState<FleetNode[]>([]);
  const [recipes, setRecipes] = useState<FleetRecipe[]>([]);
  const [jobs, setJobs] = useState<FleetJob[]>([]);
  const [loaded, setLoaded] = useState(false);
  const [loading, setLoading] = useState(false);
  const [stale, setStale] = useState(false);
  const [error, setError] = useState("");
  const [notice, setNotice] = useState("");
  const [confirm, setConfirm] = useState<{ recipe: FleetRecipe; action: RecipeAction } | null>(null);
  const [busyAction, setBusyAction] = useState("");
  const [expandedJob, setExpandedJob] = useState("");
  const loadedRef = useRef(false);
  const refreshSeqRef = useRef(0);

  const refresh = useCallback(async () => {
    if (!connected) return;
    const seq = ++refreshSeqRef.current;
    setLoading(true);
    setError("");
    const [stateResult, recipesResult, jobsResult] = await Promise.allSettled([
      fleetState(cfg),
      fleetRecipes(cfg),
      fleetJobs(cfg),
    ]);
    if (seq !== refreshSeqRef.current) return;
    let successCount = 0;
    const failures: unknown[] = [];
    if (stateResult.status === "fulfilled") {
      setNodes(asArray(stateResult.value.nodes));
      successCount += 1;
    } else failures.push(stateResult.reason);
    if (recipesResult.status === "fulfilled") {
      setRecipes(asArray(recipesResult.value));
      successCount += 1;
    } else failures.push(recipesResult.reason);
    if (jobsResult.status === "fulfilled") {
      setJobs(asArray(jobsResult.value));
      successCount += 1;
    } else failures.push(jobsResult.reason);

    if (successCount === 0) {
      setStale(loadedRef.current);
      setError(errText(failures[0] ?? new Error("플릿에 연결하지 못했습니다.")));
    } else {
      setStale(false);
    }
    loadedRef.current = true;
    setLoaded(true);
    setLoading(false);
  }, [cfg, connected]);

  useEffect(() => {
    if (!connected) {
      refreshSeqRef.current += 1;
      loadedRef.current = false;
      setLoaded(false);
      setLoading(false);
      setStale(false);
      setError("");
      return;
    }
    void refresh();
    const id = window.setInterval(() => void refresh(), 7_000);
    return () => {
      refreshSeqRef.current += 1;
      window.clearInterval(id);
    };
  }, [connected, refresh]);

  const aiText = useMemo(() => {
    const nodeText = serializeList(
      "플릿 노드",
      nodes,
      (n) =>
        `- ${n.name}${n.role ? ` (${n.role})` : ""}: ${n.reachable === false ? "연결 안 됨" : "연결됨"}` +
        `${gpuText(n) ? ` · ${gpuText(n)}` : ""}` +
        `${n.error ? ` · 오류 ${n.error}` : ""}`,
      "대",
    );
    const recipeText = serializeList(
      "플릿 레시피",
      recipes,
      (r) =>
        `- ${r.name}: ${r.status?.running ? "실행 중" : "중지"}` +
        `${recipeNode(r) ? ` · ${recipeNode(r)}` : ""}` +
        `${r.description ? ` · ${r.description}` : ""}`,
    );
    const jobText = serializeList(
      "플릿 작업",
      jobs.slice(0, 8),
      (j) => `- ${j.title || j.id}: ${j.state || "unknown"}${j.log ? ` · ${oneLine(j.log)}` : ""}`,
    );
    return [nodeText, recipeText, jobText].filter(Boolean).join("\n\n");
  }, [jobs, nodes, recipes]);
  useRegisterPane("fleet", aiText);

  async function runRecipeAction(recipe: FleetRecipe, action: RecipeAction) {
    setConfirm(null);
    setBusyAction(`${recipe.name}:${action}`);
    setNotice("");
    setError("");
    try {
      const result = await fleetRecipeAction(cfg, recipe.name, action);
      setNotice(
        result.jobId
          ? `${recipe.name} ${actionLabel(action)} 시작됨 · 작업 ${result.jobId}`
          : `${recipe.name} ${actionLabel(action)} 완료`,
      );
      await refresh();
    } catch (e) {
      setError(errText(e));
    } finally {
      setBusyAction("");
    }
  }

  const runningRecipes = recipes.filter((r) => r.status?.running).length;
  const runningJobs = jobs.filter((j) => jobState(j) === "running").length;
  const reachableNodes = nodes.filter((n) => n.reachable !== false).length;

  return (
    <>
      <div className="fleet-head">
        <h2 style={{ margin: 0 }}>플릿</h2>
        <button className="btn" type="button" onClick={refresh} disabled={!connected || loading}>
          새로고침
        </button>
      </div>
      {!connected ? (
        <p className="pane-status">미연결</p>
      ) : (
        <>
          <div className="settings-tabs fleet-tabs" role="tablist" aria-label="플릿 보기">
            {TABS.map((entry) => (
              <button
                key={entry.key}
                className={"settings-tab" + (tab === entry.key ? " active" : "")}
                type="button"
                role="tab"
                aria-selected={tab === entry.key}
                onClick={() => setTab(entry.key)}
              >
                {entry.label}
              </button>
            ))}
          </div>
          <div className="fleet-summary" aria-label="플릿 요약">
            <FleetMetric label="노드" value={`${reachableNodes}/${nodes.length || 0}`} />
            <FleetMetric label="레시피" value={`${runningRecipes}/${recipes.length || 0}`} />
            <FleetMetric label="작업" value={String(runningJobs)} />
          </div>
          {stale && <p className="pane-error">플릿 연결 끊김 · 마지막 데이터를 표시 중입니다.</p>}
          {error && <p className="pane-error">오류: {error}</p>}
          {notice && <p className="pane-status">{notice}</p>}
          {!loaded && loading ? (
            <p className="pane-status">불러오는 중...</p>
          ) : (
            <div className="settings-panel">
              {tab === "nodes" && <FleetNodesTable nodes={nodes} />}
              {tab === "recipes" && (
                <FleetRecipesTable
                  recipes={recipes}
                  busyAction={busyAction}
                  onAction={(recipe, action) => setConfirm({ recipe, action })}
                />
              )}
              {tab === "jobs" && (
                <FleetJobsTable
                  jobs={jobs}
                  expandedJob={expandedJob}
                  onToggle={(job) => setExpandedJob(expandedJob === job.id ? "" : job.id)}
                />
              )}
            </div>
          )}
        </>
      )}
      {confirm && (
        <ConfirmAction
          recipe={confirm.recipe}
          action={confirm.action}
          onClose={() => setConfirm(null)}
          onConfirm={() => void runRecipeAction(confirm.recipe, confirm.action)}
        />
      )}
    </>
  );
}

function FleetNodesTable({ nodes }: { nodes: FleetNode[] }) {
  const columns: Column<FleetNode>[] = [
    {
      header: "노드",
      width: 160,
      cell: (n) => (
        <span className="fleet-title-cell">
          <span className={"fleet-dot" + (n.reachable === false ? " off" : "")} />
          <span>
            <strong>{n.name}</strong>
            {n.error && <span className="fleet-sub danger">{n.error}</span>}
          </span>
        </span>
      ),
    },
    { header: "역할", width: 100, tdStyle: { color: "var(--muted)" }, cell: (n) => n.role ?? "" },
    { header: "GPU", width: 150, cell: gpuText },
    { header: "메모리", width: 130, cell: memoryText },
    { header: "서비스", width: 110, cell: servicesText },
    { header: "모델", cell: modelsText },
  ];
  if (nodes.length === 0) return <p className="pane-status">노드 정보가 없습니다.</p>;
  return <Grid columns={columns} rows={nodes} getKey={(n) => n.name} />;
}

function FleetRecipesTable({
  recipes,
  busyAction,
  onAction,
}: {
  recipes: FleetRecipe[];
  busyAction: string;
  onAction: (recipe: FleetRecipe, action: RecipeAction) => void;
}) {
  const columns: Column<FleetRecipe>[] = [
    {
      header: "레시피",
      cell: (r) => (
        <span>
          <strong>{r.name}</strong>
          {r.description && <span className="fleet-sub">{r.description}</span>}
        </span>
      ),
    },
    { header: "노드", width: 120, cell: recipeNode },
    { header: "상태", width: 92, cell: (r) => (r.status?.running ? "실행 중" : "중지") },
    { header: "vLLM", width: 148, tdStyle: { color: "var(--muted)", fontSize: 12 }, cell: vllmText },
    {
      header: "",
      width: 184,
      tdStyle: { textAlign: "right", whiteSpace: "nowrap" },
      cell: (r) => {
        const running = r.status?.running === true;
        const actionBusy = Boolean(busyAction);
        return (
          <span style={{ display: "inline-flex", gap: 2, justifyContent: "flex-end" }}>
            <RowBtn onClick={() => onAction(r, "launch")} disabled={running || actionBusy} title="기동">
              기동
            </RowBtn>
            <RowBtn onClick={() => onAction(r, "restart")} disabled={!running || actionBusy} title="재시작">
              재시작
            </RowBtn>
            <RowBtn onClick={() => onAction(r, "stop")} disabled={!running || actionBusy} danger title="중지">
              중지
            </RowBtn>
          </span>
        );
      },
    },
  ];
  if (recipes.length === 0) return <p className="pane-status">레시피가 없습니다.</p>;
  return <Grid columns={columns} rows={recipes} getKey={(r) => r.name} />;
}

function FleetJobsTable({
  jobs,
  expandedJob,
  onToggle,
}: {
  jobs: FleetJob[];
  expandedJob: string;
  onToggle: (job: FleetJob) => void;
}) {
  const recent = jobs.slice(0, 30);
  const columns: Column<FleetJob>[] = [
    {
      header: "작업",
      cell: (j) => (
        <span>
          <strong>{j.title || j.id}</strong>
          {j.id && j.title && <span className="fleet-sub">{j.id}</span>}
        </span>
      ),
    },
    { header: "상태", width: 96, cell: (j) => jobState(j) },
    { header: "시작", width: 116, tdStyle: { color: "var(--muted)", fontSize: 12 }, cell: (j) => fmtDate(j.startedAt) },
    { header: "종료", width: 116, tdStyle: { color: "var(--muted)", fontSize: 12 }, cell: (j) => fmtDate(j.endedAt) },
  ];
  if (recent.length === 0) return <p className="pane-status">작업이 없습니다.</p>;
  return (
    <Grid
      columns={columns}
      rows={recent}
      getKey={(j) => j.id}
      onRowClick={onToggle}
      isRowSelected={(j) => expandedJob === j.id}
      rowTitle={() => "로그 보기"}
      renderExpandedRow={(j) => <pre className="fleet-log">{j.log || "로그 없음"}</pre>}
    />
  );
}

function ConfirmAction({
  recipe,
  action,
  onClose,
  onConfirm,
}: {
  recipe: FleetRecipe;
  action: RecipeAction;
  onClose: () => void;
  onConfirm: () => void;
}) {
  return (
    <Modal
      title={`${recipe.name} ${actionLabel(action)}`}
      onClose={onClose}
      footer={<ModalFooter action={actionLabel(action)} onClose={onClose} onSubmit={onConfirm} />}
    >
      <Detail label="노드" value={recipeNode(recipe) || "—"} />
      <Detail label="상태" value={recipe.status?.running ? "실행 중" : "중지"} />
      {recipe.description && <Detail label="설명" value={recipe.description} multiline />}
    </Modal>
  );
}

function FleetMetric({ label, value }: { label: string; value: string }) {
  return (
    <div className="fleet-metric">
      <span>{label}</span>
      <strong>{value}</strong>
    </div>
  );
}

function asArray<T>(value: T[] | null | undefined): T[] {
  return Array.isArray(value) ? value : [];
}

function gpuText(node: FleetNode): string {
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

function memoryText(node: FleetNode): string {
  const mem = node.metrics?.memory;
  if (!mem?.totalKB) return "";
  const used = Math.max(0, mem.totalKB - (mem.availableKB ?? 0));
  return `${percent(used, mem.totalKB)}% · ${bytes(used * 1024)}/${bytes(mem.totalKB * 1024)}`;
}

function servicesText(node: FleetNode): string {
  const services = asArray(node.metrics?.services);
  if (services.length === 0) return "";
  const ok = services.filter((s) => s.ok).length;
  return `${ok}/${services.length}`;
}

function modelsText(node: FleetNode): string {
  const models = asArray(node.models);
  if (models.length === 0) return "";
  return models
    .map((m) => m.name ?? "")
    .filter(Boolean)
    .slice(0, 3)
    .join(", ");
}

function recipeNode(recipe: FleetRecipe): string {
  return recipe.status?.node || recipe.node || "";
}

function vllmText(recipe: FleetRecipe): string {
  const v = recipe.vllm;
  if (!v) return "";
  return [
    v.gpuMemoryUtilization != null ? `GPU ${v.gpuMemoryUtilization}` : "",
    v.maxModelLen != null ? `${v.maxModelLen} ctx` : "",
    v.maxNumSeqs != null ? `${v.maxNumSeqs} seq` : "",
  ]
    .filter(Boolean)
    .join(" · ");
}

function jobState(job: FleetJob): string {
  return (job.state || "").toLowerCase() || "unknown";
}

function actionLabel(action: RecipeAction): string {
  return action === "launch" ? "기동" : action === "restart" ? "재시작" : "중지";
}

function percent(used: number, total: number): number {
  if (!total) return 0;
  return Math.round((used / total) * 100);
}

function bytes(n: number): string {
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

function oneLine(text: string): string {
  return text.replace(/\s+/g, " ").trim().slice(0, 140);
}
