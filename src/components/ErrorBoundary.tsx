import { Component, type ReactNode } from "react";
import { log } from "../log";
import { color, pane } from "../theme";

const uiLog = log.child("ui");

interface Props {
  children: ReactNode;
}
interface State {
  error: Error | null;
}

// Catches render-time crashes so a bug in one pane shows a readable, recoverable
// error (with the stack) instead of a blank white screen — and logs it.
export class ErrorBoundary extends Component<Props, State> {
  state: State = { error: null };

  static getDerivedStateFromError(error: Error): State {
    return { error };
  }

  componentDidCatch(error: Error, info: { componentStack?: string }): void {
    uiLog.error("render error:", error.message, info.componentStack ?? "");
  }

  reset = () => this.setState({ error: null });

  render(): ReactNode {
    const { error } = this.state;
    if (!error) return this.props.children;
    return (
      <div style={{ ...pane, fontFamily: "system-ui, sans-serif", color: color.text }}>
        <h2>문제가 발생했습니다</h2>
        <p style={{ color: color.danger }}>{error.message}</p>
        {error.stack && (
          <pre style={{ whiteSpace: "pre-wrap", fontSize: 12, opacity: 0.6, maxHeight: "40vh", overflow: "auto" }}>
            {error.stack}
          </pre>
        )}
        <button onClick={this.reset} style={{ padding: "6px 12px", marginTop: 8 }}>
          다시 시도
        </button>
      </div>
    );
  }
}
