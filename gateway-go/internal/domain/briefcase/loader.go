package briefcase

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
)

const (
	maxManifestBytes = 4 << 20
	maxAssetBytes    = 64 << 20
	maxCasepackBytes = 512 << 20
)

// LoadDir opens and validates an immutable directory casepack. It rejects
// symlinks and special files anywhere in the tree, including unreferenced
// locations, before decoding the manifest.
func LoadDir(root string) (*Pack, error) {
	root = strings.TrimSpace(root)
	if root == "" {
		return nil, fmt.Errorf("briefcase: root is required")
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("briefcase: resolve root: %w", err)
	}
	root = filepath.Clean(abs)
	info, err := os.Lstat(root)
	if err != nil {
		return nil, fmt.Errorf("briefcase: inspect root: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return nil, fmt.Errorf("briefcase: root must be a real directory, not a symlink or special file")
	}

	files, err := scanTree(root)
	if err != nil {
		return nil, err
	}
	if _, ok := files[ManifestFile]; !ok {
		return nil, fmt.Errorf("briefcase: %s is required", ManifestFile)
	}

	data, err := readSecureFile(root, ManifestFile, maxManifestBytes)
	if err != nil {
		return nil, fmt.Errorf("briefcase: read manifest: %w", err)
	}
	if err := RejectDuplicateJSONKeys(data); err != nil {
		return nil, fmt.Errorf("briefcase: decode manifest: %w", err)
	}
	var manifest Manifest
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&manifest); err != nil {
		return nil, fmt.Errorf("briefcase: decode manifest: %w", err)
	}
	if err := ensureJSONEOF(dec); err != nil {
		return nil, fmt.Errorf("briefcase: decode manifest: %w", err)
	}

	pack := &Pack{Root: root, Manifest: manifest, files: files}
	if err := Validate(pack); err != nil {
		return nil, err
	}
	pack.Digest = manifest.ManifestDigest
	pack.hashes = declaredHashes(manifest)
	return pack, nil
}

// ReadFile reads a validated pack member while rechecking every path component
// against symlink replacement. Only files declared by the manifest are exposed.
func (p *Pack) ReadFile(relative string) ([]byte, error) {
	if p == nil {
		return nil, fmt.Errorf("briefcase: nil pack")
	}
	expected, ok := p.hashes[relative]
	if !ok {
		return nil, fmt.Errorf("briefcase: file is not a declared case asset: %q", relative)
	}
	data, err := readSecureFile(p.Root, relative, maxAssetBytes)
	if err != nil {
		return nil, err
	}
	if actual := DigestBytes(data); actual != expected {
		return nil, fmt.Errorf("briefcase: asset changed after validation: %q: got %s, want %s", relative, actual, expected)
	}
	return data, nil
}

func declaredHashes(manifest Manifest) map[string]string {
	hashes := make(map[string]string, len(manifest.Sources)+len(manifest.Episodes))
	for _, source := range manifest.Sources {
		hashes[source.Path] = source.SHA256
	}
	for _, episode := range manifest.Episodes {
		if episode.Input != nil {
			hashes[episode.Input.Path] = episode.Input.SHA256
		}
	}
	return hashes
}

func scanTree(root string) (map[string]struct{}, error) {
	files := make(map[string]struct{})
	var totalBytes int64
	err := filepath.WalkDir(root, func(current string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if current == root {
			return nil
		}
		rel, err := filepath.Rel(root, current)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if err := validateRelativePath(rel); err != nil {
			return err
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("briefcase: symlink is forbidden: %q", rel)
		}
		if info.IsDir() {
			return nil
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("briefcase: special file is forbidden: %q", rel)
		}
		if info.Size() > maxAssetBytes {
			return fmt.Errorf("briefcase: asset exceeds %d bytes: %q", maxAssetBytes, rel)
		}
		totalBytes += info.Size()
		if totalBytes > maxCasepackBytes {
			return fmt.Errorf("briefcase: casepack exceeds %d total bytes", maxCasepackBytes)
		}
		files[rel] = struct{}{}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("briefcase: scan pack: %w", err)
	}
	return files, nil
}

func readSecureFile(root, relative string, maxBytes int64) ([]byte, error) {
	full, err := secureJoin(root, relative, true)
	if err != nil {
		return nil, err
	}
	f, err := os.Open(full)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var reader io.Reader = f
	if maxBytes > 0 {
		reader = io.LimitReader(f, maxBytes+1)
	}
	data, err := io.ReadAll(reader)
	if err != nil {
		return nil, err
	}
	if maxBytes > 0 && int64(len(data)) > maxBytes {
		return nil, fmt.Errorf("file exceeds %d bytes: %q", maxBytes, relative)
	}
	return data, nil
}

func secureJoin(root, relative string, mustExist bool) (string, error) {
	if err := validateRelativePath(relative); err != nil {
		return "", err
	}
	full := filepath.Join(root, filepath.FromSlash(relative))
	rel, err := filepath.Rel(root, full)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("briefcase: path escapes root: %q", relative)
	}

	current := root
	parts := strings.Split(relative, "/")
	for i, part := range parts {
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if err != nil {
			if !mustExist && errors.Is(err, os.ErrNotExist) {
				return full, nil
			}
			return "", err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return "", fmt.Errorf("briefcase: symlink is forbidden: %q", strings.Join(parts[:i+1], "/"))
		}
		if i < len(parts)-1 && !info.IsDir() {
			return "", fmt.Errorf("briefcase: path component is not a directory: %q", strings.Join(parts[:i+1], "/"))
		}
		if i == len(parts)-1 && mustExist && !info.Mode().IsRegular() {
			return "", fmt.Errorf("briefcase: asset is not a regular file: %q", relative)
		}
	}
	return full, nil
}

func validateRelativePath(relative string) error {
	if relative == "" || strings.TrimSpace(relative) != relative {
		return fmt.Errorf("briefcase: path must be a non-empty canonical relative path: %q", relative)
	}
	if strings.Contains(relative, "\\") || strings.ContainsRune(relative, '\x00') {
		return fmt.Errorf("briefcase: path contains a forbidden character: %q", relative)
	}
	if strings.HasPrefix(relative, "/") || filepath.IsAbs(relative) || filepath.VolumeName(relative) != "" {
		return fmt.Errorf("briefcase: absolute path is forbidden: %q", relative)
	}
	if path.Clean(relative) != relative || relative == "." || strings.HasPrefix(relative, "../") {
		return fmt.Errorf("briefcase: non-canonical or traversing path is forbidden: %q", relative)
	}
	return nil
}

func ensureJSONEOF(dec *json.Decoder) error {
	var extra any
	if err := dec.Decode(&extra); errors.Is(err, io.EOF) {
		return nil
	} else if err != nil {
		return err
	}
	return fmt.Errorf("multiple JSON values are forbidden")
}
