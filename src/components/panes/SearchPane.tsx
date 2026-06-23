import { useState } from "react";
import { SEARCH_RPC } from "@/resources";
import { projectList } from "@/aiText";
import type { SearchHit } from "@/types";
import { useCachedRpc } from "@/useCachedRpc";
import { line } from "@/theme";
import { useRegisterPane, useWorkspace } from "@/workspaceContext";

// search.all fans out across wiki / diary / people buckets server-side. Flatten the
// three lists into one typed result stream (matching the gateway wire shape) while
// tolerating a legacy bare-array response.
interface SearchAllResult {
  wiki?: Array<{ path?: string; title?: string; summary?: string; category?: string; snippet?: string }>;
  diary?: Array<{ file?: string; header?: string; content?: string }>;
  people?: Array<{ email?: string; name?: string; lastSubject?: string; wikiSummary?: string; wikiPath?: string }>;
}

function flatten(res: SearchAllResult | SearchHit[] | null): SearchHit[] {
  if (!res) return [];
  if (Array.isArray(res)) return res;
  const out: SearchHit[] = [];
  for (const w of res.wiki ?? [])
    out.push({ type: "위키", path: w.path, title: w.title, category: w.category, snippet: w.snippet || w.summary });
  for (const d of res.diary ?? []) out.push({ type: "다이어리", path: d.file, title: d.header, snippet: d.content });
  for (const p of res.people ?? [])
    out.push({ type: "인물", path: p.wikiPath, title: p.name || p.email, snippet: p.lastSubject || p.wikiSummary });
  return out;
}

export function SearchPane() {
  const { connected, cfg, openWiki } = useWorkspace();
  const [q, setQ] = useState("");
  const [hits, setHits] = useState<SearchHit[]>([]);
  // Google-style: the box sits centered until the first search, then rises to
  // the top. Sticky once set (switching panes remounts and re-centers).
  const [searched, setSearched] = useState(false);
  const { callCached, status, setStatus } = useCachedRpc(cfg, SEARCH_RESOURCE);

  const aiText = projectList(
    `[검색 "${q}" — ${hits.length}건]`,
    hits,
    (h) => `- ${h.type ? `[${h.type}] ` : ""}${h.title ?? ""}${h.snippet ? ` — ${h.snippet}` : ""}`,
  );
  useRegisterPane(SEARCH_RESOURCE, aiText);

  async function run() {
    const query = q.trim();
    if (!query || !connected) return;
    setSearched(true);
    const r = await callCached<SearchCacheResponse>(
      SEARCH_RPC,
      { query },
      {
        pending: "검색 중…",
        scope: "search:results",
        apply: applySearch,
      },
    );
    if (!r.ok) {
      setHits([]);
      return;
    }
  }

  function applySearch(data: SearchCacheResponse) {
    const list = flatten(data);
    setHits(list);
    setStatus(list.length ? "" : "결과 없음");
  }

  return (
    <div className={"search-pane" + (searched ? " searched" : "")}>
      <div className="search-hero">
        {!searched && <div className="search-hero-title">통합 검색</div>}
        <div className="search-box">
          <input
            className="field"
            placeholder={connected ? "위키 · 다이어리 · 연락처 검색…" : "먼저 게이트웨이에 연결하세요"}
            value={q}
            disabled={!connected}
            autoFocus
            onChange={(e) => setQ(e.target.value)}
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

      {searched && (
        <div className="search-results">
          {hits.map((h, i) => {
            // Every bucket (위키/다이어리/인물) carries a memory page path → openable.
            const openable = Boolean(h.path);
            const body = (
              <>
                {h.type && (
                  <div style={{ fontSize: 11, color: "var(--accent)", letterSpacing: "0.02em" }}>{h.type}</div>
                )}
                <div style={{ fontWeight: 600 }}>{h.title ?? "(제목 없음)"}</div>
                {h.snippet && <div style={{ opacity: 0.7, fontSize: 13 }}>{h.snippet}</div>}
              </>
            );
            const key = `${h.type ?? ""}:${h.path ?? h.title ?? i}`;
            return openable ? (
              <button
                key={key}
                className="search-hit"
                onClick={() => openWiki(h.path as string)}
                title="페이지 열기"
                style={{ borderTop: line }}
              >
                {body}
              </button>
            ) : (
              <div key={key} style={{ borderTop: line, paddingTop: 8 }}>
                {body}
              </div>
            );
          })}
        </div>
      )}
    </div>
  );
}

const SEARCH_RESOURCE = "search";

type SearchCacheResponse = SearchAllResult | SearchHit[];
