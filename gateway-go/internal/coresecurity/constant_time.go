// Package coresecurity provides pure-Go security primitives equivalent to
// the Rust core-rs/core/src/security module. These are used directly by
// the noffi fallback and can be tested without CGo.
package coresecurity

import "crypto/subtle"

// ConstantTimeEq performs constant-time byte comparison.
// Uses crypto/subtle.ConstantTimeCompare which handles length mismatches
// in constant time, preventing timing side-channel leaks.
func ConstantTimeEq(a, b []byte) bool {
	return subtle.ConstantTimeCompare(a, b) == 1
}
