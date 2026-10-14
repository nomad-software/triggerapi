//! Turns an OpenAPI description of an API into saved requests. It reads
//! OpenAPI 3 and Swagger 2 documents, written as JSON or YAML.

use std::collections::{BTreeMap, HashSet};
use std::sync::LazyLock;

use regex::Regex;
use serde_json::{Map, Value};

use crate::collection::{Folder, Request};
use crate::rest::{self, Auth, KeyValue};

/// Reads YAML, or JSON, which is also YAML, into a JSON value. Keys that are
/// not strings, such as the codes of responses, are made strings.
fn read(data: &[u8]) -> Result<Value, String> {
    let yaml: serde_yaml::Value = serde_yaml::from_slice(data).map_err(|e| e.to_string())?;
    Ok(convert(yaml))
}

fn convert(v: serde_yaml::Value) -> Value {
    use serde_yaml::Value as Y;
    match v {
        Y::Null => Value::Null,
        Y::Bool(b) => Value::Bool(b),
        Y::Number(n) => {
            if let Some(i) = n.as_i64() {
                Value::from(i)
            } else if let Some(u) = n.as_u64() {
                Value::from(u)
            } else {
                serde_json::Number::from_f64(n.as_f64().unwrap_or(0.0)).map(Value::Number).unwrap_or(Value::Null)
            }
        }
        Y::String(s) => Value::String(s),
        Y::Sequence(items) => Value::Array(items.into_iter().map(convert).collect()),
        Y::Mapping(m) => {
            let mut out = Map::new();
            for (k, v) in m {
                let key = match k {
                    Y::String(s) => s,
                    Y::Number(n) => n.to_string(),
                    Y::Bool(b) => b.to_string(),
                    Y::Null => "null".into(),
                    other => serde_yaml::to_string(&other).unwrap_or_default().trim().to_string(),
                };
                out.insert(key, convert(v));
            }
            Value::Object(out)
        }
        Y::Tagged(t) => convert(t.value),
    }
}

fn str_of<'a>(v: &'a Value, key: &str) -> &'a str {
    v.get(key).and_then(Value::as_str).unwrap_or("")
}

fn obj<'a>(v: &'a Value, path: &[&str]) -> Option<&'a Map<String, Value>> {
    let mut at = v;
    for p in path {
        at = at.get(p)?;
    }
    at.as_object()
}

fn present(v: Option<&Value>) -> Option<&Value> {
    v.filter(|v| !v.is_null())
}

static PATH_PARAMETER: LazyLock<Regex> = LazyLock::new(|| Regex::new(r"\{([^{}]+)\}").unwrap());

/// Rewrites the `{name}` parameters of a path or server address as
/// `{{name}}` placeholders.
fn placeholders(s: &str) -> String {
    PATH_PARAMETER.replace_all(s, "{{$1}}").into_owned()
}

/// Returns a value as it is written in a parameter.
fn text(value: &Value) -> String {
    match value {
        Value::String(s) => s.clone(),
        Value::Object(_) | Value::Array(_) => serde_json::to_string(value).unwrap_or_default(),
        Value::Null => "<nil>".into(),
        other => other.to_string(),
    }
}

struct Document<'a> {
    doc: &'a Value,
}

/// The most deeply an example goes.
const MAX_DEPTH: usize = 8;
/// The most values made for one example. Schemas which refer to one
/// another, as those of a large API do, would otherwise make an example
/// many times the size of the document.
const MAX_VALUES: usize = 2000;

