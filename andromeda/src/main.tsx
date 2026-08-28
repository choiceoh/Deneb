import React from "react";
import ReactDOM from "react-dom/client";
import "pretendard/dist/web/variable/pretendardvariable.css";
import "katex/dist/katex.min.css";
import "./styles.css";
import { App } from "./App";
import { CygnusApp } from "./cygnus/CygnusApp";
import { windowKind } from "./cygnus/windowKind";

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

// The Cygnus companion window mounts its own root from the same bundle — the
// Rust shell flags it via init script; the dev server via ?window=cygnus.
const kind = windowKind(window.location.search, (window as { __CYGNUS__?: unknown }).__CYGNUS__);

void enableMocking().then(() => {
  ReactDOM.createRoot(document.getElementById("root")!).render(
    <React.StrictMode>{kind === "cygnus" ? <CygnusApp /> : <App />}</React.StrictMode>,
  );
});
