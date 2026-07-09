import { useWorkspace } from "@/workspaceContext";
import { Icon } from "./Icon";
import { WindowControls } from "./WindowControls";
import { orderedViews, PANES } from "./panes";

const labelStyle = { overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" } as const;

// Slim nav rail. Registry-driven pane tabs (the active one lifts like a Zen tab)
// in the user's order, with 설정 pinned bottom-left.
export function Sidebar() {
  const { view, setView, hiddenViews, viewOrder } = useWorkspace();
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

      {visiblePanes.map((p, i) => (
        <button
          key={p.key}
          className={"nav-item fade-up" + (view === p.key ? " active" : "")}
          style={{ animationDelay: `${i * 26}ms` }}
          onClick={() => setView(p.key)}
          title={p.label}
        >
          <span className="ico">
            <Icon name={p.key} />
          </span>
          <span style={labelStyle}>{p.label}</span>
        </button>
      ))}

      {/* 설정 pinned to the bottom-left (gateway config lives inside it). */}
      <button
        className={"nav-item" + (view === "settings" ? " active" : "")}
        style={{ marginTop: "auto" }}
        onClick={() => setView("settings")}
        title={settings.label}
      >
        <span className="ico">
          <Icon name="settings" />
        </span>
        <span style={labelStyle}>{settings.label}</span>
      </button>
    </nav>
  );
}
