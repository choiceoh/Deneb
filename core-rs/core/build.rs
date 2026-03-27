//! Build script for napi-rs setup and protobuf code generation via prost.
//!
//! - napi-rs: sets up Node.js native addon build hooks.
//! - prost: compiles .proto files from ../../proto/ into Rust structs in `OUT_DIR`,
//!   which are then included by src/protocol/gen.rs.

use std::path::PathBuf;

fn main() -> Result<(), Box<dyn std::error::Error>> {
    // napi-rs build setup for Node.js native addon (only when feature enabled).
    #[cfg(feature = "napi_binding")]
    napi_build::setup();

    let manifest_dir = PathBuf::from(std::env::var("CARGO_MANIFEST_DIR")?);
    let proto_dir = manifest_dir.join("../../proto");

    if !proto_dir.is_dir() {
        return Err(format!(
            "Proto directory not found: {}. \
             Ensure the proto/ directory exists at the repo root.",
            proto_dir.display()
        )
        .into());
    }

    let proto_dir = proto_dir.canonicalize()?;

    let proto_names = [
        "gateway", "channel", "session", "plugin", "provider", "agent",
    ];
    let mut protos = Vec::with_capacity(proto_names.len());

    for name in &proto_names {
        let path = proto_dir.join(format!("{name}.proto"));
        if !path.is_file() {
            return Err(format!(
                "Missing proto file: {}. \
                 Expected all of: {:?}",
                path.display(),
                proto_names
            )
            .into());
        }
        protos.push(path);
    }

    // Rerun if any proto file changes.
    for proto in &protos {
        println!("cargo:rerun-if-changed={}", proto.display());
    }
    // Also rerun if the proto directory itself changes (new/deleted files).
    println!("cargo:rerun-if-changed={}", proto_dir.display());

    // Selectively derive Serialize on types without google.protobuf.Value/Struct fields,
    // since prost_types::Value doesn't implement serde::Serialize.
    let mut config = prost_build::Config::new();
    config.type_attribute(".deneb.channel", "#[derive(serde::Serialize)]");
    config.type_attribute(".deneb.session", "#[derive(serde::Serialize)]");
    // Note: ErrorShape intentionally excluded — it contains google.protobuf.Value
    // (prost_types::Value) which doesn't implement Serialize.
    config.type_attribute(".deneb.gateway.StateVersion", "#[derive(serde::Serialize)]");
    config.type_attribute(
        ".deneb.gateway.PresenceEntry",
        "#[derive(serde::Serialize)]",
    );
    config.type_attribute(".deneb.plugin", "#[derive(serde::Serialize)]");
    config.type_attribute(".deneb.provider", "#[derive(serde::Serialize)]");
    config.type_attribute(".deneb.agent", "#[derive(serde::Serialize)]");

    config.compile_protos(&protos, &[&proto_dir])?;

    Ok(())
}
