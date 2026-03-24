use clap::Args;

use crate::errors::CliError;
use crate::terminal::Palette;

#[derive(Args, Debug)]
pub struct DocsArgs {
    /// Search query.
    pub query: Option<String>,

    /// Open docs in browser instead of searching.
    #[arg(long)]
    pub open: bool,
}

pub async fn run(args: &DocsArgs) -> Result<(), CliError> {
    let base_url = "https://docs.deneb.ai";

    if args.open || args.query.is_none() {
        let url = if let Some(ref q) = args.query {
            format!("{base_url}/search?q={}", urlencoded(q))
        } else {
            base_url.to_string()
        };

        match open::that(&url) {
            Ok(()) => {
                println!("{}", Palette::success().apply_to("Opened docs in browser."));
            }
            Err(_) => {
                let muted = Palette::muted();
                println!("Docs URL: {}", muted.apply_to(&url));
            }
        }
    } else if let Some(ref q) = args.query {
        let url = format!("{base_url}/search?q={}", urlencoded(q));
        let muted = Palette::muted();
        println!("Search docs for '{}': {}", q, muted.apply_to(&url));
        let _ = open::that(&url);
    }

    Ok(())
}

fn urlencoded(s: &str) -> String {
    let mut out = String::with_capacity(s.len());
    for ch in s.chars() {
        match ch {
            'A'..='Z' | 'a'..='z' | '0'..='9' | '-' | '_' | '.' | '~' => out.push(ch),
            ' ' => out.push('+'),
            _ => {
                for byte in ch.to_string().as_bytes() {
                    out.push_str(&format!("%{byte:02X}"));
                }
            }
        }
    }
    out
}
