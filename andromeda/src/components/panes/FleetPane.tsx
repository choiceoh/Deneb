import { type KeyboardEvent, useCallback, useEffect, useMemo, useRef, useState } from "react";

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
import { Detail, Modal, ModalFooter } from "@/components/Modal";

type RecipeAction = "launch" | "stop" | "restart";
type FleetView = "overview" | "nodes" | "models" | "services" | "recipes" | "jobs";
type RecipeFilter = "all" | "running" | "stopped";
type JobFilter = "all" | "running" | "done" | "failed";
type ServiceFilter = "all" | "healthy" | "down";

const FLEET_VIEWS: { key: FleetView; label: string }[] = [
  { key: "overview", label: "개요" },
  { key: "nodes", label: "노드" },
  { key: "models", label: "모델" },
  { key: "services", label: "서비스" },
  { key: "recipes", label: "레시피" },
  { key: "jobs", label: "작업" },
];

interface FleetIssue {
  key: string;
  title: string;
  detail: string;
  view: FleetView;
  tone: "bad" | "warn";
}

interface FleetModelRow {
  key: string;
  name: string;
  sizeBytes?: number;
  nodeName: string;
  nodeRole?: string;
  nodeReachable: boolean;
}

interface FleetServiceRow {
  key: string;
  name: string;
  ok: boolean;
  nodeName: string;
  nodeRole?: string;
  nodeReachable: boolean;
}

