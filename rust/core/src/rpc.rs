//! Discovers and calls gRPC services. Services are described by the server
//! itself through gRPC server reflection, so no protobuf files are needed.

use std::time::{Duration, Instant};

use prost::Message as _;
use prost_reflect::{DescriptorPool, DynamicMessage, Kind, MessageDescriptor, ReflectMessage, SerializeOptions, Value};
use serde::{Deserialize, Serialize};
use tonic::codec::{Codec, DecodeBuf, Decoder, EncodeBuf, Encoder};
use tonic::metadata::{MetadataKey, MetadataMap, MetadataValue};
use tonic::transport::{Channel, ClientTlsConfig, Endpoint};
use tonic::Status;

use crate::capture::Rule;
use crate::rest::{self, Auth, KeyValue};
use crate::{json, nullable};

/// A gRPC call as entered by the user.
#[derive(Debug, Clone, Default, PartialEq, Serialize, Deserialize)]
pub struct Request {
    /// The address of the server as host:port. A grpc:// or http:// scheme
    /// is allowed, and grpcs:// or https:// turn TLS on.
    #[serde(default, deserialize_with = "nullable::de")]
    pub url: String,
    #[serde(default, deserialize_with = "nullable::de", skip_serializing_if = "nullable::is_default")]
    pub tls: bool,
    /// The full name of the method, as package.Service/Method.
    #[serde(default, deserialize_with = "nullable::de")]
    pub method: String,
    /// The request message as JSON.
    #[serde(default, deserialize_with = "nullable::de", skip_serializing_if = "String::is_empty")]
    pub message: String,
    #[serde(default, deserialize_with = "nullable::de", skip_serializing_if = "Vec::is_empty")]
    pub metadata: Vec<KeyValue>,
    #[serde(default, deserialize_with = "nullable::de")]
    pub auth: Auth,
    /// The values to take from the last response message and keep in
    /// variables of the active environment.
    #[serde(default, deserialize_with = "nullable::de", skip_serializing_if = "Vec::is_empty")]
    pub captures: Vec<Rule>,
}

/// The result of a call.
#[derive(Debug, Clone, Default)]
pub struct Response {
    /// The gRPC status code the call ended with, which is 0 for OK, and its
    /// name, such as NOT_FOUND. `status` is the message that came with it.
    pub code: i32,
    pub code_name: String,
    pub status: String,
    /// The response messages as indented JSON.
    pub messages: Vec<String>,
    pub headers: Vec<(String, String)>,
    pub trailers: Vec<(String, String)>,
    pub duration: Duration,
    /// The RPC protocol the server answered in, such as "gRPC (proto)", as
    /// its Content-Type header says. It is empty if there was no answer.
    pub protocol: String,
}

impl Response {
    /// Reports if the call succeeded.
    pub fn ok(&self) -> bool {
        self.code == 0
    }
}

/// Returns the RPC protocol, and the encoding of its messages, that a
/// response with a content type is in: "gRPC (proto)" for
/// application/grpc+proto. It is empty for a content type that is not of an
/// RPC.
pub fn protocol(content_type: &str) -> String {
    let lower = content_type.to_lowercase();
    let media = lower.split(';').next().unwrap_or("").trim().to_string();
    for (prefix, name) in [
        ("application/grpc-web-text", "gRPC-Web text"),
        ("application/grpc-web", "gRPC-Web"),
        ("application/grpc", "gRPC"),
        ("application/connect", "Connect stream"),
    ] {
        if let Some(rest) = media.strip_prefix(prefix) {
            if rest.is_empty() {
                // Messages are protobuf unless it says otherwise.
                return format!("{name} (proto)");
            }
            if let Some(encoding) = rest.strip_prefix('+') {
                return format!("{name} ({encoding})");
            }
        }
    }
    match media.as_str() {
        "application/proto" => "Connect (proto)".into(),
        "application/json" => "Connect (json)".into(),
        _ => String::new(),
    }
}

