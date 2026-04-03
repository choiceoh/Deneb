#!/usr/bin/env bash
# error-code-sync-check.sh — DEPRECATED: use gen-error-codes.sh --check instead.
#
# proto/gateway.proto is the single source of truth for all error codes.
# This script is kept as a thin wrapper for backward compatibility.
#
# Use: make error-codes-gen-check   (check)
#      make error-codes-gen         (regenerate)
exec "$(dirname "$0")/gen-error-codes.sh" --check "$@"
