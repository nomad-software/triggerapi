//! The parts of TriggerAPI that do not depend on the UI: the HTTP and gRPC
//! engines, environments, saved requests, workspaces, settings and the
//! importers. The files it reads and writes are those of the Go program, so
//! the two can share them.

pub mod capture;
pub mod collection;
pub mod crypt;
pub mod curl;
pub mod env;
pub mod format;
pub mod json;
pub mod jsonfile;
mod nullable;
pub mod openapi;
pub mod postman;
pub mod query;
pub mod rest;
pub mod rpc;
pub mod settings;
pub mod workspace;
