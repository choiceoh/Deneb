#!/usr/bin/env bash
# gen-error-codes.sh — Generate Rust + Go error code files from proto/gateway.proto.
#
# proto/gateway.proto is the single source of truth for BOTH:
#   - ErrorCode      (protocol-level, positive values)
#   - FfiErrorCode   (C ABI transport, positive in proto, negated in output)
#
# Outputs:
#   1. core-rs/core/src/protocol/error_codes.rs   (Rust ErrorCode enum + FFI_ERR_* constants)
#   2. gateway-go/pkg/protocol/errors_gen.go       (Go Err* string constants)
#   3. gateway-go/internal/ffi/ffi_error_codes_gen.go (Go rc* int constants)
#
# Usage:
#   ./scripts/gen-error-codes.sh           # write all output files
#   ./scripts/gen-error-codes.sh --check   # exit 1 if any output is stale
set -euo pipefail
cd "$(git rev-parse --show-toplevel)"

PROTO_FILE="proto/gateway.proto"
RUST_OUT="core-rs/core/src/protocol/error_codes.rs"
GO_PROTO_OUT="gateway-go/pkg/protocol/errors_gen.go"
GO_FFI_OUT="gateway-go/internal/ffi/ffi_error_codes_gen.go"
CHECK=0
[[ "${1:-}" == "--check" ]] && CHECK=1

# ---------------------------------------------------------------------------
# Name conversions
# ---------------------------------------------------------------------------
to_pascal() {
  local name="$1"
  local result=""
  IFS='_' read -ra parts <<< "$name"
  for part in "${parts[@]}"; do
    local lo="${part,,}"
    result+="${lo^}"
  done
  echo "$result"
}

# SCREAMING_SNAKE suffix → Go camelCase with Err prefix.
# e.g. NOT_FOUND → ErrNotFound, AGENT_TIMEOUT → ErrAgentTimeout
to_go_err() {
  local name="$1"
  local result=""
  IFS='_' read -ra parts <<< "$name"
  for part in "${parts[@]}"; do
    local lo="${part,,}"
    result+="${lo^}"
  done
  echo "Err${result}"
}

# SCREAMING_SNAKE suffix → Go camelCase with rc prefix.
# Known acronyms kept uppercase.
# e.g. NULL_POINTER → rcNullPointer, JSON_ERROR → rcJSONError
to_go_rc() {
  local suffix="$1"
  local result=""
  IFS='_' read -ra parts <<< "$suffix"
  for part in "${parts[@]}"; do
    case "$part" in
      UTF8|UTF|JSON|HTML|URL|HTTP|API|ID|UUID)
        result+="$part" ;;
      *)
        local lo="${part,,}"
        result+="${lo^}" ;;
    esac
  done
  echo "rc${result}"
}

# ---------------------------------------------------------------------------
# Parse ErrorCode enum from proto file.
# ---------------------------------------------------------------------------
declare -a EC_NAMES=()
declare -a EC_VALUES=()
declare -a EC_RETRYABLE=()
declare -a EC_SECTION=()

