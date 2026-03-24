//! Auto-generated protobuf types (via prost-build).
//!
//! Do not edit manually. Regenerate with: cargo build (or ./scripts/proto-gen.sh --rust)

/// Gateway protocol frame types from proto/gateway.proto.
pub mod gateway {
    include!(concat!(env!("OUT_DIR"), "/deneb.gateway.rs"));
}

/// Channel types from proto/channel.proto.
pub mod channel {
    include!(concat!(env!("OUT_DIR"), "/deneb.channel.rs"));
}

/// Session types from proto/session.proto.
pub mod session {
    include!(concat!(env!("OUT_DIR"), "/deneb.session.rs"));
}
