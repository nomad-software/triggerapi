//! Turns a collection exported from Postman into saved requests, and reads
//! environments exported from Postman. It reads the version 2.0 and 2.1
//! collection formats.

use std::collections::HashMap;
use std::sync::LazyLock;

use regex::Regex;
use serde_json::Value;

use crate::collection::{Folder, Request};
use crate::env::{Environment, Variable};
use crate::query::split_url;
use crate::rest::{self, Auth, KeyValue};

fn str_of<'a>(v: &'a Value, key: &str) -> &'a str {
    v.get(key).and_then(Value::as_str).unwrap_or("")
}

/// Returns the value of a pair, which is not always a string.
fn text(value: Option<&Value>) -> String {
    match value {
        None | Some(Value::Null) => String::new(),
        Some(Value::String(s)) => s.clone(),
        Some(Value::Object(_)) | Some(Value::Array(_)) => {
            // Go prints a map or a list in its own way; this is near enough.
            serde_json::to_string(value.unwrap()).unwrap_or_default()
        }
        Some(other) => other.to_string(),
    }
}

/// An authorization, whose settings are under the name of its type, as a
/// list of pairs in version 2.1 and as an object in version 2.0.
struct Authorization {
    kind: String,
    settings: HashMap<String, String>,
}

fn read_auth(v: Option<&Value>) -> Option<Authorization> {
    let v = v?.as_object()?;
    let kind = v.get("type")?.as_str()?.to_string();
    let mut settings = HashMap::new();
    match v.get(&kind) {
        Some(Value::Array(list)) => {
            for p in list {
                settings.insert(str_of(p, "key").to_string(), text(p.get("value")));
            }
        }
        Some(Value::Object(object)) => {
            for (k, value) in object {
                settings.insert(k.clone(), text(Some(value)));
            }
        }
        _ => {}
    }
    Some(Authorization { kind, settings })
}

/// Reads a Postman collection and returns a folder of requests for it,
/// named after the collection, with its folders inside as they are in
/// Postman.
///
/// A request is given its method, address, query parameters, headers, body
/// and authorization, which is that of the nearest folder above it if it
/// has none of its own. Postman's `{{variables}}` are kept as they are.
/// Scripts, saved responses and descriptions are left out.
pub fn import(data: &[u8]) -> Result<Folder, String> {
    let doc: Value = serde_json::from_slice(data).map_err(|e| format!("the file could not be read as JSON: {e}"))?;
    let schema = doc.get("info").map(|i| str_of(i, "schema")).unwrap_or("");
    if !schema.contains("getpostman.com") && !schema.contains("postman.com") {
        return Err("the file is not a Postman collection".into());
    }
    let name = doc.get("info").map(|i| str_of(i, "name").trim().to_string()).unwrap_or_default();
    let mut root = Folder::new(if name.is_empty() { "Imported Collection".to_string() } else { name });

    let auth = read_auth(doc.get("auth"));
    let items = doc.get("item").and_then(Value::as_array).cloned().unwrap_or_default();
    if fill(&mut root, &items, auth.as_ref()) == 0 {
        return Err("the collection does not have any requests".into());
    }
    Ok(root)
}

/// Adds the items to a folder, and returns the number of requests added to
/// it and to the folders inside it.
fn fill(folder: &mut Folder, items: &[Value], inherited: Option<&Authorization>) -> usize {
    let mut count = 0;
    for it in items {
        let own = read_auth(it.get("auth"));
        let authorization = own.as_ref().or(inherited);
        if let Some(request) = it.get("request").filter(|r| !r.is_null()) {
            let request = convert(request, authorization);
            let mut name = str_of(it, "name").trim().to_string();
            if name.is_empty() {
                name = format!("{} {}", request.method, request.url);
            }
            folder.add(Request::new_rest(name, request));
            count += 1;
        } else if let Some(sub) = it.get("item").and_then(Value::as_array) {
            let mut name = str_of(it, "name").trim().to_string();
            if name.is_empty() {
                name = "Folder".into();
            }
            let id = folder.add_folder(&name);
            let inner = folder.folders.iter_mut().find(|f| f.id == id).unwrap();
            count += fill(inner, sub, authorization);
        }
    }
    count
}

/// A `:name` part of a path, which is how Postman writes a variable that
/// belongs to one request.
static PATH_VARIABLE: LazyLock<Regex> = LazyLock::new(|| Regex::new(r"(^|/):([A-Za-z_][A-Za-z0-9_]*)").unwrap());

/// Converts Postman's pairs, leaving out any with no key.
fn pairs(from: Option<&Value>) -> Vec<KeyValue> {
    from.and_then(Value::as_array)
        .into_iter()
        .flatten()
        .filter(|p| !str_of(p, "key").is_empty())
        .map(|p| KeyValue::new(str_of(p, "key"), text(p.get("value")), !p.get("disabled").and_then(Value::as_bool).unwrap_or(false)))
        .collect()
}

