#!/usr/bin/env bash
# Error code 3-way sync check.
# Extracts error codes from proto, Rust, and TypeScript sources
# and fails if any source is out of sync.
set -euo pipefail

PROTO_FILE="proto/gateway.proto"
RUST_FILE="core-rs/core/src/protocol/error_codes.rs"
TS_FILE="src/gateway/protocol/schema/error-codes.ts"

# cd to repo root
cd "$(git rev-parse --show-toplevel)"

fail() { echo "ERROR: $*" >&2; exit 1; }

# --- Extract from proto (strip ERROR_CODE_ prefix, skip UNSPECIFIED) ---
proto_codes=$(grep -oP 'ERROR_CODE_\K[A-Z_]+(?=\s*=)' "$PROTO_FILE" | grep -v '^UNSPECIFIED$' | sort)
[[ -n "$proto_codes" ]] || fail "No error codes found in $PROTO_FILE"

# --- Extract from Rust (enum variant names mapped to wire strings via as_str) ---
# Parse the as_str() match block: `Self::Foo => "FOO_BAR"` → FOO_BAR
rust_codes=$(sed -n '/fn as_str/,/^    }/p' "$RUST_FILE" | grep -oP '=> "\K[A-Z_]+(?=")' | sort)
[[ -n "$rust_codes" ]] || fail "No error codes found in $RUST_FILE"

# --- Extract from TypeScript (keys of ErrorCodes object: `KEY: "VALUE"`) ---
ts_codes=$(sed -n '/^export const ErrorCodes/,/^} as const/p' "$TS_FILE" | grep -oP '^\s+\K[A-Z_]+(?=:\s*")' | sort)
[[ -n "$ts_codes" ]] || fail "No error codes found in $TS_FILE"

errors=0

# --- Compare proto vs Rust ---
diff_proto_rust=$(diff <(echo "$proto_codes") <(echo "$rust_codes") || true)
if [[ -n "$diff_proto_rust" ]]; then
  echo "MISMATCH: proto vs Rust"
  echo "$diff_proto_rust"
  errors=1
fi

# --- Compare proto vs TypeScript ---
diff_proto_ts=$(diff <(echo "$proto_codes") <(echo "$ts_codes") || true)
if [[ -n "$diff_proto_ts" ]]; then
  echo "MISMATCH: proto vs TypeScript"
  echo "$diff_proto_ts"
  errors=1
fi

if [[ "$errors" -ne 0 ]]; then
  echo ""
  echo "Error codes are out of sync across proto/Rust/TypeScript."
  echo "  proto:      $PROTO_FILE"
  echo "  Rust:       $RUST_FILE"
  echo "  TypeScript: $TS_FILE"
  exit 1
fi

count=$(echo "$proto_codes" | wc -l)
echo "error-code-sync: all 3 sources in sync ($count codes)"
