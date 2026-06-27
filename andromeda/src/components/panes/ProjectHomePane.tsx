import { useEffect, useMemo, useState } from "react";

import { useCachedList } from "@/cachedList";
import { serializeList } from "@/aiText";
import { NOTEBOOK_RPC, PROJECT_LINKED_RPC } from "@/resources";
import type { CalEvent, Mail, NotebookSummary, ProjectDigest, ProjectLinkedOut, Todo, WorkItem } from "@/types";
import { calSpan, calStamp, fmtDate, fmtMailDate, senderName } from "@/format";
import { useCachedRpc } from "@/useCachedRpc";
import { useRegisterPane, useWorkspace, type PaneTarget } from "@/workspaceContext";
import { GridNotice } from "@/components/Grid";
import { Icon, type IconName } from "@/components/Icon";

const PROJECT_HOME_RESOURCE = "project-home";
const MAX_ROWS = 5;

interface NotebookListResponse {
  notebooks?: NotebookSummary[];
}

interface RelatedRow {
  key: string;
  title: string;
  meta?: string;
  body?: string;
  badge?: string;
  target?: PaneTarget;
}

interface RelatedSection {
  key: string;
  label: string;
  icon: IconName;
  empty: string;
  rows: RelatedRow[];
  loading?: boolean;
}

// LinkedSets is the per-type ID membership the gateway resolved for the selected
// project (miniapp.project.linked). The pane filters its already-fetched lists by
// these instead of running a local ref-matching heuristic — matching lives on the
// server now, where the wiki graph does.
interface LinkedSets {
  mail: Set<string>;
  calendar: Set<string>;
  todo: Set<string>;
  workfeed: Set<string>;
  notebook: Set<string>;
}

const EMPTY_LINKED: ProjectLinkedOut = {};

function toLinkedSets(linked: ProjectLinkedOut): LinkedSets {
  return {
    mail: new Set(linked.mail ?? []),
    calendar: new Set(linked.calendar ?? []),
    todo: new Set(linked.todo ?? []),
    workfeed: new Set(linked.workfeed ?? []),
    notebook: new Set(linked.notebook ?? []),
  };
}

