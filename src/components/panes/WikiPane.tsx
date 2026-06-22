import { useState } from "react";
import { callRpc } from "@/gateway";
import { MEMORY_RPC } from "@/resources";
import type { WikiPage } from "@/types";
import { errText } from "@/format";
import { color, field, font, line, muted } from "@/theme";
import { useRegisterPane, useWorkspace } from "@/workspaceContext";

// Wiki editor over memory.* — search pages, open one into the editor, save back.
// Query-driven (memory.search/get_page/write_page), so it calls RPCs directly.
export function WikiPane() {
  const { connected, cfg } = useWorkspace();
  const [q, setQ] = useState("");
  const [pages, setPages] = useState<WikiPage[]>([]);
  const [path, setPath] = useState<string | null>(null);
  const [content, setContent] = useState("");
  const [status, setStatus] = useState("");

  useRegisterPane(undefined, content.trim() ? `[위키${path ? ` ${path}` : ""}]\n${content}` : "");

  const keyOf = (p: WikiPage) => p.path ?? String(p.id ?? "");

  async function search() {
    if (!connected) return;
    setStatus("검색 중…");
    try {
      // Gateway memory.search wraps hits as { results: [...] } (memory.go);
      // tolerate a bare array / legacy { pages } too so we never silently drop rows.
      const res = await callRpc<WikiPage[] | { results?: WikiPage[]; pages?: WikiPage[] }>(cfg, MEMORY_RPC.search, {
        query: q.trim(),
      });
      const list = Array.isArray(res) ? res : (res?.results ?? res?.pages ?? []);
      setPages(list);
      setStatus(list.length ? "" : "결과 없음");
    } catch (e) {
      setStatus(`오류: ${errText(e)}`);
    }
  }

  async function open(p: WikiPage) {
    const key = keyOf(p);
    if (!key) return;
    setStatus("불러오는 중…");
    try {
      // get_page returns the page body under `body` (memory.go out struct),
      // not `content`. Keep string/`content` fallbacks for robustness.
      const page = await callRpc<{ body?: string; content?: string } | string>(cfg, MEMORY_RPC.getPage, { path: key });
      setPath(key);
      setContent(typeof page === "string" ? page : (page?.body ?? page?.content ?? ""));
      setStatus("");
    } catch (e) {
      setStatus(`오류: ${errText(e)}`);
    }
  }

  async function save() {
    if (!path) return;
    setStatus("저장 중…");
    try {
      // write_page reads the body from `body` (memory_write.go). Sending it as
      // `content` left body empty server-side and CLOBBERED the page to blank.
      await callRpc(cfg, MEMORY_RPC.writePage, { path, body: content });
      setStatus("저장됨");
    } catch (e) {
      setStatus(`오류: ${errText(e)}`);
    }
  }

  return (
    <div style={{ display: "grid", gridTemplateColumns: "240px 1fr", gap: 12, height: "100%" }}>
      <div style={{ borderRight: line, paddingRight: 12, overflow: "auto" }}>
        <input
          style={{ ...field, width: "100%", boxSizing: "border-box", fontSize: 12, marginBottom: 8 }}
          placeholder="위키 검색…"
          value={q}
          disabled={!connected}
          onChange={(e) => setQ(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === "Enter") void search();
          }}
        />
        {!connected ? (
          <p style={muted}>게이트웨이에 연결하세요.</p>
        ) : pages.length === 0 ? (
          <p style={muted}>검색해 페이지를 찾으세요.</p>
        ) : (
          pages.map((p, i) => (
            <button
              key={keyOf(p) || i}
              onClick={() => void open(p)}
              style={{
                display: "block",
                width: "100%",
                textAlign: "left",
                background: keyOf(p) === path ? color.active : "transparent",
                color: color.text,
                border: "none",
                borderRadius: 4,
                padding: "6px 8px",
                cursor: "pointer",
                fontSize: 13,
              }}
            >
              {p.title ?? p.path ?? "(제목 없음)"}
            </button>
          ))
        )}
      </div>
      <div style={{ display: "flex", flexDirection: "column", minWidth: 0 }}>
        <div style={{ display: "flex", alignItems: "center", gap: 8, marginBottom: 8 }}>
          <h2 style={{ margin: 0, fontSize: 18 }}>{path ?? "위키"}</h2>
          <button onClick={() => void save()} disabled={!path} style={{ padding: "4px 10px" }}>
            저장
          </button>
          {status && <span style={{ fontSize: 12, opacity: 0.6 }}>{status}</span>}
        </div>
        <textarea
          value={content}
          onChange={(e) => setContent(e.target.value)}
          disabled={!path}
          placeholder={path ? "" : "왼쪽에서 페이지를 선택하면 편집할 수 있습니다."}
          style={{
            ...field,
            flex: 1,
            resize: "none",
            fontFamily: font,
            fontSize: 13,
            lineHeight: 1.5,
          }}
        />
      </div>
    </div>
  );
}