export function FleetPane() {
  const { connected, cfg } = useWorkspace();
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
  const [view, setView] = useState<FleetView>("overview");
  const [nodeQuery, setNodeQuery] = useState("");
  const [nodeProblemsOnly, setNodeProblemsOnly] = useState(false);
  const [modelQuery, setModelQuery] = useState("");
  const [serviceFilter, setServiceFilter] = useState<ServiceFilter>("all");
  const [recipeQuery, setRecipeQuery] = useState("");
  const [recipeFilter, setRecipeFilter] = useState<RecipeFilter>("all");
  const [jobFilter, setJobFilter] = useState<JobFilter>("all");
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

  const runningRecipes = recipes.filter((r) => r.status?.running).length;
  const runningJobs = jobs.filter((j) => jobState(j) === "running").length;
  const failedJobs = jobs.filter((j) => jobState(j) === "failed").length;
  const reachableNodes = nodes.filter((n) => n.reachable !== false).length;
  const downNodes = nodes.length - reachableNodes;
  const latestJob = jobs[0];
  const nodeIssues = nodes.filter(nodeHasIssue);
  const runningRecipeList = recipes.filter((r) => r.status?.running);
  const missingWeightRecipes = recipes.filter((r) => r.status?.weightsPresent === false);
  const failedJobList = jobs.filter((j) => jobState(j) === "failed");
  const modelRows: FleetModelRow[] = nodes.flatMap((node) =>
    asArray(node.models).map((model, idx) => ({
      key: `${node.name}:${model.name || idx}`,
      name: model.name || "model",
      sizeBytes: model.sizeBytes,
      nodeName: node.name,
      nodeRole: node.role,
      nodeReachable: node.reachable !== false,
    })),
  );
  const serviceRows: FleetServiceRow[] = nodes.flatMap((node) => {
    const services = asArray(node.metrics?.services);
    if (services.length === 0 && node.reachable === false) {
      return [
        {
          key: `${node.name}:node`,
          name: "node",
          ok: false,
          nodeName: node.name,
          nodeRole: node.role,
          nodeReachable: false,
        },
      ];
    }
    return services.map((service, idx) => ({
      key: `${node.name}:${service.name || idx}`,
      name: service.name || "service",
      ok: service.ok !== false,
      nodeName: node.name,
      nodeRole: node.role,
      nodeReachable: node.reachable !== false,
    }));
  });
  const issues: FleetIssue[] = [
    ...nodeIssues.map((node) => ({
      key: `node:${node.name}`,
      title: node.name,
      detail: nodeIssueText(node),
      view: nodeIssueView(node),
      tone: node.reachable === false || node.error ? ("bad" as const) : ("warn" as const),
    })),
    ...missingWeightRecipes.map((recipe) => ({
      key: `recipe:${recipe.name}`,
      title: recipe.name,
      detail: "가중치 없음",
      view: "recipes" as const,
      tone: "warn" as const,
    })),
    ...failedJobList.slice(0, 5).map((job) => ({
      key: `job:${job.id}`,
      title: job.title || job.id,
      detail: oneLine(job.log || "실패한 작업"),
      view: "jobs" as const,
      tone: "bad" as const,
    })),
  ];
  const filteredNodes = nodes.filter((node) => {
    const q = nodeQuery.trim().toLowerCase();
    const matches =
      !q ||
      [node.name, node.role, node.error, ...asArray(node.models).map((m) => m.name)]
        .filter(Boolean)
        .some((value) => String(value).toLowerCase().includes(q));
    return matches && (!nodeProblemsOnly || nodeHasIssue(node));
  });
  const filteredModels = modelRows.filter((model) => {
    const q = modelQuery.trim().toLowerCase();
    return (
      !q ||
      [model.name, model.nodeName, model.nodeRole]
        .filter(Boolean)
        .some((value) => String(value).toLowerCase().includes(q))
    );
  });
  const filteredServices = serviceRows.filter((service) => {
    if (serviceFilter === "healthy") return service.ok && service.nodeReachable;
    if (serviceFilter === "down") return !service.ok || !service.nodeReachable;
    return true;
  });
  const filteredRecipes = recipes.filter((recipe) => {
    const q = recipeQuery.trim().toLowerCase();
    const running = recipe.status?.running === true;
    const matchesFilter = recipeFilter === "all" || (recipeFilter === "running" ? running : !running);
    const matchesQuery =
      !q ||
      [recipe.name, recipe.description, recipe.node, recipe.status?.node, recipe.container]
        .filter(Boolean)
        .some((value) => String(value).toLowerCase().includes(q));
    return matchesFilter && matchesQuery;
  });
  const filteredJobs = jobs.filter((job) => jobFilter === "all" || jobState(job) === jobFilter);
  const viewCounts: Record<FleetView, number> = {
    overview: issues.length,
    nodes: nodes.length,
    models: modelRows.length,
    services: serviceRows.length,
    recipes: recipes.length,
    jobs: jobs.length,
  };

  function onViewKey(e: KeyboardEvent<HTMLButtonElement>, idx: number) {
    const last = FLEET_VIEWS.length - 1;
    let next = idx;
    if (e.key === "ArrowRight") next = idx === last ? 0 : idx + 1;
    else if (e.key === "ArrowLeft") next = idx === 0 ? last : idx - 1;
    else if (e.key === "Home") next = 0;
    else if (e.key === "End") next = last;
    else return;
    e.preventDefault();
    const key = FLEET_VIEWS[next].key;
    setView(key);
    document.getElementById(`fleet-tab-${key}`)?.focus();
  }

  const aiText = useMemo(() => {
    const nodeText = serializeList(
      "플릿 노드",
      nodes,
      (n) =>
        `- ${n.name}${n.role ? ` (${n.role})` : ""}: ${n.reachable === false ? "연결 안 됨" : "연결됨"}` +
        `${gpuText(n) ? ` · ${gpuText(n)}` : ""}` +
        `${memoryText(n) ? ` · ${memoryText(n)}` : ""}` +
        `${n.error ? ` · 오류 ${n.error}` : ""}`,
      "대",
    );
    const recipeText = serializeList(
      "플릿 레시피",
      recipes,
      (r) =>
        `- ${r.name}: ${r.status?.running ? "실행 중" : "중지"}` +
        `${recipeNode(r) ? ` · ${recipeNode(r)}` : ""}` +
        `${vllmText(r) ? ` · ${vllmText(r)}` : ""}` +
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

  return (
    <>
      <div className="fleet-head">
        <div>
          <h2 style={{ margin: 0 }}>플릿</h2>
          <p className="fleet-subtitle">SparkFleet 노드, GPU 레시피, 최근 작업</p>
        </div>
        <button className="btn" type="button" onClick={refresh} disabled={!connected || loading}>
          {loading ? "새로고침 중" : "새로고침"}
        </button>
      </div>

      {!connected ? (
        <div className="fleet-empty">게이트웨이에 연결하면 GPU 플릿 상태가 표시됩니다.</div>
      ) : (
        <>
          <div className="fleet-summary" aria-label="플릿 요약">
            <FleetMetric
              label="노드"
              value={`${reachableNodes}/${nodes.length || 0}`}
              hint={downNodes ? `${downNodes}대 확인 필요` : "모두 응답"}
              tone={downNodes ? "bad" : "ok"}
            />
            <FleetMetric
              label="레시피"
              value={`${runningRecipes}/${recipes.length || 0}`}
              hint="실행 중 / 전체"
              tone={runningRecipes ? "ok" : "neutral"}
            />
            <FleetMetric
              label="작업"
              value={String(runningJobs)}
              hint={failedJobs ? `최근 실패 ${failedJobs}` : "진행 중"}
              tone={failedJobs ? "bad" : runningJobs ? "warn" : "neutral"}
            />
            <FleetMetric
              label="최근"
              value={latestJob ? jobStateLabel(jobState(latestJob)) : "—"}
              hint={latestJob?.title || "작업 없음"}
              tone={latestJob && jobState(latestJob) === "failed" ? "bad" : "neutral"}
            />
          </div>

          {stale && <div className="fleet-banner error">플릿 연결 끊김 · 마지막 데이터를 표시 중입니다.</div>}
          {error && <div className="fleet-banner error">오류: {error}</div>}
          {notice && <div className="fleet-banner">{notice}</div>}

          {!loaded && loading ? (
            <div className="fleet-empty">플릿 상태를 불러오는 중…</div>
          ) : (
            <>
              <div className="fleet-tabs" role="tablist" aria-label="플릿 보기">
                {FLEET_VIEWS.map((tab, idx) => (
                  <button
                    key={tab.key}
                    type="button"
                    role="tab"
                    id={`fleet-tab-${tab.key}`}
                    aria-selected={view === tab.key}
                    aria-controls="fleet-panel"
                    tabIndex={view === tab.key ? 0 : -1}
                    className={"fleet-tab" + (view === tab.key ? " active" : "")}
                    onClick={() => setView(tab.key)}
                    onKeyDown={(e) => onViewKey(e, idx)}
                  >
                    <span>{tab.label}</span>
                    <small>{viewCounts[tab.key]}</small>
                  </button>
                ))}
              </div>

              <div
                key={view}
                id="fleet-panel"
                role="tabpanel"
                aria-labelledby={`fleet-tab-${view}`}
                className="fleet-panel"
              >
                {view === "overview" && (
                  <FleetOverview
                    issues={issues}
                    runningRecipes={runningRecipeList}
                    recentJobs={jobs.slice(0, 6)}
                    busyAction={busyAction}
                    expandedJob={expandedJob}
                    onView={setView}
                    onRecipeAction={(recipe, action) => setConfirm({ recipe, action })}
                    onJobToggle={(jobId) => setExpandedJob(expandedJob === jobId ? "" : jobId)}
                  />
                )}
                {view === "nodes" && (
                  <FleetNodesView
                    nodes={filteredNodes}
                    total={nodes.length}
                    query={nodeQuery}
                    problemsOnly={nodeProblemsOnly}
                    onQuery={setNodeQuery}
                    onProblemsOnly={setNodeProblemsOnly}
                  />
                )}
                {view === "models" && (
                  <FleetModelsView
                    models={filteredModels}
                    total={modelRows.length}
                    query={modelQuery}
                    onQuery={setModelQuery}
                  />
                )}
                {view === "services" && (
                  <FleetServicesView
                    services={filteredServices}
                    total={serviceRows.length}
                    filter={serviceFilter}
                    onFilter={setServiceFilter}
                  />
                )}
                {view === "recipes" && (
                  <FleetRecipesView
                    recipes={filteredRecipes}
                    total={recipes.length}
                    query={recipeQuery}
                    filter={recipeFilter}
                    busyAction={busyAction}
                    onQuery={setRecipeQuery}
                    onFilter={setRecipeFilter}
                    onAction={(recipe, action) => setConfirm({ recipe, action })}
                  />
                )}
                {view === "jobs" && (
                  <FleetJobsView
                    jobs={filteredJobs}
                    total={jobs.length}
                    filter={jobFilter}
                    expandedJob={expandedJob}
                    onFilter={setJobFilter}
                    onToggle={(jobId) => setExpandedJob(expandedJob === jobId ? "" : jobId)}
                  />
                )}
              </div>
            </>
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

function FleetMetric({
  label,
  value,
  hint,
  tone,
}: {
  label: string;
  value: string;
  hint: string;
  tone: "ok" | "warn" | "bad" | "neutral";
}) {
  return (
    <div className={`fleet-metric ${tone}`}>
      <span>{label}</span>
      <strong>{value}</strong>
      <small>{hint}</small>
    </div>
  );
}

function FleetOverview({
  issues,
  runningRecipes,
  recentJobs,
  busyAction,
  expandedJob,
  onView,
  onRecipeAction,
  onJobToggle,
}: {
  issues: FleetIssue[];
  runningRecipes: FleetRecipe[];
  recentJobs: FleetJob[];
  busyAction: string;
  expandedJob: string;
  onView: (view: FleetView) => void;
  onRecipeAction: (recipe: FleetRecipe, action: RecipeAction) => void;
  onJobToggle: (jobId: string) => void;
}) {
  return (
    <div className="fleet-overview-grid">
      <section className="fleet-section" aria-label="확인 필요">
        <FleetSectionTitle title="확인 필요" count={issues.length} />
        <div className="fleet-list">
          {issues.length ? (
            issues.slice(0, 8).map((issue) => (
              <button key={issue.key} type="button" className="fleet-issue-row" onClick={() => onView(issue.view)}>
                <span className={`fleet-state ${issue.tone}`}>{issue.tone === "bad" ? "위험" : "주의"}</span>
                <span className="fleet-issue-copy">
                  <strong>{issue.title}</strong>
                  <small>{issue.detail}</small>
                </span>
              </button>
            ))
          ) : (
            <div className="fleet-empty compact">확인할 항목이 없습니다.</div>
          )}
        </div>
      </section>

      <section className="fleet-section" aria-label="실행 중 레시피">
        <FleetSectionTitle title="실행 중 레시피" count={runningRecipes.length} />
        <div className="fleet-list">
          {runningRecipes.length ? (
            runningRecipes
              .slice(0, 4)
              .map((recipe) => (
                <FleetRecipeCard
                  key={recipe.name}
                  recipe={recipe}
                  busyAction={busyAction}
                  onAction={(action) => onRecipeAction(recipe, action)}
                />
              ))
          ) : (
            <div className="fleet-empty compact">실행 중인 레시피가 없습니다.</div>
          )}
        </div>
      </section>

      <section className="fleet-section wide" aria-label="최근 작업">
        <FleetSectionTitle title="최근 작업" count={recentJobs.length} />
        <div className="fleet-list">
          {recentJobs.length ? (
            recentJobs.map((job) => (
              <FleetJobCard
                key={job.id}
                job={job}
                expanded={expandedJob === job.id}
                onToggle={() => onJobToggle(job.id)}
              />
            ))
          ) : (
            <div className="fleet-empty compact">작업이 없습니다.</div>
          )}
        </div>
      </section>
    </div>
  );
}

function FleetNodesView({
  nodes,
  total,
  query,
  problemsOnly,
  onQuery,
  onProblemsOnly,
}: {
  nodes: FleetNode[];
  total: number;
  query: string;
  problemsOnly: boolean;
  onQuery: (query: string) => void;
  onProblemsOnly: (enabled: boolean) => void;
}) {
  return (
    <section className="fleet-section" aria-label="노드">
      <div className="fleet-toolbar">
        <label className="fleet-search">
          <span>검색</span>
          <input
            className="field"
            value={query}
            onChange={(e) => onQuery(e.target.value)}
            placeholder="노드, 역할, 모델"
          />
        </label>
        <label className="fleet-check">
          <input type="checkbox" checked={problemsOnly} onChange={(e) => onProblemsOnly(e.target.checked)} />
          문제만
        </label>
        <span className="fleet-count">
          {nodes.length} / {total}
        </span>
      </div>
      <div className="fleet-card-grid">
        {nodes.length ? (
          nodes.map((node) => <FleetNodeCard key={node.name} node={node} />)
        ) : (
          <div className="fleet-empty compact">조건에 맞는 노드가 없습니다.</div>
        )}
      </div>
    </section>
  );
}

function FleetModelsView({
  models,
  total,
  query,
  onQuery,
}: {
  models: FleetModelRow[];
  total: number;
  query: string;
  onQuery: (query: string) => void;
}) {
  return (
    <section className="fleet-section" aria-label="모델">
      <div className="fleet-toolbar">
        <label className="fleet-search">
          <span>검색</span>
          <input
            className="field"
            value={query}
            onChange={(e) => onQuery(e.target.value)}
            placeholder="모델, 노드, 역할"
          />
        </label>
        <span className="fleet-count">
          {models.length} / {total}
        </span>
      </div>
      <div className="fleet-card-grid">
        {models.length ? (
          models.map((model) => (
            <article key={model.key} className={"fleet-card" + (model.nodeReachable ? "" : " danger")}>
              <div className="fleet-row-head">
                <span className={"fleet-dot" + (model.nodeReachable ? "" : " off")} />
                <div className="fleet-row-title">
                  <strong>{model.name}</strong>
                  <span>{model.nodeName}</span>
                </div>
                {model.sizeBytes ? <span className="fleet-pill">{bytes(model.sizeBytes)}</span> : null}
              </div>
              <div className="fleet-chip-row">
                <span className="fleet-chip">{model.nodeName}</span>
                {model.nodeRole ? <span className="fleet-chip">{model.nodeRole}</span> : null}
                {!model.nodeReachable ? <span className="fleet-chip">offline</span> : null}
              </div>
            </article>
          ))
        ) : (
          <div className="fleet-empty compact">조건에 맞는 모델이 없습니다.</div>
        )}
      </div>
    </section>
  );
}

function FleetServicesView({
  services,
  total,
  filter,
  onFilter,
}: {
  services: FleetServiceRow[];
  total: number;
  filter: ServiceFilter;
  onFilter: (filter: ServiceFilter) => void;
}) {
  return (
    <section className="fleet-section" aria-label="서비스">
      <div className="fleet-toolbar">
        <div className="fleet-filter" role="group" aria-label="서비스 상태">
          {(["all", "healthy", "down"] as const).map((key) => (
            <button key={key} type="button" className={filter === key ? "active" : ""} onClick={() => onFilter(key)}>
              {serviceFilterLabel(key)}
            </button>
          ))}
        </div>
        <span className="fleet-count">
          {services.length} / {total}
        </span>
      </div>
      <div className="fleet-card-grid">
        {services.length ? (
          services.map((service) => (
            <article
              key={service.key}
              className={"fleet-card" + (service.ok && service.nodeReachable ? "" : " danger")}
            >
              <div className="fleet-row-head">
                <span className={"fleet-dot" + (service.ok && service.nodeReachable ? "" : " off")} />
                <div className="fleet-row-title">
                  <strong>{service.name}</strong>
                  <span>{service.nodeName}</span>
                </div>
                <span className="fleet-pill">{service.ok && service.nodeReachable ? "healthy" : "down"}</span>
              </div>
              <div className="fleet-chip-row">
                <span className="fleet-chip">{service.nodeName}</span>
                {service.nodeRole ? <span className="fleet-chip">{service.nodeRole}</span> : null}
                {!service.nodeReachable ? <span className="fleet-chip">node offline</span> : null}
              </div>
            </article>
          ))
        ) : (
          <div className="fleet-empty compact">조건에 맞는 서비스가 없습니다.</div>
        )}
      </div>
    </section>
  );
}

function FleetRecipesView({
  recipes,
  total,
  query,
  filter,
  busyAction,
  onQuery,
  onFilter,
  onAction,
}: {
  recipes: FleetRecipe[];
  total: number;
  query: string;
  filter: RecipeFilter;
  busyAction: string;
  onQuery: (query: string) => void;
  onFilter: (filter: RecipeFilter) => void;
  onAction: (recipe: FleetRecipe, action: RecipeAction) => void;
}) {
  return (
    <section className="fleet-section" aria-label="레시피">
      <div className="fleet-toolbar">
        <label className="fleet-search">
          <span>검색</span>
          <input
            className="field"
            value={query}
            onChange={(e) => onQuery(e.target.value)}
            placeholder="레시피, 노드, 컨테이너"
          />
        </label>
        <div className="fleet-filter" role="group" aria-label="레시피 상태">
          {(["all", "running", "stopped"] as const).map((key) => (
            <button key={key} type="button" className={filter === key ? "active" : ""} onClick={() => onFilter(key)}>
              {recipeFilterLabel(key)}
            </button>
          ))}
        </div>
        <span className="fleet-count">
          {recipes.length} / {total}
        </span>
      </div>
      <div className="fleet-card-grid">
        {recipes.length ? (
          recipes.map((recipe) => (
            <FleetRecipeCard
              key={recipe.name}
              recipe={recipe}
              busyAction={busyAction}
              onAction={(action) => onAction(recipe, action)}
            />
          ))
        ) : (
          <div className="fleet-empty compact">조건에 맞는 레시피가 없습니다.</div>
        )}
      </div>
    </section>
  );
}

function FleetJobsView({
  jobs,
  total,
  filter,
  expandedJob,
  onFilter,
  onToggle,
}: {
  jobs: FleetJob[];
  total: number;
  filter: JobFilter;
  expandedJob: string;
  onFilter: (filter: JobFilter) => void;
  onToggle: (jobId: string) => void;
}) {
  return (
    <section className="fleet-section" aria-label="작업">
      <div className="fleet-toolbar">
        <div className="fleet-filter" role="group" aria-label="작업 상태">
          {(["all", "running", "done", "failed"] as const).map((key) => (
            <button key={key} type="button" className={filter === key ? "active" : ""} onClick={() => onFilter(key)}>
              {jobFilterLabel(key)}
            </button>
          ))}
        </div>
        <span className="fleet-count">
          {jobs.length} / {total}
        </span>
      </div>
      <div className="fleet-list relaxed">
        {jobs.length ? (
          jobs.map((job) => (
            <FleetJobCard key={job.id} job={job} expanded={expandedJob === job.id} onToggle={() => onToggle(job.id)} />
          ))
        ) : (
          <div className="fleet-empty compact">조건에 맞는 작업이 없습니다.</div>
        )}
      </div>
    </section>
  );
}

function FleetSectionTitle({ title, count }: { title: string; count: number }) {
  return (
    <div className="fleet-section-title">
      <h3>{title}</h3>
      <span>{count}</span>
    </div>
  );
}

function FleetNodeCard({ node }: { node: FleetNode }) {
  const metrics = node.metrics ?? {};
  const gpus = asArray(metrics.gpus);
  const memory = metrics.memory ?? null;
  const disk = asArray(metrics.disks)[0];
  const services = asArray(metrics.services);
  const downServices = services.filter((s) => !s.ok);
  const models = asArray(node.models);
  const memUsed = memory?.totalKB ? Math.max(0, memory.totalKB - (memory.availableKB ?? 0)) : 0;
  const issue = nodeHasIssue(node);

  return (
    <article className={"fleet-card" + (issue ? " danger" : "")}>
      <div className="fleet-row-head">
        <span className={"fleet-dot" + (node.reachable === false ? " off" : "")} />
        <div className="fleet-row-title">
          <strong>{node.name}</strong>
          <span>{node.role || "node"}</span>
        </div>
        <span className="fleet-pill">{node.reachable === false ? "offline" : "online"}</span>
      </div>
      {gpus.length > 0 && (
        <div className="fleet-chip-row">
          {gpus.map((gpu, idx) => (
            <span key={gpu.index ?? idx} className="fleet-chip">
              GPU{gpu.index ?? 0}
              {gpu.utilPct != null ? ` · ${gpu.utilPct}%` : ""}
              {gpu.tempC != null ? ` · ${gpu.tempC}°C` : ""}
            </span>
          ))}
        </div>
      )}
      {memory?.totalKB ? (
        <FleetBar
          label="메모리"
          value={`${bytes(memUsed * 1024)} / ${bytes(memory.totalKB * 1024)}`}
          pct={percent(memUsed, memory.totalKB)}
        />
      ) : null}
      {disk?.usePct != null && (
        <FleetBar label="디스크" value={`${disk.usePct}% · ${disk.path || "/"}`} pct={disk.usePct} />
      )}
      {models.length > 0 && (
        <div className="fleet-model-list" aria-label={`${node.name} 모델`}>
          {models.slice(0, 5).map((model) => (
            <span key={model.name || model.sizeBytes} className="fleet-model">
              {model.name || "model"}
              {model.sizeBytes ? <small>{bytes(model.sizeBytes)}</small> : null}
            </span>
          ))}
        </div>
      )}
      {services.length > 0 && (
        <div className="fleet-service-list" aria-label={`${node.name} 서비스`}>
          {services.map((service, idx) => (
            <span key={service.name || idx} className={"fleet-service" + (service.ok ? "" : " down")}>
              {service.name || "service"}
            </span>
          ))}
        </div>
      )}
      {downServices.length > 0 && (
        <p className="fleet-card-error">
          다운:{" "}
          {downServices
            .map((s) => s.name)
            .filter(Boolean)
            .join(", ")}
        </p>
      )}
      {node.error && <p className="fleet-card-error">{node.error}</p>}
    </article>
  );
}

function FleetRecipeCard({
  recipe,
  busyAction,
  onAction,
}: {
  recipe: FleetRecipe;
  busyAction: string;
  onAction: (action: RecipeAction) => void;
}) {
  const running = recipe.status?.running === true;
  const noWeights = recipe.status?.weightsPresent === false;
  const actionBusy = Boolean(busyAction);
  return (
    <article className={"fleet-card" + (running ? " active" : noWeights ? " danger" : "")}>
      <div className="fleet-row-head">
        <span className={"fleet-dot" + (running ? "" : " idle")} />
        <div className="fleet-row-title">
          <strong>{recipe.name}</strong>
          <span>{recipe.description || recipeNode(recipe) || "recipe"}</span>
        </div>
        <span className="fleet-pill">{running ? "running" : noWeights ? "no weights" : "idle"}</span>
      </div>
      <div className="fleet-chip-row">
        {recipeNode(recipe) && <span className="fleet-chip">{recipeNode(recipe)}</span>}
        {recipe.port ? <span className="fleet-chip">:{recipe.port}</span> : null}
        {recipe.container ? <span className="fleet-chip">{recipe.container}</span> : null}
        {vllmText(recipe) && <span className="fleet-chip">{vllmText(recipe)}</span>}
      </div>
      {recipe.description && <p className="fleet-card-note">{recipe.description}</p>}
      <div className="fleet-actions">
        {running ? (
          <>
            <button
              className="btn"
              type="button"
              aria-label={`${recipe.name} 재시작`}
              onClick={() => onAction("restart")}
              disabled={actionBusy}
            >
              재시작
            </button>
            <button
              className="btn"
              type="button"
              aria-label={`${recipe.name} 중지`}
              onClick={() => onAction("stop")}
              disabled={actionBusy}
            >
              중지
            </button>
          </>
        ) : (
          <button
            className="btn btn-accent"
            type="button"
            aria-label={`${recipe.name} 기동`}
            onClick={() => onAction("launch")}
            disabled={actionBusy}
          >
            기동
          </button>
        )}
      </div>
    </article>
  );
}

function FleetJobCard({ job, expanded, onToggle }: { job: FleetJob; expanded: boolean; onToggle: () => void }) {
  const state = jobState(job);
  return (
    <article className="fleet-card">
      <button className="fleet-job-toggle" type="button" onClick={onToggle} aria-expanded={expanded}>
        <span className={`fleet-state ${state}`}>{jobStateLabel(state)}</span>
        <span className="fleet-job-title">{job.title || job.id}</span>
      </button>
      <p className="fleet-card-note">
        {[fmtDate(job.startedAt), job.endedAt ? `종료 ${fmtDate(job.endedAt)}` : ""].filter(Boolean).join(" · ") ||
          job.id}
      </p>
      {expanded && <pre className="fleet-log">{job.log || "로그 없음"}</pre>}
    </article>
  );
}

function FleetBar({ label, value, pct }: { label: string; value: string; pct: number }) {
  return (
    <div className="fleet-bar-row">
      <div className="fleet-bar-meta">
        <span>{label}</span>
        <span>{value}</span>
      </div>
      <div className="fleet-bar" aria-hidden="true">
        <span style={{ width: `${clamp(pct)}%` }} />
      </div>
    </div>
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

function jobStateLabel(state: string): string {
  if (state === "running") return "진행";
  if (state === "done") return "완료";
  if (state === "failed") return "실패";
  return state;
}

function recipeFilterLabel(filter: RecipeFilter): string {
  if (filter === "running") return "실행";
  if (filter === "stopped") return "중지";
  return "전체";
}

function jobFilterLabel(filter: JobFilter): string {
  if (filter === "running") return "진행";
  if (filter === "done") return "완료";
  if (filter === "failed") return "실패";
  return "전체";
}

function serviceFilterLabel(filter: ServiceFilter): string {
  if (filter === "healthy") return "정상";
  if (filter === "down") return "다운";
  return "전체";
}

function nodeHasIssue(node: FleetNode): boolean {
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

function nodeIssueText(node: FleetNode): string {
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

function nodeIssueView(node: FleetNode): FleetView {
  if (node.reachable === false || node.error) return "nodes";
  if (asArray(node.metrics?.services).some((service) => service.ok === false)) return "services";
  return "nodes";
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

function clamp(n: number): number {
  return Math.max(0, Math.min(100, Number.isFinite(n) ? n : 0));
}
