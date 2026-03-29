// Re-export the unified query helper so callers that need a custom call function
// (e.g. call_gateway_with_config) can still reach it via this module.
pub use crate::subcli::rpc_helpers::rpc_query_custom as run_gateway_query;
