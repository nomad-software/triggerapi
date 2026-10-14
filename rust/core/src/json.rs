//! JSON helpers that keep a document as it was written: the order of its
//! members, the way its strings are escaped and the digits of its numbers.

use std::fmt::Write as _;

/// Returns `text` set out with each member and element on a line of its own,
/// indented by `indent`, as Go's `json.Indent` does. The tokens are kept as
/// they are; only the space between them changes. It fails if the text is
/// not one JSON value.
pub fn indent(text: &str, indent: &str) -> Result<String, String> {
    validate(text)?;

    let bytes = text.as_bytes();
    let mut out = String::with_capacity(text.len() * 2);
    let mut depth = 0usize;
    // An opening bracket has been written whose first value has not.
    let mut pending = false;
    let mut i = 0;

    let newline = |out: &mut String, depth: usize| {
        out.push('\n');
        for _ in 0..depth {
            out.push_str(indent);
        }
    };

    while i < bytes.len() {
        let c = bytes[i];
        if c.is_ascii_whitespace() {
            i += 1;
            continue;
        }
        if pending && c != b']' && c != b'}' {
            pending = false;
            depth += 1;
            newline(&mut out, depth);
        }
        match c {
            b'"' => {
                let end = string_end(bytes, i).ok_or("unterminated string")?;
                out.push_str(&text[i..end]);
                i = end;
                continue;
            }
            b'{' | b'[' => {
                out.push(c as char);
                pending = true;
            }
            b'}' | b']' => {
                if pending {
                    pending = false;
                } else {
                    depth = depth.saturating_sub(1);
                    newline(&mut out, depth);
                }
                out.push(c as char);
            }
            b',' => {
                out.push(',');
                newline(&mut out, depth);
            }
            b':' => out.push_str(": "),
            _ => {
                // A number or a literal runs to the next delimiter.
                let start = i;
                while i < bytes.len() && !b",:{}[]\" \t\r\n".contains(&bytes[i]) {
                    i += 1;
                }
                out.push_str(&text[start..i]);
                continue;
            }
        }
        i += 1;
    }
    Ok(out)
}

/// Returns where the string that starts at `start` ends, after its closing
/// quote.
fn string_end(bytes: &[u8], start: usize) -> Option<usize> {
    let mut i = start + 1;
    while i < bytes.len() {
        match bytes[i] {
            b'\\' => i += 2,
            b'"' => return Some(i + 1),
            _ => i += 1,
        }
    }
    None
}

/// Reports why `text` is not one JSON value, if it is not.
pub fn validate(text: &str) -> Result<(), String> {
    serde_json::from_str::<serde::de::IgnoredAny>(text)
        .map(|_| ())
        .map_err(|e| e.to_string())
}

/// Reports if `text` is one JSON value.
pub fn is_valid(text: &str) -> bool {
    validate(text).is_ok()
}

/// A JSON value with the order of its members and the digits of its numbers
/// as they were written.
#[derive(Debug, Clone, PartialEq)]
pub enum Value {
    Null,
    Bool(bool),
    Number(String),
    String(String),
    Array(Vec<Value>),
    Object(Vec<(String, Value)>),
}

impl Value {
    /// Reads one JSON value, which must be all of the text but for space.
    pub fn parse(text: &str) -> Result<Value, String> {
        let mut parser = Parser { text, pos: 0 };
        parser.space();
        let value = parser.value(0)?;
        parser.space();
        if parser.pos != text.len() {
            return Err("there is more after the value".into());
        }
        Ok(value)
    }

    /// Returns the value as compact JSON, as Go's `json.Marshal` writes a
    /// value it has read: the members of an object in order of their names,
    /// and `<`, `>` and `&` escaped.
    pub fn to_go_json(&self) -> String {
        let mut out = String::new();
        self.write_go(&mut out);
        out
    }

    fn write_go(&self, out: &mut String) {
        match self {
            Value::Null => out.push_str("null"),
            Value::Bool(b) => out.push_str(if *b { "true" } else { "false" }),
            Value::Number(n) => out.push_str(n),
            Value::String(s) => write_go_string(out, s),
            Value::Array(items) => {
                out.push('[');
                for (i, item) in items.iter().enumerate() {
                    if i > 0 {
                        out.push(',');
                    }
                    item.write_go(out);
                }
                out.push(']');
            }
            Value::Object(members) => {
                // A later member of the same name replaces an earlier one.
                let mut sorted: Vec<(&String, &Value)> = Vec::new();
                for (name, value) in members {
                    if let Some(at) = sorted.iter().position(|(n, _)| *n == name) {
                        sorted[at].1 = value;
                    } else {
                        sorted.push((name, value));
                    }
                }
                sorted.sort_by(|a, b| a.0.cmp(b.0));
                out.push('{');
                for (i, (name, value)) in sorted.iter().enumerate() {
                    if i > 0 {
                        out.push(',');
                    }
                    write_go_string(out, name);
                    out.push(':');
                    value.write_go(out);
                }
                out.push('}');
            }
        }
    }
}

/// Writes a string as Go's encoder does.
fn write_go_string(out: &mut String, s: &str) {
    out.push('"');
    for c in s.chars() {
        match c {
            '"' => out.push_str("\\\""),
            '\\' => out.push_str("\\\\"),
            '\n' => out.push_str("\\n"),
            '\r' => out.push_str("\\r"),
            '\t' => out.push_str("\\t"),
            '<' | '>' | '&' | '\u{2028}' | '\u{2029}' => {
                let _ = write!(out, "\\u{:04x}", c as u32);
            }
            c if (c as u32) < 0x20 => {
                let _ = write!(out, "\\u{:04x}", c as u32);
            }
            c => out.push(c),
        }
    }
    out.push('"');
}

