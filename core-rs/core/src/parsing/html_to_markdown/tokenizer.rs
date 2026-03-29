//! HTML tokenizer — single-pass scan producing a token stream.
//!
//! Replaces the 14-pass multi-scan architecture with a single linear
//! traversal. Tokens borrow from the input (`&'a str`) for zero-copy
//! text spans.

use memchr::memchr2;

use super::entities::try_decode_entity;

// ---------------------------------------------------------------------------
// TagName — known HTML tag identifiers for O(1) matching in the emitter.
// ---------------------------------------------------------------------------

/// Known HTML tag names. `Other` covers any unrecognized tag.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub(crate) enum TagName {
    Script,
    Style,
    Noscript,
    A,
    B,
    Strong,
    Em,
    I,
    S,
    Del,
    Strike,
    H1,
    H2,
    H3,
    H4,
    H5,
    H6,
    Pre,
    Code,
    Img,
    Blockquote,
    Table,
    Tr,
    Th,
    Td,
    Ol,
    Ul,
    Li,
    Br,
    Hr,
    P,
    Div,
    Section,
    Article,
    Header,
    Footer,
    Title,
    Other,
}

impl TagName {
    /// Match a lowercase tag name slice to a known variant.
    fn from_lower(s: &str) -> Self {
        match s {
            "script" => Self::Script,
            "style" => Self::Style,
            "noscript" => Self::Noscript,
            "a" => Self::A,
            "b" => Self::B,
            "strong" => Self::Strong,
            "em" => Self::Em,
            "i" => Self::I,
            "s" => Self::S,
            "del" => Self::Del,
            "strike" => Self::Strike,
            "h1" => Self::H1,
            "h2" => Self::H2,
            "h3" => Self::H3,
            "h4" => Self::H4,
            "h5" => Self::H5,
            "h6" => Self::H6,
            "pre" => Self::Pre,
            "code" => Self::Code,
            "img" => Self::Img,
            "blockquote" => Self::Blockquote,
            "table" => Self::Table,
            "tr" => Self::Tr,
            "th" => Self::Th,
            "td" => Self::Td,
            "ol" => Self::Ol,
            "ul" => Self::Ul,
            "li" => Self::Li,
            "br" => Self::Br,
            "hr" => Self::Hr,
            "p" => Self::P,
            "div" => Self::Div,
            "section" => Self::Section,
            "article" => Self::Article,
            "header" => Self::Header,
            "footer" => Self::Footer,
            "title" => Self::Title,
            _ => Self::Other,
        }
    }

    /// Tags that are void (self-closing by HTML spec, no closing tag).
    pub(crate) fn is_void(self) -> bool {
        matches!(self, Self::Br | Self::Hr | Self::Img)
    }
}

// ---------------------------------------------------------------------------
// Token — the output of the tokenizer.
// ---------------------------------------------------------------------------

/// A single HTML token. Text spans borrow from the input.
#[derive(Debug)]
pub(crate) enum Token<'a> {
    /// Raw text between tags.
    Text(&'a str),
    /// Opening tag `<tagname ...>`. `raw` is the full tag including `<` and `>`.
    TagOpen { name: TagName, raw: &'a str },
    /// Closing tag `</tagname>`.
    TagClose(TagName),
    /// Self-closing tag `<tagname .../>`. `raw` is the full tag.
    SelfClosing { name: TagName, raw: &'a str },
    /// Successfully decoded HTML entity.
    Entity(char),
    /// An `&` that could not be decoded as an entity — emit as literal.
    AmpersandLiteral,
}

// ---------------------------------------------------------------------------
// tokenize() — the main entry point.
// ---------------------------------------------------------------------------

/// Tokenize HTML input into a stream of tokens in a single pass.
pub(crate) fn tokenize<'a>(input: &'a str) -> Vec<Token<'a>> {
    let bytes = input.as_bytes();
    let len = bytes.len();
    // Rough heuristic: most HTML has more text than tags.
    let mut tokens = Vec::with_capacity(len / 8);
    let mut cursor = 0;
    let mut text_start = 0;

    while cursor < len {
        // Fast scan for the next `<` or `&` using memchr.
        let remaining = &bytes[cursor..];
        match memchr2(b'<', b'&', remaining) {
            None => break, // rest is plain text
            Some(offset) => {
                let pos = cursor + offset;
                // Flush accumulated text before this marker.
                if pos > text_start {
                    if let Some(s) = input.get(text_start..pos) {
                        tokens.push(Token::Text(s));
                    }
                }

                if bytes[pos] == b'<' {
                    cursor = scan_tag(input, pos, &mut tokens);
                } else {
                    // `&` — try entity decode.
                    cursor = scan_entity(input, pos, &mut tokens);
                }
                text_start = cursor;
            }
        }
    }

    // Flush trailing text.
    if text_start < len {
        if let Some(s) = input.get(text_start..) {
            tokens.push(Token::Text(s));
        }
    }

    tokens
}

