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

#[cfg(test)]
mod tests {
    use super::urlencoded;

    #[test]
    fn urlencoded_leaves_unreserved_ascii_unchanged() {
        let input = "abcXYZ012-_.~";
        assert_eq!(urlencoded(input), input);
    }

    #[test]
    fn urlencoded_turns_spaces_into_plus() {
        assert_eq!(urlencoded("deneb docs search"), "deneb+docs+search");
    }

    #[test]
    fn urlencoded_encodes_reserved_ascii_symbols() {
        assert_eq!(urlencoded("a/b?c=d&e"), "a%2Fb%3Fc%3Dd%26e");
    }

    #[test]
    fn urlencoded_encodes_mixed_unicode_as_utf8_bytes() {
        assert_eq!(urlencoded("한글 🚀"), "%ED%95%9C%EA%B8%80+%F0%9F%9A%80");
    }

    #[test]
    fn urlencoded_encodes_percent_sign_itself() {
        assert_eq!(urlencoded("100%"), "100%25");
    }

    #[test]
    fn urlencoded_handles_empty_string() {
        assert_eq!(urlencoded(""), "");
    }
}
