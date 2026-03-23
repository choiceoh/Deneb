export const BROWSER_HELP: Record<string, string> = {
  browser:
    "Browser runtime controls for local or remote CDP attachment, profile routing, and screenshot/snapshot behavior. Keep defaults unless your automation workflow requires custom browser transport settings.",
  "browser.enabled":
    "Enables browser capability wiring in the gateway so browser tools and CDP-driven workflows can run. Disable when browser automation is not needed to reduce surface area and startup work.",
  "browser.cdpUrl":
    "Remote CDP websocket URL used to attach to an externally managed browser instance. Use this for centralized browser hosts and keep URL access restricted to trusted network paths.",
  "browser.color":
    "Default accent color used for browser profile/UI cues where colored identity hints are displayed. Use consistent colors to help operators identify active browser profile context quickly.",
  "browser.executablePath":
    "Explicit browser executable path when auto-discovery is insufficient for your host environment. Use absolute stable paths so launch behavior stays deterministic across restarts.",
  "browser.headless":
    "Forces browser launch in headless mode when the local launcher starts browser instances. Keep headless enabled for server environments and disable only when visible UI debugging is required.",
  "browser.noSandbox":
    "Disables Chromium sandbox isolation flags for environments where sandboxing fails at runtime. Keep this off whenever possible because process isolation protections are reduced.",
  "browser.attachOnly":
    "Restricts browser mode to attach-only behavior without starting local browser processes. Use this when all browser sessions are externally managed by a remote CDP provider.",
  "browser.cdpPortRangeStart":
    "Starting local CDP port used for auto-allocated browser profile ports. Increase this when host-level port defaults conflict with other local services.",
  "browser.defaultProfile":
    "Default browser profile name selected when callers do not explicitly choose a profile. Use a stable low-privilege profile as the default to reduce accidental cross-context state use.",
  "browser.profiles":
    "Named browser profile connection map used for explicit routing to CDP ports or URLs with optional metadata. Keep profile names consistent and avoid overlapping endpoint definitions.",
  "browser.profiles.*.cdpPort":
    "Per-profile local CDP port used when connecting to browser instances by port instead of URL. Use unique ports per profile to avoid connection collisions.",
  "browser.profiles.*.cdpUrl":
    "Per-profile CDP websocket URL used for explicit remote browser routing by profile name. Use this when profile connections terminate on remote hosts or tunnels.",
  "browser.profiles.*.userDataDir":
    "Per-profile Chromium user data directory for existing-session attachment through Chrome DevTools MCP. Use this for host-local Brave, Edge, Chromium, or non-default Chrome profiles when the built-in auto-connect path would pick the wrong browser data directory.",
  "browser.profiles.*.driver":
    'Per-profile browser driver mode. Use "deneb" (or legacy "clawd") for CDP-based profiles, or use "existing-session" for host-local Chrome DevTools MCP attachment.',
  "browser.profiles.*.attachOnly":
    "Per-profile attach-only override that skips local browser launch and only attaches to an existing CDP endpoint. Useful when one profile is externally managed but others are locally launched.",
  "browser.profiles.*.color":
    "Per-profile accent color for visual differentiation in dashboards and browser-related UI hints. Use distinct colors for high-signal operator recognition of active profiles.",
  "browser.evaluateEnabled":
    "Enables browser-side evaluate helpers for runtime script evaluation capabilities where supported. Keep disabled unless your workflows require evaluate semantics beyond snapshots/navigation.",
  "browser.snapshotDefaults":
    "Default snapshot capture configuration used when callers do not provide explicit snapshot options. Tune this for consistent capture behavior across channels and automation paths.",
  "browser.snapshotDefaults.mode":
    "Default snapshot extraction mode controlling how page content is transformed for agent consumption. Choose the mode that balances readability, fidelity, and token footprint for your workflows.",
  "browser.ssrfPolicy":
    "Server-side request forgery guardrail settings for browser/network fetch paths that could reach internal hosts. Keep restrictive defaults in production and open only explicitly approved targets.",
  "browser.ssrfPolicy.allowPrivateNetwork":
    "Legacy alias for browser.ssrfPolicy.dangerouslyAllowPrivateNetwork. Prefer the dangerously-named key so risk intent is explicit.",
  "browser.ssrfPolicy.dangerouslyAllowPrivateNetwork":
    "Allows access to private-network address ranges from browser tooling. Default is enabled for trusted-network operator setups; disable to enforce strict public-only resolution checks.",
  "browser.ssrfPolicy.allowedHostnames":
    "Explicit hostname allowlist exceptions for SSRF policy checks on browser/network requests. Keep this list minimal and review entries regularly to avoid stale broad access.",
  "browser.ssrfPolicy.hostnameAllowlist":
    "Legacy/alternate hostname allowlist field used by SSRF policy consumers for explicit host exceptions. Use stable exact hostnames and avoid wildcard-like broad patterns.",
  "browser.remoteCdpTimeoutMs":
    "Timeout in milliseconds for connecting to a remote CDP endpoint before failing the browser attach attempt. Increase for high-latency tunnels, or lower for faster failure detection.",
  "browser.remoteCdpHandshakeTimeoutMs":
    "Timeout in milliseconds for post-connect CDP handshake readiness checks against remote browser targets. Raise this for slow-start remote browsers and lower to fail fast in automation loops.",
};
