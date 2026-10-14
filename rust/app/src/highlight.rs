//! Colouring text: `{{name}}` placeholders, by whether the active
//! environment gives them a value, the tokens of JSON, and the matches of a
//! search.

use std::ops::Range;

use egui::text::{LayoutJob, TextFormat};
use egui::{Color32, FontId};

use crate::theme::{Kind, Theme, Token};

/// The size of text above which it is not coloured as JSON, as colouring it
/// would take too long.
pub const MAX_HIGHLIGHT: usize = 2 << 20;

/// Says how text is to be coloured.
pub struct Colouring<'a> {
    pub theme: &'a Theme,
    pub dark: bool,
    pub font: FontId,
    pub colour: Color32,
    /// The tokens of JSON are coloured.
    pub json: bool,
    /// Reports if a placeholder's name has a value, if placeholders are
    /// coloured.
    pub placeholders: Option<&'a dyn Fn(&str) -> bool>,
    /// The matches of a search, which are marked, and the one gone to.
    pub matches: &'a [Range<usize>],
    pub current: Option<usize>,
    /// Wrap at this width, or not at all.
    pub wrap: Option<f32>,
}

const PLAIN: u8 = 0;
const KEY: u8 = 1;
const STRING: u8 = 2;
const NUMBER: u8 = 3;
const LITERAL: u8 = 4;
const FOUND: u8 = 5;
const NOT_FOUND: u8 = 6;

/// Returns the ranges of the tokens of JSON in text, with their kind. Text
/// that is not JSON is coloured as far as it looks like JSON.
pub fn json_tokens(text: &str) -> Vec<(Range<usize>, Token)> {
    let b = text.as_bytes();
    let mut out = Vec::new();
    let mut i = 0;
    while i < b.len() {
        match b[i] {
            b'"' => {
                let start = i;
                i += 1;
                while i < b.len() && b[i] != b'"' && b[i] != b'\n' {
                    if b[i] == b'\\' {
                        i += 1;
                    }
                    i += 1;
                }
                i = (i + 1).min(b.len());
                // A string followed by a colon is the name of a member.
                let mut j = i;
                while j < b.len() && (b[j] == b' ' || b[j] == b'\t') {
                    j += 1;
                }
                let kind = if b.get(j) == Some(&b':') { Token::Key } else { Token::String };
                let end = floor_char(text, i);
                out.push((start..end, kind));
                i = end.max(start + 1);
            }
            b'-' | b'0'..=b'9' => {
                let start = i;
                while i < b.len() && (b[i].is_ascii_digit() || b"-+.eE".contains(&b[i])) {
                    i += 1;
                }
                out.push((start..i, Token::Number));
            }
            b't' | b'f' | b'n' => {
                let word = ["true", "false", "null"].into_iter().find(|w| text[i..].starts_with(w));
                let after = word.map(|w| i + w.len());
                let boundary = |k: usize| k >= b.len() || !b[k].is_ascii_alphanumeric();
                let before_ok = i == 0 || !b[i - 1].is_ascii_alphanumeric();
                match after {
                    Some(end) if boundary(end) && before_ok => {
                        out.push((i..end, Token::Literal));
                        i = end;
                    }
                    _ => i += 1,
                }
            }
            _ => i += 1,
        }
    }
    out
}

fn floor_char(text: &str, mut i: usize) -> usize {
    i = i.min(text.len());
    while !text.is_char_boundary(i) {
        i -= 1;
    }
    i
}

