//go:build !no_ffi && cgo

package ffi

/*
// Memory search FFI functions (from core-rs/core/src/lib.rs).
extern double deneb_memory_cosine_similarity(
	const double *a_ptr, unsigned long a_len,
	const double *b_ptr, unsigned long b_len);
extern double deneb_memory_bm25_rank_to_score(double rank);
extern int deneb_memory_build_fts_query(
	const unsigned char *raw_ptr, unsigned long raw_len,
	unsigned char *out_ptr, unsigned long out_len);
extern int deneb_memory_merge_hybrid_results(
	const unsigned char *params_ptr, unsigned long params_len,
	unsigned char *out_ptr, unsigned long out_len);
extern int deneb_memory_extract_keywords(
	const unsigned char *query_ptr, unsigned long query_len,
	unsigned char *out_ptr, unsigned long out_len);
*/
import "C"
import (
	"context"
	"encoding/json"
	"fmt"
	"unsafe"
)

// MemoryCosineSimilarity computes cosine similarity between two float64 vectors
// using SIMD-accelerated Rust implementation. Returns value in [-1.0, 1.0].
func MemoryCosineSimilarity(a, b []float64) float64 {
	if len(a) == 0 || len(b) == 0 {
		return 0.0
	}
	aPtr := (*C.double)(unsafe.Pointer(&a[0]))
	bPtr := (*C.double)(unsafe.Pointer(&b[0]))
	return float64(C.deneb_memory_cosine_similarity(
		aPtr, C.ulong(len(a)),
		bPtr, C.ulong(len(b)),
	))
}

// MemoryBm25RankToScore converts a BM25 rank position to a normalized score.
func MemoryBm25RankToScore(rank float64) float64 {
	return float64(C.deneb_memory_bm25_rank_to_score(C.double(rank)))
}

// MemoryBuildFtsQuery builds a full-text search query from raw text.
// Returns empty string if no valid tokens are found.
// The output buffer grows automatically if the Rust side signals it is too small.
func MemoryBuildFtsQuery(raw string) (string, error) {
	if len(raw) == 0 {
		return "", nil
	}

	initialSize := initialBufSize(len(raw), 3, 4096)

	rawPtr := (*C.uchar)(unsafe.Pointer(unsafe.StringData(raw)))
	data, err := ffiCallWithGrow("memory_build_fts_query", initialSize,
		func(outPtr unsafe.Pointer, outLen int) int {
			return int(C.deneb_memory_build_fts_query(
				rawPtr, C.ulong(len(raw)),
				(*C.uchar)(outPtr), C.ulong(outLen),
			))
		})
	if err != nil {
		return "", err
	}
	if len(data) == 0 {
		return "", nil
	}
	return string(data), nil
}

// maxMergeParamsSize is the maximum accepted paramsJSON size for
// MemoryMergeHybridResults. Inputs above this threshold are rejected early to
// avoid large buffer allocations before the FFI call.
const maxMergeParamsSize = 2 * 1024 * 1024 // 2 MB

// MemoryMergeHybridResultsCtx merges vector and FTS search results using the
// Rust hybrid merge pipeline, respecting context cancellation.
func MemoryMergeHybridResultsCtx(ctx context.Context, paramsJSON string) (json.RawMessage, error) {
	if len(paramsJSON) == 0 {
		return nil, fmt.Errorf("ffi: memory_merge: empty params")
	}
	if len(paramsJSON) > maxMergeParamsSize {
		return nil, fmt.Errorf("ffi: memory_merge_hybrid_results: input too large (%d bytes, max %d)", len(paramsJSON), maxMergeParamsSize)
	}

	// Pre-estimate output size: merged results are typically 3-5x input size
	// due to JSON structure expansion. Use 4x with 16 KB floor to minimize
	// grow-and-retry FFI round trips.
	initialSize := initialBufSize(len(paramsJSON), 4, 16384)

	paramsPtr := (*C.uchar)(unsafe.Pointer(unsafe.StringData(paramsJSON)))
	data, err := ffiCallWithGrowCtx(ctx, "memory_merge_hybrid_results", initialSize,
		func(outPtr unsafe.Pointer, outLen int) int {
			return int(C.deneb_memory_merge_hybrid_results(
				paramsPtr, C.ulong(len(paramsJSON)),
				(*C.uchar)(outPtr), C.ulong(outLen),
			))
		})
	if err != nil {
		return nil, err
	}
	return json.RawMessage(data), nil
}

// MemoryMergeHybridResults merges vector and FTS search results using the Rust
// hybrid merge pipeline. Takes JSON-encoded MergeParams, returns JSON results.
func MemoryMergeHybridResults(paramsJSON string) (json.RawMessage, error) {
	return MemoryMergeHybridResultsCtx(context.Background(), paramsJSON)
}

// MemoryExtractKeywords extracts searchable keywords from a query string
// for full-text search expansion.
// The output buffer grows automatically if the Rust side signals it is too small.
func MemoryExtractKeywords(query string) ([]string, error) {
	if len(query) == 0 {
		return nil, nil
	}

	initialSize := initialBufSize(len(query), 4, 4096)

	queryPtr := (*C.uchar)(unsafe.Pointer(unsafe.StringData(query)))
	data, err := ffiCallWithGrow("memory_extract_keywords", initialSize,
		func(outPtr unsafe.Pointer, outLen int) int {
			return int(C.deneb_memory_extract_keywords(
				queryPtr, C.ulong(len(query)),
				(*C.uchar)(outPtr), C.ulong(outLen),
			))
		})
	if err != nil {
		return nil, err
	}

	var keywords []string
	if err := json.Unmarshal(data, &keywords); err != nil {
		return nil, fmt.Errorf("ffi: memory_extract_keywords: invalid JSON: %w", err)
	}
	return keywords, nil
}
