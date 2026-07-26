import { useState } from "react";
import { errText } from "@/format";
import { listGroupwareERP } from "@/gateway";
import { useAsyncOnOpen } from "@/useAsyncOnOpen";
import { useRegisterPane, useWorkspace } from "@/workspaceContext";
import { ErpSnapshot } from "./ErpSnapshot";

const AREAS: { id: string; label: string; queryable?: boolean; hint?: string }[] = [
  { id: "stock", label: "재고", queryable: true, hint: "품목·코드" },
  { id: "po", label: "발주" },
  { id: "receive", label: "입고" },
  { id: "ship", label: "출고" },
  { id: "price", label: "단가" },
  { id: "sales", label: "매출" },
  { id: "people", label: "사원", queryable: true, hint: "이름·부서" },
  { id: "board", label: "게시판" },
];

// 매출 기간 (reader sales folder). 기본(빈 값) = 연초~오늘 누계 — ytd/올해/오늘
// 탭은 중복이라 뺐고, 비교에 실제로 쓰는 전년동기·상반기를 올렸다 (2026-07-16).
const SALES_PERIODS: { id: string; label: string }[] = [
  { id: "", label: "누계" },
  { id: "month", label: "이번 달" },
  { id: "h1", label: "상반기" },
  { id: "yoy", label: "전년동기" },
  { id: "last_year", label: "작년" },
];

/** Read-only Amaranth ERP hub — area tabs, auto-fetch, markdown snapshot. */
export function GroupwareERPPane() {
  const { cfg, connected } = useWorkspace();
  const [area, setArea] = useState(AREAS[0].id);
  const [query, setQuery] = useState("");
  const [searchQ, setSearchQ] = useState("");
  const [salesPeriod, setSalesPeriod] = useState("");
  const [error, setError] = useState("");
  const [searchBusy, setSearchBusy] = useState(false);

  const areaDef = AREAS.find((a) => a.id === area) ?? AREAS[0];
  // Auto-load key: area + applied search (not the in-progress input) + sales period.
  const appliedQ = areaDef.queryable ? searchQ.trim() : "";
  const appliedFolder = area === "sales" ? salesPeriod : "";

  // 사원 is a directory lookup — the reader requires a name/부서 query, so an
  // unqueried auto-fetch would just surface a dependency error.
  const needsQuery = area === "people" && !appliedQ;

  const [text, setText] = useAsyncOnOpen(
    async () => {
      if (needsQuery) return "(검색 필요)";
      const r = await listGroupwareERP(cfg, area, {
        folder: appliedFolder || undefined,
        query: appliedQ || undefined,
        limit: 40,
      });
      return (r?.text ?? "").trim() || "(결과 없음)";
    },
    [cfg, area, appliedQ, appliedFolder],
    {
      enabled: connected,
      onError: (e) => {
        setError(errText(e));
        setText("");
      },
    },
  );

  useRegisterPane(
    "groupware",
    text ? `[그룹웨어 · ${areaDef.label}]\n${text}` : `[그룹웨어 · ${areaDef.label}]\n(조회 결과 없음)`,
  );

  async function applySearch() {
    if (!connected || !areaDef.queryable) return;
    const next = query.trim();
    if (next === searchQ.trim()) {
      // Same filter — force refetch via setText(null) then re-run load path.
      setSearchBusy(true);
      setError("");
      try {
        const r = await listGroupwareERP(cfg, area, { query: next || undefined, limit: 40 });
        setText((r?.text ?? "").trim() || "(결과 없음)");
      } catch (e) {
        setError(errText(e));
        setText("");
      } finally {
        setSearchBusy(false);
      }
      return;
    }
    setSearchQ(next);
  }

  const busy = connected && text === null && !error;
  const showStale = areaDef.queryable && query.trim() !== searchQ.trim() && text !== null;

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
              setSearchQ("");
              setSalesPeriod("");
              setError("");
            }}
          >
            {a.label}
          </button>
        ))}
      </div>

      {area === "sales" && (
        <div className="groupware-period-chips" role="group" aria-label="매출 기간">
          {SALES_PERIODS.map((p) => (
            <button
              key={p.id || "recent"}
              type="button"
              className={"row-btn" + (salesPeriod === p.id ? " active" : "")}
              aria-pressed={salesPeriod === p.id}
              onClick={() => setSalesPeriod(p.id)}
            >
              {p.label}
            </button>
          ))}
        </div>
      )}

      {areaDef.queryable && (
        <form
          className="groupware-search"
          onSubmit={(e) => {
            e.preventDefault();
            void applySearch();
          }}
        >
          <input
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            placeholder={areaDef.hint ?? "검색어"}
            aria-label={`${areaDef.label} 검색`}
            disabled={!connected || searchBusy}
          />
          <button className="btn" type="submit" disabled={!connected || searchBusy || busy}>
            {searchBusy || busy ? "조회 중…" : "검색"}
          </button>
        </form>
      )}

      {!connected ? (
        <p className="groupware-status">미연결</p>
      ) : busy ? (
        <p className="groupware-status">불러오는 중…</p>
      ) : (
        <div className={"groupware-result" + (showStale ? " stale" : "")}>
          {showStale && (
            <div className="groupware-stale-hint">
              검색어가 바뀌었습니다.{" "}
              <button type="button" className="row-btn" onClick={() => void applySearch()}>
                다시 조회
              </button>
            </div>
          )}
          {text ? (
            text === "(결과 없음)" ? (
              <p className="groupware-status">결과 없음</p>
            ) : text === "(검색 필요)" ? (
              <p className="groupware-status">이름이나 부서로 검색하세요</p>
            ) : (
              <div className="mail-body groupware-body">
                <ErpSnapshot text={text} />
              </div>
            )
          ) : null}
        </div>
      )}
    </section>
  );
}
