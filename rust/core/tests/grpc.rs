//! Calls a gRPC server with an Echo service, described through reflection,
//! as Go's rpctest server does:
//!
//! ```proto
//! package test.v1;
//! message Inner { bool flag = 1; }
//! message EchoRequest { string text = 1; int32 count = 2; Inner inner = 3; repeated string tags = 4; }
//! message EchoResponse { string text = 1; string authorization = 2; string trace = 3; int32 index = 4; }
//! service Echo {
//!   rpc Say(EchoRequest) returns (EchoResponse);
//!   rpc Repeat(EchoRequest) returns (stream EchoResponse);
//!   rpc Fail(EchoRequest) returns (EchoResponse);
//! }
//! ```

use std::convert::Infallible;
use std::future::Future;
use std::pin::Pin;
use std::task::{Context, Poll};

use prost::Message as _;
use prost_reflect::{DescriptorPool, DynamicMessage, MessageDescriptor, Value};
use prost_types::field_descriptor_proto::{Label, Type};
use prost_types::{DescriptorProto, FieldDescriptorProto, FileDescriptorProto, FileDescriptorSet, MethodDescriptorProto, ServiceDescriptorProto};
use tonic::body::BoxBody;
use tonic::codec::{Codec, DecodeBuf, Decoder, EncodeBuf, Encoder};
use tonic::server::NamedService;
use tonic::Status;
use triggerapi_core::rest::{Auth, KeyValue, AUTH_BEARER};
use triggerapi_core::rpc;

fn field(name: &str, number: i32, kind: Type, label: Label, type_name: Option<&str>) -> FieldDescriptorProto {
    FieldDescriptorProto {
        name: Some(name.into()),
        json_name: Some(name.into()),
        number: Some(number),
        r#type: Some(kind as i32),
        label: Some(label as i32),
        type_name: type_name.map(Into::into),
        ..Default::default()
    }
}

fn file() -> FileDescriptorProto {
    let message = |name: &str, fields: Vec<FieldDescriptorProto>| DescriptorProto { name: Some(name.into()), field: fields, ..Default::default() };
    let method = |name: &str, out_stream: bool| MethodDescriptorProto {
        name: Some(name.into()),
        input_type: Some(".test.v1.EchoRequest".into()),
        output_type: Some(".test.v1.EchoResponse".into()),
        server_streaming: Some(out_stream),
        ..Default::default()
    };
    let opt = Label::Optional;
    FileDescriptorProto {
        name: Some("test/v1/echo.proto".into()),
        package: Some("test.v1".into()),
        syntax: Some("proto3".into()),
        message_type: vec![
            message("Inner", vec![field("flag", 1, Type::Bool, opt, None)]),
            message(
                "EchoRequest",
                vec![
                    field("text", 1, Type::String, opt, None),
                    field("count", 2, Type::Int32, opt, None),
                    field("inner", 3, Type::Message, opt, Some(".test.v1.Inner")),
                    field("tags", 4, Type::String, Label::Repeated, None),
                ],
            ),
            message(
                "EchoResponse",
                vec![
                    field("text", 1, Type::String, opt, None),
                    field("authorization", 2, Type::String, opt, None),
                    field("trace", 3, Type::String, opt, None),
                    field("index", 4, Type::Int32, opt, None),
                ],
            ),
        ],
        service: vec![ServiceDescriptorProto {
            name: Some("Echo".into()),
            method: vec![method("Say", false), method("Repeat", true), method("Fail", false)],
            ..Default::default()
        }],
        ..Default::default()
    }
}

#[derive(Clone)]
struct ServerCodec(MessageDescriptor);
struct Enc;
struct Dec(MessageDescriptor);

impl Encoder for Enc {
    type Item = DynamicMessage;
    type Error = Status;
    fn encode(&mut self, item: DynamicMessage, dst: &mut EncodeBuf<'_>) -> Result<(), Status> {
        item.encode(dst).map_err(|e| Status::internal(e.to_string()))
    }
}

impl Decoder for Dec {
    type Item = DynamicMessage;
    type Error = Status;
    fn decode(&mut self, src: &mut DecodeBuf<'_>) -> Result<Option<DynamicMessage>, Status> {
        DynamicMessage::decode(self.0.clone(), src).map(Some).map_err(|e| Status::internal(e.to_string()))
    }
}

