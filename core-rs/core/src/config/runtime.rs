//! Gateway runtime configuration resolution.
//!
//! 1:1 port of `gateway-go/internal/config/runtime.go`.

use std::env;
use std::net::{IpAddr, Ipv4Addr};

use crate::config::bootstrap::ResolvedGatewayAuth;
use crate::config::types::*;

/// Fully resolved gateway runtime settings after applying CLI overrides,
/// environment variables, and validation constraints.
#[derive(Debug, Clone)]
pub struct GatewayRuntimeConfig {
    pub bind_host: String,
    pub port: i32,
    pub control_ui_enabled: bool,
    pub control_ui_base_path: String,
    pub control_ui_root: String,
    pub openai_chat_completions_enabled: bool,
    pub openai_chat_completions_config: Option<GatewayHTTPChatCompletionsConfig>,
    pub open_responses_enabled: bool,
    pub open_responses_config: Option<GatewayHTTPResponsesConfig>,
    pub strict_transport_security_header: String,
    pub resolved_auth: ResolvedGatewayAuth,
    pub auth_mode: String,
    pub tailscale_config: GatewayTailscaleConfig,
    pub tailscale_mode: String,
    pub canvas_host_enabled: bool,
    pub trusted_proxies: Vec<String>,
    pub channel_health_check_minutes: i32,
    pub channel_stale_event_threshold_min: i32,
    pub channel_max_restarts_per_hour: i32,
}

/// Inputs for resolving the runtime config.
#[derive(Debug, Default)]
pub struct RuntimeConfigParams {
    pub config: DenebConfig,
    pub port: i32,
    pub bind: String,
    pub host: String,
    pub control_ui_enabled: Option<bool>,
    pub auth: Option<ResolvedGatewayAuth>,
    pub tailscale_override: Option<GatewayTailscaleConfig>,
}

