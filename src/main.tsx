import React from "react";
import ReactDOM from "react-dom/client";
import { App } from "./App";

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

void enableMocking().then(() => {
  ReactDOM.createRoot(document.getElementById("root")!).render(
    <React.StrictMode>
      <App />
    </React.StrictMode>,
  );
});