impl Request {
    /// Returns a copy of the request with `f` applied to every field the
    /// user can type into.
    pub fn expand(&self, f: &dyn Fn(&str) -> String) -> Request {
        Request {
            url: f(&self.url),
            message: f(&self.message),
            metadata: rest::expand_pairs(&self.metadata, f),
            auth: self.auth.expand(f),
            ..self.clone()
        }
    }

    /// Returns the base URL of the server, which has an https scheme if TLS
    /// is used and an http scheme if not.
    pub fn target(&self) -> Result<String, String> {
        let mut address = self.url.trim();
        let mut secure = self.tls;
        if let Some((scheme, rest)) = address.split_once("://") {
            match scheme.to_lowercase().as_str() {
                "grpc" | "http" => {}
                "grpcs" | "https" => secure = true,
                _ => return Err(format!("unsupported scheme {scheme:?}")),
            }
            address = rest;
        }
        let address = address.trim_end_matches('/');
        if address.is_empty() {
            return Err("URL is empty".into());
        }
        Ok(format!("{}://{address}", if secure { "https" } else { "http" }))
    }

    /// Returns the metadata and authorization of the request as the headers
    /// gRPC sends them as. User supplied metadata takes precedence over the
    /// authorization.
    pub fn headers(&self) -> Vec<(String, String)> {
        let mut headers: Vec<(String, String)> = Vec::new();
        if let Some(value) = self.auth.header() {
            headers.push(("authorization".into(), value));
        }
        let mut seen = std::collections::HashSet::new();
        for m in &self.metadata {
            if !m.enabled || m.key.is_empty() {
                continue;
            }
            let key = m.key.to_lowercase();
            if seen.insert(key.clone()) {
                headers.retain(|(k, _)| *k != key);
            }
            headers.push((key, m.value.clone()));
        }
        headers
    }

    /// Returns the metadata of the request for tonic.
    fn metadata(&self) -> Result<MetadataMap, String> {
        let mut map = MetadataMap::new();
        for (key, value) in self.headers() {
            if key.ends_with("-bin") {
                let k = MetadataKey::from_bytes(key.as_bytes()).map_err(|e| format!("metadata {key}: {e}"))?;
                map.append_bin(k, tonic::metadata::MetadataValue::from_bytes(value.as_bytes()));
            } else {
                let k = MetadataKey::from_bytes(key.as_bytes()).map_err(|e| format!("metadata {key}: {e}"))?;
                let v = MetadataValue::try_from(value.as_str()).map_err(|e| format!("metadata {key}: {e}"))?;
                map.append(k, v);
            }
        }
        Ok(map)
    }

    /// Connects to the server. gRPC needs HTTP/2, which is used without TLS
    /// if the server is not secure.
    async fn channel(&self) -> Result<Channel, ChannelError> {
        let target = self.target().map_err(ChannelError::Request)?;
        let mut endpoint = Endpoint::from_shared(target.clone()).map_err(|e| ChannelError::Request(e.to_string()))?;
        if target.starts_with("https://") {
            endpoint = endpoint
                .tls_config(ClientTlsConfig::new().with_native_roots())
                .map_err(|e| ChannelError::Request(e.to_string()))?;
        }
        endpoint
            .connect()
            .await
            .map_err(|e| ChannelError::Connect(rest::error_chain(&e)))
    }
}

enum ChannelError {
    /// The request does not say where to connect.
    Request(String),
    /// The server could not be reached.
    Connect(String),
}

/// A method of a service on the server.
#[derive(Debug, Clone)]
pub struct Method {
    /// The full name of the method, as package.Service/Method.
    pub name: String,
    pub client_streaming: bool,
    pub server_streaming: bool,
    input: MessageDescriptor,
    output: MessageDescriptor,
}

/// Describes the services of a server.
#[derive(Debug, Clone)]
pub struct Schema {
    /// Sorted by name.
    pub methods: Vec<Method>,
}

impl Schema {
    /// Finds a method by its full name.
    pub fn method(&self, name: &str) -> Option<&Method> {
        self.methods.iter().find(|m| m.name == name)
    }

