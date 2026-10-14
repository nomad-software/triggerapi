//! Keeps the query string shown in the URL field and the params table in
//! step. The query is shown as typed rather than fully encoded, so
//! placeholders and most values stay readable; only the characters that
//! would change its structure are escaped.

use crate::rest::KeyValue;

fn escape_value(s: &str) -> String {
    s.replace('%', "%25").replace('&', "%26").replace('#', "%23")
}

fn escape_key(s: &str) -> String {
    escape_value(s).replace('=', "%3D")
}

/// Appends the enabled params to `base` as a query string for display.
pub fn join_url(base: &str, params: &[KeyValue]) -> String {
    let parts: Vec<String> = params
        .iter()
        .filter(|p| p.enabled && !p.key.is_empty())
        .map(|p| format!("{}={}", escape_key(&p.key), escape_value(&p.value)))
        .collect();
    if parts.is_empty() {
        base.to_string()
    } else {
        format!("{base}?{}", parts.join("&"))
    }
}

/// Separates a URL as typed into the part before the query string and the
/// params in it. It is the inverse of `join_url`.
pub fn split_url(raw: &str) -> (String, Vec<KeyValue>) {
    let (base, query) = raw.split_once('?').unwrap_or((raw, ""));
    let mut params = Vec::new();
    for part in query.split('&') {
        let (key, value) = part.split_once('=').unwrap_or((part, ""));
        let (key, value) = (unescape(key), unescape(value));
        if key.is_empty() {
            continue;
        }
        params.push(KeyValue { key, value, enabled: true });
    }
    (base.to_string(), params)
}

/// Decodes percent escapes, leaving text that is not validly escaped as it
/// is, as Go's `url.PathUnescape` is used.
fn unescape(s: &str) -> String {
    let bytes = s.as_bytes();
    let mut out = Vec::with_capacity(bytes.len());
    let mut i = 0;
    while i < bytes.len() {
        if bytes[i] == b'%' {
            let hex = |b: u8| (b as char).to_digit(16);
            match (bytes.get(i + 1).and_then(|&b| hex(b)), bytes.get(i + 2).and_then(|&b| hex(b))) {
                (Some(h), Some(l)) => {
                    out.push((h * 16 + l) as u8);
                    i += 3;
                    continue;
                }
                _ => return s.to_string(),
            }
        }
        out.push(bytes[i]);
        i += 1;
    }
    String::from_utf8(out).unwrap_or_else(|_| s.to_string())
}

/// Returns `existing` with its enabled params replaced, in order, by those
/// typed into the URL. Disabled params are not part of the URL and keep
/// their place.
pub fn merge_params(existing: &[KeyValue], typed: &[KeyValue]) -> Vec<KeyValue> {
    let mut typed = typed.iter();
    let mut merged = Vec::new();
    for p in existing {
        if !p.enabled {
            merged.push(p.clone());
        } else if let Some(t) = typed.next() {
            merged.push(t.clone());
        }
    }
    merged.extend(typed.cloned());
    merged
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn round_trips() {
        let params = vec![KeyValue::new("a=b", "1&2", true), KeyValue::new("off", "x", false), KeyValue::new("q", "{{v}}", true)];
        let joined = join_url("host/path", &params);
        assert_eq!(joined, "host/path?a%3Db=1%262&q={{v}}");
        let (base, typed) = split_url(&joined);
        assert_eq!(base, "host/path");
        assert_eq!(typed, vec![KeyValue::new("a=b", "1&2", true), KeyValue::new("q", "{{v}}", true)]);
        let merged = merge_params(&params, &typed);
        assert_eq!(merged[1], KeyValue::new("off", "x", false));
        assert_eq!(unescape("100%"), "100%");
    }
}