parse_error_code() {
  local in_enum=0
  local pending_section=""

  while IFS= read -r line; do
    if [[ "$line" =~ ^enum[[:space:]]+ErrorCode[[:space:]]*\{ ]]; then
      in_enum=1; continue
    fi
    if [[ "$in_enum" -eq 1 && "$line" =~ ^\} ]]; then break; fi
    [[ "$in_enum" -eq 0 ]] && continue

    # Section comment
    if [[ "$line" =~ ^[[:space:]]*//(.*) ]]; then
      local comment="${BASH_REMATCH[1]# }"
      if [[ "$comment" != *retryable* ]] && [[ -n "$comment" ]]; then
        pending_section="$comment"
      fi
      continue
    fi

    local re='ERROR_CODE_([A-Z_]+)[[:space:]]*=[[:space:]]*([0-9]+)[[:space:]]*;(.*)'
    if [[ "$line" =~ $re ]]; then
      local name="${BASH_REMATCH[1]}"
      local value="${BASH_REMATCH[2]}"
      local rest="${BASH_REMATCH[3]}"
      [[ "$name" == "UNSPECIFIED" ]] && { pending_section=""; continue; }

      local retryable=0
      [[ "$rest" == *retryable* ]] && retryable=1

      EC_NAMES+=("$name")
      EC_VALUES+=("$value")
      EC_RETRYABLE+=("$retryable")
      EC_SECTION+=("$pending_section")
      pending_section=""
    fi
  done < "$PROTO_FILE"
}

# ---------------------------------------------------------------------------
# Parse FfiErrorCode enum from proto file.
# ---------------------------------------------------------------------------
declare -a FFI_NAMES=()
declare -a FFI_VALUES=()

parse_ffi_error_code() {
  local in_enum=0

  while IFS= read -r line; do
    if [[ "$line" =~ ^enum[[:space:]]+FfiErrorCode[[:space:]]*\{ ]]; then
      in_enum=1; continue
    fi
    if [[ "$in_enum" -eq 1 && "$line" =~ ^\} ]]; then break; fi
    [[ "$in_enum" -eq 0 ]] && continue

    local re='FFI_ERROR_CODE_([A-Z_0-9]+)[[:space:]]*=[[:space:]]*([0-9]+)[[:space:]]*;'
    if [[ "$line" =~ $re ]]; then
      local name="${BASH_REMATCH[1]}"
      local value="${BASH_REMATCH[2]}"
      [[ "$name" == "UNSPECIFIED" ]] && continue
      FFI_NAMES+=("$name")
      FFI_VALUES+=("$value")
    fi
  done < "$PROTO_FILE"
}

# ---------------------------------------------------------------------------
# Parse both enums
# ---------------------------------------------------------------------------
parse_error_code
parse_ffi_error_code

[[ ${#EC_NAMES[@]} -gt 0 ]] || { echo "ERROR: no ErrorCode variants found in $PROTO_FILE" >&2; exit 1; }
[[ ${#FFI_NAMES[@]} -gt 0 ]] || { echo "ERROR: no FfiErrorCode variants found in $PROTO_FILE" >&2; exit 1; }

# ===========================================================================
# Output 1: Rust error_codes.rs
# ===========================================================================
generate_rust() {
  printf '// Code generated by scripts/gen-error-codes.sh; DO NOT EDIT.\n'
  printf '// Source of truth: proto/gateway.proto\n'
  printf '// Regenerate with: make error-codes-gen\n'
  printf '\n'
  printf '//! Gateway error codes and FFI return codes — auto-generated from `proto/gateway.proto`.\n'
  printf '\n'

  # --- ErrorCode enum ---
  printf '/// Gateway RPC error codes.\n'
  printf '///\n'
  printf '/// Each variant maps to a stable string code used on the wire.\n'
  printf '/// The i32 discriminant provides a compact representation for FFI.\n'
  printf '#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash)]\n'
  printf '#[repr(i32)]\n'
  printf 'pub enum ErrorCode {\n'

  local prev_section=""
  local i
  for i in "${!EC_NAMES[@]}"; do
    local section="${EC_SECTION[$i]}"
    local pascal
    pascal=$(to_pascal "${EC_NAMES[$i]}")
    if [[ -n "$section" && "$section" != "$prev_section" ]]; then
      printf '    // %s\n' "$section"
      prev_section="$section"
    fi
    printf '    %s = %s,\n' "$pascal" "${EC_VALUES[$i]}"
  done

  printf '}\n'
  printf '\n'

  # --- ErrorCode impl ---
  printf 'impl ErrorCode {\n'
  printf "    /// Wire-format string code used on the wire.\n"
  printf "    pub fn as_str(&self) -> &'static str {\n"
  printf '        match self {\n'
  for i in "${!EC_NAMES[@]}"; do
    local pascal
    pascal=$(to_pascal "${EC_NAMES[$i]}")
    printf '            Self::%s => "%s",\n' "$pascal" "${EC_NAMES[$i]}"
  done
  printf '        }\n'
  printf '    }\n'
  printf '\n'

  printf '    /// Parse a wire-format string into an `ErrorCode`.\n'
  printf '    pub fn parse(s: &str) -> Option<Self> {\n'
  printf '        match s {\n'
  for i in "${!EC_NAMES[@]}"; do
    local pascal
    pascal=$(to_pascal "${EC_NAMES[$i]}")
    printf '            "%s" => Some(Self::%s),\n' "${EC_NAMES[$i]}" "$pascal"
  done
  printf '            _ => None,\n'
  printf '        }\n'
  printf '    }\n'
  printf '\n'

  printf '    /// Whether this code is retryable by default.\n'
  printf '    pub fn is_retryable(&self) -> bool {\n'
  local -a retryable_variants=()
  for i in "${!EC_NAMES[@]}"; do
    if [[ "${EC_RETRYABLE[$i]}" == "1" ]]; then
      retryable_variants+=("$(to_pascal "${EC_NAMES[$i]}")")
    fi
  done
  if [[ ${#retryable_variants[@]} -eq 0 ]]; then
    printf '        false\n'
  else
    printf '        matches!(\n'
    printf '            self,\n'
    local j
    for j in "${!retryable_variants[@]}"; do
      if [[ "$j" -eq 0 ]]; then
        printf '            Self::%s' "${retryable_variants[$j]}"
      else
        printf '\n                | Self::%s' "${retryable_variants[$j]}"
      fi
    done
    printf '\n'
    printf '        )\n'
  fi
  printf '    }\n'
  printf '}\n'
  printf '\n'

  # --- Display ---
  printf 'impl std::fmt::Display for ErrorCode {\n'
  printf "    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {\n"
  printf '        f.write_str(self.as_str())\n'
  printf '    }\n'
  printf '}\n'
  printf '\n'

  # --- ALL_ERROR_CODES ---
  printf '/// All known error codes in declaration order.\n'
  printf 'pub const ALL_ERROR_CODES: &[ErrorCode] = &[\n'
  for i in "${!EC_NAMES[@]}"; do
    local pascal
    pascal=$(to_pascal "${EC_NAMES[$i]}")
    printf '    ErrorCode::%s,\n' "$pascal"
  done
  printf '];\n'
  printf '\n'

  # --- is_valid_error_code ---
  printf '/// Validate that an error code string is a known code.\n'
  printf 'pub fn is_valid_error_code(code: &str) -> bool {\n'
  printf '    ErrorCode::parse(code).is_some()\n'
  printf '}\n'
  printf '\n'

  # --- FFI error code constants ---
  printf '// ---------------------------------------------------------------------------\n'
  printf '// FFI error codes — negated from proto FfiErrorCode enum values.\n'
  printf '// Negative i32 values returned by extern "C" functions.\n'
  printf '// Positive return values from buffer-writing functions are bytes written.\n'
  printf '// ---------------------------------------------------------------------------\n'
  printf '\n'

  for i in "${!FFI_NAMES[@]}"; do
    printf 'pub(crate) const FFI_ERR_%s: i32 = -%s;\n' "${FFI_NAMES[$i]}" "${FFI_VALUES[$i]}"
  done

  printf '\n'

  # --- Tests ---
  printf '#[cfg(test)]\n'
  printf '#[allow(clippy::expect_used)]\n'
  printf 'mod tests {\n'
  printf '    use super::*;\n'
  printf '    use crate::protocol::gen;\n'
  printf '\n'

  printf '    #[test]\n'
  printf '    fn test_roundtrip() {\n'
  printf '        for code in ALL_ERROR_CODES {\n'
  printf '            let s = code.as_str();\n'
  printf '            let parsed = ErrorCode::parse(s).expect("known code should round-trip through parse");\n'
  printf '            assert_eq!(*code, parsed);\n'
  printf '        }\n'
  printf '    }\n'
  printf '\n'

  printf '    #[test]\n'
  printf '    fn test_unknown_code() {\n'
  printf '        assert!(ErrorCode::parse("UNKNOWN_CODE").is_none());\n'
  printf '        assert!(!is_valid_error_code("BOGUS"));\n'
  printf '    }\n'
  printf '\n'

  printf '    #[test]\n'
  printf '    fn test_retryable() {\n'
  for i in "${!EC_NAMES[@]}"; do
    local pascal
    pascal=$(to_pascal "${EC_NAMES[$i]}")
    if [[ "${EC_RETRYABLE[$i]}" == "1" ]]; then
      printf '        assert!(ErrorCode::%s.is_retryable());\n' "$pascal"
    else
      printf '        assert!(!ErrorCode::%s.is_retryable());\n' "$pascal"
    fi
  done
  printf '    }\n'
  printf '\n'

  printf '    #[test]\n'
  printf '    fn test_discriminants_unique() {\n'
  printf '        let mut seen = std::collections::HashSet::new();\n'
  printf '        for code in ALL_ERROR_CODES {\n'
  printf '            assert!(\n'
  printf '                seen.insert(*code as i32),\n'
  printf '                "duplicate discriminant for {code}"\n'
  printf '            );\n'
  printf '        }\n'
  printf '    }\n'
  printf '\n'

  printf '    #[test]\n'
  printf '    fn test_display() {\n'
  printf '        assert_eq!(format!("{}", ErrorCode::NotFound), "NOT_FOUND");\n'
  printf '    }\n'
  printf '\n'

  printf '    #[test]\n'
  printf '    fn test_generated_proto_error_code_mapping_consistency() {\n'
  printf '        let mapping = [\n'
  for i in "${!EC_NAMES[@]}"; do
    local pascal
    pascal=$(to_pascal "${EC_NAMES[$i]}")
    printf '            (ErrorCode::%s, gen::gateway::ErrorCode::%s),\n' "$pascal" "$pascal"
  done
  printf '        ];\n'
  printf '\n'
  printf '        for (local, proto) in mapping {\n'
  printf '            assert_eq!(local as i32, proto as i32, "numeric mapping mismatch");\n'
  printf '            let local_name = format!("ERROR_CODE_{}", local.as_str());\n'
  printf '            assert_eq!(local_name, proto.as_str_name(), "string mapping mismatch");\n'
  printf '        }\n'
  printf '    }\n'
  printf '\n'

  printf '    #[test]\n'
  printf '    fn test_ffi_error_code_proto_consistency() {\n'
  printf '        // Verify FFI_ERR_* constants match negated FfiErrorCode proto values.\n'
  for i in "${!FFI_NAMES[@]}"; do
    local pascal
    pascal=$(to_pascal "${FFI_NAMES[$i]}")
    printf '        assert_eq!(\n'
    printf '            FFI_ERR_%s,\n' "${FFI_NAMES[$i]}"
    printf '            -(gen::gateway::FfiErrorCode::%s as i32),\n' "$pascal"
    printf '            "FFI_ERR_%s must be negated proto value"\n' "${FFI_NAMES[$i]}"
    printf '        );\n'
  done
  printf '    }\n'

  printf '}\n'
}

# ===========================================================================
# Output 2: Go errors_gen.go (protocol error string constants)
# ===========================================================================
generate_go_proto() {
  printf '// Code generated by scripts/gen-error-codes.sh; DO NOT EDIT.\n'
  printf '// Source of truth: proto/gateway.proto\n'
  printf '// Regenerate with: make error-codes-gen\n'
  printf '\n'
  printf 'package protocol\n'
  printf '\n'
  printf '// Error code constants — protocol-level error codes from ErrorCode enum.\n'
  printf 'const (\n'
  for i in "${!EC_NAMES[@]}"; do
    local go_name
    go_name=$(to_go_err "${EC_NAMES[$i]}")
    printf '\t%s = "%s"\n' "$go_name" "${EC_NAMES[$i]}"
  done
  printf ')\n'
}

# ===========================================================================
# Output 3: Go ffi_error_codes_gen.go (FFI return code int constants)
# ===========================================================================
generate_go_ffi() {
  printf '// Code generated by scripts/gen-error-codes.sh; DO NOT EDIT.\n'
  printf '// Source of truth: proto/gateway.proto\n'
  printf '// Regenerate with: make error-codes-gen\n'
  printf '\n'
  printf 'package ffi\n'
  printf '\n'
  printf '// FFI return codes — negative i32 values returned by core-rs C ABI functions.\n'
  printf '// Positive return values from buffer-writing functions are bytes written, not error codes.\n'
  printf 'const (\n'
  for i in "${!FFI_NAMES[@]}"; do
    local go_const
    go_const=$(to_go_rc "${FFI_NAMES[$i]}")
    printf '\t%s = -%s\n' "$go_const" "${FFI_VALUES[$i]}"
  done
  printf ')\n'
}

# ===========================================================================
# Check or write
# ===========================================================================
if [[ "$CHECK" -eq 1 ]]; then
  tmp_rust=$(mktemp --suffix=.rs)
  tmp_go_proto=$(mktemp --suffix=.go)
  tmp_go_ffi=$(mktemp --suffix=.go)
  trap "rm -f '$tmp_rust' '$tmp_go_proto' '$tmp_go_ffi'" EXIT

  generate_rust | rustfmt --edition 2021 > "$tmp_rust"
  generate_go_proto | gofmt > "$tmp_go_proto"
  generate_go_ffi | gofmt > "$tmp_go_ffi"

  failed=0
  for pair in "$tmp_rust:$RUST_OUT" "$tmp_go_proto:$GO_PROTO_OUT" "$tmp_go_ffi:$GO_FFI_OUT"; do
    tmp="${pair%%:*}"
    out="${pair##*:}"
    if ! diff -q "$tmp" "$out" > /dev/null 2>&1; then
      echo "FAIL: $out is out of sync with $PROTO_FILE" >&2
      diff "$tmp" "$out" >&2 || true
      failed=1
    fi
  done

  if [[ "$failed" -eq 1 ]]; then
    echo "Run 'make error-codes-gen' to regenerate." >&2
    exit 1
  fi
  echo "error-codes-gen-check: OK (all outputs up to date)"
else
  generate_rust | rustfmt --edition 2021 > "$RUST_OUT"
  generate_go_proto | gofmt > "$GO_PROTO_OUT"
  generate_go_ffi | gofmt > "$GO_FFI_OUT"
  echo "error-codes-gen: wrote $RUST_OUT"
  echo "error-codes-gen: wrote $GO_PROTO_OUT"
  echo "error-codes-gen: wrote $GO_FFI_OUT"
fi
