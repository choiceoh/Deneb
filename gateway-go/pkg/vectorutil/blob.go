// blob.go — the float32 sidecar format shared by every on-disk vector cache.
//
// Vectors used to live inside their cache's JSON as number arrays. At 2048
// dimensions that is ~12.6 bytes of decimal text per float, so the live wiki's
// 6,887 vectors filled a 181MB file the gateway parsed synchronously on every
// boot (measured 1.39s of json.Unmarshal, ~335MB transient heap) and rewrote in
// full whenever any item changed. The same numbers are ~56MB and a memcpy as
// raw little-endian float32.
//
// Split rather than compressed: the manifest stays human-inspectable (ids,
// hashes, chunk metadata) and only the numbers move out.
package vectorutil

import (
	"encoding/binary"
	"fmt"
	"math"
	"os"
)

// BlobWriter lays vectors out end to end, handing back each one's index.
type BlobWriter struct {
	buf  []byte
	dims int
	n    int
}

// NewBlobWriter starts a blob of dims-sized vectors, preallocated for the
// expected count.
func NewBlobWriter(dims, estimatedVectors int) *BlobWriter {
	return &BlobWriter{buf: make([]byte, 0, dims*estimatedVectors*4), dims: dims}
}

// Add appends a vector and returns its index, or -1 when the vector does not
// match the blob's dimension (a mismatched vector is dropped rather than
// silently shifting every later offset).
func (w *BlobWriter) Add(vec []float32) int {
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

// Bytes is the blob to write to disk.
func (w *BlobWriter) Bytes() []byte { return w.buf }

// Len is how many vectors have been added.
func (w *BlobWriter) Len() int { return w.n }

// BlobReader slices a loaded blob back into vectors.
type BlobReader struct {
	data []byte
	dims int
}

// NewBlobReader validates that the blob divides evenly into dims-sized vectors.
// A blob that does not is treated as corrupt: silently mis-slicing it would hand
// every item someone else's embedding.
func NewBlobReader(data []byte, dims int) (*BlobReader, error) {
	if dims <= 0 {
		return nil, fmt.Errorf("vector blob: dimensions unknown")
	}
	stride := dims * 4
	if len(data)%stride != 0 {
		return nil, fmt.Errorf("vector blob: %d bytes is not a multiple of %d", len(data), stride)
	}
	return &BlobReader{data: data, dims: dims}, nil
}

// Count is how many vectors the blob holds.
func (r *BlobReader) Count() int { return len(r.data) / (r.dims * 4) }

// At returns the vector at idx, or nil when idx is out of range.
func (r *BlobReader) At(idx int) []float32 {
	if idx < 0 || idx >= r.Count() {
		return nil
	}
	out := make([]float32, r.dims)
	base := idx * r.dims * 4
	for i := range out {
		out[i] = math.Float32frombits(binary.LittleEndian.Uint32(r.data[base+i*4 : base+i*4+4]))
	}
	return out
}

// ReadBlob loads a blob file. A missing blob is not an error for callers that
// can fall back to re-embedding — they see the os error and rebuild.
func ReadBlob(path string, dims int) (*BlobReader, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return NewBlobReader(data, dims)
}