export function ProjectHomePane() {
  const { connected, cfg, openPane, openWiki } = useWorkspace();
  const progress = useCachedList<ProjectDigest>("progress", connected);
  const mail = useCachedList<Mail>("mail", connected);
  const calendar = useCachedList<CalEvent>("calendar", connected);
  const todo = useCachedList<Todo>("todo", connected);
  const workfeed = useCachedList<WorkItem>("workfeed", connected);
  const { callCached, readCache, busy: notebooksBusy } = useCachedRpc(cfg, PROJECT_HOME_RESOURCE);
  const [notebookSnapshot] = useState(() => readCache<NotebookListResponse>(NOTEBOOK_RPC.list));
  const [notebooks, setNotebooks] = useState<NotebookSummary[]>(notebookSnapshot?.data.notebooks ?? []);
  const [selectedKey, setSelectedKey] = useState("");
  const [linked, setLinked] = useState<ProjectLinkedOut>(EMPTY_LINKED);

  useEffect(() => {
    if (!connected) return;
    void callCached<NotebookListResponse>(
      NOTEBOOK_RPC.list,
      {},
      {
        scope: "project-home:notebooks",
        apply: (data) => setNotebooks(data.notebooks ?? []),
      },
    );
  }, [callCached, connected]);

  const digestData = progress.result?.data;
  const digests = useMemo(() => digestData ?? [], [digestData]);
  const selected = useMemo(() => {
    if (digests.length === 0) return undefined;
    return digests.find((digest) => digestKey(digest) === selectedKey) ?? digests[0];
  }, [digests, selectedKey]);

  // Resolve the selected project's linked-item IDs server-side. Clear the stale
  // set first so a project switch never briefly shows the previous project's items.
  const selectedPath = selected?.path ?? "";
  useEffect(() => {
    setLinked(EMPTY_LINKED);
    if (!connected || !selectedPath) return;
    void callCached<ProjectLinkedOut>(
      PROJECT_LINKED_RPC,
      { path: selectedPath },
      {
        scope: `project-home:linked:${selectedPath}`,
        apply: (data) => setLinked(data),
      },
    );
  }, [callCached, connected, selectedPath]);

  const linkedSets = useMemo(() => toLinkedSets(linked), [linked]);

  const sections = useMemo<RelatedSection[]>(() => {
    if (!selected) return [];
    const mails = (mail.result?.data ?? [])
      .filter((m) => linkedSets.mail.has(String(m.id)))
      .sort((a, b) => timeValue(b.date) - timeValue(a.date))
      .slice(0, MAX_ROWS)
      .map((m) => ({
        key: String(m.id),
        title: m.subject || "(제목 없음)",
        meta: [senderName(m.from), fmtMailDate(m.date)].filter(Boolean).join(" · "),
        body: m.snippet || m.body || m.text || undefined,
        badge: m.isUnread ? "미열람" : undefined,
        target: { view: "mail" as const, id: m.id },
      }));

    const events = (calendar.result?.data ?? [])
      .filter((event) => linkedSets.calendar.has(String(event.id)))
      .sort((a, b) => eventStartMs(a) - eventStartMs(b))
      .slice(0, MAX_ROWS)
      .map((event) => ({
        key: String(event.id),
        title: event.summary || event.title || "(제목 없음)",
        meta: calSpan(event.start, event.end) || undefined,
        body: event.location || event.description || undefined,
        badge: event.category,
        target: { view: "calendar" as const, id: event.id },
      }));

    const todos = (todo.result?.data ?? [])
      .filter((t) => !t.done && linkedSets.todo.has(String(t.id)))
      .sort((a, b) => timeValue(a.due) - timeValue(b.due))
      .slice(0, MAX_ROWS)
      .map((t) => ({
        key: String(t.id),
        title: t.title,
        meta: t.due ? `마감 ${fmtDate(t.due)}` : undefined,
        body: t.note,
        target: { view: "todo" as const, id: t.id },
      }));

    const work = (workfeed.result?.data ?? [])
      .filter((item) => linkedSets.workfeed.has(String(item.id)))
      .sort((a, b) => (b.createdAtMs ?? 0) - (a.createdAtMs ?? 0))
      .slice(0, MAX_ROWS)
      .map((item) => ({
        key: String(item.id),
        title: item.title || "(항목)",
        meta: [item.source, fmtDate(item.createdAtMs)].filter(Boolean).join(" · "),
        body: item.body,
        target: { view: "workfeed" as const, id: item.id },
      }));

    const relatedNotebooks = notebooks
      .filter((notebook) => linkedSets.notebook.has(notebook.id))
      .sort((a, b) => (b.updated ?? 0) - (a.updated ?? 0))
      .slice(0, MAX_ROWS)
      .map((notebook) => ({
        key: notebook.id,
        title: notebook.name,
        meta: [notebook.sourceCount ? `자료 ${notebook.sourceCount}` : "", fmtDate(notebook.updated)]
          .filter(Boolean)
          .join(" · "),
        body: notebook.dealRef,
        target: { view: "notebook" as const, id: notebook.id },
      }));

    return [
      {
        key: "mail",
        label: "관련 메일",
        icon: "mail",
        empty: "연결된 메일 없음",
        rows: mails,
        loading: mail.query.isLoading,
      },
      {
        key: "calendar",
        label: "관련 일정",
        icon: "calendar",
        empty: "연결된 일정 없음",
        rows: events,
        loading: calendar.query.isLoading,
      },
      {
        key: "todo",
        label: "관련 할일",
        icon: "todo",
        empty: "연결된 할일 없음",
        rows: todos,
        loading: todo.query.isLoading,
      },
      {
        key: "workfeed",
        label: "관련 작업피드",
        icon: "workfeed",
        empty: "연결된 작업피드 없음",
        rows: work,
        loading: workfeed.query.isLoading,
      },
      {
        key: "notebook",
        label: "연결 노트북",
        icon: "notebook",
        empty: "연결된 노트북 없음",
        rows: relatedNotebooks,
        loading: notebooksBusy,
      },
    ];
  }, [
    calendar.result?.data,
    calendar.query.isLoading,
    linkedSets,
    mail.result?.data,
    mail.query.isLoading,
    notebooks,
    notebooksBusy,
    selected,
    todo.result?.data,
    todo.query.isLoading,
    workfeed.result?.data,
    workfeed.query.isLoading,
  ]);

  const focusLines = useMemo(() => {
    if (!selected) return [];
    const due = selected.due ? [`마감 ${selected.due}`] : [];
    const related = sections
      .filter((section) => section.rows.length > 0)
      .map((section) => `${section.label.replace(/^관련 /, "")} ${section.rows.length}`);
    return [...due, ...related].slice(0, 6);
  }, [sections, selected]);

  const aiText = useMemo(() => {
    if (!selected) return "";
    const status = serializeList("현재 상태", selected.bullets ?? [], (bullet) => `- ${bullet}`);
    const related = sections
      .filter((section) => section.rows.length > 0)
      .map(
        (section) =>
          `[${section.label} ${section.rows.length}건]\n` +
          section.rows.map((row) => `- ${row.title}${row.meta ? ` · ${row.meta}` : ""}`).join("\n"),
      )
      .join("\n\n");
    return [
      `[프로젝트 홈] ${selected.project}`,
      selected.headline,
      selected.due ? `마감: ${selected.due}` : "",
      selected.path ? `위키: ${selected.path}` : "",
      status,
      related,
    ]
      .filter(Boolean)
      .join("\n");
  }, [sections, selected]);
  useRegisterPane(undefined, aiText);

  return (
    <>
      <h2 style={{ marginTop: 2 }}>프로젝트 홈</h2>
      <GridNotice query={progress.query} count={digests.length} empty="진행 중인 프로젝트가 없습니다.">
        {selected && (
          <div className="project-home">
            <aside className="project-home-list" aria-label="프로젝트 목록">
              {digests.map((digest, index) => {
                const active = digestKey(digest) === digestKey(selected);
                return (
                  <button
                    key={digestKey(digest)}
                    type="button"
                    className={"project-home-project" + (active ? " active" : "")}
                    style={{ animationDelay: `${index * 36}ms` }}
                    onClick={() => setSelectedKey(digestKey(digest))}
                  >
                    <span className="project-home-project-name">{digest.project}</span>
                    <span className="project-home-project-meta">
                      {digest.headline || digest.path || "상태 요약 없음"}
                    </span>
                  </button>
                );
              })}
            </aside>

            <main className="project-home-main">
              <section className="project-home-hero">
                <div className="project-home-titleline">
                  <div>
                    <div className="micro">프로젝트</div>
                    <h3>{selected.project}</h3>
                  </div>
                  {selected.path && (
                    <button className="row-btn" type="button" onClick={() => openWiki(selected.path as string)}>
                      위키 열기
                    </button>
                  )}
                </div>
                {selected.headline && <p className="project-home-headline">{selected.headline}</p>}
                <div className="project-home-facts">
                  {selected.due && <span>마감 {selected.due}</span>}
                  {selected.updatedAtMs && <span>업데이트 {fmtDate(selected.updatedAtMs)}</span>}
                  {selected.path && <span>{selected.path}</span>}
                </div>
              </section>

              <div className="project-home-split">
                <section className="project-home-status" aria-label="현재 상태">
                  <div className="project-home-section-head">
                    <span>현재 상태</span>
                  </div>
                  {(selected.bullets ?? []).length === 0 ? (
                    <p className="project-home-empty">상태 항목 없음</p>
                  ) : (
                    <ul>
                      {(selected.bullets ?? []).map((bullet, index) => (
                        <li key={index}>{bullet}</li>
                      ))}
                    </ul>
                  )}
                </section>

                <section className="project-home-focus" aria-label="지금 볼 것">
                  <div className="project-home-section-head">
                    <span>지금 볼 것</span>
                  </div>
                  {focusLines.length === 0 ? (
                    <p className="project-home-empty">연결된 항목 없음</p>
                  ) : (
                    <div className="project-home-focus-list">
                      {focusLines.map((line) => (
                        <span key={line}>{line}</span>
                      ))}
                    </div>
                  )}
                </section>
              </div>

              <div className="project-home-sections">
                {sections.map((section) => (
                  <ProjectSection
                    key={section.key}
                    section={section}
                    onOpen={(target) => {
                      const { view, ...paneTarget } = target;
                      openPane(view, paneTarget);
                    }}
                  />
                ))}
              </div>
            </main>
          </div>
        )}
      </GridNotice>
    </>
  );
}