    /// Returns the names of the services, in order.
    pub fn services(&self) -> Vec<String> {
        let mut out: Vec<String> = Vec::new();
        for m in &self.methods {
            let service = m.name.split('/').next().unwrap_or("").to_string();
            if out.last() != Some(&service) {
                out.push(service);
            }
        }
        out
    }

    /// Returns a JSON message for the method with every field present, as a
    /// starting point to edit.
    pub fn example(&self, method: &str) -> Result<String, String> {
        let m = self
            .method(method)
            .ok_or_else(|| format!("the server does not have a method named {method}"))?;
        let mut message = DynamicMessage::new(m.input.clone());
        populate(&mut message, &mut Vec::new());
        let options = SerializeOptions::new().skip_default_fields(false);
        format_message(&message, &options)
    }
}

/// Returns a message as indented JSON.
fn format_message(message: &DynamicMessage, options: &SerializeOptions) -> Result<String, String> {
    let mut serializer = serde_json::Serializer::new(Vec::new());
    message
        .serialize_with_options(&mut serializer, options)
        .map_err(|e| e.to_string())?;
    let text = String::from_utf8(serializer.into_inner()).map_err(|e| e.to_string())?;
    json::indent(&text, "  ")
}

/// Creates the nested messages of a message, and gives its lists one
/// element, so that their fields show up in the example. Scalar fields are
/// left for the encoder to emit with their default values. A message type
/// is not created inside itself, which would never end.
fn populate(message: &mut DynamicMessage, within: &mut Vec<String>) {
    let desc = message.descriptor();
    within.push(desc.full_name().to_string());
    for field in desc.fields() {
        // Only the first member of a oneof can be set.
        if let Some(oneof) = field.containing_oneof() {
            if oneof.fields().next().map(|f| f.number()) != Some(field.number()) {
                continue;
            }
        }
        if field.is_map() {
            continue;
        }
        let nested = match field.kind() {
            Kind::Message(m) => Some(m),
            _ => None,
        };
        if let Some(n) = &nested {
            if within.iter().any(|w| w == n.full_name()) || n.package_name() == "google.protobuf" {
                continue;
            }
        }
        if field.is_list() {
            let element = match &nested {
                Some(n) => {
                    let mut m = DynamicMessage::new(n.clone());
                    populate(&mut m, within);
                    Value::Message(m)
                }
                None => Value::default_value(&field.kind()),
            };
            message.set_field(&field, Value::List(vec![element]));
        } else if let Some(n) = &nested {
            let mut m = DynamicMessage::new(n.clone());
            populate(&mut m, within);
            message.set_field(&field, Value::Message(m));
        } else if field.containing_oneof().is_some() {
            message.set_field(&field, Value::default_value_for_field(&field));
        }
    }
    within.pop();
}

/// Encodes and decodes messages that have no type of their own.
#[derive(Clone)]
struct DynamicCodec {
    output: MessageDescriptor,
}

struct DynamicEncoder;
struct DynamicDecoder(MessageDescriptor);

impl Encoder for DynamicEncoder {
    type Item = DynamicMessage;
    type Error = Status;
    fn encode(&mut self, item: DynamicMessage, dst: &mut EncodeBuf<'_>) -> Result<(), Status> {
        item.encode(dst).map_err(|e| Status::internal(e.to_string()))
    }
}

impl Decoder for DynamicDecoder {
    type Item = DynamicMessage;
    type Error = Status;
    fn decode(&mut self, src: &mut DecodeBuf<'_>) -> Result<Option<DynamicMessage>, Status> {
        DynamicMessage::decode(self.0.clone(), src)
            .map(Some)
            .map_err(|e| Status::internal(e.to_string()))
    }
}

impl Codec for DynamicCodec {
    type Encode = DynamicMessage;
    type Decode = DynamicMessage;
    type Encoder = DynamicEncoder;
    type Decoder = DynamicDecoder;
    fn encoder(&mut self) -> DynamicEncoder {
        DynamicEncoder
    }
    fn decoder(&mut self) -> DynamicDecoder {
        DynamicDecoder(self.output.clone())
    }
}

