//! Builds and sends HTTP requests.

use std::collections::BTreeMap;
use std::time::{Duration, Instant};

use base64::Engine as _;
use serde::{Deserialize, Serialize};

use crate::capture::Rule;
use crate::nullable;

/// The HTTP methods offered by the client.
pub const METHODS: &[&str] = &["GET", "POST", "PUT", "PATCH", "DELETE", "HEAD", "OPTIONS"];

/// A single query parameter, header or form field.
#[derive(Debug, Clone, Default, PartialEq, Eq, Serialize, Deserialize)]
pub struct KeyValue {
    #[serde(default, deserialize_with = "nullable::de")]
    pub key: String,
    #[serde(default, deserialize_with = "nullable::de")]
    pub value: String,
    #[serde(default, deserialize_with = "nullable::de")]
    pub enabled: bool,
}

impl KeyValue {
    pub fn new(key: impl Into<String>, value: impl Into<String>, enabled: bool) -> Self {
        KeyValue { key: key.into(), value: value.into(), enabled }
    }
}

/// How a request is authorised. An empty type means none.
pub const AUTH_NONE: &str = "none";
pub const AUTH_BASIC: &str = "basic";
pub const AUTH_BEARER: &str = "bearer";

/// The credentials for a request.
#[derive(Debug, Clone, Default, PartialEq, Eq, Serialize, Deserialize)]
pub struct Auth {
    #[serde(rename = "type", default, deserialize_with = "nullable::de", skip_serializing_if = "String::is_empty")]
    pub kind: String,
    #[serde(default, deserialize_with = "nullable::de", skip_serializing_if = "String::is_empty")]
    pub username: String,
    #[serde(default, deserialize_with = "nullable::de", skip_serializing_if = "String::is_empty")]
    pub password: String,
    #[serde(default, deserialize_with = "nullable::de", skip_serializing_if = "String::is_empty")]
    pub token: String,
}

impl Auth {
    /// Returns a copy of the credentials with `f` applied to each of them.
    pub fn expand(&self, f: &dyn Fn(&str) -> String) -> Auth {
        Auth {
            kind: self.kind.clone(),
            username: f(&self.username),
            password: f(&self.password),
            token: f(&self.token),
        }
    }

    /// Returns the value of the Authorization header for the credentials, if
    /// the type uses one.
    pub fn header(&self) -> Option<String> {
        match self.kind.as_str() {
            AUTH_BASIC => {
                let credentials = format!("{}:{}", self.username, self.password);
                Some(format!("Basic {}", base64::engine::general_purpose::STANDARD.encode(credentials)))
            }
            AUTH_BEARER => Some(format!("Bearer {}", self.token)),
            _ => None,
        }
    }
}

/// What is sent as the request body. An empty type means none.
pub const BODY_NONE: &str = "none";
pub const BODY_JSON: &str = "json";
pub const BODY_TEXT: &str = "text";
pub const BODY_XML: &str = "xml";
pub const BODY_FORM: &str = "form";

/// Returns the Content-Type header that goes with a body type.
pub fn content_type(body_type: &str) -> &'static str {
    match body_type {
        BODY_JSON => "application/json",
        BODY_TEXT => "text/plain",
        BODY_XML => "application/xml",
        BODY_FORM => "application/x-www-form-urlencoded",
        _ => "",
    }
}

/// An HTTP request as entered by the user. It holds everything in the
/// request UI, including fields the chosen auth and body types do not use,
/// so it can be saved and restored without loss.
#[derive(Debug, Clone, Default, PartialEq, Serialize, Deserialize)]
pub struct Request {
    #[serde(default, deserialize_with = "nullable::de")]
    pub method: String,
    #[serde(default, deserialize_with = "nullable::de")]
    pub url: String,
    #[serde(default, deserialize_with = "nullable::de", skip_serializing_if = "Vec::is_empty")]
    pub params: Vec<KeyValue>,
    #[serde(default, deserialize_with = "nullable::de", skip_serializing_if = "Vec::is_empty")]
    pub headers: Vec<KeyValue>,
    #[serde(default, deserialize_with = "nullable::de")]
    pub auth: Auth,
    /// `body` is sent for the raw body types and `form` for a form. The
    /// body type's content type is used unless the user sets that header.
    #[serde(rename = "bodyType", default, deserialize_with = "nullable::de", skip_serializing_if = "String::is_empty")]
    pub body_type: String,
    #[serde(default, deserialize_with = "nullable::de", skip_serializing_if = "String::is_empty")]
    pub body: String,
    #[serde(default, deserialize_with = "nullable::de", skip_serializing_if = "Vec::is_empty")]
    pub form: Vec<KeyValue>,
    /// The values to take from the response and keep in variables of the
    /// active environment. Placeholders in them are not replaced.
    #[serde(default, deserialize_with = "nullable::de", skip_serializing_if = "Vec::is_empty")]
    pub captures: Vec<Rule>,
    /// How the request is sent, which is not saved with it: whoever sends
    /// it gives them.
    #[serde(skip)]
    pub options: Options,
}

