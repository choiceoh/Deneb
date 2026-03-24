// Package ffi provides Go bindings to the Rust deneb-core library via CGo.
//
// The Rust library (core-rs/) is compiled to a C-compatible static library
// (libdeneb_core.a) and linked here via CGo.
//
// Build requirements:
//   - Rust toolchain with cargo
//   - Run `make rust` first to produce the static library
//   - CGO_ENABLED=1 (default) when building Go
//
// When the Rust library is not available (e.g. CI without Rust, pure-Go
// development), use the `no_ffi` build tag to compile with pure-Go
// fallbacks instead: `go build -tags no_ffi ./...`
package ffi
