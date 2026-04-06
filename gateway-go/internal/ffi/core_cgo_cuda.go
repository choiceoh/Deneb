//go:build !no_ffi && cgo && cuda

package ffi

// CUDA driver + runtime libraries required when core-rs is built with
// the "cuda" feature (make rust-dgx).  The two -L paths cover both
// x86_64 (/usr/local/cuda/lib64) and sbsa/aarch64 DGX Spark layouts.
// CGO_LDFLAGS can still override for non-standard CUDA installations.

/*
#cgo LDFLAGS: -L/usr/local/cuda/lib64 -L/usr/local/cuda/targets/sbsa-linux/lib -lcuda -lcudart -lcublas -lcublasLt
*/
import "C"
