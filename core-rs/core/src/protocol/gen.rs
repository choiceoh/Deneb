//! Auto-generated protobuf types (via prost-build).
//!
//! Do not edit manually. Regenerate with: cargo check (or ./scripts/proto-gen.sh --rust)

/// Gateway protocol frame types from proto/gateway.proto.
#[allow(clippy::doc_markdown)] // prost-generated enum docs contain unescaped "ProtoBuf"
pub mod gateway {
    include!(concat!(env!("OUT_DIR"), "/deneb.gateway.rs"));
}

/// Channel types from proto/channel.proto.
#[allow(clippy::doc_markdown)] // prost-generated enum docs contain unescaped "ProtoBuf"
pub mod channel {
    include!(concat!(env!("OUT_DIR"), "/deneb.channel.rs"));
}

/// Session types from proto/session.proto.
#[allow(clippy::doc_markdown)] // prost-generated enum docs contain unescaped "ProtoBuf"
pub mod session {
    include!(concat!(env!("OUT_DIR"), "/deneb.session.rs"));
}

/// Plugin lifecycle types from proto/plugin.proto.
#[allow(clippy::doc_markdown)] // prost-generated enum docs contain unescaped "ProtoBuf"
pub mod plugin {
    include!(concat!(env!("OUT_DIR"), "/deneb.plugin.rs"));
}

/// Provider catalog types from proto/provider.proto.
#[allow(clippy::doc_markdown)] // prost-generated enum docs contain unescaped "ProtoBuf"
pub mod provider {
    include!(concat!(env!("OUT_DIR"), "/deneb.provider.rs"));
}

/// Agent execution types from proto/agent.proto.
#[allow(clippy::doc_markdown)] // prost-generated enum docs contain unescaped "ProtoBuf"
pub mod agent {
    include!(concat!(env!("OUT_DIR"), "/deneb.agent.rs"));
}
