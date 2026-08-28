import React from "react";
import ReactDOM from "react-dom/client";
import "pretendard/dist/web/variable/pretendardvariable.css";
import "katex/dist/katex.min.css";
import "./styles.css";
import { App } from "./App";
import { CygnusApp } from "./cygnus/CygnusApp";
import { windowKind, type WindowKind } from "./cygnus/windowKind";
import { isTauri } from "./tauri";

// Dev mock mode (`pnpm dev:mock`): start the MSW mock gateway and seed a dummy
// connection so the workstation runs fully populated with no live gateway.
async function enableMocking(): Promise<void> {
  if (!import.meta.env.VITE_MOCK) return;
  const { worker } = await import("./mocks/browser");
  await worker.start({ onUnhandledRequest: "bypass" });
  if (!localStorage.getItem("andromeda.gateway")) {
    localStorage.setItem("andromeda.gateway", JSON.stringify({ url: "http://mock.local", token: "mock-token" }));
  }
}

// The Cygnus companion window mounts its own root from the same bundle. The
// window LABEL is the canonical identity on the desktop; the ?window=cygnus
// query is both the shell's actual URL and the browser dev loop's switch; the
// __CYGNUS__ init-script flag stays as a third signal. Triple-carried because
// the real-shell (Xvfb/webkit) run showed a single carrier is fragile — the
// companion webview failed to take the cygnus branch until the query+label
// signals were added (verified live via the vite console relay).
async function resolveWindowKind(): Promise<WindowKind> {
  const fromPage = windowKind(window.location.search, (window as { __CYGNUS__?: unknown }).__CYGNUS__);
  if (fromPage === "cygnus" || !isTauri()) return fromPage;
  try {
    const { getCurrentWebviewWindow } = await import("@tauri-apps/api/webviewWindow");
    if (getCurrentWebviewWindow().label === "cygnus") return "cygnus";
  } catch {
    /* pre-Tauri or API mismatch — fall through to the page signals */
  }
  return fromPage;
}

void Promise.all([enableMocking(), resolveWindowKind()]).then(([, kind]) => {
  ReactDOM.createRoot(document.getElementById("root")!).render(
    <React.StrictMode>{kind === "cygnus" ? <CygnusApp /> : <App />}</React.StrictMode>,
  );
});