/// Makes a request from one of Postman's, which is an object or just the
/// address to get.
fn convert(r: &Value, inherited: Option<&Authorization>) -> rest::Request {
    let empty = Value::Null;
    let (method, url) = match r {
        Value::String(raw) => ("", Value::String(raw.clone())),
        _ => (str_of(r, "method"), r.get("url").cloned().unwrap_or(Value::Null)),
    };
    let mut request = rest::Request { method: method.trim().to_uppercase(), ..Default::default() };
    if request.method.is_empty() {
        request.method = "GET".into();
    }

    // The address is an object or a string.
    let (raw, query, variables) = match &url {
        Value::String(s) => (s.clone(), None, None),
        Value::Object(_) => (str_of(&url, "raw").to_string(), url.get("query"), url.get("variable")),
        _ => (String::new(), None, None),
    };

    // The query parameters are listed apart from the address, which has
    // only those that are switched on.
    let (address, typed) = split_url(raw.trim());
    request.params = match query {
        Some(q) if !q.is_null() => pairs(Some(q)),
        _ => typed,
    };

    // A path variable is given its value, or made a placeholder if it has
    // none.
    let mut values = HashMap::new();
    for v in variables.and_then(Value::as_array).into_iter().flatten() {
        values.insert(str_of(v, "key").to_string(), text(v.get("value")));
    }
    let (scheme, remainder) = match address.split_once("://") {
        Some((s, r)) => (Some(s.to_string()), r.to_string()),
        None => (None, address.clone()),
    };
    let remainder = PATH_VARIABLE
        .replace_all(&remainder, |caps: &regex::Captures| {
            let name = &caps[2];
            match values.get(name).filter(|v| !v.is_empty()) {
                Some(value) => format!("{}{value}", &caps[1]),
                None => format!("{}{{{{{name}}}}}", &caps[1]),
            }
        })
        .into_owned();
    request.url = match scheme {
        Some(s) => format!("{s}://{remainder}"),
        None => remainder,
    };

    request.headers = pairs(r.get("header").or(Some(&empty)));
    if let Some(body) = r.get("body").filter(|b| !b.is_null()) {
        set_body(&mut request, body);
    }

    let own = read_auth(r.get("auth"));
    set_auth(&mut request, own.as_ref().or(inherited));
    request
}

/// Gives the request the body of one of Postman's.
fn set_body(request: &mut rest::Request, b: &Value) {
    match str_of(b, "mode") {
        "raw" => {
            // Line endings are made the same, as collections made on
            // Windows have both kinds.
            request.body = str_of(b, "raw").replace("\r\n", "\n");
            let language = b
                .get("options")
                .and_then(|o| o.get("raw"))
                .map(|r| str_of(r, "language"))
                .unwrap_or("");
            request.body_type = raw_type(language, &request.headers, &request.body).into();
        }
        "urlencoded" => {
            request.body_type = rest::BODY_FORM.into();
            request.form = pairs(b.get("urlencoded"));
        }
        "formdata" => {
            // There is no multipart body, so the fields that are text are
            // kept as a form. Files to upload are left out.
            request.body_type = rest::BODY_FORM.into();
            for field in b.get("formdata").and_then(Value::as_array).into_iter().flatten() {
                if str_of(field, "type") != "file" && !str_of(field, "key").is_empty() {
                    let enabled = !field.get("disabled").and_then(Value::as_bool).unwrap_or(false);
                    request.form.push(KeyValue::new(str_of(field, "key"), text(field.get("value")), enabled));
                }
            }
        }
        "graphql" => {
            let Some(graphql) = b.get("graphql").filter(|g| !g.is_null()) else { return };
            // A GraphQL request is sent as JSON holding the query.
            let mut payload = serde_json::Map::new();
            payload.insert("query".into(), Value::String(str_of(graphql, "query").into()));
            if let Ok(variables) = serde_json::from_str::<Value>(str_of(graphql, "variables")) {
                if !variables.is_null() {
                    payload.insert("variables".into(), variables);
                }
            }
            if let Ok(text) = serde_json::to_string_pretty(&Value::Object(payload)) {
                request.body_type = rest::BODY_JSON.into();
                request.body = text;
            }
        }
        _ => {}
    }
}

/// Works out what kind of text a raw body is: from the language Postman
/// recorded for it, or else its Content-Type header, or else what it looks
/// like.
fn raw_type(language: &str, headers: &[KeyValue], text: &str) -> &'static str {
    let mut kind = language.to_lowercase();
    if kind.is_empty() || kind == "text" {
        if let Some(h) = headers.iter().find(|h| h.enabled && h.key.eq_ignore_ascii_case("Content-Type")) {
            kind = h.value.to_lowercase();
        }
    }
    let trimmed = text.trim();
    if kind.contains("json") {
        rest::BODY_JSON
    } else if kind.contains("xml") {
        rest::BODY_XML
    } else if !kind.is_empty() {
        rest::BODY_TEXT
    } else if trimmed.starts_with('{') || trimmed.starts_with('[') {
        rest::BODY_JSON
    } else if trimmed.starts_with('<') {
        rest::BODY_XML
    } else {
        rest::BODY_TEXT
    }
}

