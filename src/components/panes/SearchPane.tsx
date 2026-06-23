import { useState } from "react";
import { callRpc } from "@/gateway";
import { SEARCH_RPC } from "@/resources";
import type { SearchHit } from "@/types";
import { errText } from "@/format";
import { field, line, muted } from "@/theme";
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
  const [status, setStatus] = useState("");

  const aiText = hits.length
    ? `[검색 "${q}" — ${hits.length}건]\n` +
      hits
        .map((h) => `- ${h.type ? `[${h.type}] ` : ""}${h.title ?? ""}${h.snippet ? ` — ${h.snippet}` : ""}`)
        .join("\n")
    : "";
  useRegisterPane(undefined, aiText);

  async function run() {
    const query = q.trim();
    if (!query || !connected) return;
    setStatus("검색 중…");
    try {
      const res = await callRpc<SearchAllResult | SearchHit[]>(cfg, SEARCH_RPC, { query });
      const list = flatten(res);
      setHits(list);
      setStatus(list.length ? "" : "결과 없음");
    } catch (e) {
      setHits([]);
      setStatus(`오류: ${errText(e)}`);
    }
  }

  return (
    <>
      <h2 style={{ marginTop: 2 }}>통합 검색</h2>
      <div style={{ display: "flex", gap: 6, marginBottom: 12, maxWidth: 640 }}>
        <input
          style={{ ...field, flex: 1 }}
          placeholder={connected ? "위키·다이어리·연락처 통합 검색…" : "먼저 게이트웨이에 연결하세요"}
          value={q}
          disabled={!connected}
          onChange={(e) => setQ(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === "Enter") void run();
          }}
        />
        <button
          className="btn btn-accent"
          onClick={() => void run()}
          disabled={!connected}
          style={{ padding: "8px 14px" }}
        >
          검색
        </button>
      </div>
      {status && <p style={muted}>{status}</p>}
      <div style={{ display: "grid", gap: 8, maxWidth: 760 }}>
        {hits.map((h, i) => {
          // Every bucket (위키/다이어리/인물) carries a memory page path → openable.
          const openable = Boolean(h.path);
          const body = (
            <>
              {h.type && <div style={{ fontSize: 11, color: "var(--accent)", letterSpacing: "0.02em" }}>{h.type}</div>}
              <div style={{ fontWeight: 600 }}>{h.title ?? "(제목 없음)"}</div>
              {h.snippet && <div style={{ opacity: 0.7, fontSize: 13 }}>{h.snippet}</div>}
            </>
          );
          const key = (h.path ?? h.id ?? i) + String(i);
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
    </>
  );
}