/// Returns the name of a status code as gRPC writes it, such as NOT_FOUND.
pub fn code_name(code: tonic::Code) -> &'static str {
    use tonic::Code::*;
    match code {
        Ok => "OK",
        Cancelled => "CANCELED",
        Unknown => "UNKNOWN",
        InvalidArgument => "INVALID_ARGUMENT",
        DeadlineExceeded => "DEADLINE_EXCEEDED",
        NotFound => "NOT_FOUND",
        AlreadyExists => "ALREADY_EXISTS",
        PermissionDenied => "PERMISSION_DENIED",
        ResourceExhausted => "RESOURCE_EXHAUSTED",
        FailedPrecondition => "FAILED_PRECONDITION",
        Aborted => "ABORTED",
        OutOfRange => "OUT_OF_RANGE",
        Unimplemented => "UNIMPLEMENTED",
        Internal => "INTERNAL",
        Unavailable => "UNAVAILABLE",
        DataLoss => "DATA_LOSS",
        Unauthenticated => "UNAUTHENTICATED",
    }
}

fn pairs(map: &MetadataMap) -> Vec<(String, String)> {
    map.clone()
        .into_headers()
        .iter()
        .map(|(k, v)| (rest::canonical_header_key(k.as_str()), String::from_utf8_lossy(v.as_bytes()).into_owned()))
        .collect()
}

/// Calls the method of the request, sending its message and collecting the
/// responses until the server ends the call. The schema describes the
/// server; if it is `None` it is fetched first using reflection.
///
/// A call that the server rejects, or which cannot reach it, is not an
/// error: the status and any messages received before it are in the
/// response. Dropping the future cancels the call.
pub async fn invoke(request: &Request, schema: Option<&Schema>) -> Result<Response, String> {
    let fetched;
    let schema = match schema {
        Some(s) => s,
        None => {
            fetched = reflect(request).await?;
            &fetched
        }
    };

    let method = match schema.method(&request.method) {
        Some(m) => m.clone(),
        None if request.method.is_empty() => return Err("no method selected".into()),
        None => return Err(format!("the server does not have a method named {}", request.method)),
    };

    let message = if request.message.trim().is_empty() { "{}" } else { request.message.as_str() };
    let mut deserializer = serde_json::Deserializer::from_str(message);
    let input = DynamicMessage::deserialize(method.input.clone(), &mut deserializer)
        .and_then(|m| deserializer.end().map(|_| m))
        .map_err(|e| format!("invalid message: {e}"))?;
    let metadata = request.metadata()?;

    let start = Instant::now();
    let mut res = Response { code_name: "OK".into(), ..Default::default() };
    let fail = |res: &mut Response, status: &Status| {
        res.code = status.code() as i32;
        res.code_name = code_name(status.code()).into();
        res.status = status.message().to_string();
    };

    let channel = match request.channel().await {
        Ok(c) => c,
        Err(ChannelError::Request(e)) => return Err(e),
        Err(ChannelError::Connect(e)) => {
            res.duration = start.elapsed();
            fail(&mut res, &Status::unavailable(e));
            return Ok(res);
        }
    };

    let mut client = tonic::client::Grpc::new(channel);
    let path: http::uri::PathAndQuery = format!("/{}", method.name).parse().map_err(|e: http::uri::InvalidUri| e.to_string())?;
    let mut call = tonic::Request::new(tokio_stream::once(input));
    *call.metadata_mut() = metadata;

    let options = SerializeOptions::new();
    let outcome = async {
        client.ready().await.map_err(|e| Status::unavailable(rest::error_chain(&e)))?;
        client.streaming(call, path, DynamicCodec { output: method.output.clone() }).await
    }
    .await;

    match outcome {
        Err(status) => {
            res.headers = pairs(status.metadata());
            fail(&mut res, &status);
        }
        Ok(response) => {
            res.headers = pairs(response.metadata());
            let mut stream = response.into_inner();
            loop {
                match stream.message().await {
                    Ok(Some(m)) => res.messages.push(format_message(&m, &options)?),
                    Ok(None) => break,
                    Err(status) => {
                        fail(&mut res, &status);
                        break;
                    }
                }
            }
            if res.ok() {
                if let Ok(Some(trailers)) = stream.trailers().await {
                    res.trailers = pairs(&trailers);
                }
            }
        }
    }
    res.duration = start.elapsed();
    res.protocol = protocol(
        res.headers
            .iter()
            .find(|(k, _)| k.eq_ignore_ascii_case("content-type"))
            .map(|(_, v)| v.as_str())
            .unwrap_or(""),
    );
    Ok(res)
}

