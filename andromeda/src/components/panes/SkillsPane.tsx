import { useCallback, useEffect, useMemo, useState } from "react";
import type {
  SkillRow,
  SkillDetailResponse,
  SkillLifecycleEvent,
  SkillsLifecycleResponse,
  PropusLifecycleSummary,
} from "@/types";
import { callRpc } from "@/gateway";
import { SKILLS_RPC } from "@/resources";
import { useCachedList } from "@/cachedList";
import { serializeList } from "@/aiText";
import { errText, fmtDate } from "@/format";
import { color } from "@/theme";
import { useRegisterPane, useWorkspace } from "@/workspaceContext";
import { Column, Grid, GridNotice } from "@/components/Grid";
import { Modal } from "@/components/Modal";
import { AssistantText } from "@/components/DenebUi";

// SkillsPane mirrors the native client's Settings "스킬" tab (ConfigSkillsTab):
// a catalog of the skills the agent can use plus the Propus lifecycle log. The
// list flows through the resource registry (miniapp.skills.list); detail,
// lifecycle, and the guarded local-skill edit/delete are query-driven RPCs the
// pane calls directly. Copy and field labels track the native tab so the two
// surfaces read the same.

// --- label helpers (verbatim native copy) --------------------------------

const SOURCE_LABELS: Record<string, string> = {
  managed: "관리형",
  workspace: "워크스페이스",
  "agents-skills-personal": "개인",
  "agents-skills-project": "프로젝트",
  bundled: "기본 제공",
  plugin: "플러그인",
  extra: "추가",
};
function sourceLabel(source?: string): string {
  const key = (source ?? "").trim();
  if (!key) return "";
  return SOURCE_LABELS[key] ?? key;
}

const LIFECYCLE_TYPES: Record<string, { label: string; bg: string; fg: string }> = {
  genesis: { label: "생성", bg: "var(--accent-soft)", fg: "var(--accent)" },
  evolved: { label: "진화", bg: "var(--accent-soft)", fg: "var(--accent)" },
  evolve_rejected: { label: "기각", bg: "var(--accent-soft)", fg: "var(--due)" },
  evolve_rolled_back: { label: "롤백", bg: "var(--accent-soft)", fg: "var(--ink-2)" },
};
function lifecycleType(type?: string): { label: string; bg: string; fg: string } {
  return LIFECYCLE_TYPES[(type ?? "").trim()] ?? { label: "리뷰", bg: "var(--panel)", fg: "var(--muted)" };
}

function routeLabel(route?: string): string {
  switch ((route ?? "").trim()) {
    case "no-op":
      return "판정: 변경 없음";
    case "evolve":
      return "판정: 기존 스킬 진화";
    case "create":
    case "genesis":
      return "판정: 새 스킬 생성";
    default:
      return route?.trim() ? `판정: ${route.trim()}` : "";
  }
}

const PROPUS_STATES: Record<string, string> = {
  idle: "아직 관찰 대기 중",
  steady: "정상 관찰 중",
  has_backlog: "개선 후보 대기",
  needs_validation: "검증 필요",
  needs_evolution: "진화 검토 필요",
  needs_review: "리뷰 필요",
  needs_attention: "주의 필요",
  attention: "주의 필요",
  reviewing: "리뷰 판정 관찰 중",
};
function propusStateLabel(state?: string): string {
  return PROPUS_STATES[(state ?? "").trim()] ?? "정상 관찰 중";
}

// Live relative stamp: 방금 / N분 전 / N시간 전 / N일 전. Blank for absent/zero.
function relativeTime(ms?: number): string {
  if (!ms || ms <= 0) return "";
  const diff = Date.now() - ms;
  if (diff < 60_000) return "방금";
  const min = Math.floor(diff / 60_000);
  if (min < 60) return `${min}분 전`;
  const hr = Math.floor(min / 60);
  if (hr < 24) return `${hr}시간 전`;
  return `${Math.floor(hr / 24)}일 전`;
}

