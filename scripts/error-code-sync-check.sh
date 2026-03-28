#!/usr/bin/env bash
# Error code sync check.
#
# Part 1: Protocol error codes — 2-way sync (proto / Rust).
# Part 2: FFI error codes — 2-way sync (Rust lib.rs / Go errors.go).
set -euo pipefail

PROTO_FILE="proto/gateway.proto"
RUST_FILE="core-rs/core/src/protocol/error_codes.rs"
RUST_FFI_FILE="core-rs/core/src/lib.rs"
GO_FFI_FILE="gateway-go/internal/ffi/errors.go"

# cd to repo root
cd "$(git rev-parse --show-toplevel)"

fail() { echo "ERROR: $*" >&2; exit 1; }

errors=0

# =========================================================================
# Part 1: Protocol error codes (proto / Rust)
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
  echo "MISMATCH: proto vs Rust"
  echo "$diff_proto_rust"
  errors=1
fi

proto_count=$(echo "$proto_codes" | wc -l)

# =========================================================================
# Part 2: FFI error codes (Rust lib.rs / Go errors.go)
# =========================================================================

# Extract Rust FFI error codes: `const FFI_ERR_FOO: i32 = -N;` → "FOO = -N"
rust_ffi=$(grep -oP 'const FFI_ERR_\K[A-Z_]+:\s*i32\s*=\s*-?\d+' "$RUST_FFI_FILE" \
  | sed 's/:\s*i32\s*=\s*/ = /' | sort)
[[ -n "$rust_ffi" ]] || fail "No FFI error codes found in $RUST_FFI_FILE"

# Extract Go FFI error codes: `rcFooBar = -N` → map to UPPER_SNAKE = -N
# Go uses camelCase (rcNullPointer), Rust uses UPPER_SNAKE (NULL_PTR).
# Compare numeric values (naming conventions differ) AND counts so that a new
# Rust code without a matching Go entry is never silently missed.
rust_ffi_values=$(grep -oP 'const FFI_ERR_[A-Z_]+:\s*i32\s*=\s*\K-?\d+' "$RUST_FFI_FILE" | sort -n)
go_ffi_values=$(grep -oP 'rc[A-Z][a-zA-Z]+\s*=\s*\K-?\d+' "$GO_FFI_FILE" | sort -n)

rust_ffi_count=$(echo "$rust_ffi_values" | grep -c . || true)
go_ffi_count=$(echo "$go_ffi_values" | grep -c . || true)

diff_ffi=$(diff <(echo "$rust_ffi_values") <(echo "$go_ffi_values") || true)
if [[ -n "$diff_ffi" ]] || [[ "$rust_ffi_count" -ne "$go_ffi_count" ]]; then
  echo "MISMATCH: Rust FFI vs Go FFI error code values"
  if [[ "$rust_ffi_count" -ne "$go_ffi_count" ]]; then
    echo "  Count mismatch: Rust has $rust_ffi_count, Go has $go_ffi_count"
  fi
  [[ -n "$diff_ffi" ]] && echo "$diff_ffi"
  echo ""
  echo "  Rust ($RUST_FFI_FILE):"
  echo "$rust_ffi" | sed 's/^/    /'
  echo "  Go ($GO_FFI_FILE):"
  grep -oP 'rc[A-Z][a-zA-Z]+\s*=\s*-?\d+' "$GO_FFI_FILE" | sed 's/^/    /'
  errors=1
fi

ffi_count=$rust_ffi_count

# =========================================================================
# Summary
# =========================================================================

if [[ "$errors" -ne 0 ]]; then
  echo ""
  echo "Error codes are out of sync."
  echo "  proto:    $PROTO_FILE"
  echo "  Rust:     $RUST_FILE"
  echo "  Rust FFI: $RUST_FFI_FILE"
  echo "  Go FFI:   $GO_FFI_FILE"
  exit 1
fi

echo "error-code-sync: protocol codes in sync ($proto_count codes), FFI codes in sync ($ffi_count codes)"