/// Gives the request an authorization of Postman's. Kinds there is no
/// equivalent for are left out.
fn set_auth(request: &mut rest::Request, a: Option<&Authorization>) {
    let Some(a) = a else { return };
    let setting = |k: &str| a.settings.get(k).cloned().unwrap_or_default();
    match a.kind.to_lowercase().as_str() {
        "basic" => {
            request.auth = Auth { kind: rest::AUTH_BASIC.into(), username: setting("username"), password: setting("password"), ..Default::default() };
        }
        "bearer" => request.auth = Auth { kind: rest::AUTH_BEARER.into(), token: setting("token"), ..Default::default() },
        "oauth2" => {
            let mut token = setting("accessToken");
            if token.is_empty() {
                token = "{{token}}".into();
            }
            request.auth = Auth { kind: rest::AUTH_BEARER.into(), token, ..Default::default() };
        }
        "apikey" => {
            let key = KeyValue::new(setting("key"), setting("value"), true);
            if key.key.is_empty() {
                return;
            }
            if setting("in").eq_ignore_ascii_case("query") {
                request.params.push(key);
            } else {
                request.headers.push(key);
            }
        }
        _ => {}
    }
}

/// Reads an environment exported from Postman. Variables without a name are
/// left out.
pub fn import_environment(data: &[u8]) -> Result<Environment, String> {
    let doc: Value = serde_json::from_slice(data).map_err(|e| format!("not valid JSON: {e}"))?;
    let Some(values) = doc.get("values").and_then(Value::as_array) else {
        return Err("not a Postman environment".into());
    };
    let name = str_of(&doc, "name").trim().to_string();
    let mut environment = Environment { name: if name.is_empty() { "Imported Environment".into() } else { name }, variables: Vec::new() };
    for v in values {
        let key = str_of(v, "key");
        if key.is_empty() {
            continue;
        }
        // A value that is not a string is kept as it was written.
        let value = match v.get("value") {
            None | Some(Value::Null) => String::new(),
            Some(Value::String(s)) => s.clone(),
            Some(other) => other.to_string(),
        };
        let enabled = v.get("enabled").and_then(Value::as_bool).unwrap_or(true);
        environment.variables.push(Variable { key: key.into(), value, enabled, description: String::new() });
    }
    Ok(environment)
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn imports_a_collection() {
        let data = br#"{
            "info": {"name": "Hotel", "schema": "https://schema.getpostman.com/json/collection/v2.1.0/collection.json"},
            "auth": {"type": "bearer", "bearer": [{"key": "token", "value": "{{token}}"}]},
            "item": [
                {"name": "Rooms", "item": [
                    {"name": "Book", "request": {"method": "post", "url": {"raw": "{{host}}/rooms/:id?x=1", "query": [{"key": "x", "value": "1"}, {"key": "y", "value": "2", "disabled": true}], "variable": [{"key": "id", "value": ""}]},
                        "header": [{"key": "Content-Type", "value": "application/json"}],
                        "body": {"mode": "raw", "raw": "{\r\n}"}}}
                ]},
                {"name": "Ping", "request": "https://h/ping", "auth": {"type": "basic", "basic": {"username": "u", "password": "p"}}}
            ]}"#;
        let root = import(data).unwrap();
        assert_eq!(root.name, "Hotel");
        let book = root.folders[0].requests[0].rest.as_ref().unwrap();
        assert_eq!(book.method, "POST");
        assert_eq!(book.url, "{{host}}/rooms/{{id}}");
        assert_eq!(book.params, vec![KeyValue::new("x", "1", true), KeyValue::new("y", "2", false)]);
        assert_eq!(book.body_type, "json");
        assert_eq!(book.body, "{\n}");
        assert_eq!(book.auth.token, "{{token}}");
        let ping = root.requests[0].rest.as_ref().unwrap();
        assert_eq!((ping.method.as_str(), ping.url.as_str(), ping.auth.kind.as_str()), ("GET", "https://h/ping", "basic"));
        assert!(import(br#"{"info": {"schema": "x"}}"#).is_err());
    }

    #[test]
    fn imports_an_environment() {
        let e = import_environment(br#"{"name": " Dev ", "values": [{"key": "a", "value": 1}, {"key": "b", "value": "x", "enabled": false}, {"key": "", "value": "y"}]}"#).unwrap();
        assert_eq!(e.name, "Dev");
        assert_eq!(e.variables.len(), 2);
        assert_eq!(e.variables[0].value, "1");
        assert!(!e.variables[1].enabled);
        assert!(import_environment(b"{}").is_err());
    }
}