impl Codec for ServerCodec {
    type Encode = DynamicMessage;
    type Decode = DynamicMessage;
    type Encoder = Enc;
    type Decoder = Dec;
    fn encoder(&mut self) -> Enc {
        Enc
    }
    fn decoder(&mut self) -> Dec {
        Dec(self.0.clone())
    }
}

#[derive(Clone)]
struct Echo {
    request: MessageDescriptor,
    response: MessageDescriptor,
}

impl NamedService for Echo {
    const NAME: &'static str = "test.v1.Echo";
}

fn text_of(m: &DynamicMessage, name: &str) -> String {
    m.get_field_by_name(name).and_then(|v| v.as_str().map(str::to_string)).unwrap_or_default()
}

impl Echo {
    fn reply(&self, req: &tonic::Request<DynamicMessage>, index: i32) -> DynamicMessage {
        let mut out = DynamicMessage::new(self.response.clone());
        let header = |name: &str| req.metadata().get(name).and_then(|v| v.to_str().ok()).unwrap_or("").to_string();
        out.set_field_by_name("text", Value::String(text_of(req.get_ref(), "text")));
        out.set_field_by_name("authorization", Value::String(header("authorization")));
        out.set_field_by_name("trace", Value::String(header("x-trace")));
        out.set_field_by_name("index", Value::I32(index));
        out
    }
}

#[derive(Clone)]
struct Unary(Echo, bool);

impl tonic::server::UnaryService<DynamicMessage> for Unary {
    type Response = DynamicMessage;
    type Future = Pin<Box<dyn Future<Output = Result<tonic::Response<DynamicMessage>, Status>> + Send>>;
    fn call(&mut self, request: tonic::Request<DynamicMessage>) -> Self::Future {
        let (echo, fail) = (self.0.clone(), self.1);
        Box::pin(async move {
            if fail {
                return Err(Status::not_found("nothing here"));
            }
            Ok(tonic::Response::new(echo.reply(&request, 0)))
        })
    }
}

#[derive(Clone)]
struct Repeat(Echo);

type MessageStream = Pin<Box<dyn tokio_stream::Stream<Item = Result<DynamicMessage, Status>> + Send>>;

impl tonic::server::ServerStreamingService<DynamicMessage> for Repeat {
    type Response = DynamicMessage;
    type ResponseStream = MessageStream;
    type Future = Pin<Box<dyn Future<Output = Result<tonic::Response<MessageStream>, Status>> + Send>>;
    fn call(&mut self, request: tonic::Request<DynamicMessage>) -> Self::Future {
        let echo = self.0.clone();
        Box::pin(async move {
            let count = request.get_ref().get_field_by_name("count").and_then(|v| v.as_i32()).unwrap_or(0);
            let replies: Vec<Result<DynamicMessage, Status>> = (0..count).map(|i| Ok(echo.reply(&request, i))).collect();
            let stream: MessageStream = Box::pin(tokio_stream::iter(replies));
            Ok(tonic::Response::new(stream))
        })
    }
}

impl tower_service::Service<http::Request<BoxBody>> for Echo {
    type Response = http::Response<BoxBody>;
    type Error = Infallible;
    type Future = Pin<Box<dyn Future<Output = Result<Self::Response, Infallible>> + Send>>;

    fn poll_ready(&mut self, _: &mut Context<'_>) -> Poll<Result<(), Infallible>> {
        Poll::Ready(Ok(()))
    }

    fn call(&mut self, req: http::Request<BoxBody>) -> Self::Future {
        let echo = self.clone();
        Box::pin(async move {
            let mut grpc = tonic::server::Grpc::new(ServerCodec(echo.request.clone()));
            let response = match req.uri().path() {
                "/test.v1.Echo/Say" => grpc.unary(Unary(echo, false), req).await,
                "/test.v1.Echo/Fail" => grpc.unary(Unary(echo, true), req).await,
                "/test.v1.Echo/Repeat" => grpc.server_streaming(Repeat(echo), req).await,
                _ => Status::unimplemented("no such method").into_http(),
            };
            Ok(response)
        })
    }
}