/// Asks the server of the request to describe its services, through the
/// newer version of the reflection service or, failing that, the older one,
/// which is all many servers have.
pub async fn reflect(request: &Request) -> Result<Schema, String> {
    let channel = match request.channel().await {
        Ok(c) => c,
        Err(ChannelError::Request(e)) | Err(ChannelError::Connect(e)) => return Err(e),
    };
    let metadata = request.metadata()?;

    let files = match reflection::v1(channel.clone(), metadata.clone()).await {
        Err(status) if status.code() == tonic::Code::Unimplemented => reflection::v1alpha(channel, metadata).await,
        other => other,
    };
    let (services, files) = files.map_err(|status| {
        if status.code() == tonic::Code::Unimplemented {
            "the server does not support reflection, so its services cannot be discovered".to_string()
        } else {
            status.message().to_string()
        }
    })?;
    build_schema(&services, files)
}

/// Builds the schema from the names of the services and the files that
/// describe them.
fn build_schema(services: &[String], files: Vec<prost_types::FileDescriptorProto>) -> Result<Schema, String> {
    // The well-known types are known already, for servers that do not
    // describe them.
    let mut pool = DescriptorPool::global();
    pool.add_file_descriptor_protos(files).map_err(|e| e.to_string())?;

    let mut methods = Vec::new();
    for name in services {
        let service = pool
            .get_service_by_name(name)
            .ok_or_else(|| format!("the server did not describe its service {name}"))?;
        for m in service.methods() {
            methods.push(Method {
                name: format!("{name}/{}", m.name()),
                client_streaming: m.is_client_streaming(),
                server_streaming: m.is_server_streaming(),
                input: m.input(),
                output: m.output(),
            });
        }
    }
    methods.sort_by(|a, b| a.name.cmp(&b.name));
    Ok(Schema { methods })
}

/// The two versions of the reflection service, which have the same messages
/// in different packages.
mod reflection {
    use std::collections::{HashMap, HashSet};

    use prost::Message as _;
    use prost_types::FileDescriptorProto;
    use tokio::sync::mpsc;
    use tokio_stream::wrappers::ReceiverStream;
    use tonic::metadata::MetadataMap;
    use tonic::transport::Channel;
    use tonic::Status;