impl<'a> Document<'a> {
    /// Follows a reference to something defined elsewhere in the document,
    /// such as #/components/schemas/User, looking in each place in turn.
    fn resolve(&self, reference: &str, places: &[&[&str]]) -> Option<&'a Value> {
        let name = reference.rsplit('/').next().unwrap_or(reference);
        places
            .iter()
            .find_map(|place| obj(self.doc, place).and_then(|m| m.get(name)))
            .filter(|v| !v.is_null())
    }

    fn schema_of(&self, s: Option<&'a Value>) -> Option<&'a Value> {
        let mut s = s?;
        for _ in 0..MAX_DEPTH {
            let reference = str_of(s, "$ref");
            if reference.is_empty() {
                return if s.is_null() { None } else { Some(s) };
            }
            s = self.resolve(reference, &[&["components", "schemas"], &["definitions"]])?;
        }
        None
    }

    /// Returns the address of the server the requests are sent to. It is
    /// the `{{baseUrl}}` placeholder if the document does not give one, and
    /// starts with it if the document gives only a path.
    fn base_url(&self) -> String {
        let d = self.doc;
        let servers = d.get("servers").and_then(Value::as_array);
        let base = if let Some(first) = servers.and_then(|s| s.first()) {
            str_of(first, "url").to_string()
        } else if !str_of(d, "host").is_empty() {
            let scheme = d
                .get("schemes")
                .and_then(Value::as_array)
                .and_then(|s| s.first())
                .and_then(Value::as_str)
                .unwrap_or("https");
            format!("{scheme}://{}{}", str_of(d, "host"), str_of(d, "basePath"))
        } else {
            str_of(d, "basePath").to_string()
        };
        let base = placeholders(&base).trim_end_matches('/').to_string();
        if base.contains("://") {
            base
        } else {
            format!("{{{{baseUrl}}}}{base}")
        }
    }

    /// Gives the request the query parameters and headers of an operation.
    /// Those of the path apply to every operation on it, and are replaced
    /// by any of the operation with the same name. A parameter that is not
    /// required is switched off.
    fn add_parameters(&self, request: &mut rest::Request, parameters: Vec<&'a Value>) {
        let mut unique: Vec<&Value> = Vec::new();
        let mut seen: BTreeMap<(String, String), usize> = BTreeMap::new();
        for p in parameters {
            let mut p = p;
            if p.is_null() {
                continue;
            }
            let reference = str_of(p, "$ref");
            if !reference.is_empty() {
                match self.resolve(reference, &[&["components", "parameters"], &["parameters"]]) {
                    Some(found) => p = found,
                    None => continue,
                }
            }
            let key = (str_of(p, "in").to_string(), str_of(p, "name").to_string());
            if let Some(&at) = seen.get(&key) {
                unique[at] = p;
                continue;
            }
            seen.insert(key, unique.len());
            unique.push(p);
        }

        let mut form = Vec::new();
        for p in unique {
            let required = p.get("required").and_then(Value::as_bool).unwrap_or(false);
            let pair = KeyValue::new(str_of(p, "name"), self.parameter_value(p), required);
            match str_of(p, "in") {
                "query" => request.params.push(pair),
                "header" => request.headers.push(pair),
                // Swagger 2 lists the fields of a form as parameters.
                "formData" => form.push(pair),
                // Swagger 2 gives the body as a parameter with a schema.
                "body" => {
                    let example = self.example(p.get("schema"), 0);
                    set_json_body(request, example);
                }
                _ => {}
            }
        }
        if !form.is_empty() {
            request.body_type = rest::BODY_FORM.into();
            request.form = form;
        }
    }

    /// Returns a value to start a parameter off with, which is empty if the
    /// document does not suggest one.
    fn parameter_value(&self, p: &Value) -> String {
        for key in ["example", "default"] {
            if let Some(v) = present(p.get(key)) {
                return text(v);
            }
        }
        if let Some(first) = p.get("enum").and_then(Value::as_array).and_then(|e| e.first()) {
            return text(first);
        }
        if let Some(s) = self.schema_of(p.get("schema")) {
            for key in ["example", "default"] {
                if let Some(v) = present(s.get(key)) {
                    return text(v);
                }
            }
            if let Some(first) = s.get("enum").and_then(Value::as_array).and_then(|e| e.first()) {
                return text(first);
            }
        }
        String::new()
    }

    /// Gives the request the body of an operation, from the first kind of
    /// content it can send: JSON, then a form, then text or XML.
    fn add_body(&self, request: &mut rest::Request, body: Option<&'a Value>) {
        let Some(mut body) = present(body) else { return };
        let reference = str_of(body, "$ref");
        if !reference.is_empty() {
            match self.resolve(reference, &[&["components", "requestBodies"]]) {
                Some(found) => body = found,
                None => return,
            }
        }
        let Some(content) = body.get("content").and_then(Value::as_object) else { return };
        let mut types: Vec<&String> = content.keys().collect();
        types.sort();
        let find = |matches: &dyn Fn(&str) -> bool| -> Option<&'a Value> {
            types
                .iter()
                .find(|t| matches(&t.to_lowercase()) && !content[t.as_str()].is_null())
                .map(|t| &content[t.as_str()])
        };

        if let Some(media) = find(&|t| t.contains("json")) {
            let example = self.media_example(media);
            set_json_body(request, example);
            return;
        }
        if let Some(media) = find(&|t| t == "application/x-www-form-urlencoded") {
            request.body_type = rest::BODY_FORM.into();
            if let Some(s) = self.schema_of(media.get("schema")) {
                if let Some(props) = s.get("properties").and_then(Value::as_object) {
                    for (name, prop) in props {
                        let value = self.example(Some(prop), 1).unwrap_or(Value::String(String::new()));
                        request.form.push(KeyValue::new(name.clone(), text(&value), true));
                    }
                }
            }
            return;
        }
        for (kind, body_type) in [("xml", rest::BODY_XML), ("text/", rest::BODY_TEXT)] {
            if let Some(media) = find(&|t| t.contains(kind)) {
                request.body_type = body_type.into();
                if let Some(example) = media.get("example").and_then(Value::as_str) {
                    request.body = example.into();
                }
                return;
            }
        }
    }

    /// Returns an example of the content: the one the document gives, or
    /// one made from its schema.
    fn media_example(&self, media: &'a Value) -> Option<Value> {
        if let Some(e) = present(media.get("example")) {
            return Some(e.clone());
        }
        if let Some(examples) = media.get("examples").and_then(Value::as_object) {
            for example in examples.values() {
                if let Some(v) = present(example.get("value")) {
                    return Some(v.clone());
                }
            }
        }
        self.example(media.get("schema"), 0)
    }

    /// Makes a value that fits a schema: the example or default it gives,
    /// or a placeholder value of its type. It is `None` if there is nothing
    /// to go on.
    fn example(&self, s: Option<&'a Value>, depth: usize) -> Option<Value> {
        let mut maker = ExampleMaker { doc: self, within: HashSet::new(), left: MAX_VALUES };
        maker.example(s, depth)
    }

    /// Gives the request the authorization an operation needs, which is the
    /// default of the document unless the operation says otherwise. Only
    /// the first of the schemes it could use is taken.
    fn add_security(&self, request: &mut rest::Request, op: &Value) {
        let requirements = match op.get("security") {
            Some(Value::Array(own)) => own,
            _ => match self.doc.get("security").and_then(Value::as_array) {
                Some(d) => d,
                None => return,
            },
        };
        for requirement in requirements {
            let Some(requirement) = requirement.as_object() else { continue };
            for name in requirement.keys() {
                let Some(scheme) = self.resolve(&format!("/{name}"), &[&["components", "securitySchemes"], &["securityDefinitions"]]) else {
                    continue;
                };
                let kind = str_of(scheme, "type").to_lowercase();
                let inside = str_of(scheme, "in");
                if kind == "basic" || (kind == "http" && str_of(scheme, "scheme").eq_ignore_ascii_case("basic")) {
                    request.auth = Auth { kind: rest::AUTH_BASIC.into(), username: "{{username}}".into(), password: "{{password}}".into(), ..Default::default() };
                } else if kind == "http" || kind == "oauth2" || kind == "openidconnect" {
                    request.auth = Auth { kind: rest::AUTH_BEARER.into(), token: "{{token}}".into(), ..Default::default() };
                } else if kind == "apikey" && inside == "header" {
                    request.headers.push(KeyValue::new(str_of(scheme, "name"), "{{apiKey}}", true));
                } else if kind == "apikey" && inside == "query" {
                    request.params.push(KeyValue::new(str_of(scheme, "name"), "{{apiKey}}", true));
                } else {
                    continue;
                }
                return;
            }
        }
    }
}