// Drop a leading YAML frontmatter fence so the detail renders prose. Mirrors the
// native stripFrontmatter(): an unterminated fence falls back to the raw body.
function stripFrontmatter(body: string): string {
  const s = body.replace(/^\uFEFF/, "");
  if (!s.startsWith("---")) return body;
  const firstNl = s.indexOf("\n");
  if (firstNl === -1) return body;
  const rest = s.slice(firstNl + 1);
  const close = rest.search(/^---[ \t]*$/m);
  if (close === -1) return body; // unterminated — show raw
  const afterClose = rest.indexOf("\n", close);
  return afterClose === -1 ? "" : rest.slice(afterClose + 1).replace(/^\n+/, "");
}

// One hint-colored meta line for a list row — blanks/zeros omitted.
function rowMeta(s: SkillRow): string {
  const parts: string[] = [];
  if (s.category?.trim()) parts.push(s.category.trim());
  const src = sourceLabel(s.source);
  if (src) parts.push(src);
  if (s.editable && s.deletable) parts.push("수정/삭제 가능");
  if (s.version?.trim()) parts.push(`v${s.version.trim()}`);
  if ((s.dependencySummary?.length ?? 0) > 0) parts.push(`요구 ${s.dependencySummary!.length}개`);
  if ((s.installSummary?.length ?? 0) > 0) parts.push(`설치 ${s.installSummary!.length}개`);
  if ((s.evolveCount ?? 0) > 0) parts.push(`진화 ${s.evolveCount}회`);
  if ((s.totalUses ?? 0) > 0) parts.push(`사용 ${s.totalUses}회`);
  return parts.join(" · ");
}

function OriginBadge({ origin }: { origin?: string }) {
  const genesis = origin === "genesis";
  return (
    <span
      style={{
        fontSize: 11,
        padding: "1px 6px",
        borderRadius: 4,
        whiteSpace: "nowrap",
        color: genesis ? color.accent : color.muted,
        border: `1px solid ${genesis ? color.accent : color.line}`,
      }}
    >
      {genesis ? "생성" : "최초"}
    </span>
  );
}

// --- pane -----------------------------------------------------------------

export function SkillsPane() {
  const { connected, cfg } = useWorkspace();
  const { result, query } = useCachedList<SkillRow>("skills", connected);
  const skills = useMemo(() => result?.data ?? [], [result?.data]);
  const [view, setView] = useState<"list" | "propus">("list");
  const [selected, setSelected] = useState<string | null>(null);

  const aiText = serializeList(
    "스킬",
    skills,
    (s) =>
      `- ${s.name ?? "(이름 없음)"}${s.description ? ` — ${s.description}` : ""}${rowMeta(s) ? ` [${rowMeta(s)}]` : ""}`,
  );
  useRegisterPane("skills", aiText);

  const columns: Column<SkillRow>[] = [
    {
      header: "스킬",
      cell: (s) => (
        <div className="workfeed-row-main">
          <div style={{ display: "flex", alignItems: "center", gap: 6 }}>
            <span style={{ fontWeight: 600 }}>{s.name ?? "—"}</span>
            <OriginBadge origin={s.origin} />
          </div>
          {s.description && <div className="workfeed-row-preview">{s.description}</div>}
        </div>
      ),
    },
    {
      header: "메타",
      width: 320,
      tdStyle: { fontSize: 12, color: color.muted, verticalAlign: "top" },
      cell: (s) => rowMeta(s),
    },
  ];

  return (
    <>
      <h2 style={{ marginTop: 2 }}>스킬</h2>
      <SkillViewSwitcher view={view} onChange={setView} />
      {view === "list" ? (
        <GridNotice query={query} count={skills.length} empty="사용할 수 있는 스킬이 없습니다.">
          <Grid
            columns={columns}
            rows={skills}
            getKey={(s) => String(s.name)}
            onRowClick={(s) => setSelected(s.name ?? null)}
          />
        </GridNotice>
      ) : (
        <PropusFeed
          cfg={cfg}
          connected={connected}
          onOpenSkill={(name) => {
            setView("list");
            setSelected(name);
          }}
        />
      )}
      {selected && (
        <SkillDetailModal
          cfg={cfg}
          name={selected}
          onClose={() => setSelected(null)}
          onChanged={() => void query.refetch()}
        />
      )}
    </>
  );
}