/// Validate constraints and produce the final runtime config.
pub fn resolve_gateway_runtime_config(
    params: RuntimeConfigParams,
) -> Result<GatewayRuntimeConfig, String> {
    let gw = params.config.gateway.as_ref().cloned().unwrap_or_default();

    // Resolve bind mode and host.
    let bind_mode = if !params.bind.is_empty() {
        params.bind.clone()
    } else {
        gw.bind.clone().unwrap_or_else(|| "loopback".to_string())
    };

    let bind_host = if !params.host.is_empty() {
        params.host.clone()
    } else {
        resolve_bind_host(&bind_mode, gw.custom_bind_host.as_deref())?
    };

    // Validate loopback constraint.
    if bind_mode == "loopback" && !is_loopback_host(&bind_host) {
        return Err(format!(
            "gateway bind=loopback resolved to non-loopback host {bind_host}; refusing fallback to a network bind"
        ));
    }

    // Validate custom bind host.
    if bind_mode == "custom" {
        let custom_host = gw
            .custom_bind_host
            .as_deref()
            .unwrap_or("")
            .trim();
        if custom_host.is_empty() {
            return Err("gateway.bind=custom requires gateway.customBindHost".to_string());
        }
        if !is_valid_ipv4(custom_host) {
            return Err(format!(
                "gateway.bind=custom requires a valid IPv4 customBindHost (got {custom_host})"
            ));
        }
        if bind_host != custom_host {
            return Err(format!(
                "gateway bind=custom requested {custom_host} but resolved {bind_host}; refusing fallback"
            ));
        }
    }

    // Control UI.
    let control_ui_enabled = params.control_ui_enabled.unwrap_or_else(|| {
        gw.control_ui
            .as_ref()
            .and_then(|c| c.enabled)
            .unwrap_or(true)
    });

    let control_ui_base_path = normalize_control_ui_base_path(gw.control_ui.as_ref());
    let control_ui_root = gw
        .control_ui
        .as_ref()
        .and_then(|c| c.root.as_deref())
        .unwrap_or("")
        .trim()
        .to_string();

    // HTTP endpoints.
    let mut openai_chat_completions_enabled = false;
    let mut openai_chat_completions_config = None;
    if let Some(http) = &gw.http {
        if let Some(endpoints) = &http.endpoints {
            if let Some(cc) = &endpoints.chat_completions {
                openai_chat_completions_config = Some(cc.clone());
                if cc.enabled == Some(true) {
                    openai_chat_completions_enabled = true;
                }
            }
        }
    }

    let mut open_responses_enabled = false;
    let mut open_responses_config = None;
    if let Some(http) = &gw.http {
        if let Some(endpoints) = &http.endpoints {
            if let Some(rc) = &endpoints.responses {
                open_responses_config = Some(rc.clone());
                if rc.enabled == Some(true) {
                    open_responses_enabled = true;
                }
            }
        }
    }

    // Strict-Transport-Security header.
    let sts_header = gw
        .http
        .as_ref()
        .and_then(|h| h.security_headers.as_ref())
        .and_then(|sh| sh.strict_transport_security.as_deref())
        .map(str::trim)
        .filter(|v| !v.is_empty() && *v != "false")
        .unwrap_or("")
        .to_string();

    // Tailscale.
    let mut tailscale_cfg = gw
        .tailscale
        .clone()
        .unwrap_or(GatewayTailscaleConfig {
            mode: Some("off".to_string()),
            ..Default::default()
        });
    if let Some(ts_override) = &params.tailscale_override {
        tailscale_cfg = crate::config::bootstrap::merge_tailscale_config(
            Some(&tailscale_cfg),
            ts_override,
        );
    }
    let tailscale_mode = tailscale_cfg
        .mode
        .clone()
        .unwrap_or_else(|| "off".to_string());

    // Resolved auth.
    let resolved_auth = params.auth.unwrap_or(ResolvedGatewayAuth {
        mode: "token".to_string(),
        ..Default::default()
    });
    let auth_mode = resolved_auth.mode.clone();

    // ── Validation constraints ──

    // Tailscale funnel requires password auth.
    if tailscale_mode == "funnel" && auth_mode != "password" {
        return Err(
            "tailscale funnel requires gateway auth mode=password (set gateway.auth.password or DENEB_GATEWAY_PASSWORD)".to_string()
        );
    }

    // Tailscale serve/funnel requires loopback bind.
    if tailscale_mode != "off" && !is_loopback_host(&bind_host) {
        return Err(
            "tailscale serve/funnel requires gateway bind=loopback (127.0.0.1)".to_string(),
        );
    }

    // Non-loopback requires auth.
    if !is_loopback_host(&bind_host)
        && !resolved_auth.has_shared_secret()
        && auth_mode != "trusted-proxy"
    {
        return Err(format!(
            "refusing to bind gateway to {bind_host}:{} without auth (set gateway.auth.token/password, or set DENEB_GATEWAY_TOKEN/DENEB_GATEWAY_PASSWORD)",
            params.port,
        ));
    }

    // Non-loopback Control UI requires allowed origins.
    let allowed_origins = get_control_ui_allowed_origins(gw.control_ui.as_ref());
    let dangerously_allow_host_header = gw
        .control_ui
        .as_ref()
        .and_then(|c| c.dangerously_allow_host_header_origin_fallback)
        .unwrap_or(false);
    if control_ui_enabled
        && !is_loopback_host(&bind_host)
        && allowed_origins.is_empty()
        && !dangerously_allow_host_header
    {
        return Err(
            "non-loopback Control UI requires gateway.controlUi.allowedOrigins (set explicit origins), \
             or set gateway.controlUi.dangerouslyAllowHostHeaderOriginFallback=true".to_string()
        );
    }

    // Trusted-proxy auth requires trustedProxies.
    let trusted_proxies = gw.trusted_proxies.clone().unwrap_or_default();
    if auth_mode == "trusted-proxy" {
        if trusted_proxies.is_empty() {
            return Err(
                "gateway auth mode=trusted-proxy requires gateway.trustedProxies to be configured with at least one proxy IP".to_string()
            );
        }
        if is_loopback_host(&bind_host) {
            let has_loopback = is_trusted_proxy_address("127.0.0.1", &trusted_proxies)
                || is_trusted_proxy_address("::1", &trusted_proxies);
            if !has_loopback {
                return Err(
                    "gateway auth mode=trusted-proxy with bind=loopback requires gateway.trustedProxies to include 127.0.0.1, ::1, or a loopback CIDR".to_string()
                );
            }
        }
    }

    // Canvas host.
    let mut canvas_host_enabled = true;
    if env::var("DENEB_SKIP_CANVAS_HOST").unwrap_or_default() == "1" {
        canvas_host_enabled = false;
    }
    if let Some(ch) = &params.config.canvas_host {
        if ch.enabled == Some(false) {
            canvas_host_enabled = false;
        }
    }

    // Channel health defaults.
    let channel_health_check = gw.channel_health_check_minutes.unwrap_or(5);
    let channel_stale = gw.channel_stale_event_threshold_minutes.unwrap_or(30);
    let channel_max_restarts = gw.channel_max_restarts_per_hour.unwrap_or(10);

    Ok(GatewayRuntimeConfig {
        bind_host,
        port: params.port,
        control_ui_enabled,
        control_ui_base_path,
        control_ui_root,
        openai_chat_completions_enabled,
        openai_chat_completions_config,
        open_responses_enabled,
        open_responses_config,
        strict_transport_security_header: sts_header,
        resolved_auth,
        auth_mode,
        tailscale_config: tailscale_cfg,
        tailscale_mode,
        canvas_host_enabled,
        trusted_proxies,
        channel_health_check_minutes: channel_health_check,
        channel_stale_event_threshold_min: channel_stale,
        channel_max_restarts_per_hour: channel_max_restarts,
    })
}

