// ⌘K command palette / launcher — the human side of the workspace command bus.
// Mouse-first: opened from the rail's 명령·검색 button (⌘K also works), every row
// clickable. One input drives four sources: workspace commands (이동/분할/닫기/
// 레이아웃), wiki quick-open (memory.search as you type), 통합 검색 handoff, and
// "데네브에게 묻기" (routes the query into the AI panel).
import { useEffect, useMemo, useRef, useState } from "react";
import { callRpc } from "@/gateway";
import { errText } from "@/format";
import { log } from "@/log";
import { MEMORY_RPC } from "@/resources";
import { isTileable, MAX_TILES } from "@/tiling";
import type { View, WikiPage } from "@/types";
import { useWorkspace } from "@/workspaceContext";
import { Icon, type IconName } from "./Icon";
import { orderedViews, paneLabel } from "./panes";

const palLog = log.child("palette");

interface Row {
  key: string;
  icon: IconName;
  label: string;
  hint?: string;
  run: () => void;
  // Optional inline secondary action (e.g. delete a saved layout).
  aux?: { label: string; run: () => void };
}

// Substring match, case-insensitive — Korean labels don't benefit from fancy
// fuzzy scoring; earlier match = better.
function matchScore(query: string, text: string): number {
  if (query === "") return 0;
  const idx = text.toLowerCase().indexOf(query.toLowerCase());
  return idx < 0 ? -1 : 1000 - idx * 10 - text.length;
}

// Stable empty-hits identity so the rows memo doesn't churn on every render
// while the wiki tag and the live query disagree.
const NO_HITS: WikiPage[] = [];