function SkillViewSwitcher({ view, onChange }: { view: "list" | "propus"; onChange: (v: "list" | "propus") => void }) {
  const tab = (key: "list" | "propus", label: string) => (
    <button
      onClick={() => onChange(key)}
      style={{
        background: "none",
        border: "none",
        cursor: "pointer",
        padding: "4px 0",
        fontSize: 14,
        fontWeight: view === key ? 600 : 400,
        color: view === key ? color.accent : color.muted,
      }}
    >
      {label}
    </button>
  );
  return (
    <div style={{ display: "flex", gap: 18, borderBottom: `1px solid ${color.line}`, marginBottom: 10 }}>
      {tab("list", "스킬 목록")}
      {tab("propus", "Propus 로그")}
    </div>
  );
}

// --- detail modal ---------------------------------------------------------

function SkillDetailModal({
  cfg,
  name,
  onClose,
  onChanged,
}: {
  cfg: ReturnType<typeof useWorkspace>["cfg"];
  name: string;
  onClose: () => void;
  onChanged: () => void;
}) {
  const [detail, setDetail] = useState<SkillDetailResponse | null>(null);
  const [events, setEvents] = useState<SkillLifecycleEvent[]>([]);
  const [loadErr, setLoadErr] = useState("");
  const [editing, setEditing] = useState(false);
  const [draft, setDraft] = useState("");
  const [confirmDelete, setConfirmDelete] = useState(false);
  const [busy, setBusy] = useState(false);
  const [actionMsg, setActionMsg] = useState<{ text: string; error: boolean } | null>(null);

  const loadLifecycle = useCallback(async () => {
    try {
      const lc = await callRpc<SkillsLifecycleResponse>(cfg, SKILLS_RPC.lifecycle, { limit: 30, skillName: name });
      setEvents(lc?.events ?? []);
    } catch {
      // Lifecycle enrichment is best-effort — a transport failure leaves the
      // section empty without failing the whole detail.
    }
  }, [cfg, name]);

  const load = useCallback(async () => {
    setLoadErr("");
    try {
      const d = await callRpc<SkillDetailResponse>(cfg, SKILLS_RPC.detail, { name });
      setDetail(d);
      setDraft(d?.body ?? "");
    } catch (e) {
      setLoadErr(errText(e));
    }
    await loadLifecycle();
  }, [cfg, name, loadLifecycle]);

  useEffect(() => {
    void load();
  }, [load]);

  const skill = detail?.skill;
  const body = detail?.body ?? "";
  const truncated = detail?.bodyTruncated === true;
  const canEdit = Boolean(skill?.editable) && body.trim() !== "" && !truncated;

  async function save() {
    if (!draft.trim() || busy) return;
    setBusy(true);
    setActionMsg(null);
    try {
      const d = await callRpc<SkillDetailResponse>(cfg, SKILLS_RPC.update, { name, body: draft });
      setDetail(d);
      setDraft(d?.body ?? draft);
      setEditing(false);
      setActionMsg({ text: "저장했습니다.", error: false });
      onChanged();
      await loadLifecycle();
    } catch (e) {
      setActionMsg({ text: errText(e), error: true });
    } finally {
      setBusy(false);
    }
  }

  async function del() {
    if (busy) return;
    setBusy(true);
    setActionMsg(null);
    try {
      await callRpc(cfg, SKILLS_RPC.delete, { name });
      onChanged();
      onClose();
    } catch (e) {
      setActionMsg({ text: errText(e), error: true });
      setBusy(false);
      setConfirmDelete(false);
    }
  }

  return (
    <Modal
      title={name}
      onClose={onClose}
      footer={
        <button className="btn" onClick={onClose}>
          닫기
        </button>
      }
    >
      {loadErr && <p className="pane-error">스킬을 불러오지 못했습니다: {loadErr}</p>}
      {!detail && !loadErr && <p style={{ color: color.muted }}>불러오는 중…</p>}

      {skill && (
        <>
          <div style={{ display: "flex", alignItems: "center", gap: 8, marginBottom: 6 }}>
            <span style={{ fontSize: 16, fontWeight: 600 }}>{skill.name}</span>
            <OriginBadge origin={skill.origin} />
          </div>
          {skill.description && <p style={{ color: color.text2, marginTop: 0 }}>{skill.description}</p>}

          <SkillFacts skill={skill} path={detail?.path} />

          {(skill.editable || skill.deletable) && (
            <div style={{ display: "flex", gap: 8, alignItems: "center", margin: "12px 0", flexWrap: "wrap" }}>
              {!editing && skill.editable && (
                <button
                  className="btn"
                  disabled={!canEdit}
                  onClick={() => {
                    setDraft(body);
                    setEditing(true);
                    setActionMsg(null);
                  }}
                >
                  수정
                </button>
              )}
              {editing && (
                <>
                  <button className="btn" disabled={!draft.trim() || busy} onClick={() => void save()}>
                    {busy ? "저장 중…" : "저장"}
                  </button>
                  <button
                    className="btn"
                    disabled={busy}
                    onClick={() => {
                      setEditing(false);
                      setDraft(body);
                    }}
                  >
                    취소
                  </button>
                </>
              )}
              {!editing && skill.deletable && (
                <button className="btn" style={{ color: color.danger }} onClick={() => setConfirmDelete(true)}>
                  삭제
                </button>
              )}
              {!editing && skill.editable && !canEdit && (
                <span style={{ fontSize: 12, color: color.muted }}>
                  {truncated
                    ? "문서가 길어 일부만 표시되어 수정할 수 없습니다."
                    : "SKILL.md 본문을 읽을 수 없어 수정할 수 없습니다."}
                </span>
              )}
            </div>
          )}
          {actionMsg && (
            <p style={{ fontSize: 13, color: actionMsg.error ? color.danger : color.muted }}>{actionMsg.text}</p>
          )}

          <h3 style={{ color: color.accent, fontSize: 13, marginBottom: 6 }}>문서</h3>
          {editing ? (
            <textarea
              className="field"
              style={{ width: "100%", minHeight: 320, fontFamily: "monospace", fontSize: 13 }}
              value={draft}
              onChange={(e) => setDraft(e.target.value)}
              disabled={busy}
            />
          ) : (
            <SkillBody body={body} truncated={truncated} />
          )}

          <h3 style={{ color: color.accent, fontSize: 13, margin: "16px 0 6px" }}>Propus 로그</h3>
          {events.length === 0 ? (
            <p style={{ color: color.muted, fontSize: 13 }}>이 스킬의 Propus 활동이 아직 없습니다.</p>
          ) : (
            events.map((ev, i) => <LifecycleRow key={i} event={ev} showSkillName={false} />)
          )}
        </>
      )}

      {confirmDelete && (
        <Modal
          title="스킬 삭제"
          onClose={() => !busy && setConfirmDelete(false)}
          footer={
            <>
              <button className="btn" disabled={busy} onClick={() => setConfirmDelete(false)}>
                취소
              </button>
              <button className="btn" style={{ color: color.danger }} disabled={busy} onClick={() => void del()}>
                삭제
              </button>
            </>
          }
        >
          <p>{name} 스킬 디렉터리를 삭제합니다. 되돌릴 수 없습니다.</p>
          {actionMsg?.error && <p style={{ color: color.danger, fontSize: 13 }}>{actionMsg.text}</p>}
        </Modal>
      )}
    </Modal>
  );
}