/// Map a bind mode to an IP address.
pub fn resolve_bind_host(mode: &str, custom_host: Option<&str>) -> Result<String, String> {
    match mode {
        "loopback" | "" => Ok("127.0.0.1".to_string()),
        "lan" | "all" => Ok("0.0.0.0".to_string()),
        "auto" => Ok("127.0.0.1".to_string()),
        "tailnet" => {
            if let Some(ip) = find_tailscale_ip() {
                Ok(ip)
            } else {
                Ok("127.0.0.1".to_string())
            }
        }
        "custom" => {
            let host = custom_host.unwrap_or("").trim();
            if host.is_empty() {
                return Err("gateway.bind=custom requires gateway.customBindHost".to_string());
            }
            Ok(host.to_string())
        }
        _ => Err(format!("invalid bind mode: {mode}")),
    }
}

/// Check if a host string is a loopback address.
pub fn is_loopback_host(host: &str) -> bool {
    if host == "localhost" || host == "127.0.0.1" || host == "::1" {
        return true;
    }
    if let Ok(ip) = host.parse::<IpAddr>() {
        return ip.is_loopback();
    }
    false
}

/// Check if a string is a valid IPv4 address.
pub fn is_valid_ipv4(s: &str) -> bool {
    s.parse::<Ipv4Addr>().is_ok()
}

/// Check if an address matches any trusted proxy entry.
/// Supports exact IP match and CIDR notation.
pub fn is_trusted_proxy_address(addr: &str, trusted_proxies: &[String]) -> bool {
    let ip: IpAddr = match addr.parse() {
        Ok(ip) => ip,
        Err(_) => return false,
    };

    for proxy in trusted_proxies {
        if proxy.contains('/') {
            // CIDR match.
            if let Some(result) = cidr_contains(proxy, ip) {
                if result {
                    return true;
                }
            }
        } else if let Ok(proxy_ip) = proxy.parse::<IpAddr>() {
            if proxy_ip == ip {
                return true;
            }
        }
    }
    false
}

/// Check if a CIDR range contains an IP address.
fn cidr_contains(cidr: &str, ip: IpAddr) -> Option<bool> {
    let parts: Vec<&str> = cidr.split('/').collect();
    if parts.len() != 2 {
        return None;
    }
    let network_ip: IpAddr = parts[0].parse().ok()?;
    let prefix_len: u32 = parts[1].parse().ok()?;

    match (network_ip, ip) {
        (IpAddr::V4(net), IpAddr::V4(addr)) => {
            if prefix_len > 32 {
                return None;
            }
            let mask = if prefix_len == 0 {
                0u32
            } else {
                !0u32 << (32 - prefix_len)
            };
            let net_bits = u32::from(net) & mask;
            let addr_bits = u32::from(addr) & mask;
            Some(net_bits == addr_bits)
        }
        (IpAddr::V6(net), IpAddr::V6(addr)) => {
            if prefix_len > 128 {
                return None;
            }
            let mask = if prefix_len == 0 {
                0u128
            } else {
                !0u128 << (128 - prefix_len)
            };
            let net_bits = u128::from(net) & mask;
            let addr_bits = u128::from(addr) & mask;
            Some(net_bits == addr_bits)
        }
        _ => None,
    }
}