struct Parser<'a> {
    text: &'a str,
    pos: usize,
}

const MAX_DEPTH: usize = 10_000;

impl Parser<'_> {
    fn bytes(&self) -> &[u8] {
        self.text.as_bytes()
    }

    fn space(&mut self) {
        while self.pos < self.text.len() && b" \t\r\n".contains(&self.bytes()[self.pos]) {
            self.pos += 1;
        }
    }

    fn expect(&mut self, c: u8) -> Result<(), String> {
        self.space();
        if self.bytes().get(self.pos) == Some(&c) {
            self.pos += 1;
            Ok(())
        } else {
            Err(format!("expected {} at {}", c as char, self.pos))
        }
    }

    fn value(&mut self, depth: usize) -> Result<Value, String> {
        if depth > MAX_DEPTH {
            return Err("too deeply nested".into());
        }
        self.space();
        let Some(&c) = self.bytes().get(self.pos) else {
            return Err("unexpected end of JSON".into());
        };
        match c {
            b'{' => {
                self.pos += 1;
                let mut members = Vec::new();
                self.space();
                if self.bytes().get(self.pos) == Some(&b'}') {
                    self.pos += 1;
                    return Ok(Value::Object(members));
                }
                loop {
                    self.space();
                    let name = self.string()?;
                    self.expect(b':')?;
                    let value = self.value(depth + 1)?;
                    members.push((name, value));
                    self.space();
                    match self.bytes().get(self.pos) {
                        Some(b',') => self.pos += 1,
                        Some(b'}') => {
                            self.pos += 1;
                            return Ok(Value::Object(members));
                        }
                        _ => return Err(format!("expected , or }} at {}", self.pos)),
                    }
                }
            }
            b'[' => {
                self.pos += 1;
                let mut items = Vec::new();
                self.space();
                if self.bytes().get(self.pos) == Some(&b']') {
                    self.pos += 1;
                    return Ok(Value::Array(items));
                }
                loop {
                    items.push(self.value(depth + 1)?);
                    self.space();
                    match self.bytes().get(self.pos) {
                        Some(b',') => self.pos += 1,
                        Some(b']') => {
                            self.pos += 1;
                            return Ok(Value::Array(items));
                        }
                        _ => return Err(format!("expected , or ] at {}", self.pos)),
                    }
                }
            }
            b'"' => Ok(Value::String(self.string()?)),
            b't' => self.literal("true", Value::Bool(true)),
            b'f' => self.literal("false", Value::Bool(false)),
            b'n' => self.literal("null", Value::Null),
            b'-' | b'0'..=b'9' => self.number(),
            _ => Err(format!("unexpected {} at {}", c as char, self.pos)),
        }
    }

    fn literal(&mut self, word: &str, value: Value) -> Result<Value, String> {
        if self.text[self.pos..].starts_with(word) {
            self.pos += word.len();
            Ok(value)
        } else {
            Err(format!("invalid literal at {}", self.pos))
        }
    }

    fn number(&mut self) -> Result<Value, String> {
        let start = self.pos;
        let b = self.bytes();
        let mut i = self.pos;
        if b.get(i) == Some(&b'-') {
            i += 1;
        }
        let digits = |i: &mut usize| {
            let from = *i;
            while *i < b.len() && b[*i].is_ascii_digit() {
                *i += 1;
            }
            *i > from
        };
        if b.get(i) == Some(&b'0') {
            i += 1;
        } else if !digits(&mut i) {
            return Err(format!("invalid number at {start}"));
        }
        if b.get(i) == Some(&b'.') {
            i += 1;
            if !digits(&mut i) {
                return Err(format!("invalid number at {start}"));
            }
        }
        if matches!(b.get(i), Some(b'e') | Some(b'E')) {
            i += 1;
            if matches!(b.get(i), Some(b'+') | Some(b'-')) {
                i += 1;
            }
            if !digits(&mut i) {
                return Err(format!("invalid number at {start}"));
            }
        }
        self.pos = i;
        Ok(Value::Number(self.text[start..i].to_string()))
    }

    fn string(&mut self) -> Result<String, String> {
        let start = self.pos;
        let end = string_end(self.bytes(), start).ok_or("unterminated string")?;
        if self.bytes().get(start) != Some(&b'"') {
            return Err(format!("expected a string at {start}"));
        }
        self.pos = end;
        serde_json::from_str::<String>(&self.text[start..end]).map_err(|e| e.to_string())
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn indents_as_go_does() {
        let got = indent(r#"{"a":[1,2,{}],"b":{"c":"x\"y"},"d":[]}"#, "  ").unwrap();
        let want = "{\n  \"a\": [\n    1,\n    2,\n    {}\n  ],\n  \"b\": {\n    \"c\": \"x\\\"y\"\n  },\n  \"d\": []\n}";
        assert_eq!(got, want);
        assert!(indent("{", "  ").is_err());
        assert_eq!(indent(" 5 ", "  ").unwrap(), "5");
    }

    #[test]
    fn keeps_numbers_and_order() {
        let v = Value::parse(r#"{"z":1.50,"a":[true,null,"<&>"]}"#).unwrap();
        assert_eq!(v.to_go_json(), "{\"a\":[true,null,\"\\u003c\\u0026\\u003e\"],\"z\":1.50}");
        assert!(Value::parse("[1,]").is_err());
        assert!(Value::parse("1 2").is_err());
    }
}
