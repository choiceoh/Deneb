// semantic_cache_blob.go — the wiki's binding to the shared float32 vector blob
// (pkg/vectorutil/blob.go), which holds the numbers the manifest used to inline.
//
// The format and its rationale live in vectorutil so every on-disk vector cache
// (wiki pages, diary entries, mailstore, workfeed) shares one implementation;
// this file only names the wiki's sidecar and keeps the package-local aliases
// the cache code reads.
package wiki

import "github.com/choiceoh/deneb/gateway-go/pkg/vectorutil"

// semanticBlobFile is the wiki's vector blob; it sits next to the manifest.
const semanticBlobFile = ".semantic-cache.f32"

type (
	blobWriter = vectorutil.BlobWriter
	blobReader = vectorutil.BlobReader
)

func newBlobWriter(dims, estimatedVectors int) *blobWriter {
	return vectorutil.NewBlobWriter(dims, estimatedVectors)
}

func newBlobReader(data []byte, dims int) (*blobReader, error) {
	return vectorutil.NewBlobReader(data, dims)
}

// readSemanticBlob loads the blob beside a manifest path. A missing blob is not
// an error for callers that can fall back to re-embedding.
func readSemanticBlob(blobPath string, dims int) (*blobReader, error) {
	return vectorutil.ReadBlob(blobPath, dims)
}
