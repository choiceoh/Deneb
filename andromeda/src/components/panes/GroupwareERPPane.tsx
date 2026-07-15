import { useEffect, useState } from "react";
import { errText } from "@/format";
import { listGroupwareERP } from "@/gateway";
import { erpTextToMarkdown } from "@/erpText";
import { useRegisterPane, useWorkspace } from "@/workspaceContext";
import { Markdown } from "@/components/Markdown";

const AREAS: { id: string; label: string; queryable?: boolean; hint?: string }[] = [
  { id: "stock", label: "재고", queryable: true, hint: "품목·코드" },
  { id: "po", label: "발주" },
  { id: "receive", label: "입고" },
  { id: "ship", label: "출고" },
  { id: "price", label: "단가" },
  { id: "sales", label: "매출" },
  { id: "people", label: "사원", queryable: true, hint: "이름·부서" },
];

/** Read-only Amaranth ERP hub — area tabs, auto-fetch, markdown snapshot. */
export function GroupwareERPPane() {
  const { cfg, connected } = useWorkspace();
  const [area, setArea] = useState(AREAS[0].id);
  const [query, setQuery] = useState("");
  const [text, setText] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [loadedFor, setLoadedFor] = useState("");

  const areaDef = AREAS.find((a) => a.id === area) ?? AREAS[0];
  const cacheKey = `${area}|${areaDef.queryable ? query.trim() : ""}`;

  useRegisterPane(
    "groupware",
    text
      ? `[그룹웨어 · ${areaDef.label}]\n${text}`
      : `[그룹웨어 · ${areaDef.label}]\n(조회 결과 없음)`,
  );

  async function load(forceQuery?: string) {
    if (!connected) return;
    const q = (forceQuery ?? query).trim();
    setBusy(true);
    setError("");
    setText(null);
    try {
      const r = await listGroupwareERP(cfg, area, {
        query: areaDef.queryable ? q || undefined : undefined,
        limit: 40,
      });
      setText((r?.text ?? "").trim() || "(결과 없음)");
      setLoadedFor(`${area}|${areaDef.queryable ? q : ""}`);
    } catch (e) {
      setError(errText(e));
      setText("");
    } finally {
      setBusy(false);
    }
  }

  // Auto-fetch when area changes (and on connect). Queryable areas still load
  // without a filter so the hub isn't an empty shell until the user types.
  useEffect(() => {
    if (!connected) return;
    void load();
    // eslint-disable-next-line react-hooks/exhaustive-deps -- intentional: area/connected only
  }, [area, connected]);

  const showStale = text !== null && loadedFor !== cacheKey && areaDef.queryable;

  return (
    <section className="groupware-pane" aria-label="그룹웨어 ERP">
      <h2 style={{ marginTop: 2 }}>그룹웨어</h2>
      <p className="groupware-lede">
        Amaranth ERP 조회 · 결재 승인/반려는 <strong>결재</strong> 탭
      </p>
      {error && <p className="pane-error">오류: {error}</p>}

      <div className="mail-view-tabs groupware-area-tabs" role="tablist" aria-label="ERP 영역">
        {AREAS.map((a) => (
          <button
            key={a.id}
            type="button"
            role="tab"
            className={"mail-view-tab" + (a.id === area ? " active" : "")}
            aria-selected={a.id === area}
            onClick={() => {
              setArea(a.id);
              setQuery("");
              setError("");
            }}
          >
            {a.label}
          </button>
        ))}
      </div>

      {areaDef.queryable && (
        <form
          className="groupware-search"
          onSubmit={(e) => {
            e.preventDefault();
            void load();
          }}
        >
          <input
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            placeholder={areaDef.hint ?? "검색어"}
            aria-label={`${areaDef.label} 검색`}
            disabled={!connected || busy}
          />
          <button className="btn" type="submit" disabled={!connected || busy}>
            {busy ? "조회 중…" : "검색"}
          </button>
        </form>
      )}

      {!connected ? (
        <p className="groupware-status">미연결</p>
      ) : busy && text === null ? (
        <p className="groupware-status">불러오는 중…</p>
      ) : (
        <div className={"groupware-result" + (showStale ? " stale" : "")}>
          {showStale && (
            <div className="groupware-stale-hint">
              검색어가 바뀌었습니다.{" "}
              <button type="button" className="row-btn" onClick={() => void load()}>
                다시 조회
              </button>
            </div>
          )}
          {text ? (
            text === "(결과 없음)" ? (
              <p className="groupware-status">결과 없음</p>
            ) : (
              <div className="mail-body groupware-body">
                <Markdown text={erpTextToMarkdown(text)} />
              </div>
            )
          ) : null}
        </div>
      )}
    </section>
  );
}