/// Makes the body of the request the value, as indented JSON.
fn set_json_body(request: &mut rest::Request, value: Option<Value>) {
    request.body_type = rest::BODY_JSON.into();
    if let Some(v) = value {
        if let Ok(text) = serde_json::to_string_pretty(&v) {
            request.body = text;
        }
    }
}

/// Makes one example from the schemas of a document.
struct ExampleMaker<'d, 'a> {
    doc: &'d Document<'a>,
    /// The schemas the value being made is inside, as a schema is not made
    /// inside itself, which would never end.
    within: HashSet<usize>,
    /// The number of values that can still be made.
    left: usize,
}

fn kind(s: &Value) -> String {
    match s.get("type") {
        Some(Value::String(t)) => t.clone(),
        // OpenAPI 3.1 allows a list of types, of which the first that is
        // not null is taken.
        Some(Value::Array(types)) => types
            .iter()
            .filter_map(Value::as_str)
            .find(|t| *t != "null")
            .unwrap_or("")
            .to_string(),
        _ => String::new(),
    }
}

impl<'a> ExampleMaker<'_, 'a> {
    fn example(&mut self, s: Option<&'a Value>, depth: usize) -> Option<Value> {
        if self.left == 0 {
            return None;
        }
        self.left -= 1;

        let s = self.doc.schema_of(s)?;
        let key = s as *const Value as usize;
        if depth > MAX_DEPTH || self.within.contains(&key) {
            return None;
        }
        self.within.insert(key);
        let made = self.make(s, depth);
        self.within.remove(&key);
        made
    }

    fn make(&mut self, s: &'a Value, depth: usize) -> Option<Value> {
        for key in ["example", "default"] {
            if let Some(v) = present(s.get(key)) {
                return Some(v.clone());
            }
        }
        if let Some(first) = s.get("enum").and_then(Value::as_array).and_then(|e| e.first()) {
            return Some(first.clone());
        }

        // A schema made of others has the properties of them all, or is the
        // first of its alternatives.
        if let Some(parts) = s.get("allOf").and_then(Value::as_array).filter(|p| !p.is_empty()) {
            let mut merged = Map::new();
            for part in parts {
                if let Some(Value::Object(o)) = self.example(Some(part), depth + 1) {
                    merged.extend(o);
                }
            }
            if let Some(props) = s.get("properties").and_then(Value::as_object) {
                if let Some(Value::Object(o)) = self.object(props, depth + 1) {
                    merged.extend(o);
                }
            }
            return Some(Value::Object(merged));
        }
        for alternatives in ["oneOf", "anyOf"] {
            if let Some(first) = s.get(alternatives).and_then(Value::as_array).and_then(|a| a.first()) {
                return self.example(Some(first), depth + 1);
            }
        }

        let props = s.get("properties").and_then(Value::as_object).filter(|p| !p.is_empty());
        let k = kind(s);
        if k == "object" || props.is_some() {
            return match props {
                Some(props) => self.object(props, depth),
                None => Some(Value::Object(Map::new())),
            };
        }
        match k.as_str() {
            "array" => Some(Value::Array(self.example(s.get("items"), depth + 1).into_iter().collect())),
            "string" => Some(Value::String(
                match str_of(s, "format") {
                    "date" => "2024-01-01",
                    "date-time" => "2024-01-01T00:00:00Z",
                    "uuid" => "00000000-0000-0000-0000-000000000000",
                    "email" => "user@example.com",
                    _ => "string",
                }
                .into(),
            )),
            "integer" | "number" => Some(Value::from(0)),
            "boolean" => Some(Value::Bool(false)),
            _ => None,
        }
    }

    fn object(&mut self, props: &'a Map<String, Value>, depth: usize) -> Option<Value> {
        // The properties that are plain values come first, so that they are
        // not the ones left out if there are too many values.
        let mut names: Vec<&String> = props.keys().collect();
        names.sort_by_key(|n| self.nests(props.get(n.as_str())));
        let mut object = Map::new();
        for name in names {
            if self.left == 0 {
                break;
            }
            let value = self.example(props.get(name.as_str()), depth + 1).unwrap_or(Value::Null);
            object.insert(name.clone(), value);
        }
        Some(Value::Object(object))
    }

    /// Reports, as 1 or 0, if a schema is of a value that has others inside
    /// it.
    fn nests(&self, s: Option<&'a Value>) -> u8 {
        match self.doc.schema_of(s) {
            Some(s)
                if s.get("properties").and_then(Value::as_object).is_some_and(|p| !p.is_empty())
                    || present(s.get("items")).is_some()
                    || ["allOf", "oneOf", "anyOf"].iter().any(|k| s.get(k).and_then(Value::as_array).is_some_and(|a| !a.is_empty())) =>
            {
                1
            }
            _ => 0,
        }
    }
}

