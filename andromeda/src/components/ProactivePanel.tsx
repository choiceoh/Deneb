import type { GatewayConfig } from "@/gateway";
import { useEvents } from "@/hooks";
import { useWorkspace } from "@/workspaceContext";
import { Icon } from "./Icon";

// Proactive nudges pushed by Deneb (events SSE). Sits atop the AI panel; renders
// nothing until something arrives, so it stays out of the way when quiet. Each
// nudge gets a warm accent rule on its left.
export function ProactivePanel({ cfg }: { cfg: GatewayConfig }) {
  const { connected } = useWorkspace();
  const { events, dismiss } = useEvents(cfg, connected);
  if (events.length === 0) return null;

  return (
    <div style={{ display: "grid", gap: 7, maxHeight: 180, overflow: "auto", marginBottom: 13 }}>
      {events.map((e) => (
        <div
          key={e.id}
          className="proactive-nudge"
          style={{
            display: "flex",
            gap: 8,
            alignItems: "flex-start",
            borderLeft: "2px solid var(--accent)",
            padding: "1px 0 1px 10px",
          }}
        >
          <div style={{ flex: 1, minWidth: 0 }}>
            <div style={{ fontSize: 12, fontWeight: 500, color: "var(--ink-2)" }}>{e.title ?? e.kind ?? "알림"}</div>
            {e.body && <div style={{ fontSize: 11, color: "var(--muted)", lineHeight: 1.45 }}>{e.body}</div>}
          </div>
          <button
            onClick={() => dismiss(e.id)}
            title="닫기"
            aria-label="닫기"
            style={{
              background: "none",
              border: "none",
              color: "var(--muted-2)",
              cursor: "pointer",
              padding: 2,
              display: "inline-flex",
            }}
          >
            <Icon name="close" size={13} />
          </button>
        </div>
      ))}
    </div>
  );
}
