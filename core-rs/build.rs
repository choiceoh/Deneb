//! Build script for napi-rs setup and protobuf code generation via prost.
//!
//! - napi-rs: sets up Node.js native addon build hooks.
//! - prost: compiles .proto files from ../proto/ into Rust structs in OUT_DIR,
//!   which are then included by src/protocol/gen.rs.

extern crate napi_build;

use std::path::PathBuf;

fn main() -> Result<(), Box<dyn std::error::Error>> {
    // napi-rs build setup for Node.js native addon.
    napi_build::setup();

    let manifest_dir = PathBuf::from(env!("CARGO_MANIFEST_DIR"));
    let proto_dir = manifest_dir.join("../proto");
    let proto_dir_str = proto_dir.to_str().expect("proto dir path is valid UTF-8");

    let protos: Vec<String> = ["gateway", "channel", "session"]
        .iter()
        .map(|name| format!("{proto_dir_str}/{name}.proto"))
        .collect();

    // Rerun if any proto file changes.
    for proto in &protos {
        println!("cargo:rerun-if-changed={proto}");
    }

    // Selectively derive Serialize on types without google.protobuf.Value/Struct fields,
    // since prost_types::Value doesn't implement serde::Serialize.
    let mut config = prost_build::Config::new();
    config.type_attribute("deneb.channel", "#[derive(serde::Serialize)]");
    config.type_attribute("deneb.session", "#[derive(serde::Serialize)]");
    config.type_attribute("deneb.gateway.StateVersion", "#[derive(serde::Serialize)]");
    config.type_attribute("deneb.gateway.PresenceEntry", "#[derive(serde::Serialize)]");

    config.compile_protos(&protos, &[proto_dir_str])?;

    Ok(())
}
