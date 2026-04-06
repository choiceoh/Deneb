use clap::Args;

use crate::config;
use crate::errors::CliError;
use crate::terminal::Palette;

#[derive(Args, Debug)]
pub struct DashboardArgs {
    /// Print URL but do not open the browser.
    #[arg(long)]
    pub no_open: bool,
}

pub async fn run(args: &DashboardArgs) -> Result<(), CliError> {
    let config_path = config::resolve_config_path();
    let cfg = config::load_config_best_effort(&config_path);

    // Resolve HTTP base URL
    let port = config::resolve_gateway_port(cfg.gateway_port());
    let http_url = if cfg.is_remote_mode() {
        if let Some(url) = cfg.remote_url() {
            // Convert ws(s):// to http(s)://
            url.replace("wss://", "https://")
                .replace("ws://", "http://")
        } else {
            return Err(CliError::User(
                "Remote mode configured but no gateway.remote.url set.".to_string(),
            ));
        }
    } else {
        format!("http://localhost:{port}")
    };

    // Resolve auth token for URL fragment
    let token = cfg.auth_token().map(std::string::ToString::to_string);

    // Build dashboard URL
    let dashboard_url = if let Some(ref token) = token {
        // Use URL fragment to avoid query param leakage in referrer
        let encoded = urlencoded_token(token);
        format!("{http_url}#token={encoded}")
    } else {
        http_url.clone()
    };

    let muted = Palette::muted();
    println!();
    println!("    Dashboard   {}", muted.apply_to(&dashboard_url));

    if !args.no_open {
        match open::that(&dashboard_url) {
            Ok(()) => {
                use crate::terminal::Symbols;
                let success = Palette::success();
                println!(
                    "    {}  {}",
                    success.apply_to(Symbols::SUCCESS),
                    success.apply_to("Opened in browser")
                );
            }
            Err(_) => {
                use crate::terminal::Symbols;
                let warn = Palette::warn();
                println!(
                    "    {}  {}",
                    warn.apply_to(Symbols::WARNING),
                    warn.apply_to("Could not open browser — copy the URL above")
                );
            }
        }
    }
    println!();

    Ok(())
}

/// Minimal percent-encoding for a token value in a URL fragment.
fn urlencoded_token(token: &str) -> String {
    let mut out = String::with_capacity(token.len());
    for ch in token.chars() {
        match ch {
            'A'..='Z' | 'a'..='z' | '0'..='9' | '-' | '_' | '.' | '~' => out.push(ch),
            _ => {
                for byte in ch.to_string().as_bytes() {
                    out.push_str(&format!("%{byte:02X}"));
                }
            }
        }
    }
    out
}