    macro_rules! client {
        ($name:ident, $pb:path) => {
            pub async fn $name(channel: Channel, metadata: MetadataMap) -> Result<(Vec<String>, Vec<FileDescriptorProto>), Status> {
                use $pb as pb;
                use pb::server_reflection_client::ServerReflectionClient;
                use pb::server_reflection_request::MessageRequest;
                use pb::server_reflection_response::MessageResponse;

                let (tx, rx) = mpsc::channel(4);
                let mut request = tonic::Request::new(ReceiverStream::new(rx));
                *request.metadata_mut() = metadata;
                let mut client = ServerReflectionClient::new(channel);

                let ask = |message: MessageRequest| pb::ServerReflectionRequest { host: String::new(), message_request: Some(message) };
                // The first question goes with the call, which the server
                // can refuse before answering anything.
                tx.send(ask(MessageRequest::ListServices(String::new()))).await.ok();
                let mut stream = client.server_reflection_info(request).await?.into_inner();

                let next = async |stream: &mut tonic::Streaming<pb::ServerReflectionResponse>| -> Result<MessageResponse, Status> {
                    match stream.message().await? {
                        Some(pb::ServerReflectionResponse { message_response: Some(MessageResponse::ErrorResponse(e)), .. }) => {
                            Err(Status::new(tonic::Code::from(e.error_code), e.error_message))
                        }
                        Some(pb::ServerReflectionResponse { message_response: Some(m), .. }) => Ok(m),
                        _ => Err(Status::unknown("the reflection stream ended")),
                    }
                };

                let names = match next(&mut stream).await? {
                    MessageResponse::ListServicesResponse(list) => list.service.into_iter().map(|s| s.name).collect::<Vec<_>>(),
                    _ => return Err(Status::unknown("the server did not list its services")),
                };
                // The reflection service itself is of no interest.
                let services: Vec<String> = names.into_iter().filter(|n| !n.starts_with("grpc.reflection.")).collect();

                let mut protos: HashMap<String, FileDescriptorProto> = HashMap::new();
                let add = |response: MessageResponse, protos: &mut HashMap<String, FileDescriptorProto>| {
                    if let MessageResponse::FileDescriptorResponse(files) = response {
                        for bytes in files.file_descriptor_proto {
                            if let Ok(file) = FileDescriptorProto::decode(bytes.as_slice()) {
                                protos.insert(file.name().to_string(), file);
                            }
                        }
                    }
                };

                for service in &services {
                    tx.send(ask(MessageRequest::FileContainingSymbol(service.clone()))).await.ok();
                    let response = next(&mut stream).await?;
                    add(response, &mut protos);
                }

                // The files those import, until everything is known. A file
                // the server cannot provide may be a well-known type, which
                // is known already.
                let mut asked: HashSet<String> = HashSet::new();
                loop {
                    let missing: Vec<String> = protos
                        .values()
                        .flat_map(|f| f.dependency.iter().cloned())
                        .filter(|d| !protos.contains_key(d) && !asked.contains(d))
                        .collect::<HashSet<_>>()
                        .into_iter()
                        .collect();
                    if missing.is_empty() {
                        break;
                    }
                    for name in missing {
                        asked.insert(name.clone());
                        tx.send(ask(MessageRequest::FileByFilename(name))).await.ok();
                        match next(&mut stream).await {
                            Ok(response) => add(response, &mut protos),
                            Err(s) if s.code() == tonic::Code::NotFound => {}
                            Err(s) => return Err(s),
                        }
                    }
                }
                drop(tx);
                Ok((services, protos.into_values().collect()))
            }
        };
    }

    client!(v1, tonic_reflection::pb::v1);
    client!(v1alpha, tonic_reflection::pb::v1alpha);
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn names_protocols() {
        assert_eq!(protocol("application/grpc"), "gRPC (proto)");
        assert_eq!(protocol("application/grpc+json; charset=utf-8"), "gRPC (json)");
        assert_eq!(protocol("application/grpc-web-text+proto"), "gRPC-Web text (proto)");
        assert_eq!(protocol("application/json"), "Connect (json)");
        assert_eq!(protocol("text/html"), "");
    }

    #[test]
    fn targets() {
        let r = |url: &str, tls: bool| Request { url: url.into(), tls, ..Default::default() }.target();
        assert_eq!(r("localhost:50051", false).unwrap(), "http://localhost:50051");
        assert_eq!(r("grpcs://h:1/", false).unwrap(), "https://h:1");
        assert_eq!(r("h:1", true).unwrap(), "https://h:1");
        assert!(r("ftp://h", false).is_err());
        assert!(r(" ", false).is_err());
    }

    #[test]
    fn metadata_wins_over_auth() {
        let r = Request {
            auth: Auth { kind: rest::AUTH_BEARER.into(), token: "t".into(), ..Default::default() },
            metadata: vec![KeyValue::new("Authorization", "mine", true), KeyValue::new("x-a", "1", true), KeyValue::new("X-A", "2", true)],
            ..Default::default()
        };
        assert_eq!(
            r.headers(),
            vec![("authorization".into(), "mine".into()), ("x-a".into(), "1".into()), ("x-a".into(), "2".into())]
        );
    }
}
