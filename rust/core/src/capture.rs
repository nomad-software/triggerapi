//! Takes values out of the JSON of a response, so that they can be kept in
//! the variables of an environment.

use serde::{Deserialize, Serialize};

use crate::json::Value;
use crate::nullable;

/// What to capture from a response: the variable to keep it in, the path of
/// the value in the JSON of the response, and a regular expression which
/// picks out the part of the value that is wanted, if not all of it is. One
/// that is not enabled is not followed.
#[derive(Debug, Clone, Default, PartialEq, Eq, Serialize, Deserialize)]
pub struct Rule {
    #[serde(rename = "key", default, deserialize_with = "nullable::de")]
    pub variable: String,
    #[serde(rename = "value", default, deserialize_with = "nullable::de")]
    pub path: String,
    #[serde(default, deserialize_with = "nullable::de", skip_serializing_if = "String::is_empty")]
    pub regex: String,
    #[serde(default, deserialize_with = "nullable::de")]
    pub enabled: bool,
}

impl Rule {
    /// Returns the value the rule captures from a JSON document: the value
    /// at its path, or the part of that which its regular expression picks
    /// out.
    pub fn extract(&self, document: &str) -> Result<String, String> {
        let value = extract(document, &self.path)?;
        matched(&value, &self.regex)
    }
}

/// Returns the part of a value that a regular expression picks out: what
/// its first group matched, if it has groups, and otherwise all that it
/// matched, at the first place it matches. With no expression that is the
/// whole value.
pub fn matched(value: &str, pattern: &str) -> Result<String, String> {
    if pattern.is_empty() {
        return Ok(value.to_string());
    }
    let expression = regex::Regex::new(pattern).map_err(|e| {
        let text = e.to_string();
        let reason = text.lines().last().unwrap_or(&text).trim().trim_start_matches("error: ");
        format!("the regex is not valid: {reason}")
    })?;
    let found = expression.captures(value).ok_or("the regex does not match the value")?;
    if found.len() > 1 {
        return Ok(found.get(1).map(|m| m.as_str()).unwrap_or("").to_string());
    }
    Ok(found.get(0).map(|m| m.as_str()).unwrap_or("").to_string())
}

/// One step of a path: into the member of an object with a name, or into
/// the element of an array at an index.
#[derive(Debug, Clone, PartialEq)]
enum Step {
    Name(String),
    Index(i64),
}

impl std::fmt::Display for Step {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        match self {
            Step::Index(i) => write!(f, "[{i}]"),
            Step::Name(n) => write!(f, "{}", serde_json::to_string(n).unwrap_or_default()),
        }
    }
}

/// Reads a path. Names are separated by dots, and an index into an array is
/// in square brackets: `data.items[0].id`. An index below zero counts from
/// the end. A name that has a dot or a bracket in it is written in brackets
/// and quotes: `data["a.b"]`. A `$` before the path is allowed.
fn parse(path: &str) -> Result<Vec<Step>, String> {
    let path = path.trim();
    let path = path.strip_prefix('$').unwrap_or(path);
    let bytes = path.as_bytes();
    let mut steps = Vec::new();
    let mut i = 0;
    while i < bytes.len() {
        match bytes[i] {
            b'.' => {
                i += 1;
                if i == bytes.len() || bytes[i] == b'.' || bytes[i] == b'[' {
                    return Err("there is a name missing after a dot in the path".into());
                }
            }
            b'[' => {
                let end = path[i..].find(']').ok_or("there is a [ with no ] in the path")?;
                let inside = path[i + 1..i + end].trim();
                i += end + 1;
                let ib = inside.as_bytes();
                if ib.len() >= 2 && (ib[0] == b'"' || ib[0] == b'\'') && ib[ib.len() - 1] == ib[0] {
                    steps.push(Step::Name(inside[1..inside.len() - 1].to_string()));
                    continue;
                }
                let index: i64 = inside
                    .parse()
                    .map_err(|_| format!("[{inside}] in the path is not a number or a name in quotes"))?;
                steps.push(Step::Index(index));
            }
            _ => {
                let end = path[i..].find(['.', '[']).unwrap_or(path.len() - i);
                steps.push(Step::Name(path[i..i + end].trim().to_string()));
                i += end;
            }
        }
    }
    Ok(steps)
}