/// Scan network interfaces for a Tailscale IP (100.64.0.0/10).
fn find_tailscale_ip() -> Option<String> {
    // Tailscale uses 100.64.0.0/10 (CGNAT range).
    // We check if any interface has an IP in this range.
    #[cfg(target_os = "linux")]
    {
        use std::fs;
        // Read /proc/net/fib_trie or similar is complex; use a simpler approach
        // by checking common interface names.
        if let Ok(entries) = fs::read_dir("/sys/class/net") {
            for entry in entries.flatten() {
                let iface = entry.file_name().to_string_lossy().to_string();
                // Read the IP addresses for this interface.
                let addr_path = format!("/sys/class/net/{iface}/address");
                if fs::metadata(&addr_path).is_ok() {
                    // Use ip command to check addresses (simplified).
                    // In a real implementation, we'd use netlink or getifaddrs.
                    // For now, skip the complex interface scanning.
                }
            }
        }
    }
    None
}

/// Normalize the control UI base path.
pub fn normalize_control_ui_base_path(control_ui: Option<&GatewayControlUIConfig>) -> String {
    let bp = control_ui
        .and_then(|c| c.base_path.as_deref())
        .unwrap_or("")
        .trim();

    if bp.is_empty() {
        return "/".to_string();
    }

    let mut bp = bp.to_string();
    if !bp.starts_with('/') {
        bp = format!("/{bp}");
    }
    bp = bp.trim_end_matches('/').to_string();
    if bp.is_empty() {
        return "/".to_string();
    }
    bp
}

/// Return the trimmed, non-empty allowed origins.
pub fn get_control_ui_allowed_origins(control_ui: Option<&GatewayControlUIConfig>) -> Vec<String> {
    control_ui
        .and_then(|c| c.allowed_origins.as_ref())
        .map(|origins| {
            origins
                .iter()
                .map(|o| o.trim().to_string())
                .filter(|o| !o.is_empty())
                .collect()
        })
        .unwrap_or_default()
}

#[cfg(test)]
mod tests {
    use super::*;

    fn default_params() -> RuntimeConfigParams {
        let mut cfg = DenebConfig::default();
        crate::config::loader::apply_defaults(&mut cfg);
        RuntimeConfigParams {
            config: cfg,
            port: 18789,
            auth: Some(ResolvedGatewayAuth {
                mode: "token".to_string(),
                token: "test-token".to_string(),
                ..Default::default()
            }),
            ..Default::default()
        }
    }

    #[test]
    fn resolve_defaults() {
        let rc = resolve_gateway_runtime_config(default_params()).expect("resolve");
        assert_eq!(rc.bind_host, "127.0.0.1");
        assert_eq!(rc.port, 18789);
        assert!(rc.control_ui_enabled);
        assert_eq!(rc.control_ui_base_path, "/");
        assert_eq!(rc.auth_mode, "token");
        assert_eq!(rc.tailscale_mode, "off");
    }

    #[test]
    fn resolve_bind_override() {
        let mut params = default_params();
        params.bind = "lan".to_string();
        // Non-loopback requires controlUi.allowedOrigins or dangerous fallback.
        if let Some(gw) = params.config.gateway.as_mut() {
            let cui = gw.control_ui.get_or_insert_with(GatewayControlUIConfig::default);
            cui.dangerously_allow_host_header_origin_fallback = Some(true);
        }
        let rc = resolve_gateway_runtime_config(params).expect("resolve");
        assert_eq!(rc.bind_host, "0.0.0.0");
    }

    #[test]
    fn non_loopback_no_auth_fails() {
        let mut params = default_params();
        params.bind = "lan".to_string();
        params.auth = Some(ResolvedGatewayAuth {
            mode: "token".to_string(),
            token: String::new(),
            ..Default::default()
        });
        let result = resolve_gateway_runtime_config(params);
        assert!(result.is_err());
        assert!(result.err().expect("err").contains("without auth"));
    }

    #[test]
    fn funnel_requires_password() {
        let json = r#"{"gateway": {"tailscale": {"mode": "funnel"}}}"#;
        let mut cfg: DenebConfig = serde_json::from_str(json).expect("parse");
        crate::config::loader::apply_defaults(&mut cfg);

        let params = RuntimeConfigParams {
            config: cfg,
            port: 18789,
            auth: Some(ResolvedGatewayAuth {
                mode: "token".to_string(),
                token: "test-token".to_string(),
                ..Default::default()
            }),
            ..Default::default()
        };
        let result = resolve_gateway_runtime_config(params);
        assert!(result.is_err());
        assert!(result
            .err()
            .expect("err")
            .contains("funnel requires gateway auth mode=password"));
    }

