import { useState } from "react";
import { callRpc } from "../../gateway";
import { SEARCH_RPC } from "../../resources";
import type { SearchHit } from "../../types";
import { errText } from "../../format";
import { field, line, muted } from "../../theme";
import { useRegisterPane, useWorkspace } from "../../workspaceContext";

// Unified search (search.all) — query-driven, so it calls the RPC directly rather
// than through the CRUD data provider.
export function SearchPane() {
  const { connected, cfg } = useWorkspace();
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
      const res = await callRpc<SearchHit[] | { hits?: SearchHit[] }>(cfg, SEARCH_RPC, { query });
      const list = Array.isArray(res) ? res : (res?.hits ?? []);
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
          placeholder={connected ? "메일·일정·위키·연락처 통합 검색…" : "먼저 게이트웨이에 연결하세요"}
          value={q}
          disabled={!connected}
          onChange={(e) => setQ(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === "Enter") void run();
          }}
        />
        <button onClick={() => void run()} disabled={!connected} style={{ padding: "8px 14px" }}>
          검색
        </button>
      </div>
      {status && <p style={muted}>{status}</p>}
      <div style={{ display: "grid", gap: 8, maxWidth: 760 }}>
        {hits.map((h, i) => (
          <div key={h.id ?? i} style={{ borderTop: line, paddingTop: 8 }}>
            {h.type && <div style={{ fontSize: 12, opacity: 0.5 }}>{h.type}</div>}
            <div style={{ fontWeight: 600 }}>{h.title ?? "(제목 없음)"}</div>
            {h.snippet && <div style={{ opacity: 0.7, fontSize: 13 }}>{h.snippet}</div>}
          </div>
        ))}
      </div>
    </>
  );
}