/// Returns steps as the path that `parse` reads them from, and the last
/// name among them, which is "value" if there is none. A name with a `]` in
/// it cannot be written.
fn write(steps: &[Step]) -> Option<(String, String)> {
    let mut b = String::new();
    let mut name = String::from("value");
    for s in steps {
        match s {
            Step::Index(i) => b.push_str(&format!("[{i}]")),
            Step::Name(n) => {
                let plain = !n.is_empty()
                    && !n.contains(['.', '['])
                    && n.trim() == n
                    && (!b.is_empty() || !n.starts_with('$'));
                if plain {
                    if !b.is_empty() {
                        b.push('.');
                    }
                    b.push_str(n);
                } else if n.contains(']') {
                    return None;
                } else {
                    b.push_str(&format!("[\"{n}\"]"));
                }
                if !n.is_empty() {
                    name = n.clone();
                }
            }
        }
    }
    Some((b, name))
}

/// A token of a JSON document and where it is.
#[derive(Debug)]
enum Token {
    Open(u8),
    Close,
    Str(String),
    Scalar,
}

/// Reads the tokens of a JSON document with where each starts and ends,
/// failing at the first that is not JSON.
fn tokens(document: &str) -> Vec<Result<(Token, usize, usize), ()>> {
    let b = document.as_bytes();
    let mut out = Vec::new();
    let mut i = 0;
    loop {
        while i < b.len() && b" \t\r\n,:".contains(&b[i]) {
            i += 1;
        }
        if i >= b.len() {
            return out;
        }
        let start = i;
        match b[i] {
            b'{' | b'[' => {
                out.push(Ok((Token::Open(b[i]), start, i + 1)));
                i += 1;
            }
            b'}' | b']' => {
                out.push(Ok((Token::Close, start, i + 1)));
                i += 1;
            }
            b'"' => {
                let mut j = i + 1;
                let mut end = None;
                while j < b.len() {
                    match b[j] {
                        b'\\' => j += 2,
                        b'"' => {
                            end = Some(j + 1);
                            break;
                        }
                        _ => j += 1,
                    }
                }
                let Some(end) = end else {
                    out.push(Err(()));
                    return out;
                };
                match serde_json::from_str::<String>(&document[start..end]) {
                    Ok(s) => out.push(Ok((Token::Str(s), start, end))),
                    Err(_) => {
                        out.push(Err(()));
                        return out;
                    }
                }
                i = end;
            }
            _ => {
                let mut j = i;
                while j < b.len() && !b",:{}[]\" \t\r\n".contains(&b[j]) {
                    j += 1;
                }
                if Value::parse(&document[start..j]).is_err() {
                    out.push(Err(()));
                    return out;
                }
                out.push(Ok((Token::Scalar, start, j)));
                i = j;
            }
        }
    }
}

/// Returns the path of the value at an offset, in bytes, into a JSON
/// document: the innermost value that the offset is in or at the end of.
/// The name of a member of an object counts as part of its value. It also
/// returns the last name in the path, to call the value by. Nothing is
/// found if the offset is outside the document, the document is not JSON as
/// far as the value, or the path cannot be written.
pub fn path_at(document: &str, offset: usize) -> Option<(String, String)> {
    struct Frame {
        array: bool,
        index: i64,
        key: String,
        keyed: bool,
        key_start: usize,
        start: usize,
        depth: usize,
    }

    let mut steps: Vec<Step> = Vec::new();
    let mut frames: Vec<Frame> = Vec::new();

    fn next(frames: &mut [Frame]) -> bool {
        match frames.last_mut() {
            None => false,
            Some(f) => {
                if f.array {
                    f.index += 1;
                } else {
                    f.keyed = false;
                }
                true
            }
        }
    }

    for token in tokens(document) {
        let (token, start, end) = token.ok()?;
        if let Token::Close = token {
            let closed = frames.pop()?;
            if closed.start <= offset && offset <= end {
                return write(&steps);
            }
            steps.truncate(closed.depth);
            if !next(&mut frames) {
                return None;
            }
            continue;
        }

        // The name of a member comes before its value.
        if let Some(f) = frames.last_mut() {
            if !f.array && !f.keyed {
                let Token::Str(key) = token else { return None };
                f.key = key;
                f.keyed = true;
                f.key_start = start;
                continue;
            }
        }

        let mut value_start = start;
        let depth = steps.len();
        let mut value = steps.clone();
        if let Some(f) = frames.last() {
            if f.array {
                value.push(Step::Index(f.index));
            } else {
                value.push(Step::Name(f.key.clone()));
                value_start = f.key_start;
            }
        }

        if let Token::Open(c) = token {
            frames.push(Frame { array: c == b'[', index: 0, key: String::new(), keyed: false, key_start: 0, start: value_start, depth });
            steps = value;
            continue;
        }
        if value_start <= offset && offset <= end {
            return write(&value);
        }
        if !next(&mut frames) {
            return None;
        }
    }
    None
}

