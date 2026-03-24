//! Build script for napi-rs setup and protobuf code generation via prost.
//!
//! - napi-rs: sets up Node.js native addon build hooks.
//! - prost: compiles .proto files from ../proto/ into Rust structs in OUT_DIR,
//!   which are then included by src/protocol/gen.rs.

extern crate napi_build;

fn main() -> Result<(), Box<dyn std::error::Error>> {
    // napi-rs build setup for Node.js native addon.
    napi_build::setup();

    let proto_dir = "../proto";
    let protos = &[
        format!("{proto_dir}/gateway.proto"),
        format!("{proto_dir}/channel.proto"),
        format!("{proto_dir}/session.proto"),
    ];

    // Rerun if any proto file changes.
    for proto in protos {
        println!("cargo:rerun-if-changed={proto}");
    }

    // Generate without blanket Serialize — prost_types::Value doesn't impl Serialize.
    // Types that don't use google.protobuf.Value/Struct get Serialize via selective attributes.
    let mut config = prost_build::Config::new();

    // Channel and session types have no well-known type fields, so Serialize is safe.
    config.type_attribute("deneb.channel", "#[derive(serde::Serialize)]");
    config.type_attribute("deneb.session", "#[derive(serde::Serialize)]");

    // Gateway types that don't contain Value/Struct fields.
    config.type_attribute("deneb.gateway.StateVersion", "#[derive(serde::Serialize)]");
    config.type_attribute("deneb.gateway.PresenceEntry", "#[derive(serde::Serialize)]");

    config.compile_protos(protos, &[proto_dir])?;

    Ok(())
}