function ProjectSection({ section, onOpen }: { section: RelatedSection; onOpen: (target: PaneTarget) => void }) {
  return (
    <section className="project-home-section">
      <div className="project-home-section-head">
        <Icon name={section.icon} size={15} />
        <span>{section.label}</span>
        {section.rows.length > 0 && <span className="project-home-count">{section.rows.length}</span>}
      </div>
      {section.loading ? (
        <p className="project-home-empty">불러오는 중…</p>
      ) : section.rows.length === 0 ? (
        <p className="project-home-empty">{section.empty}</p>
      ) : (
        <div className="project-home-rows">
          {section.rows.map((row) => (
            <button
              key={row.key}
              type="button"
              className="project-home-row"
              onClick={() => (row.target ? onOpen(row.target) : undefined)}
            >
              <span className="project-home-row-main">
                <span className="project-home-row-title">{row.title}</span>
                {row.meta && <span className="project-home-row-meta">{row.meta}</span>}
                {row.body && <span className="project-home-row-body">{row.body}</span>}
              </span>
              {row.badge && <span className="project-home-badge">{row.badge}</span>}
            </button>
          ))}
        </div>
      )}
    </section>
  );
}

function digestKey(digest: ProjectDigest): string {
  return digest.path || digest.project;
}

function timeValue(value?: string | number): number {
  if (value == null || value === "") return Infinity;
  const ms = new Date(value).getTime();
  return Number.isNaN(ms) ? Infinity : ms;
}

function eventStartMs(event: CalEvent): number {
  const stamp = calStamp(event.start);
  return timeValue(stamp.iso);
}
