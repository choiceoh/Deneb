import { useState } from "react";
import { errText } from "@/format";
import { listGroupwareERP } from "@/gateway";
import { useRegisterPane, useWorkspace } from "@/workspaceContext";

const AREAS: { id: string; label: string; queryable?: boolean }[] = [
  { id: "stock", label: "재고", queryable: true },
  { id: "po", label: "발주" },
  { id: "receive", label: "입고" },
  { id: "ship", label: "출고" },
  { id: "price", label: "단가" },
  { id: "sales", label: "매출" },
  { id: "people", label: "사원", queryable: true },
];

/** Read-only Amaranth ERP hub — area chips + text snapshot (챗 groupware 패리티). */
export function GroupwareERPPane() {
  const { cfg, connected } = useWorkspace();
  const [area, setArea] = useState(AREAS[0].id);
  const [query, setQuery] = useState("");
  const [text, setText] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  const areaDef = AREAS.find((a) => a.id === area) ?? AREAS[0];

  useRegisterPane(
    "groupware",
    text
      ? `[그룹웨어 · ${areaDef.label}]\n${text}`
      : `[그룹웨어 · ${areaDef.label}]\n(조회 결과 없음 — 영역을 고르고 조회하세요)`,
  );

  async function load() {
    if (!connected) return;
    setBusy(true);
    setError("");
    try {
      const r = await listGroupwareERP(cfg, area, {
        query: areaDef.queryable ? query.trim() || undefined : undefined,
        limit: 40,
      });
      setText((r?.text ?? "").trim() || "(결과 없음)");
    } catch (e) {
      setError(errText(e));
      setText("");
    } finally {
      setBusy(false);
    }
  }

  return (
    <>
      <h2 style={{ marginTop: 2 }}>그룹웨어</h2>
      <p style={{ opacity: 0.7, fontSize: 13, marginTop: 0 }}>
        Amaranth ERP 조회 전용 (재고·발주·입출고·단가·매출·사원). 결재 승인/반려는 「결재」 탭.
      </p>
      {error && <p className="pane-error">오류: {error}</p>}
      <div style={{ display: "flex", flexWrap: "wrap", gap: 6, marginBottom: 10 }}>
        {AREAS.map((a) => (
          <button
            key={a.id}
            className={"row-btn" + (a.id === area ? " active" : "")}
            aria-pressed={a.id === area}
            onClick={() => {
              setArea(a.id);
              setText("");
              setError("");
            }}
          >
            {a.label}
          </button>
        ))}
      </div>
      {areaDef.queryable && (
        <div style={{ display: "flex", gap: 8, marginBottom: 10 }}>
          <input
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            placeholder={area === "people" ? "사원 이름·부서" : "품목·코드"}
            style={{ flex: 1, minWidth: 0 }}
            onKeyDown={(e) => {
              if (e.key === "Enter") void load();
            }}
          />
        </div>
      )}
      <button className="btn" disabled={!connected || busy} onClick={() => void load()}>
        {busy ? "조회 중…" : "조회"}
      </button>
      {text && (
        <pre
          style={{
            marginTop: 12,
            whiteSpace: "pre-wrap",
            wordBreak: "break-word",
            fontSize: 13,
            lineHeight: 1.45,
            opacity: 0.92,
          }}
        >
          {text}
        </pre>
      )}
    </>
  );
}
