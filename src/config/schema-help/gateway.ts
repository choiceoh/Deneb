export const GATEWAY_HELP: Record<string, string> = {
  gateway:
    "Gateway runtime surface for bind mode, auth, control UI, remote transport, and operational safety controls. Keep conservative defaults unless you intentionally expose the gateway beyond trusted local interfaces.",
  "gateway.port":
    "TCP port used by the gateway listener for API, control UI, and channel-facing ingress paths. Use a dedicated port and avoid collisions with reverse proxies or local developer services.",
  "gateway.mode":
    'Gateway operation mode: "local" runs channels and agent runtime on this host, while "remote" connects through remote transport. Keep "local" unless you intentionally run a split remote gateway topology.',
  "gateway.bind":
    'Network bind profile: "auto", "lan", "loopback", "custom", or "tailnet" to control interface exposure. Keep "loopback" or "auto" for safest local operation unless external clients must connect.',
  "gateway.customBindHost":
    "Explicit bind host/IP used when gateway.bind is set to custom for manual interface targeting. Use a precise address and avoid wildcard binds unless external exposure is required.",
  "gateway.controlUi":
    "Control UI hosting settings including enablement, pathing, and browser-origin/auth hardening behavior. Keep UI exposure minimal and pair with strong auth controls before internet-facing deployments.",
  "gateway.controlUi.enabled":
    "Enables serving the gateway Control UI from the gateway HTTP process when true. Keep enabled for local administration, and disable when an external control surface replaces it.",
  "gateway.auth":
    "Authentication policy for gateway HTTP/WebSocket access including mode, credentials, trusted-proxy behavior, and rate limiting. Keep auth enabled for every non-loopback deployment.",
  "gateway.auth.mode":
    'Gateway auth mode: "none", "token", "password", or "trusted-proxy" depending on your edge architecture. Use token/password for direct exposure, and trusted-proxy only behind hardened identity-aware proxies.',
  "gateway.auth.allowTailscale":
    "Allows trusted Tailscale identity paths to satisfy gateway auth checks when configured. Use this only when your tailnet identity posture is strong and operator workflows depend on it.",
  "gateway.auth.rateLimit":
    "Login/auth attempt throttling controls to reduce credential brute-force risk at the gateway boundary. Keep enabled in exposed environments and tune thresholds to your traffic baseline.",
  "gateway.auth.trustedProxy":
    "Trusted-proxy auth header mapping for upstream identity providers that inject user claims. Use only with known proxy CIDRs and strict header allowlists to prevent spoofed identity headers.",
  "gateway.trustedProxies":
    "CIDR/IP allowlist of upstream proxies permitted to provide forwarded client identity headers. Keep this list narrow so untrusted hops cannot impersonate users.",
  "gateway.allowRealIpFallback":
    "Enables x-real-ip fallback when x-forwarded-for is missing in proxy scenarios. Keep disabled unless your ingress stack requires this compatibility behavior.",
  "gateway.tools":
    "Gateway-level tool exposure allow/deny policy that can restrict runtime tool availability independent of agent/tool profiles. Use this for coarse emergency controls and production hardening.",
  "gateway.tools.allow":
    "Explicit gateway-level tool allowlist when you want a narrow set of tools available at runtime. Use this for locked-down environments where tool scope must be tightly controlled.",
  "gateway.tools.deny":
    "Explicit gateway-level tool denylist to block risky tools even if lower-level policies allow them. Use deny rules for emergency response and defense-in-depth hardening.",
  "gateway.channelHealthCheckMinutes":
    "Interval in minutes for automatic channel health probing and status updates. Use lower intervals for faster detection, or higher intervals to reduce periodic probe noise.",
  "gateway.channelStaleEventThresholdMinutes":
    "How many minutes a connected channel can go without receiving any event before the health monitor treats it as a stale socket and triggers a restart. Default: 30.",
  "gateway.channelMaxRestartsPerHour":
    "Maximum number of health-monitor-initiated channel restarts allowed within a rolling one-hour window. Once hit, further restarts are skipped until the window expires. Default: 10.",
  "gateway.tailscale":
    "Tailscale integration settings for Serve/Funnel exposure and lifecycle handling on gateway start/exit. Keep off unless your deployment intentionally relies on Tailscale ingress.",
  "gateway.tailscale.mode":
    'Tailscale publish mode: "off", "serve", or "funnel" for private or public exposure paths. Use "serve" for tailnet-only access and "funnel" only when public internet reachability is required.',
  "gateway.tailscale.resetOnExit":
    "Resets Tailscale Serve/Funnel state on gateway exit to avoid stale published routes after shutdown. Keep enabled unless another controller manages publish lifecycle outside the gateway.",
  "gateway.remote":
    "Remote gateway connection settings for direct or SSH transport when this instance proxies to another runtime host. Use remote mode only when split-host operation is intentionally configured.",
  "gateway.remote.transport":
    'Remote connection transport: "direct" uses configured URL connectivity, while "ssh" tunnels through SSH. Use SSH when you need encrypted tunnel semantics without exposing remote ports.',
  "gateway.remote.url": "Remote Gateway WebSocket URL (ws:// or wss://).",
  "gateway.remote.token":
    "Bearer token used to authenticate this client to a remote gateway in token-auth deployments. Store via secret/env substitution and rotate alongside remote gateway auth changes.",
  "gateway.remote.password":
    "Password credential used for remote gateway authentication when password mode is enabled. Keep this secret managed externally and avoid plaintext values in committed config.",
  "gateway.remote.tlsFingerprint":
    "Expected sha256 TLS fingerprint for the remote gateway (pin to avoid MITM).",
  "gateway.remote.sshTarget":
    "Remote gateway over SSH (tunnels the gateway port to localhost). Format: user@host or user@host:port.",
  "gateway.remote.sshIdentity": "Optional SSH identity file path (passed to ssh -i).",
  "gateway.reload":
    "Live config-reload policy for how edits are applied and when full restarts are triggered. Keep hybrid behavior for safest operational updates unless debugging reload internals.",
  "gateway.reload.mode":
    'Controls how config edits are applied: "off" ignores live edits, "restart" always restarts, "hot" applies in-process, and "hybrid" tries hot then restarts if required. Keep "hybrid" for safest routine updates.',
  "gateway.reload.debounceMs": "Debounce window (ms) before applying config changes.",
  "gateway.reload.deferralTimeoutMs":
    "Maximum time (ms) to wait for in-flight operations to complete before forcing a SIGUSR1 restart. Default: 300000 (5 minutes). Lower values risk aborting active subagent LLM calls.",
  "gateway.tls":
    "TLS certificate and key settings for terminating HTTPS directly in the gateway process. Use explicit certificates in production and avoid plaintext exposure on untrusted networks.",
  "gateway.tls.enabled":
    "Enables TLS termination at the gateway listener so clients connect over HTTPS/WSS directly. Keep enabled for direct internet exposure or any untrusted network boundary.",
  "gateway.tls.autoGenerate":
    "Auto-generates a local TLS certificate/key pair when explicit files are not configured. Use only for local/dev setups and replace with real certificates for production traffic.",
  "gateway.tls.certPath":
    "Filesystem path to the TLS certificate file used by the gateway when TLS is enabled. Use managed certificate paths and keep renewal automation aligned with this location.",
  "gateway.tls.keyPath":
    "Filesystem path to the TLS private key file used by the gateway when TLS is enabled. Keep this key file permission-restricted and rotate per your security policy.",
  "gateway.tls.caPath":
    "Optional CA bundle path for client verification or custom trust-chain requirements at the gateway edge. Use this when private PKI or custom certificate chains are part of deployment.",
  "gateway.http":
    "Gateway HTTP API configuration grouping endpoint toggles and transport-facing API exposure controls. Keep only required endpoints enabled to reduce attack surface.",
  "gateway.http.endpoints":
    "HTTP endpoint feature toggles under the gateway API surface for compatibility routes and optional integrations. Enable endpoints intentionally and monitor access patterns after rollout.",
  "gateway.http.securityHeaders":
    "Optional HTTP response security headers applied by the gateway process itself. Prefer setting these at your reverse proxy when TLS terminates there.",
  "gateway.http.securityHeaders.strictTransportSecurity":
    "Value for the Strict-Transport-Security response header. Set only on HTTPS origins that you fully control; use false to explicitly disable.",
  "gateway.http.endpoints.chatCompletions.enabled":
    "Enable the OpenAI-compatible `POST /v1/chat/completions` endpoint (default: false).",
  "gateway.http.endpoints.chatCompletions.maxBodyBytes":
    "Max request body size in bytes for `/v1/chat/completions` (default: 20MB).",
  "gateway.http.endpoints.chatCompletions.maxImageParts":
    "Max number of `image_url` parts accepted from the latest user message (default: 8).",
  "gateway.http.endpoints.chatCompletions.maxTotalImageBytes":
    "Max cumulative decoded bytes across all `image_url` parts in one request (default: 20MB).",
  "gateway.http.endpoints.chatCompletions.images":
    "Image fetch/validation controls for OpenAI-compatible `image_url` parts.",
  "gateway.http.endpoints.chatCompletions.images.allowUrl":
    "Allow server-side URL fetches for `image_url` parts (default: false; data URIs remain supported). Set this to `false` to disable URL fetching entirely.",
  "gateway.http.endpoints.chatCompletions.images.urlAllowlist":
    "Optional hostname allowlist for `image_url` URL fetches; supports exact hosts and `*.example.com` wildcards. Empty or omitted lists mean no hostname allowlist restriction.",
  "gateway.http.endpoints.chatCompletions.images.allowedMimes":
    "Allowed MIME types for `image_url` parts (case-insensitive list).",
  "gateway.http.endpoints.chatCompletions.images.maxBytes":
    "Max bytes per fetched/decoded `image_url` image (default: 10MB).",
  "gateway.http.endpoints.chatCompletions.images.maxRedirects":
    "Max HTTP redirects allowed when fetching `image_url` URLs (default: 3).",
  "gateway.http.endpoints.chatCompletions.images.timeoutMs":
    "Timeout in milliseconds for `image_url` URL fetches (default: 10000).",
  "gateway.auth.token":
    "Required by default for gateway access (unless using Tailscale Serve identity); required for non-loopback binds.",
  "gateway.auth.password": "Required for Tailscale funnel.",
  "gateway.controlUi.basePath": "Optional URL prefix where the Control UI is served (e.g. /deneb).",
  "gateway.controlUi.root":
    "Optional filesystem root for Control UI assets (defaults to dist/control-ui).",
  "gateway.controlUi.allowedOrigins":
    'Allowed browser origins for Control UI/WebChat websocket connections (full origins only, e.g. https://control.example.com). Required for non-loopback Control UI deployments unless dangerous Host-header fallback is explicitly enabled. Setting ["*"] means allow any browser origin and should be avoided outside tightly controlled local testing.',
  "gateway.controlUi.dangerouslyAllowHostHeaderOriginFallback":
    "DANGEROUS toggle that enables Host-header based origin fallback for Control UI/WebChat websocket checks. This mode is supported when your deployment intentionally relies on Host-header origin policy; explicit gateway.controlUi.allowedOrigins remains the recommended hardened default.",
  "gateway.controlUi.allowInsecureAuth":
    "Loosens strict browser auth checks for Control UI when you must run a non-standard setup. Keep this off unless you trust your network and proxy path, because impersonation risk is higher.",
  "gateway.controlUi.dangerouslyDisableDeviceAuth":
    "Disables Control UI device identity checks and relies on token/password only. Use only for short-lived debugging on trusted networks, then turn it off immediately.",
  "gateway.push":
    "Push-delivery settings used by the gateway when it needs to wake or notify paired devices. Configure relay-backed APNs here for official iOS builds; direct APNs auth remains env-based for local/manual builds.",
  "gateway.push.apns":
    "APNs delivery settings for iOS devices paired to this gateway. Use relay settings for official/TestFlight builds that register through the external push relay.",
  "gateway.push.apns.relay":
    "External relay settings for relay-backed APNs sends. The gateway uses this relay for push.test, wake nudges, and reconnect wakes after a paired official iOS build publishes a relay-backed registration.",
  "gateway.push.apns.relay.baseUrl":
    "Base HTTPS URL for the external APNs relay service used by official/TestFlight iOS builds. Keep this aligned with the relay URL baked into the iOS build so registration and send traffic hit the same deployment.",
  "gateway.push.apns.relay.timeoutMs":
    "Timeout in milliseconds for relay send requests from the gateway to the APNs relay (default: 10000). Increase for slower relays or networks, or lower to fail wake attempts faster.",
  "gateway.nodes.browser.mode":
    'Node browser routing ("auto" = pick single connected browser node, "manual" = require node param, "off" = disable).',
  "gateway.nodes.browser.node": "Pin browser routing to a specific node id or name (optional).",
  "gateway.nodes.allowCommands":
    "Extra node.invoke commands to allow beyond the gateway defaults (array of command strings). Enabling dangerous commands here is a security-sensitive override and is flagged by `deneb security audit`.",
  "gateway.nodes.denyCommands":
    "Node command names to block even if present in node claims or default allowlist (exact command-name matching only, e.g. `system.run`; does not inspect shell text inside that command).",
};