/// Lays text out with its colours.
pub fn job(text: &str, c: &Colouring) -> LayoutJob {
    let mut job = LayoutJob::default();
    job.wrap.max_width = c.wrap.unwrap_or(f32::INFINITY);

    let mut styles = vec![PLAIN; text.len()];
    if c.json && text.len() <= MAX_HIGHLIGHT {
        for (range, token) in json_tokens(text) {
            let code = match token {
                Token::Key => KEY,
                Token::String => STRING,
                Token::Number => NUMBER,
                Token::Literal => LITERAL,
            };
            styles[range].fill(code);
        }
    }
    if let Some(found) = c.placeholders {
        for (start, end, name) in triggerapi_core::env::placeholders(text) {
            styles[start..end].fill(if found(&name) { FOUND } else { NOT_FOUND });
        }
    }

    let colour_of = |code: u8| match code {
        KEY => c.theme.json(Token::Key, c.dark),
        STRING => c.theme.json(Token::String, c.dark),
        NUMBER => c.theme.json(Token::Number, c.dark),
        LITERAL => c.theme.json(Token::Literal, c.dark),
        FOUND => c.theme.colour(Kind::Found),
        NOT_FOUND => c.theme.colour(Kind::NotFound),
        _ => c.colour,
    };
    let mark = c.theme.colour(Kind::Match);
    // 0 for none, 1 for a match and 2 for the one gone to.
    let marked = |at: usize| -> u8 {
        let index = c.matches.partition_point(|m| m.end <= at);
        match c.matches.get(index) {
            Some(m) if m.start <= at => {
                if Some(index) == c.current {
                    2
                } else {
                    1
                }
            }
            _ => 0,
        }
    };

    // The text is cut where its style changes, at the start and end of each
    // match, and at the boundaries of its characters only.
    let mut cuts: Vec<usize> = vec![0, text.len()];
    for i in 1..text.len() {
        if styles[i] != styles[i - 1] && text.is_char_boundary(i) {
            cuts.push(i);
        }
    }
    for m in c.matches {
        cuts.push(floor_char(text, m.start));
        cuts.push(floor_char(text, m.end));
    }
    cuts.sort_unstable();
    cuts.dedup();

    for pair in cuts.windows(2) {
        let (start, end) = (pair[0], pair[1]);
        if start >= end {
            continue;
        }
        let mut format = TextFormat::simple(c.font.clone(), colour_of(styles[start]));
        match marked(start) {
            1 => {
                format.background = mark;
                format.color = Color32::BLACK;
            }
            2 => {
                format.background = mark;
                format.color = Color32::BLACK;
                format.underline = egui::Stroke::new(2.0_f32, Color32::BLACK);
            }
            _ => {}
        }
        job.append(&text[start..end], 0.0, format);
    }
    if text.is_empty() {
        job.append("", 0.0, TextFormat::simple(c.font.clone(), c.colour));
    }
    job
}

/// Returns the places a query is found in text, in bytes, ignoring case
/// unless it is to be matched.
pub fn find_all(text: &str, query: &str, match_case: bool) -> Vec<Range<usize>> {
    if query.is_empty() {
        return Vec::new();
    }
    if match_case {
        return text.match_indices(query).map(|(i, m)| i..i + m.len()).collect();
    }
    // Letters are compared in lower case one by one, which keeps the places
    // in the text as they are.
    let needle: Vec<char> = query.chars().flat_map(char::to_lowercase).collect();
    let hay: Vec<(usize, char)> = text.char_indices().collect();
    let lower: Vec<char> = hay.iter().map(|&(_, c)| c.to_lowercase().next().unwrap_or(c)).collect();
    let mut out = Vec::new();
    let mut i = 0;
    while i + needle.len() <= lower.len() {
        if lower[i..i + needle.len()] == needle[..] {
            let start = hay[i].0;
            let end = hay.get(i + needle.len()).map(|&(at, _)| at).unwrap_or(text.len());
            out.push(start..end);
            i += needle.len();
        } else {
            i += 1;
        }
    }
    out
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn tokens() {
        let t = json_tokens(r#"{"a": "b", "n": -1.5, "t": true, "x": nullx}"#);
        let kinds: Vec<Token> = t.iter().map(|(_, k)| *k).collect();
        assert_eq!(kinds, vec![Token::Key, Token::String, Token::Key, Token::Number, Token::Key, Token::Literal, Token::Key]);
    }

    #[test]
    fn finds() {
        assert_eq!(find_all("Abc abc ABC", "abc", false), vec![0..3, 4..7, 8..11]);
        assert_eq!(find_all("Abc abc", "abc", true), vec![4..7]);
        assert_eq!(find_all("éa ÉA", "éa", false), vec![0..3, 4..7]);
    }
}
