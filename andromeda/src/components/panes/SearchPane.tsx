import { useCallback, useEffect, useRef, useState } from "react";
import type { SearchAllResult, SearchFileHit, SearchSourceStatus } from "@/gen/miniappWire";
import { callRpc } from "@/gateway";
import { errText } from "@/format";
import { MEMORY_RPC, SEARCH_RPC } from "@/resources";
import { usePaneTarget } from "@/usePaneTarget";
import { line } from "@/theme";
import { useRegisterPane, useWorkspace } from "@/workspaceContext";

type SearchSource = "wiki" | "diary" | "people" | "files" | "mail";
type SearchSourceState = "ok" | "partial" | "unavailable" | "error" | "timeout";

interface FactMetadata {
  path?: string;
  resultKind?: string;
  readOnly?: boolean;
  factId?: string;
  subjectId?: string;
}

interface LegacySearchHit extends FactMetadata {
  id?: string;
  type?: string;
  title?: string;
  summary?: string;
  category?: string;
  snippet?: string;
}

interface SearchDisplayHit {
  key: string;
  title: string;
  snippet?: string;
  locator?: string;
  meta?: string;
  wikiPath?: string;
  filePath?: string;
  fileSize?: number;
  mailId?: string;
  factPath?: string;
  resultKind?: string;
  readOnly?: boolean;
  factId?: string;
  subjectId?: string;
}

interface SearchSection {
  source: SearchSource;
  label: string;
  status: SearchSourceState;
  hits: SearchDisplayHit[];
}

const SOURCE_DEFS: Array<{ source: SearchSource; label: string }> = [
  { source: "wiki", label: "위키" },
  { source: "diary", label: "일기" },
  { source: "people", label: "인물" },
  { source: "files", label: "파일" },
  { source: "mail", label: "메일" },
];
const SEARCH_QUERY_MAX_CODE_POINTS = 500;

function boundSearchQuery(value: string): string {
  return Array.from(value).slice(0, SEARCH_QUERY_MAX_CODE_POINTS).join("");
}

function sourceState(value?: string): SearchSourceState {
  if (value == null || value === "") return "ok"; // legacy response without `sources`
  if (value === "ok" || value === "partial" || value === "unavailable" || value === "error" || value === "timeout") {
    return value;
  }
  return "error";
}

function sourceStatusText(status: Exclude<SearchSourceState, "ok">): string {
  switch (status) {
    case "partial":
      return "일부 검색 결과만 표시합니다.";
    case "unavailable":
      return "검색 소스를 사용할 수 없습니다.";
    case "timeout":
      return "검색 시간이 초과되었습니다.";
    case "error":
      return "검색 중 오류가 발생했습니다.";
  }
}

function legacySource(type?: string): SearchSource {
  if (type === "다이어리" || type === "일기") return "diary";
  if (type === "인물") return "people";
  return "wiki";
}

function isFactMetadata(hit: FactMetadata): boolean {
  return hit.resultKind === "fact" || hit.readOnly === true || hit.path?.startsWith("@facts/") === true;
}

function isFactDisplayHit(hit: SearchDisplayHit): boolean {
  return hit.resultKind === "fact" || hit.readOnly === true || Boolean(hit.factPath);
}

function fileLocator(hit: SearchFileHit): string {
  const path = hit.path?.trim() || hit.name?.trim() || "(경로 없음)";
  const start = hit.startLine && hit.startLine > 0 ? hit.startLine : undefined;
  const end = hit.endLine && hit.endLine > 0 ? hit.endLine : undefined;
  if (!start) return path;
  if (!end || end === start) return `${path}:${start}`;
  return `${path}:${start}–${end}`;
}

function fileSize(hit: SearchFileHit): number | undefined {
  const value = hit.size;
  return typeof value === "number" && Number.isFinite(value) && value >= 0 ? value : undefined;
}

function wikiDisplayHit(hit: LegacySearchHit, keyPrefix: string): SearchDisplayHit {
  const fact = isFactMetadata(hit);
  return {
    key: `${keyPrefix}:${hit.factId ?? hit.path ?? hit.id ?? hit.title ?? "unknown"}`,
    title: hit.title || hit.subjectId || "(제목 없음)",
    snippet: fact ? undefined : hit.snippet || hit.summary,
    meta: fact ? undefined : hit.category,
    wikiPath: fact ? undefined : hit.path,
    factPath: fact ? hit.path : undefined,
    resultKind: hit.resultKind,
    readOnly: hit.readOnly,
    factId: hit.factId,
    subjectId: hit.subjectId,
  };
}