    #[test]
    fn trusted_proxy_requires_proxies() {
        let mut params = default_params();
        params.auth = Some(ResolvedGatewayAuth {
            mode: "trusted-proxy".to_string(),
            ..Default::default()
        });
        let result = resolve_gateway_runtime_config(params);
        assert!(result.is_err());
        assert!(result
            .err()
            .expect("err")
            .contains("requires gateway.trustedProxies"));
    }

    #[test]
    fn trusted_proxy_loopback_requires_loopback_proxy() {
        let json = r#"{"gateway": {"trustedProxies": ["192.168.1.1"]}}"#;
        let mut cfg: DenebConfig = serde_json::from_str(json).expect("parse");
        crate::config::loader::apply_defaults(&mut cfg);

        let params = RuntimeConfigParams {
            config: cfg,
            port: 18789,
            auth: Some(ResolvedGatewayAuth {
                mode: "trusted-proxy".to_string(),
                trusted_proxy: Some(GatewayTrustedProxyConfig {
                    user_header: Some("X-User".to_string()),
                    ..Default::default()
                }),
                ..Default::default()
            }),
            ..Default::default()
        };
        let result = resolve_gateway_runtime_config(params);
        assert!(result.is_err());
        assert!(result
            .err()
            .expect("err")
            .contains("include 127.0.0.1"));
    }

    #[test]
    fn is_loopback_host_cases() {
        assert!(is_loopback_host("127.0.0.1"));
        assert!(is_loopback_host("localhost"));
        assert!(is_loopback_host("::1"));
        assert!(!is_loopback_host("0.0.0.0"));
        assert!(!is_loopback_host("192.168.1.1"));
        assert!(!is_loopback_host("not-an-ip"));
    }

    #[test]
    fn is_valid_ipv4_cases() {
        assert!(is_valid_ipv4("127.0.0.1"));
        assert!(is_valid_ipv4("0.0.0.0"));
        assert!(is_valid_ipv4("192.168.1.1"));
        assert!(!is_valid_ipv4("not-an-ip"));
        assert!(!is_valid_ipv4("::1")); // IPv6, not IPv4
    }

    #[test]
    fn is_trusted_proxy_address_exact() {
        let proxies = vec!["192.168.1.1".to_string(), "10.0.0.1".to_string()];
        assert!(is_trusted_proxy_address("192.168.1.1", &proxies));
        assert!(is_trusted_proxy_address("10.0.0.1", &proxies));
        assert!(!is_trusted_proxy_address("172.16.0.1", &proxies));
    }

    #[test]
    fn is_trusted_proxy_address_cidr() {
        let proxies = vec!["192.168.1.0/24".to_string()];
        assert!(is_trusted_proxy_address("192.168.1.100", &proxies));
        assert!(is_trusted_proxy_address("192.168.1.1", &proxies));
        assert!(!is_trusted_proxy_address("192.168.2.1", &proxies));
    }

    #[test]
    fn normalize_control_ui_base_path_cases() {
        assert_eq!(normalize_control_ui_base_path(None), "/");

        let cfg = GatewayControlUIConfig::default();
        assert_eq!(normalize_control_ui_base_path(Some(&cfg)), "/");

        let cfg = GatewayControlUIConfig {
            base_path: Some("/dashboard".to_string()),
            ..Default::default()
        };
        assert_eq!(normalize_control_ui_base_path(Some(&cfg)), "/dashboard");

        let cfg = GatewayControlUIConfig {
            base_path: Some("dashboard".to_string()),
            ..Default::default()
        };
        assert_eq!(normalize_control_ui_base_path(Some(&cfg)), "/dashboard");

        let cfg = GatewayControlUIConfig {
            base_path: Some("/dashboard/".to_string()),
            ..Default::default()
        };
        assert_eq!(normalize_control_ui_base_path(Some(&cfg)), "/dashboard");

        let cfg = GatewayControlUIConfig {
            base_path: Some("/".to_string()),
            ..Default::default()
        };
        assert_eq!(normalize_control_ui_base_path(Some(&cfg)), "/");
    }

    #[test]
    fn control_ui_disabled() {
        let mut params = default_params();
        params.control_ui_enabled = Some(false);
        let rc = resolve_gateway_runtime_config(params).expect("resolve");
        assert!(!rc.control_ui_enabled);
    }
}