/// Scan a tag starting at `pos` (which points to `<`).
/// Returns the new cursor position after the tag.
fn scan_tag<'a>(input: &'a str, pos: usize, tokens: &mut Vec<Token<'a>>) -> usize {
    let bytes = input.as_bytes();
    let len = bytes.len();

    // Find the closing `>` for this tag.
    let gt = match input.get(pos..).and_then(|s| s.find('>')) {
        Some(rel) => pos + rel,
        None => {
            // Malformed: no closing `>`. Emit `<` as text.
            tokens.push(Token::Text("<"));
            return pos + 1;
        }
    };

    let Some(tag_str) = input.get(pos..gt + 1) else {
        tokens.push(Token::Text("<"));
        return pos + 1;
    };

    let Some(inner) = input.get(pos + 1..gt) else {
        tokens.push(Token::Text(tag_str));
        return gt + 1;
    };

    // Check if this is a closing tag.
    if inner.starts_with('/') {
        let name_str = inner.get(1..).unwrap_or("");
        let name_end = name_str
            .find(|c: char| c.is_ascii_whitespace() || c == '>')
            .unwrap_or(name_str.len());
        let name_lower: String = name_str.get(..name_end).unwrap_or("").to_ascii_lowercase();
        let tag_name = TagName::from_lower(&name_lower);
        tokens.push(Token::TagClose(tag_name));
        return gt + 1;
    }

    // Opening or self-closing tag. Extract tag name.
    let name_end = inner
        .find(|c: char| c.is_ascii_whitespace() || c == '/' || c == '>')
        .unwrap_or(inner.len());
    let name_lower: String = inner.get(..name_end).unwrap_or("").to_ascii_lowercase();

    // Skip things like `<!doctype`, `<!--`, `<!` etc.
    if name_lower.starts_with('!') || name_lower.starts_with('?') {
        // Treat as comment/doctype — skip entirely.
        return gt + 1;
    }

    let tag_name = TagName::from_lower(&name_lower);

    // Check if self-closing: ends with `/` before `>`, or is a void element.
    let is_self_closing = inner.ends_with('/') || (tag_name.is_void() && !inner.starts_with('/'));

    if is_self_closing {
        tokens.push(Token::SelfClosing {
            name: tag_name,
            raw: tag_str,
        });
    } else {
        tokens.push(Token::TagOpen {
            name: tag_name,
            raw: tag_str,
        });
    }

    // For <script>, <style>, <noscript>: we need to find the matching
    // closing tag because their content may contain `<` and `&` that
    // should NOT be tokenized as HTML.
    if matches!(
        tag_name,
        TagName::Script | TagName::Style | TagName::Noscript
    ) {
        let close_tag = format!("</{name_lower}>");
        let search_from = gt + 1;
        let lower_rest = input.get(search_from..).unwrap_or("").to_ascii_lowercase();
        if let Some(close_rel) = lower_rest.find(&close_tag) {
            // Emit the raw content inside as text (will be suppressed by emitter).
            let content_end = search_from + close_rel;
            if content_end > search_from {
                if let Some(s) = input.get(search_from..content_end) {
                    tokens.push(Token::Text(s));
                }
            }
            tokens.push(Token::TagClose(tag_name));
            return content_end + close_tag.len();
        }
        // No closing tag found — the rest is all suppressed content.
        if search_from < len {
            if let Some(s) = input.get(search_from..) {
                tokens.push(Token::Text(s));
            }
        }
        return len;
    }

    gt + 1
}

/// Scan an entity starting at `pos` (which points to `&`).
/// Returns the new cursor position after the entity.
fn scan_entity<'a>(input: &'a str, pos: usize, tokens: &mut Vec<Token<'a>>) -> usize {
    if let Some((ch, advance)) = try_decode_entity(input, pos) {
        tokens.push(Token::Entity(ch));
        pos + advance
    } else {
        tokens.push(Token::AmpersandLiteral);
        pos + 1
    }
}