function normalizeSearch(res: SearchAllResult | LegacySearchHit[]): SearchSection[] {
  const buckets: Record<SearchSource, SearchDisplayHit[]> = {
    wiki: [],
    diary: [],
    people: [],
    files: [],
    mail: [],
  };
  let statuses: SearchSourceStatus | undefined;

  if (Array.isArray(res)) {
    res.forEach((hit, index) => {
      const source = legacySource(hit.type);
      if (source === "wiki") {
        buckets.wiki.push(wikiDisplayHit(hit, `legacy:${index}`));
        return;
      }
      buckets[source].push({
        key: `legacy:${source}:${hit.path ?? hit.id ?? hit.title ?? index}`,
        title: hit.title ?? "(제목 없음)",
        snippet: hit.snippet || hit.summary,
        meta: hit.category,
        wikiPath: hit.path,
      });
    });
  } else {
    statuses = res.sources;
    for (const [index, hit] of (res.wiki ?? []).entries()) {
      buckets.wiki.push(wikiDisplayHit(hit, `wiki:${index}`));
    }
    for (const [index, hit] of (res.diary ?? []).entries()) {
      buckets.diary.push({
        key: `diary:${hit.file ?? hit.header ?? index}`,
        title: hit.header ?? "(제목 없음)",
        snippet: hit.content,
        wikiPath: hit.file,
      });
    }
    for (const [index, hit] of (res.people ?? []).entries()) {
      buckets.people.push({
        key: `people:${hit.wikiPath ?? hit.email ?? hit.name ?? index}`,
        title: hit.name || hit.email || "(이름 없음)",
        snippet: hit.lastSubject || hit.wikiSummary,
        meta: hit.name && hit.email ? hit.email : undefined,
        wikiPath: hit.wikiPath,
      });
    }
    for (const [index, hit] of (res.files ?? []).entries()) {
      buckets.files.push({
        key: `files:${hit.path ?? hit.name ?? index}:${hit.startLine ?? ""}`,
        title: hit.name || hit.path || "(파일명 없음)",
        snippet: hit.snippet,
        locator: fileLocator(hit),
        meta: [hit.kind, hit.heading].filter(Boolean).join(" · ") || undefined,
        filePath: hit.path,
        fileSize: fileSize(hit),
      });
    }
    for (const [index, hit] of (res.mail ?? []).entries()) {
      buckets.mail.push({
        key: `mail:${hit.id ?? hit.threadId ?? hit.subject ?? index}`,
        title: hit.subject || "(제목 없음)",
        snippet: hit.snippet,
        meta: [hit.from, hit.date, hit.mailbox].filter(Boolean).join(" · ") || undefined,
        mailId: hit.id,
      });
    }
  }

  return SOURCE_DEFS.map(({ source, label }) => ({
    source,
    label,
    status: sourceState(statuses?.[source]),
    hits: buckets[source],
  }));
}

function searchAiText(query: string, sections: SearchSection[]): string {
  if (!query) return "";
  const total = sections.reduce((count, section) => count + section.hits.length, 0);
  // Search-hit fields come from mail and indexed user files, so treat every
  // title/path/snippet as attacker-authored. The AI projection gets only the
  // server-produced source counts/status; the visible pane keeps full detail.
  const lines = [`[검색 "${query}" — ${total}건]`];
  for (const section of sections) {
    if (section.status !== "ok") {
      lines.push(`[${section.label} · ${section.hits.length}건] ${sourceStatusText(section.status)}`);
      continue;
    }
    lines.push(`[${section.label} · ${section.hits.length}건]`);
  }
  return lines.join("\n");
}

