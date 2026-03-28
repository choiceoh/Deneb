#!/usr/bin/env bash
# error-code-sync-check.sh — DEPRECATED: use gen-proto-error-codes.sh --check instead.
#
# proto/gateway.proto is now the single source of truth for ErrorCode.
# error_codes.rs is generated from it via scripts/gen-proto-error-codes.sh.
# This script is kept as a thin wrapper for backward compatibility.
#
# Use: make proto-error-codes-gen-check   (check)
#      make proto-error-codes-gen         (regenerate)
exec "$(dirname "$0")/gen-proto-error-codes.sh" --check "$@"