/// Reads an OpenAPI document and returns a folder of requests for it: one
/// for each operation, named after its summary. The folder is named after
/// the API. Operations with a tag are in a folder inside it for their first
/// tag.
pub fn import(data: &[u8]) -> Result<Folder, String> {
    let doc = read(data).map_err(|e| format!("the file could not be read as JSON or YAML: {e}"))?;
    if str_of(&doc, "openapi").is_empty() && str_of(&doc, "swagger").is_empty() {
        return Err("the file is not an OpenAPI document".into());
    }
    let d = Document { doc: &doc };

    let title = doc.get("info").map(|i| str_of(i, "title").trim().to_string()).unwrap_or_default();
    let mut root = Folder::new(if title.is_empty() { "Imported API".to_string() } else { title });

    // The folders for the tags are in the order the document lists them,
    // followed by any it does not list, in the order they are come across.
    let mut tagged: Vec<String> = Vec::new();
    for tag in doc.get("tags").and_then(Value::as_array).into_iter().flatten() {
        let name = str_of(tag, "name");
        if !tagged.iter().any(|t| t == name) {
            tagged.push(name.to_string());
            root.add_folder(name);
        }
    }

    let base = d.base_url();
    let mut count = 0;
    if let Some(paths) = doc.get("paths").and_then(Value::as_object) {
        for (path, item) in paths {
            if item.is_null() {
                continue;
            }
            for method in ["get", "post", "put", "patch", "delete", "head", "options"] {
                let Some(op) = present(item.get(method)) else { continue };
                let upper = method.to_uppercase();
                let mut request = rest::Request { method: upper.clone(), url: format!("{base}{}", placeholders(path)), ..Default::default() };

                let mut parameters: Vec<&Value> = item.get("parameters").and_then(Value::as_array).map(|a| a.iter().collect()).unwrap_or_default();
                parameters.extend(op.get("parameters").and_then(Value::as_array).into_iter().flatten());
                d.add_parameters(&mut request, parameters);
                d.add_body(&mut request, op.get("requestBody"));
                d.add_security(&mut request, op);

                let mut name = str_of(op, "summary").trim().to_string();
                if name.is_empty() {
                    name = str_of(op, "operationId").to_string();
                }
                if name.is_empty() {
                    name = format!("{upper} {path}");
                }

                let tag = op
                    .get("tags")
                    .and_then(Value::as_array)
                    .and_then(|t| t.first())
                    .and_then(Value::as_str)
                    .unwrap_or("");
                let folder = if tag.is_empty() {
                    &mut root
                } else {
                    if !tagged.iter().any(|t| t == tag) {
                        tagged.push(tag.to_string());
                        root.add_folder(tag);
                    }
                    let at = tagged.iter().position(|t| t == tag).unwrap();
                    &mut root.folders[at]
                };
                folder.add(Request::new_rest(name, request));
                count += 1;
            }
        }
    }
    if count == 0 {
        return Err("the document does not describe any operations".into());
    }
    // A tag the document lists but does not use has an empty folder.
    root.folders.retain(|f| !f.requests.is_empty());
    Ok(root)
}