function SkillBody({ body, truncated }: { body: string; truncated: boolean }) {
  const prose = stripFrontmatter(body).trim();
  if (!prose) return <p style={{ color: color.muted, fontSize: 13 }}>SKILL.md 본문을 읽을 수 없습니다.</p>;
  return (
    <>
      <AssistantText text={prose} onUiSubmit={() => {}} />
      {truncated && <p style={{ color: color.muted, fontSize: 12 }}>(문서가 길어 일부만 표시합니다)</p>}
    </>
  );
}

// Meta facts block — one fact per line, blanks/zeros omitted (native parity).
function SkillFacts({ skill, path }: { skill: SkillRow; path?: string }) {
  const facts: string[] = [];
  const identity = [skill.category?.trim(), sourceLabel(skill.source), skill.version ? `v${skill.version}` : ""]
    .filter(Boolean)
    .join(" · ");
  if (identity) facts.push(identity);
  if (skill.homepage?.trim()) facts.push(`홈페이지 ${skill.homepage.trim()}`);
  if (skill.tags?.length) facts.push(`태그 ${skill.tags.join(" · ")}`);
  if (skill.relatedSkills?.length) facts.push(`관련 스킬 ${skill.relatedSkills.join(" · ")}`);
  if (skill.dependencySummary?.length) facts.push(`요구조건 ${skill.dependencySummary.join(" · ")}`);
  if (skill.installSummary?.length) facts.push(`설치 힌트 ${skill.installSummary.join(" · ")}`);
  const mut =
    skill.editable && skill.deletable
      ? "앱에서 수정/삭제 가능"
      : skill.editable
        ? "앱에서 수정 가능"
        : skill.deletable
          ? "앱에서 삭제 가능"
          : "";
  if (mut) facts.push(mut);
  if ((skill.createdAt ?? 0) > 0) facts.push(`생성일 ${fmtDate(skill.createdAt)}`);
  const curator = { active: "상태 활성", stale: "상태 정체", archived: "상태 보관됨" }[
    (skill.curatorState ?? "").trim()
  ];
  if (curator) facts.push(curator);
  if ((skill.totalUses ?? 0) > 0)
    facts.push(`사용 ${skill.totalUses}회 · 마지막 사용 ${relativeTime(skill.lastUsedAt)}`);
  if ((skill.evolveCount ?? 0) > 0)
    facts.push(`진화 ${skill.evolveCount}회 · 마지막 진화 ${relativeTime(skill.lastEvolvedAt)}`);
  if (path?.trim()) facts.push(path.trim());

  return (
    <div style={{ fontSize: 12, color: color.muted, lineHeight: 1.7 }}>
      {facts.map((f, i) => (
        <div key={i}>{f}</div>
      ))}
    </div>
  );
}

