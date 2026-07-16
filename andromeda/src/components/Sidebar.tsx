import { isTileable } from "@/tiling";
import { useWorkspace } from "@/workspaceContext";
import { Icon } from "./Icon";
import { WindowControls } from "./WindowControls";
import { orderedViews, PANES } from "./panes";

const labelStyle = { overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" } as const;

// Slim nav rail. Registry-driven pane tabs (the active one lifts like a Zen tab)
// in the user's order, with 설정 pinned bottom-left. Alt+click splits the pane
// into the tiled work area instead of switching to it.
export function Sidebar() {
  const { view, setView, tiles, splitPane, hiddenViews, viewOrder, setPaletteOpen } = useWorkspace();
  const visiblePanes = orderedViews(viewOrder)
    .filter((k) => !hiddenViews.includes(k))
    .map((k) => PANES.find((p) => p.key === k)!);
  const settings = PANES.find((p) => p.key === "settings")!;

  return (
    <nav
      data-tauri-drag-region
      style={{
        width: "var(--rail-w)",
        flex: "0 0 auto",
        display: "flex",
        flexDirection: "column",
        gap: 2,
        padding: "2px 2px",
        position: "relative",
      }}
    >
      <WindowControls />

      {/* 명령·검색 런처 — 팔레트(⌘K)의 마우스 진입점. 이동·분할·위키 열기·데네브에게
          묻기가 전부 클릭+타이핑으로 된다. */}
      <button className="nav-item nav-launcher" onClick={() => setPaletteOpen(true)} title="명령·검색 (⌘K)">
        <span className="ico">
          <Icon name="search" />
        </span>
        <span style={labelStyle}>명령·검색</span>
      </button>

      {visiblePanes.map((p, i) => {
        const tiled = tiles.length > 1 && tiles.includes(p.key);
        return (
          <button
            key={p.key}
            className={"nav-item fade-up" + (view === p.key ? " active" : "") + (tiled ? " tiled" : "")}
            style={{ animationDelay: `${i * 26}ms` }}
            onClick={(e) => {
              if (e.altKey && isTileable(p.key)) splitPane(p.key);
              else setView(p.key);
            }}
            title={isTileable(p.key) ? `${p.label} — Alt+클릭: 분할로 열기` : p.label}
            aria-current={view === p.key ? "page" : undefined}
          >
            <span className="ico">
              <Icon name={p.key} />
            </span>
            <span style={labelStyle}>{p.label}</span>
          </button>
        );
      })}

      {/* 설정 pinned to the bottom-left (gateway config lives inside it). */}
      <button
        className={"nav-item" + (view === "settings" ? " active" : "")}
        style={{ marginTop: "auto" }}
        onClick={() => setView("settings")}
        title={settings.label}
        aria-current={view === "settings" ? "page" : undefined}
      >
        <span className="ico">
          <Icon name="settings" />
        </span>
        <span style={labelStyle}>{settings.label}</span>
      </button>
    </nav>
  );
}
