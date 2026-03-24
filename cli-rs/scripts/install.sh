#!/usr/bin/env bash
# Install the Rust CLI binary as 'deneb' with TS CLI fallback detection.
#
# Usage:
#   ./cli-rs/scripts/install.sh [--prefix /usr/local]
#
# This installs the binary as 'deneb' in PREFIX/bin.
# If the Node.js 'deneb' binary exists, it is renamed to 'deneb-ts'
# so the Rust binary takes precedence while preserving TS CLI access.

set -euo pipefail

PREFIX="${1:-/usr/local}"
BIN_DIR="$PREFIX/bin"
CARGO_BIN="$(dirname "$0")/../target/release/deneb-rs"

if [ ! -f "$CARGO_BIN" ]; then
    echo "Error: Release binary not found. Run 'make cli' first."
    exit 1
fi

mkdir -p "$BIN_DIR"

# Detect existing Node.js deneb binary
if [ -f "$BIN_DIR/deneb" ] && file "$BIN_DIR/deneb" | grep -q "text"; then
    echo "Found existing Node.js 'deneb' binary — renaming to 'deneb-ts'"
    mv "$BIN_DIR/deneb" "$BIN_DIR/deneb-ts"
fi

# Install the Rust binary as 'deneb'
cp "$CARGO_BIN" "$BIN_DIR/deneb"
chmod +x "$BIN_DIR/deneb"

# Also keep deneb-rs as an alias
cp "$CARGO_BIN" "$BIN_DIR/deneb-rs"
chmod +x "$BIN_DIR/deneb-rs"

echo "Installed deneb (Rust) to $BIN_DIR/deneb"
echo "  Binary size: $(du -h "$BIN_DIR/deneb" | cut -f1)"
echo "  Version: $("$BIN_DIR/deneb" --version 2>/dev/null || echo 'unknown')"