// --- Propus lifecycle feed (view=propus) ----------------------------------

function PropusFeed({
  cfg,
  connected,
  onOpenSkill,
}: {
  cfg: ReturnType<typeof useWorkspace>["cfg"];
  connected: boolean;
  onOpenSkill: (name: string) => void;
}) {
  const [data, setData] = useState<SkillsLifecycleResponse | null>(null);
  const [err, setErr] = useState("");
  const [loading, setLoading] = useState(true);

  const load = useCallback(async () => {
    if (!connected) return;
    setLoading(true);
    setErr("");
    try {
      setData(await callRpc<SkillsLifecycleResponse>(cfg, SKILLS_RPC.lifecycle, { limit: 60 }));
    } catch (e) {
      setErr(errText(e));
    } finally {
      setLoading(false);
    }
  }, [cfg, connected]);

  useEffect(() => {
    void load();
  }, [load]);

  if (!connected) return <p style={{ color: color.muted }}>미연결</p>;
  if (loading && !data) return <p style={{ color: color.muted }}>불러오는 중…</p>;
  if (err) return <p className="pane-error">Propus 로그를 불러오지 못했습니다: {err}</p>;
  const events = data?.events ?? [];
  return (
    <div>
      {data?.summary && <PropusSummary summary={data.summary} />}
      {events.length === 0 ? (
        <p style={{ color: color.muted }}>아직 Propus 로그가 없습니다.</p>
      ) : (
        events.map((ev, i) => <LifecycleRow key={i} event={ev} showSkillName onOpenSkill={onOpenSkill} />)
      )}
    </div>
  );
}