export function CommandPalette() {
  const {
    connected,
    cfg,
    view,
    tiles,
    setView,
    splitPane,
    closePane,
    layouts,
    saveLayout,
    deleteLayout,
    applyLayout,
    openPane,
    openWiki,
    setPaletteOpen,
    setAiCollapsed,
    askDeneb,
    hiddenViews,
    viewOrder,
  } = useWorkspace();
  const [query, setQuery] = useState("");
  const [active, setActive] = useState(0);
  // Wiki quick-open results, tagged with the query they answer. Hits derive
  // below: a tag that no longer matches the live query renders as empty — no
  // synchronous setState in the effect, and stale responses drop naturally.
  const [wikiRes, setWikiRes] = useState<{ q: string; hits: WikiPage[] }>({ q: "", hits: [] });
  const inputRef = useRef<HTMLInputElement>(null);
  const listRef = useRef<HTMLDivElement>(null);

  useEffect(() => inputRef.current?.focus(), []);

  const close = () => setPaletteOpen(false);

  // Wiki quick-open: debounce keystrokes into memory.search.
  useEffect(() => {
    const q = query.trim();
    if (!connected || q.length < 2) return;
    const timer = setTimeout(() => {
      callRpc<{ results?: WikiPage[]; pages?: WikiPage[] } | WikiPage[]>(cfg, MEMORY_RPC.search, { query: q })
        .then((data) => {
          const list = Array.isArray(data) ? data : (data?.results ?? data?.pages ?? []);
          setWikiRes({ q, hits: list.slice(0, 5) });
        })
        .catch((e) => {
          palLog.debug("wiki quick-open failed", errText(e));
          setWikiRes({ q, hits: [] });
        });
    }, 250);
    return () => clearTimeout(timer);
  }, [query, cfg, connected]);

  const wikiHits = wikiRes.q === query.trim() ? wikiRes.hits : NO_HITS;

  const rows = useMemo(() => {
    const q = query.trim();
    const out: (Row & { score: number })[] = [];
    const push = (row: Row, score: number) => {
      if (score >= 0) out.push({ ...row, score });
    };

    // 빈 쿼리 = 런처 첫 화면. 상위 12행에 다 못 들어가므로 우선순위가 곧 노출이다:
    // 분할 닫기(현재 상태 조작) > 레이아웃 > 이동(레일 순) > 분할 추가(+ 스트립이 주 경로).
    // Close tiles (only in a split).
    if (tiles.length > 1)
      for (const t of tiles)
        push(
          { key: `close:${t}`, icon: "close", label: `${paneLabel(t)} 분할 닫기`, run: () => closePane(t) },
          q === "" ? 900 : matchScore(q, `${paneLabel(t)} 분할 닫기`),
        );

    // Saved layouts: apply (with inline delete), plus save-current when typing a
    // fresh name while split (below).
    for (const l of layouts)
      push(
        {
          key: `layout:${l.name}`,
          icon: "progress",
          label: `레이아웃: ${l.name}`,
          hint: l.views.map((v) => paneLabel(v)).join(" · "),
          run: () => applyLayout(l.views),
          aux: { label: "삭제", run: () => deleteLayout(l.name) },
        },
        q === "" ? 800 : matchScore(q, `레이아웃 ${l.name}`),
      );

    // Pane navigation + split, in the user's rail order (hidden panes still
    // reachable here via query — the palette is the superset surface).
    const paneKeys: View[] = [...orderedViews(viewOrder), "settings"];
    for (const [idx, key] of paneKeys.entries()) {
      const label = paneLabel(key);
      const hidden = hiddenViews.includes(key);
      push(
        { key: `go:${key}`, icon: key, label, hint: view === key ? "현재 화면" : "이동", run: () => setView(key) },
        q === "" ? (hidden ? -1 : 700 - idx) : matchScore(q, `${label} 이동`),
      );
      if (isTileable(key) && !tiles.includes(key) && tiles.length < MAX_TILES)
        push(
          {
            key: `split:${key}`,
            icon: key,
            label: `${label} 분할로 열기`,
            hint: "현재 화면 옆에",
            run: () => splitPane(key),
          },
          q === "" ? (hidden ? -1 : 200 - idx) : matchScore(q, `${label} 분할`),
        );
    }
    if (tiles.length > 1 && q !== "" && !layouts.some((l) => l.name === q))
      push(
        {
          key: "layout:save",
          icon: "plus",
          label: `현재 분할을 '${q}' 레이아웃으로 저장`,
          hint: tiles.map((v) => paneLabel(v)).join(" · "),
          run: () => saveLayout(q),
        },
        350,
      );

    // Wiki quick-open hits (async, above the generic handoffs).
    for (const [i, hit] of wikiHits.entries()) {
      const path = hit.path ?? hit.id;
      if (!path) continue;
      push(
        {
          key: `wiki:${path}`,
          icon: "wiki",
          label: hit.title ?? path,
          hint: `위키 열기 · ${path}`,
          run: () => openWiki(path),
        },
        300 - i,
      );
    }

    // Handoffs for any non-empty query: 통합 검색, 데네브에게 묻기.
    if (q !== "") {
      push(
        {
          key: "search:q",
          icon: "search",
          label: `통합 검색: ${q}`,
          run: () => openPane("search", { query: q }),
        },
        200,
      );
      push(
        {
          key: "ask:q",
          icon: "chat",
          label: `데네브에게 묻기: ${q}`,
          run: () => {
            setAiCollapsed(false);
            if (!askDeneb(q)) palLog.warn("ask sink unavailable");
          },
        },
        190,
      );
    }

    out.sort((a, b) => b.score - a.score);
    return out.slice(0, 12);
  }, [
    query,
    wikiHits,
    tiles,
    view,
    layouts,
    hiddenViews,
    viewOrder,
    setView,
    splitPane,
    closePane,
    applyLayout,
    saveLayout,
    deleteLayout,
    openPane,
    openWiki,
    setAiCollapsed,
    askDeneb,
  ]);

  // Clamp the highlighted row while the list reshapes under the query.
  const activeIdx = Math.min(active, Math.max(0, rows.length - 1));

  const runRow = (row: Row) => {
    close();
    row.run();
  };

  const onKeyDown = (e: React.KeyboardEvent) => {
    if (e.key === "Escape") {
      e.preventDefault();
      close();
    } else if (e.key === "ArrowDown") {
      e.preventDefault();
      setActive(Math.min(activeIdx + 1, rows.length - 1));
    } else if (e.key === "ArrowUp") {
      e.preventDefault();
      setActive(Math.max(activeIdx - 1, 0));
    } else if (e.key === "Enter") {
      e.preventDefault();
      const row = rows[activeIdx];
      if (row) runRow(row);
    }
  };

  useEffect(() => {
    // scrollIntoView is absent in jsdom — optional-call it.
    listRef.current?.querySelector(".cmdk-item.active")?.scrollIntoView?.({ block: "nearest" });
  }, [activeIdx]);

  return (
    <div className="cmdk-overlay" onMouseDown={close} role="presentation">
      <div
        className="cmdk"
        role="dialog"
        aria-label="명령·검색"
        onMouseDown={(e) => e.stopPropagation()}
        onKeyDown={onKeyDown}
      >
        <div className="cmdk-input-row">
          <Icon name="search" size={15} />
          <input
            ref={inputRef}
            className="cmdk-input"
            placeholder="이동 · 분할 · 위키 · 검색 · 데네브에게 묻기…"
            value={query}
            onChange={(e) => {
              setQuery(e.target.value);
              setActive(0);
            }}
            aria-label="명령 입력"
          />
        </div>
        <div className="cmdk-list" ref={listRef} role="listbox" aria-label="명령 목록">
          {rows.length === 0 && <div className="cmdk-empty">일치하는 명령이 없습니다</div>}
          {rows.map((row, i) => (
            <div
              key={row.key}
              className={"cmdk-item" + (i === activeIdx ? " active" : "")}
              role="option"
              aria-selected={i === activeIdx}
              onMouseEnter={() => setActive(i)}
              onClick={() => runRow(row)}
            >
              <span className="cmdk-ico">
                <Icon name={row.icon} size={14} />
              </span>
              <span className="cmdk-label">{row.label}</span>
              {row.hint && <span className="cmdk-hint">{row.hint}</span>}
              {row.aux && (
                <button
                  className="row-btn cmdk-aux"
                  onClick={(e) => {
                    e.stopPropagation();
                    row.aux!.run();
                  }}
                >
                  {row.aux.label}
                </button>
              )}
            </div>
          ))}
        </div>
      </div>
    </div>
  );
}
