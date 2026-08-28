// Which UI a webview window should mount. The Cygnus companion window is the
// same Vite bundle as the workstation; the Rust shell marks it with an
// initialization script (`window.__CYGNUS__ = true`) and the browser dev server
// can request it with `?window=cygnus`. Pure so the branch is unit-testable.
export type WindowKind = "main" | "cygnus";

export function windowKind(search: string, cygnusFlag?: unknown): WindowKind {
  if (cygnusFlag === true) return "cygnus";
  return new URLSearchParams(search).get("window") === "cygnus" ? "cygnus" : "main";
}