/// Starts a server, with reflection unless `reflection` is false, and
/// returns its address.
async fn start(reflection: bool) -> String {
    let file = file();
    let mut pool = DescriptorPool::new();
    pool.add_file_descriptor_proto(file.clone()).unwrap();
    let echo = Echo {
        request: pool.get_message_by_name("test.v1.EchoRequest").unwrap(),
        response: pool.get_message_by_name("test.v1.EchoResponse").unwrap(),
    };
    let set = FileDescriptorSet { file: vec![file] }.encode_to_vec();

    let listener = tokio::net::TcpListener::bind("127.0.0.1:0").await.unwrap();
    let address = listener.local_addr().unwrap().to_string();
    let incoming = tokio_stream::wrappers::TcpListenerStream::new(listener);
    let mut router = tonic::transport::Server::builder().add_service(echo);
    if reflection {
        let service = tonic_reflection::server::Builder::configure()
            .register_encoded_file_descriptor_set(&set)
            .build_v1alpha()
            .unwrap();
        router = router.add_service(service);
    }
    tokio::spawn(router.serve_with_incoming(incoming));
    address
}

#[tokio::test]
async fn reflects_and_invokes() {
    let address = start(true).await;
    let mut request = rpc::Request {
        url: address.clone(),
        method: "test.v1.Echo/Say".into(),
        message: r#"{"text": "hello"}"#.into(),
        auth: Auth { kind: AUTH_BEARER.into(), token: "t".into(), ..Default::default() },
        metadata: vec![KeyValue::new("X-Trace", "abc", true)],
        ..Default::default()
    };

    // Only the older version of reflection is served, which is fallen back
    // on.
    let schema = rpc::reflect(&request).await.unwrap();
    let names: Vec<&str> = schema.methods.iter().map(|m| m.name.as_str()).collect();
    assert_eq!(names, ["test.v1.Echo/Fail", "test.v1.Echo/Repeat", "test.v1.Echo/Say"]);
    assert!(schema.method("test.v1.Echo/Repeat").unwrap().server_streaming);
    assert_eq!(schema.services(), ["test.v1.Echo"]);

    let example: serde_json::Value = serde_json::from_str(&schema.example("test.v1.Echo/Say").unwrap()).unwrap();
    assert_eq!(example, serde_json::json!({"text": "", "count": 0, "inner": {"flag": false}, "tags": [""]}));

    let res = rpc::invoke(&request, Some(&schema)).await.unwrap();
    assert!(res.ok(), "{res:?}");
    assert_eq!(res.code_name, "OK");
    assert_eq!(res.protocol, "gRPC (proto)");
    let reply: serde_json::Value = serde_json::from_str(&res.messages[0]).unwrap();
    assert_eq!(reply, serde_json::json!({"text": "hello", "authorization": "Bearer t", "trace": "abc"}));
    assert!(res.messages[0].contains("\n  \"text\": \"hello\""), "{}", res.messages[0]);

    request.method = "test.v1.Echo/Repeat".into();
    request.message = r#"{"text": "x", "count": 3}"#.into();
    let res = rpc::invoke(&request, None).await.unwrap();
    assert_eq!(res.messages.len(), 3);

    request.method = "test.v1.Echo/Fail".into();
    let res = rpc::invoke(&request, Some(&schema)).await.unwrap();
    assert_eq!((res.code, res.code_name.as_str(), res.status.as_str()), (5, "NOT_FOUND", "nothing here"));

    request.message = "{nope".into();
    assert!(rpc::invoke(&request, Some(&schema)).await.unwrap_err().starts_with("invalid message"));
    request.method = "test.v1.Echo/None".into();
    assert!(rpc::invoke(&request, Some(&schema)).await.unwrap_err().contains("does not have a method"));
}

#[tokio::test]
async fn reports_what_cannot_be_reached() {
    let address = start(false).await;
    let request = rpc::Request { url: address, method: "test.v1.Echo/Say".into(), ..Default::default() };
    let err = rpc::reflect(&request).await.unwrap_err();
    assert_eq!(err, "the server does not support reflection, so its services cannot be discovered");

    // A server that is not there is a status, not an error.
    let listener = std::net::TcpListener::bind("127.0.0.1:0").unwrap();
    let gone = listener.local_addr().unwrap().to_string();
    drop(listener);
    let request = rpc::Request { url: gone, method: "test.v1.Echo/Say".into(), ..Default::default() };
    assert!(rpc::reflect(&request).await.is_err());
}
