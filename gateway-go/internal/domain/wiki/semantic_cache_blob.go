// semantic_cache_blob.go — the vector half of the semantic cache, stored as a
// flat float32 blob beside the JSON manifest.
//
// Vectors used to live inside the manifest as JSON number arrays. At 2048
// dimensions that is ~12.6 bytes of decimal text per float, so the live wiki's
// 6,887 vectors (1,165 page centroids + their structure chunks) filled a 181MB
// file which the gateway parsed synchronously on every boot — measured 1.39s of
// json.Unmarshal and ~335MB of transient heap, and rewritten in full whenever
// any page changed. The same numbers are 56MB and a memcpy as raw float32.
//
// Split rather than compressed: the manifest stays human-inspectable (paths,
// hashes, chunk snippets and line ranges) and only the numbers move out.
package wiki

import (
	"encoding/binary"
	"fmt"
	"math"
	"os"
)

// semanticBlobFile is the vector blob's name; it sits next to the manifest.
const semanticBlobFile = ".semantic-cache.f32"

// blobWriter lays vectors out end to end, handing back each one's index.
type blobWriter struct {
	buf  []byte
	dims int
	n    int
}

func newBlobWriter(dims, estimatedVectors int) *blobWriter {
	return &blobWriter{buf: make([]byte, 0, dims*estimatedVectors*4), dims: dims}
}

// add appends a vector and returns its index, or -1 when the vector does not
// match the cache's dimension (a mismatched vector is dropped rather than
// silently shifting every later offset).
func (w *blobWriter) add(vec []float32) int {
	if w.dims <= 0 || len(vec) != w.dims {
		return -1
	}
	idx := w.n
	var scratch [4]byte
	for _, f := range vec {
		binary.LittleEndian.PutUint32(scratch[:], math.Float32bits(f))
		w.buf = append(w.buf, scratch[:]...)
	}
	w.n++
	return idx
}

// blobReader slices a loaded blob back into vectors.
type blobReader struct {
	data []byte
	dims int
}

// newBlobReader validates that the blob divides evenly into dims-sized vectors.
// A blob that does not is treated as corrupt: silently mis-slicing it would
// hand every page someone else's embedding.
func newBlobReader(data []byte, dims int) (*blobReader, error) {
	if dims <= 0 {
		return nil, fmt.Errorf("semantic blob: dimensions unknown")
	}
	stride := dims * 4
	if len(data)%stride != 0 {
		return nil, fmt.Errorf("semantic blob: %d bytes is not a multiple of %d", len(data), stride)
	}
	return &blobReader{data: data, dims: dims}, nil
}

// count is how many vectors the blob holds.
func (r *blobReader) count() int { return len(r.data) / (r.dims * 4) }

// at returns the vector at idx, or nil when idx is out of range.
func (r *blobReader) at(idx int) []float32 {
	if idx < 0 || idx >= r.count() {
		return nil
	}
	out := make([]float32, r.dims)
	base := idx * r.dims * 4
	for i := range out {
		out[i] = math.Float32frombits(binary.LittleEndian.Uint32(r.data[base+i*4 : base+i*4+4]))
	}
	return out
}

// readSemanticBlob loads the blob beside a manifest path. A missing blob is not
// an error for callers that can fall back to re-embedding.
func readSemanticBlob(blobPath string, dims int) (*blobReader, error) {
	data, err := os.ReadFile(blobPath)
	if err != nil {
		return nil, err
	}
	return newBlobReader(data, dims)
}