/// Returns the value at a path in a JSON document, as text: a string as it
/// is, without its quotes; a number, true or false as written; nothing for
/// null; and an object or an array as JSON. An empty path is the whole
/// document.
pub fn extract(document: &str, path: &str) -> Result<String, String> {
    let steps = parse(path)?;
    let mut value = Value::parse(document).map_err(|_| "the response is not JSON".to_string())?;

    for s in &steps {
        value = match value {
            Value::Object(members) => {
                let Step::Name(name) = s else {
                    return Err(format!("{s} is for an array, but there is an object there"));
                };
                // A later member of the same name is the one that counts.
                match members.into_iter().rev().find(|(n, _)| n == name) {
                    Some((_, v)) => v,
                    None => return Err(format!("there is no {s} in the response")),
                }
            }
            Value::Array(mut items) => {
                let Step::Index(index) = s else {
                    return Err(format!("{s} is for an object, but there is an array there"));
                };
                let len = items.len() as i64;
                let at = if *index < 0 { index + len } else { *index };
                if at < 0 || at >= len {
                    return Err(format!("there is no {s} in an array of {len}"));
                }
                items.swap_remove(at as usize)
            }
            _ => return Err(format!("there is nothing to look for {s} in")),
        };
    }

    Ok(match value {
        Value::Null => String::new(),
        Value::String(s) => s,
        Value::Number(n) => n,
        Value::Bool(b) => b.to_string(),
        other => other.to_go_json(),
    })
}

#[cfg(test)]
mod tests {
    use super::*;

    const DOC: &str = r#"{"data": {"items": [{"id": 7, "name": "a"}, {"id": 1.50}], "a.b": "dot", "t": true, "n": null}}"#;

    #[test]
    fn extracts() {
        assert_eq!(extract(DOC, "data.items[0].id").unwrap(), "7");
        assert_eq!(extract(DOC, "$.data.items[-1].id").unwrap(), "1.50");
        assert_eq!(extract(DOC, r#"data["a.b"]"#).unwrap(), "dot");
        assert_eq!(extract(DOC, "data.t").unwrap(), "true");
        assert_eq!(extract(DOC, "data.n").unwrap(), "");
        assert_eq!(extract(DOC, "data.items[0]").unwrap(), r#"{"id":7,"name":"a"}"#);
        assert_eq!(extract(DOC, "data.items[5]").unwrap_err(), "there is no [5] in an array of 2");
        assert_eq!(extract(DOC, "data.x").unwrap_err(), "there is no \"x\" in the response");
        assert_eq!(extract(DOC, "data.items.x").unwrap_err(), "\"x\" is for an object, but there is an array there");
        assert_eq!(extract("nope", "a").unwrap_err(), "the response is not JSON");
    }

    #[test]
    fn matches() {
        assert_eq!(matched("Bearer abc", r"Bearer (\w+)").unwrap(), "abc");
        assert_eq!(matched("abc123", r"\d+").unwrap(), "123");
        assert_eq!(matched("x", "y").unwrap_err(), "the regex does not match the value");
        assert!(matched("x", "(").unwrap_err().starts_with("the regex is not valid"));
    }

    #[test]
    fn finds_paths() {
        let at = |needle: &str| DOC.find(needle).unwrap() + 1;
        assert_eq!(path_at(DOC, at("7")), Some(("data.items[0].id".into(), "id".into())));
        assert_eq!(path_at(DOC, at("\"name\"")), Some(("data.items[0].name".into(), "name".into())));
        assert_eq!(path_at(DOC, at("dot")), Some((r#"data["a.b"]"#.into(), "a.b".into())));
        assert_eq!(path_at(DOC, 0), Some((String::new(), "value".into())));
        assert_eq!(path_at("{\"a\": [1, 2]}", 10), Some(("a[1]".into(), "a".into())));
        assert_eq!(path_at("{\"a\": [1, 2]}", 9), Some(("a".into(), "a".into())));
        assert_eq!(path_at("{\"a\": ", 3), None);
    }
}
