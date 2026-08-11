package briefcase

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
)

// CanonicalDigest returns the SHA-256 digest of the typed manifest encoded as
// compact JSON with HTML escaping disabled and ManifestDigest blank. Go's JSON
// encoder sorts map keys, and this schema has no floating-point fields, making
// the representation stable across source whitespace and object key order.
// Array order remains significant because episode and policy order is semantic.
func CanonicalDigest(manifest Manifest) (string, error) {
	manifest.ManifestDigest = ""

	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(manifest); err != nil {
		return "", fmt.Errorf("briefcase: encode canonical manifest: %w", err)
	}
	canonical := bytes.TrimSuffix(buf.Bytes(), []byte{'\n'})
	return DigestBytes(canonical), nil
}

func DigestBytes(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// FileDigest hashes a regular file without following a final symlink. Secure
// casepack reads additionally validate every parent component in loader.go.
func FileDigest(path string) (string, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("not a regular file: %s", path)
	}
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