/// How a request is sent, whatever the request is.
#[derive(Debug, Clone, Copy, Default, PartialEq, Eq)]
pub struct Options {
    /// How long the request can take, to the end of its response. There is
    /// no limit if it is `None`.
    pub timeout: Option<Duration>,
    /// Has a response that redirects returned as it is.
    pub no_redirects: bool,
}

/// The result of sending a request.
#[derive(Debug, Clone, Default)]
pub struct Response {
    /// The status as Go gives it, such as `200 OK`.
    pub status: String,
    pub status_code: u16,
    pub proto: String,
    pub headers: Vec<(String, String)>,
    pub body: Vec<u8>,
    pub duration: Duration,
}

impl Response {
    /// Returns the first value of a header, ignoring the case of its name.
    pub fn header(&self, name: &str) -> Option<&str> {
        self.headers
            .iter()
            .find(|(n, _)| n.eq_ignore_ascii_case(name))
            .map(|(_, v)| v.as_str())
    }

    /// Reports if the response declares a JSON content type.
    pub fn is_json(&self) -> bool {
        self.header("Content-Type")
            .map(|ct| ct.to_lowercase().contains("json"))
            .unwrap_or(false)
    }

    /// Returns the body for display, indented if it is valid JSON.
    pub fn text(&self) -> String {
        let text = String::from_utf8_lossy(&self.body).into_owned();
        if self.is_json() {
            if let Ok(indented) = crate::json::indent(&text, "  ") {
                return indented;
            }
        }
        text
    }
}

/// Puts the pairs in alphabetical order of their keys, ignoring case, with
/// those that have no key last. Pairs with the same key keep their order.
pub fn sort_pairs(pairs: &mut [KeyValue]) {
    pairs.sort_by(compare_pairs);
}

/// The order `sort_pairs` puts two pairs in.
pub fn compare_pairs(a: &KeyValue, b: &KeyValue) -> std::cmp::Ordering {
    match (a.key.is_empty(), b.key.is_empty()) {
        (true, false) => std::cmp::Ordering::Greater,
        (false, true) => std::cmp::Ordering::Less,
        _ => a.key.to_lowercase().cmp(&b.key.to_lowercase()),
    }
}

/// Returns a copy of the pairs with `f` applied to every key and value.
pub fn expand_pairs(pairs: &[KeyValue], f: &dyn Fn(&str) -> String) -> Vec<KeyValue> {
    pairs
        .iter()
        .map(|p| KeyValue { key: f(&p.key), value: f(&p.value), enabled: p.enabled })
        .collect()
}

/// Returns a header name as Go's `http.CanonicalHeaderKey` does:
/// `content-type` becomes `Content-Type`. A name with characters that are
/// not allowed in one is left as it is.
pub fn canonical_header_key(key: &str) -> String {
    let token = |c: char| c.is_ascii_alphanumeric() || "!#$%&'*+-.^_`|~".contains(c);
    if !key.chars().all(token) {
        return key.to_string();
    }
    let mut upper = true;
    key.chars()
        .map(|c| {
            let out = if upper { c.to_ascii_uppercase() } else { c.to_ascii_lowercase() };
            upper = c == '-';
            out
        })
        .collect()
}

/// A request as it is sent: the address, headers and body.
#[derive(Debug, Clone)]
pub struct Built {
    pub method: String,
    pub url: url::Url,
    /// The headers by their canonical names, each with its values in order.
    pub headers: BTreeMap<String, Vec<String>>,
    /// The Host header the user gave, which is sent in place of the host of
    /// the address.
    pub host: Option<String>,
    pub body: String,
}

impl Request {
    /// Returns a copy of the request with `f` applied to every field the
    /// user can type into, which is how environment variables are put in.
    pub fn expand(&self, f: &dyn Fn(&str) -> String) -> Request {
        Request {
            url: f(&self.url),
            params: expand_pairs(&self.params, f),
            headers: expand_pairs(&self.headers, f),
            auth: self.auth.expand(f),
            body: f(&self.body),
            form: expand_pairs(&self.form, f),
            ..self.clone()
        }
    }