export function SearchPane() {
  const { connected, cfg, openPane, openWiki } = useWorkspace();
  const [q, setQ] = useState("");
  const [sections, setSections] = useState<SearchSection[]>([]);
  const [searchedQuery, setSearchedQuery] = useState("");
  const [factDetail, setFactDetail] = useState<{ key: string; body: string } | null>(null);
  const [status, setStatus] = useState("");
  // Google-style: the box sits centered until the first search, then rises to
  // the top. Sticky once set (switching panes remounts and re-centers).
  const [searched, setSearched] = useState(false);
  const requestSeqRef = useRef(0);
  const factRequestSeqRef = useRef(0);
  const activeRequestRef = useRef<{ id: number; query: string } | null>(null);
  const provenanceQueryRef = useRef("");

  useRegisterPane(SEARCH_RESOURCE, searchAiText(searchedQuery, sections));

  // Deep-link query (⌘K 팔레트 "통합 검색: q" · 데네브 workspace 커맨드): open with
  // the query prefilled and already running. The latest `run` rides a ref
  // (updated post-commit) so a long-mounted pane never fires a stale closure —
  // e.g. after the gateway URL/token changed while this pane stayed mounted.
  const runRef = useRef(run);
  useEffect(() => {
    runRef.current = run;
  });
  usePaneTarget(
    "search",
    useCallback((target) => {
      if (!target.query) return;
      const query = boundSearchQuery(target.query);
      setQ(query);
      void runRef.current(query);
    }, []),
  );

  function invalidateFactDetail() {
    factRequestSeqRef.current += 1;
    setFactDetail(null);
  }

  async function run(raw = q) {
    const query = boundSearchQuery(raw.trim());
    if (!query || !connected) return;
    // Match the Android policy: keep one in-flight request for an unchanged
    // query. A different submitted query replaces it through the sequence gate.
    if (activeRequestRef.current?.query === query) return;
    invalidateFactDetail();
    setSearched(true);
    setSearchedQuery(query);
    setSections([]);
    setStatus("검색 중…");
    const requestId = ++requestSeqRef.current;
    activeRequestRef.current = { id: requestId, query };
    provenanceQueryRef.current = query;
    try {
      const data = await callRpc<SearchCacheResponse>(cfg, SEARCH_RPC, { query });
      if (requestSeqRef.current !== requestId) return;
      activeRequestRef.current = null;
      applySearch(data, query);
    } catch (error) {
      if (requestSeqRef.current !== requestId) return;
      activeRequestRef.current = null;
      setSections([]);
      setStatus(`오류: ${errText(error)}`);
    }
  }

  function updateQuery(value: string) {
    const next = boundSearchQuery(value);
    setQ(next);
    const provenanceQuery = provenanceQueryRef.current;
    if (!provenanceQuery || next.trim() === provenanceQuery) return;

    // The visible input no longer describes either the pending request or the
    // displayed evidence. Invalidate both async generations before a late
    // search or fact-detail response can restore evidence from the old query.
    requestSeqRef.current += 1;
    activeRequestRef.current = null;
    provenanceQueryRef.current = "";
    invalidateFactDetail();
    setSections([]);
    setSearchedQuery("");
    setStatus("");
  }

  function applySearch(data: SearchCacheResponse, query: string) {
    const next = normalizeSearch(data);
    const total = next.reduce((count, section) => count + section.hits.length, 0);
    const failed = next.filter((section) => section.status !== "ok").length;
    setSections(next);
    setSearchedQuery(query);
    setFactDetail(null);
    setStatus(failed > 0 ? `일부 검색 소스를 확인하지 못했습니다 · ${failed}곳` : total > 0 ? "" : "결과 없음");
  }

  async function openFact(hit: SearchDisplayHit) {
    if (factDetail?.key === hit.key) {
      invalidateFactDetail();
      setStatus("");
      return;
    }
    const ref = hit.factPath?.trim();
    if (!ref) {
      invalidateFactDetail();
      setStatus("사실 참조가 없습니다. 다시 검색하세요.");
      return;
    }

    const requestId = ++factRequestSeqRef.current;
    setFactDetail(null);
    setStatus("현재 사실 확인 중...");
    try {
      // Fact refs are capabilities to the current canonical value. Always
      // revalidate them directly; never route this payload through RPC caches.
      const data = await callRpc<FactPageResponse>(cfg, MEMORY_RPC.getPage, { path: ref });
      if (factRequestSeqRef.current !== requestId) return;
      const body = typeof data === "string" ? data : (data?.body ?? data?.content ?? "");
      setFactDetail({ key: hit.key, body });
      setStatus("");
    } catch {
      if (factRequestSeqRef.current !== requestId) return;
      setStatus("사실이 변경되었습니다. 다시 검색하세요.");
    }
  }

  return (
    <div className={"search-pane" + (searched ? " searched" : "")}>
      <div className="search-hero">
        {!searched && <div className="search-hero-title">통합 검색</div>}
        <div className="search-box">
          {/* HTML maxLength counts UTF-16 units; handlers enforce the exact 500-code-point server contract. */}
          <input
            className="field"
            placeholder={connected ? "위키 · 일기 · 인물 · 파일 · 메일 검색…" : "먼저 게이트웨이에 연결하세요"}
            value={q}
            maxLength={SEARCH_QUERY_MAX_CODE_POINTS * 2}
            disabled={!connected}
            autoFocus
            onChange={(e) => updateQuery(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === "Enter") void run();
            }}
          />
          <button className="btn btn-accent" onClick={() => void run()} disabled={!connected}>
            검색
          </button>
        </div>
        {status && <p className="pane-status">{status}</p>}
      </div>

      {searched && sections.length > 0 && (
        <div className="search-results">
          {sections.map((section) => (
            <section
              key={section.source}
              className="search-section"
              aria-labelledby={`search-source-${section.source}`}
            >
              <div className="search-section-head">
                <h3 id={`search-source-${section.source}`}>{section.label}</h3>
                {section.status === "ok" || section.status === "partial" ? <span>{section.hits.length}건</span> : null}
              </div>
              {section.status !== "ok" && section.status !== "partial" ? (
                <p className="search-source-status" role="status">
                  {sourceStatusText(section.status)}
                </p>
              ) : section.hits.length === 0 ? (
                <>
                  {section.status === "partial" ? (
                    <p className="search-source-status" role="status">
                      {sourceStatusText(section.status)}
                    </p>
                  ) : null}
                  <p className="search-source-status">결과 없음</p>
                </>
              ) : (
                <>
                  {section.status === "partial" ? (
                    <p className="search-source-status" role="status">
                      {sourceStatusText(section.status)}
                    </p>
                  ) : null}
                  <div className="search-section-hits">
                    {section.hits.map((hit) => {
                      const fact = isFactDisplayHit(hit);
                      const factOpen = fact && factDetail?.key === hit.key;
                      const visibleSnippet = fact
                        ? factOpen
                          ? (factDetail?.body ?? "")
                          : "클릭해 현재 사실 확인"
                        : hit.snippet;
                      const body = (
                        <>
                          <div className="search-hit-title">{hit.title}</div>
                          {hit.locator ? <div className="search-hit-locator">{hit.locator}</div> : null}
                          {hit.meta ? <div className="search-hit-meta">{hit.meta}</div> : null}
                          {visibleSnippet ? (
                            <div
                              className="search-hit-snippet"
                              style={{ whiteSpace: factOpen ? "pre-wrap" : undefined }}
                            >
                              {visibleSnippet}
                            </div>
                          ) : null}
                          {factOpen ? (
                            <div className="search-hit-meta">
                              읽기 전용 사실{hit.subjectId ? ` · ${hit.subjectId}` : ""}
                              {hit.factId ? ` · ${hit.factId}` : ""}
                            </div>
                          ) : null}
                        </>
                      );
                      if (fact) {
                        return (
                          <button
                            key={hit.key}
                            className="search-hit"
                            onClick={() => void openFact(hit)}
                            title="읽기 전용 사실 열기"
                            aria-expanded={factOpen}
                            style={{ borderTop: line }}
                          >
                            {body}
                          </button>
                        );
                      }
                      if (hit.wikiPath) {
                        return (
                          <button
                            key={hit.key}
                            className="search-hit"
                            onClick={() => openWiki(hit.wikiPath as string)}
                            title="페이지 열기"
                            style={{ borderTop: line }}
                          >
                            {body}
                          </button>
                        );
                      }
                      if (hit.mailId) {
                        return (
                          <button
                            key={hit.key}
                            className="search-hit"
                            onClick={() => openPane("mail", { id: hit.mailId })}
                            title="메일 열기"
                            style={{ borderTop: line }}
                          >
                            {body}
                          </button>
                        );
                      }
                      if (hit.filePath) {
                        return (
                          <button
                            key={hit.key}
                            className="search-hit"
                            onClick={() => openPane("files", { path: hit.filePath, size: hit.fileSize })}
                            title="파일 열기"
                            style={{ borderTop: line }}
                          >
                            {body}
                          </button>
                        );
                      }
                      return (
                        <div key={hit.key} className="search-hit search-hit-static" style={{ borderTop: line }}>
                          {body}
                        </div>
                      );
                    })}
                  </div>
                </>
              )}
            </section>
          ))}
        </div>
      )}
    </div>
  );
}

const SEARCH_RESOURCE = "search";

type SearchCacheResponse = SearchAllResult | LegacySearchHit[];
type FactPageResponse = { body?: string; content?: string } | string;
