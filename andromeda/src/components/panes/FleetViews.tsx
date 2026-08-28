import { type FleetJob, type FleetNode } from "@/fleet";
import { fmtDate } from "@/format";
import {
  type FleetIssue,
  type FleetModelRow,
  type FleetServiceRow,
  type FleetView,
  type JobFilter,
  type ServiceFilter,
  asArray,
  bytes,
  clamp,
  jobFilterLabel,
  jobState,
  jobStateLabel,
  nodeHasIssue,
  percent,
  serviceFilterLabel,
} from "./fleetHelpers";

// FleetPane's tab views and cards — split from FleetPane.tsx so the pane file
// keeps only state/polling/derivation and stays within the size guideline.
// The cards (node/job/bar/section-title) are internal to the views.

export function FleetMetric({
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

export function FleetOverview({
  issues,
  recentJobs,
  expandedJob,
  onView,
  onJobToggle,
}: {
  issues: FleetIssue[];
  recentJobs: FleetJob[];
  expandedJob: string;
  onView: (view: FleetView) => void;
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

export function FleetNodesView({
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

export function FleetModelsView({
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

export function FleetServicesView({
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

export function FleetJobsView({
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