    /// Returns the body to send for the body type.
    pub fn content(&self) -> String {
        match self.body_type.as_str() {
            BODY_JSON | BODY_TEXT | BODY_XML => self.body.clone(),
            BODY_FORM => encode_form(&self.form),
            _ => String::new(),
        }
    }

    /// Turns the request into what is sent.
    pub fn build(&self) -> Result<Built, String> {
        let url = self.build_url()?;
        let content = self.content();
        let mut headers: BTreeMap<String, Vec<String>> = BTreeMap::new();

        if !content.is_empty() {
            headers.insert("Content-Type".into(), vec![content_type(&self.body_type).into()]);
        }
        if let Some(value) = self.auth.header() {
            headers.insert("Authorization".into(), vec![value]);
        }

        // User supplied headers take precedence over generated ones.
        let mut host = None;
        let mut seen = std::collections::HashSet::new();
        for h in &self.headers {
            if !h.enabled || h.key.is_empty() {
                continue;
            }
            let key = canonical_header_key(&h.key);
            if key == "Host" {
                host = Some(h.value.clone());
                continue;
            }
            if seen.insert(key.clone()) {
                headers.remove(&key);
            }
            headers.entry(key).or_default().push(h.value.clone());
        }

        let method = if self.method.is_empty() { "GET".to_string() } else { self.method.clone() };
        Ok(Built { method, url, headers, host, body: content })
    }

    /// Adds a default scheme and appends the enabled params to the query,
    /// in the order they were entered.
    fn build_url(&self) -> Result<url::Url, String> {
        let mut raw = self.url.trim().to_string();
        if raw.is_empty() {
            return Err("URL is empty".into());
        }
        if !raw.contains("://") {
            raw = format!("http://{raw}");
        }
        let mut url = url::Url::parse(&raw).map_err(|e| format!("parse {raw:?}: {e}"))?;
        if url.scheme() != "http" && url.scheme() != "https" {
            return Err(format!("unsupported scheme {:?}", url.scheme()));
        }
        if url.host_str().unwrap_or("").is_empty() {
            return Err("URL has no host".into());
        }
        let query = encode(&self.params);
        if !query.is_empty() {
            let combined = match url.query() {
                Some(existing) if !existing.is_empty() => format!("{existing}&{query}"),
                _ => query,
            };
            url.set_query(Some(&combined));
        }
        Ok(url)
    }
}

/// Encodes the enabled fields as an application/x-www-form-urlencoded body.
pub fn encode_form(fields: &[KeyValue]) -> String {
    encode(fields)
}

fn encode(pairs: &[KeyValue]) -> String {
    pairs
        .iter()
        .filter(|p| p.enabled && !p.key.is_empty())
        .map(|p| format!("{}={}", query_escape(&p.key), query_escape(&p.value)))
        .collect::<Vec<_>>()
        .join("&")
}

/// Escapes text for a query as Go's `url.QueryEscape` does.
pub fn query_escape(s: &str) -> String {
    let mut out = String::with_capacity(s.len());
    for b in s.bytes() {
        match b {
            b'A'..=b'Z' | b'a'..=b'z' | b'0'..=b'9' | b'-' | b'_' | b'.' | b'~' => out.push(b as char),
            b' ' => out.push('+'),
            _ => out.push_str(&format!("%{b:02X}")),
        }
    }
    out
}

/// The client requests are sent with, built for the options.
fn client(options: &Options) -> Result<reqwest::Client, String> {
    let mut builder = reqwest::Client::builder().user_agent(concat!("TriggerAPI/", env!("CARGO_PKG_VERSION")));
    if options.no_redirects {
        builder = builder.redirect(reqwest::redirect::Policy::none());
    } else {
        builder = builder.redirect(reqwest::redirect::Policy::limited(10));
    }
    if let Some(timeout) = options.timeout {
        builder = builder.timeout(timeout);
    }
    builder.build().map_err(|e| e.to_string())
}

/// The error a request gives when it has taken longer than it was allowed.
pub const TIMED_OUT: &str = "context deadline exceeded";

