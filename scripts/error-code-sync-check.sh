#!/usr/bin/env bash
# Error code sync check — protocol error codes (proto ↔ Rust).
#
# Verifies that the ErrorCode enum in proto/gateway.proto and the wire-string
# definitions in core-rs/core/src/protocol/error_codes.rs are in sync.
#
# FFI error codes (Rust ↔ Go) are handled separately by scripts/gen-ffi-errors.sh
# and the `make ffi-gen-check` target.
set -euo pipefail

PROTO_FILE="proto/gateway.proto"
RUST_FILE="core-rs/core/src/protocol/error_codes.rs"

# cd to repo root
cd "$(git rev-parse --show-toplevel)"

fail() { echo "ERROR: $*" >&2; exit 1; }

errors=0

# =========================================================================
# Protocol error codes (proto / Rust)
# =========================================================================

# --- Extract from proto (strip ERROR_CODE_ prefix, skip UNSPECIFIED) ---
proto_codes=$(grep -oP 'ERROR_CODE_\K[A-Z_]+(?=\s*=)' "$PROTO_FILE" | grep -v '^UNSPECIFIED$' | sort)
[[ -n "$proto_codes" ]] || fail "No error codes found in $PROTO_FILE"

# --- Extract from Rust (enum variant names mapped to wire strings via as_str) ---
# Parse the as_str() match block: `Self::Foo => "FOO_BAR"` → FOO_BAR
rust_codes=$(sed -n '/fn as_str/,/^    }/p' "$RUST_FILE" | grep -oP '=> "\K[A-Z_]+(?=")' | sort)
[[ -n "$rust_codes" ]] || fail "No error codes found in $RUST_FILE"

# --- Compare proto vs Rust ---
diff_proto_rust=$(diff <(echo "$proto_codes") <(echo "$rust_codes") || true)
if [[ -n "$diff_proto_rust" ]]; then
  echo "MISMATCH: proto vs Rust protocol error codes"
  echo "$diff_proto_rust"
  echo ""
  echo "  proto: $PROTO_FILE"
  echo "  Rust:  $RUST_FILE"
  errors=1
fi

proto_count=$(echo "$proto_codes" | wc -l)

# =========================================================================
# Summary
# =========================================================================

if [[ "$errors" -ne 0 ]]; then
  exit 1
fi

echo "error-code-sync: protocol codes in sync ($proto_count codes)"
