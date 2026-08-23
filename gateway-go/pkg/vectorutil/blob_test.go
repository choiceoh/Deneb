package vectorutil

import "testing"

func TestBlobRoundTrip(t *testing.T) {
	w := NewBlobWriter(3, 2)
	if idx := w.Add([]float32{1, 2, 3}); idx != 0 {
		t.Fatalf("first index = %d", idx)
	}
	if idx := w.Add([]float32{-1, 0.5, 0}); idx != 1 {
		t.Fatalf("second index = %d", idx)
	}
	if w.Len() != 2 || len(w.Bytes()) != 2*3*4 {
		t.Fatalf("len=%d bytes=%d", w.Len(), len(w.Bytes()))
	}

	r, err := NewBlobReader(w.Bytes(), 3)
	if err != nil {
		t.Fatalf("reader: %v", err)
	}
	if r.Count() != 2 {
		t.Fatalf("count = %d", r.Count())
	}
	got := r.At(1)
	if len(got) != 3 || got[0] != -1 || got[1] != 0.5 {
		t.Fatalf("vector 1 = %v", got)
	}
	if r.At(2) != nil || r.At(-1) != nil {
		t.Error("out-of-range index must return nil, not a mis-sliced vector")
	}
}

// A wrong-width vector is dropped instead of appended: writing it would shift
// every later vector's offset, so the manifest's indexes would silently point
// at the wrong embeddings.
func TestBlobWriterRejectsWrongWidth(t *testing.T) {
	w := NewBlobWriter(3, 1)
	if idx := w.Add([]float32{1, 2}); idx != -1 {
		t.Fatalf("short vector accepted at %d", idx)
	}
	if w.Len() != 0 {
		t.Fatalf("writer holds %d vectors", w.Len())
	}
}

// A blob that does not divide evenly is corrupt — reading it anyway would hand
// every item someone else's embedding.
func TestBlobReaderRejectsRaggedData(t *testing.T) {
	if _, err := NewBlobReader(make([]byte, 13), 3); err == nil {
		t.Fatal("ragged blob accepted")
	}
	if _, err := NewBlobReader(nil, 0); err == nil {
		t.Fatal("unknown dimensions accepted")
	}
}