#[cfg(test)]
mod tests {
    use super::*;

    const DOC: &str = r##"
openapi: 3.0.0
info: {title: Shop}
servers: [{url: "https://{region}.shop.test/v1/"}]
tags: [{name: items}, {name: unused}]
security: [{bearer: []}]
components:
  securitySchemes:
    bearer: {type: http, scheme: bearer}
    key: {type: apiKey, in: header, name: X-Key}
  schemas:
    Item:
      type: object
      properties:
        id: {type: integer}
        name: {type: string, example: Pen}
        tags: {type: array, items: {type: string}}
        parent: {$ref: "#/components/schemas/Item"}
paths:
  /items/{id}:
    parameters: [{name: id, in: path, required: true}]
    get:
      tags: [items]
      summary: Get item
      parameters:
        - {name: verbose, in: query, schema: {type: boolean, default: false}}
      security: [{key: []}]
      responses: {200: {description: ok}}
    put:
      tags: [items]
      operationId: putItem
      requestBody:
        content:
          application/json: {schema: {$ref: "#/components/schemas/Item"}}
  /ping:
    get: {}
"##;

    #[test]
    fn imports() {
        let root = import(DOC.as_bytes()).unwrap();
        assert_eq!(root.name, "Shop");
        assert_eq!(root.folders.len(), 1);
        let items = &root.folders[0];
        assert_eq!(items.name, "items");
        let get = items.requests[0].rest.as_ref().unwrap();
        assert_eq!(items.requests[0].name, "Get item");
        assert_eq!(get.url, "https://{{region}}.shop.test/v1/items/{{id}}");
        assert_eq!(get.params, vec![KeyValue::new("verbose", "false", false)]);
        assert_eq!(get.headers, vec![KeyValue::new("X-Key", "{{apiKey}}", true)]);
        let put = items.requests[1].rest.as_ref().unwrap();
        assert_eq!(items.requests[1].name, "putItem");
        assert_eq!(put.auth.kind, "bearer");
        let body: Value = serde_json::from_str(&put.body).unwrap();
        assert_eq!(body, serde_json::json!({"id": 0, "name": "Pen", "tags": ["string"], "parent": null}));
        assert_eq!(root.requests[0].name, "GET /ping");
        assert!(import(b"{}").is_err());
    }
}
