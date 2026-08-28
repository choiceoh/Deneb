import { type KeyboardEvent, useCallback, useEffect, useMemo, useRef, useState } from "react";

import { serializeList } from "@/aiText";
import { type FleetJob, type FleetNode, fleetJobs, fleetState } from "@/fleet";
import { errText } from "@/format";
import { useRegisterPane, useWorkspace } from "@/workspaceContext";
import {
  type FleetIssue,
  type FleetModelRow,
  type FleetServiceRow,
  type FleetView,
  type JobFilter,
  type ServiceFilter,
  FLEET_VIEWS,
  asArray,
  gpuText,
  jobState,
  jobStateLabel,
  memoryText,
  nodeHasIssue,
  nodeIssueText,
  nodeIssueView,
  oneLine,
} from "./fleetHelpers";
import {
  FleetJobsView,
  FleetMetric,
  FleetModelsView,
  FleetNodesView,
  FleetOverview,
  FleetServicesView,
} from "./FleetViews";

// SparkFleet 플릿 pane — 상태 폴링·파생 계산·탭 셸을 소유한다. 탭 뷰/카드는
// FleetViews.tsx, 타입·순수 헬퍼는 fleetHelpers.ts (순수 분할, 동작 동일).
export function FleetPane() {
  const { connected, cfg } = useWorkspace();
  const [nodes, setNodes] = useState<FleetNode[]>([]);
  const [jobs, setJobs] = useState<FleetJob[]>([]);
  const [loaded, setLoaded] = useState(false);
  const [loading, setLoading] = useState(false);
  const [stale, setStale] = useState(false);
  const [error, setError] = useState("");
  const [expandedJob, setExpandedJob] = useState("");
  const [view, setView] = useState<FleetView>("overview");
  const [nodeQuery, setNodeQuery] = useState("");
  const [nodeProblemsOnly, setNodeProblemsOnly] = useState(false);
  const [modelQuery, setModelQuery] = useState("");
  const [serviceFilter, setServiceFilter] = useState<ServiceFilter>("all");
  const [jobFilter, setJobFilter] = useState<JobFilter>("all");
  const loadedRef = useRef(false);
  const refreshSeqRef = useRef(0);

  const refresh = useCallback(async () => {
    if (!connected) return;
    const seq = ++refreshSeqRef.current;
    setLoading(true);
    setError("");
    const [stateResult, jobsResult] = await Promise.allSettled([fleetState(cfg), fleetJobs(cfg)]);
    if (seq !== refreshSeqRef.current) return;
    let successCount = 0;
    const failures: unknown[] = [];
    if (stateResult.status === "fulfilled") {
      setNodes(asArray(stateResult.value.nodes));
      successCount += 1;
    } else failures.push(stateResult.reason);
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

  // Disconnect wipes the visible load state during render (adjust pattern);
  // the effect keeps only the ref bookkeeping and the polling loop.
  const [prevFleetConn, setPrevFleetConn] = useState(connected);
  if (prevFleetConn !== connected) {
    setPrevFleetConn(connected);
    if (!connected) {
      setLoaded(false);
      setLoading(false);
      setStale(false);
      setError("");
    }
  }

  useEffect(() => {
    if (!connected) {
      refreshSeqRef.current += 1;
      loadedRef.current = false;
      return;
    }
    // First load deferred one microtask (still pre-paint) — refresh() is shared
    // with handlers and the interval, and its body trips the lint's
    // interprocedural sync-setState pass.
    queueMicrotask(() => void refresh());
    const id = window.setInterval(() => void refresh(), 7_000);
    return () => {
      refreshSeqRef.current += 1;
      window.clearInterval(id);
    };
  }, [connected, refresh]);

  const runningJobs = jobs.filter((j) => jobState(j) === "running").length;
  const failedJobs = jobs.filter((j) => jobState(j) === "failed").length;
  const reachableNodes = nodes.filter((n) => n.reachable !== false).length;
  const downNodes = nodes.length - reachableNodes;
  const latestJob = jobs[0];
  const nodeIssues = nodes.filter(nodeHasIssue);
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
  const filteredJobs = jobs.filter((job) => jobFilter === "all" || jobState(job) === jobFilter);
  const viewCounts: Record<FleetView, number> = {
    overview: issues.length,
    nodes: nodes.length,
    models: modelRows.length,
    services: serviceRows.length,
    jobs: jobs.length,
  };

  function onViewKey(e: KeyboardEvent<HTMLButtonElement>, idx: number) {
    const last = FLEET_VIEWS.length - 1;
    let next: number;
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
    const jobText = serializeList(
      "플릿 작업",
      jobs.slice(0, 8),
      (j) => `- ${j.title || j.id}: ${j.state || "unknown"}${j.log ? ` · ${oneLine(j.log)}` : ""}`,
    );
    return [nodeText, jobText].filter(Boolean).join("\n\n");
  }, [jobs, nodes]);
  useRegisterPane("fleet", aiText);

  return (
    <>
      <div className="fleet-head">
        <div>
          <h2 style={{ margin: 0 }}>플릿</h2>
          <p className="fleet-subtitle">SparkFleet 노드·모델·최근 작업 (레시피 제어는 채팅의 AI에게)</p>
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
                    recentJobs={jobs.slice(0, 6)}
                    expandedJob={expandedJob}
                    onView={setView}
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
    </>
  );
}