function PropusSummary({ summary }: { summary: PropusLifecycleSummary }) {
  const meta: string[] = [];
  if (summary.doctrineVersion?.trim()) meta.push(summary.doctrineVersion.trim());
  if (summary.sourcePapers?.length) meta.push(`논문 ${summary.sourcePapers.length}개`);
  if (summary.filteredSources?.length) meta.push(`보류 ${summary.filteredSources.length}개`);
  if (summary.qualityGates?.length) meta.push(`게이트 ${summary.qualityGates.length}개`);

  const activity: string[] = [`최근 ${summary.total ?? 0}건`];
  if ((summary.genesis ?? 0) > 0) activity.push(`생성 ${summary.genesis}`);
  if ((summary.evolved ?? 0) > 0) activity.push(`진화 ${summary.evolved}`);
  if ((summary.review ?? 0) > 0) activity.push(`리뷰 ${summary.review}`);
  if ((summary.latestAt ?? 0) > 0) activity.push(`마지막 ${relativeTime(summary.latestAt)}`);

  const attention = (summary.attention ?? 0) > 0;
  const state = attention ? `주의 ${summary.attention}건 · 기각/롤백 포함` : propusStateLabel(summary.state);
  const nextCue = summary.attentionCue?.trim() || summary.nextCue?.trim();

  return (
    <div style={{ marginBottom: 12, paddingBottom: 10, borderBottom: `1px solid ${color.line}` }}>
      <div style={{ color: color.accent, fontWeight: 600, fontSize: 14 }}>{summary.system?.trim() || "Propus"}</div>
      {meta.length > 0 && <div style={{ fontSize: 12, color: color.muted }}>{meta.join(" · ")}</div>}
      <div style={{ fontSize: 12, color: color.muted, marginTop: 2 }}>{activity.join(" · ")}</div>
      <div style={{ fontSize: 13, color: attention ? color.danger : color.text2, marginTop: 4 }}>{state}</div>
      {nextCue && <div style={{ fontSize: 12, color: color.muted, marginTop: 2 }}>{nextCue}</div>}
    </div>
  );
}

function LifecycleRow({
  event,
  showSkillName,
  onOpenSkill,
}: {
  event: SkillLifecycleEvent;
  showSkillName: boolean;
  onOpenSkill?: (name: string) => void;
}) {
  const [open, setOpen] = useState(false);
  const ty = lifecycleType(event.type);
  const detailLine = [event.version ? `v${event.version}` : "", event.detail?.trim()].filter(Boolean).join(" — ");
  const absolute = (event.at ?? 0) > 0 ? fmtDate(event.at) : "";
  const verdict = routeLabel(event.route);
  const metaLine = [absolute, verdict].filter(Boolean).join(" · ");

  return (
    <div
      onClick={() => setOpen((o) => !o)}
      style={{ padding: "8px 0", borderBottom: `1px solid ${color.line}`, cursor: "pointer" }}
    >
      <div style={{ display: "flex", alignItems: "center", gap: 8 }}>
        <span
          style={{
            fontSize: 11,
            padding: "1px 6px",
            borderRadius: 4,
            background: ty.bg,
            color: ty.fg,
            whiteSpace: "nowrap",
          }}
        >
          {ty.label}
        </span>
        {showSkillName && (
          <span
            style={{
              fontWeight: 600,
              fontSize: 13,
              color: color.text,
              flex: 1,
              overflow: "hidden",
              textOverflow: "ellipsis",
              whiteSpace: "nowrap",
            }}
          >
            {event.skillName?.trim() || "(스킬 미지정)"}
          </span>
        )}
        <span style={{ fontSize: 12, color: color.muted, marginLeft: "auto", whiteSpace: "nowrap" }}>
          {relativeTime(event.at)}
        </span>
      </div>
      {detailLine && (
        <div style={{ fontSize: 13, color: color.muted, marginTop: 3, ...(open ? {} : clampLines(3)) }}>
          {detailLine}
        </div>
      )}
      {open && (
        <>
          {event.evidence?.trim() && (
            <div style={{ fontSize: 13, color: color.muted, marginTop: 4 }}>근거: {event.evidence.trim()}</div>
          )}
          {metaLine && <div style={{ fontSize: 12, color: color.muted, marginTop: 4 }}>{metaLine}</div>}
          {onOpenSkill && event.skillName?.trim() && (
            <button
              onClick={(e) => {
                e.stopPropagation();
                onOpenSkill(event.skillName!.trim());
              }}
              style={{
                background: "none",
                border: "none",
                color: color.accent,
                cursor: "pointer",
                padding: "4px 0",
                fontSize: 13,
              }}
            >
              스킬 보기 →
            </button>
          )}
        </>
      )}
    </div>
  );
}

function clampLines(lines: number) {
  return {
    display: "-webkit-box",
    WebkitLineClamp: lines,
    WebkitBoxOrient: "vertical" as const,
    overflow: "hidden",
  };
}