/// Sends the request and reads the whole response body. The duration covers
/// the time until the body is fully read. A request that takes too long
/// fails with `TIMED_OUT`.
pub async fn send(request: &Request) -> Result<Response, String> {
    let built = request.build()?;
    let client = client(&request.options)?;

    let method = reqwest::Method::from_bytes(built.method.as_bytes()).map_err(|e| e.to_string())?;
    let mut req = client.request(method, built.url.clone());
    for (name, values) in &built.headers {
        for value in values {
            req = req.header(name.as_str(), value.as_str());
        }
    }
    if let Some(host) = &built.host {
        req = req.header("Host", host.as_str());
    }
    if !built.body.is_empty() {
        req = req.body(built.body.clone());
    }

    let start = Instant::now();
    let describe = |e: reqwest::Error| {
        if e.is_timeout() {
            TIMED_OUT.to_string()
        } else {
            error_chain(&e)
        }
    };
    let res = req.send().await.map_err(describe)?;
    let status = res.status();
    let proto = match res.version() {
        reqwest::Version::HTTP_09 => "HTTP/0.9",
        reqwest::Version::HTTP_10 => "HTTP/1.0",
        reqwest::Version::HTTP_2 => "HTTP/2.0",
        reqwest::Version::HTTP_3 => "HTTP/3.0",
        _ => "HTTP/1.1",
    };
    let headers = res
        .headers()
        .iter()
        .map(|(n, v)| (canonical_header_key(n.as_str()), String::from_utf8_lossy(v.as_bytes()).into_owned()))
        .collect();
    let body = res.bytes().await.map_err(describe)?.to_vec();

    Ok(Response {
        status: status_text(status.as_u16()),
        status_code: status.as_u16(),
        proto: proto.into(),
        headers,
        body,
        duration: start.elapsed(),
    })
}

/// Returns a status as Go gives it: the code and its reason, such as
/// `404 Not Found`.
pub fn status_text(code: u16) -> String {
    match http::StatusCode::from_u16(code).ok().and_then(|s| s.canonical_reason()) {
        Some(reason) => format!("{code} {reason}"),
        None => code.to_string(),
    }
}

/// Returns an error with the errors that caused it, which reqwest keeps
/// apart.
pub fn error_chain(e: &dyn std::error::Error) -> String {
    let mut message = e.to_string();
    let mut source = e.source();
    while let Some(cause) = source {
        let text = cause.to_string();
        if !message.contains(&text) {
            message.push_str(": ");
            message.push_str(&text);
        }
        source = cause.source();
    }
    message
}

#[cfg(test)]
mod tests {
    use super::*;

    fn kv(k: &str, v: &str, e: bool) -> KeyValue {
        KeyValue::new(k, v, e)
    }

    #[test]
    fn builds_url_with_params_in_order() {
        let r = Request {
            method: "GET".into(),
            url: "example.com/a?x=1".into(),
            params: vec![kv("b", "2 3", true), kv("off", "x", false), kv("a", "&", true)],
            ..Default::default()
        };
        let built = r.build().unwrap();
        assert_eq!(built.url.as_str(), "http://example.com/a?x=1&b=2+3&a=%26");
    }

    #[test]
    fn refuses_bad_urls() {
        for (url, want) in [("", "URL is empty"), ("ftp://x", "unsupported scheme")] {
            let r = Request { url: url.into(), ..Default::default() };
            assert!(r.build().unwrap_err().contains(want), "{url}");
        }
    }

    #[test]
    fn user_headers_win() {
        let r = Request {
            method: "POST".into(),
            url: "example.com".into(),
            auth: Auth { kind: AUTH_BEARER.into(), token: "t".into(), ..Default::default() },
            headers: vec![kv("authorization", "mine", true), kv("x-a", "1", true), kv("X-A", "2", true), kv("host", "h", true)],
            body_type: BODY_JSON.into(),
            body: "{}".into(),
            ..Default::default()
        };
        let built = r.build().unwrap();
        assert_eq!(built.headers["Authorization"], vec!["mine"]);
        assert_eq!(built.headers["X-A"], vec!["1", "2"]);
        assert_eq!(built.headers["Content-Type"], vec!["application/json"]);
        assert_eq!(built.host.as_deref(), Some("h"));
    }

    #[test]
    fn saved_form_round_trips() {
        let text = r#"{"method":"GET","url":"u","params":null,"auth":{},"captures":[{"key":"id","value":"a.b","enabled":true}]}"#;
        let r: Request = serde_json::from_str(text).unwrap();
        assert_eq!(r.captures[0].variable, "id");
        let out = serde_json::to_string(&r).unwrap();
        assert_eq!(out, r#"{"method":"GET","url":"u","auth":{},"captures":[{"key":"id","value":"a.b","enabled":true}]}"#);
    }

    #[test]
    fn canonical_keys() {
        assert_eq!(canonical_header_key("content-TYPE"), "Content-Type");
        assert_eq!(canonical_header_key("bad key"), "bad key");
    }
}
