package tools

import (
	"os/exec"
	"strings"
	"sync"
)

// binaryCache stores resolved binary paths so exec.LookPath is called at most
// once per candidate key per process lifetime. Binaries like rg, fd, eza don't
// appear or disappear within a gateway session.
var binaryCache sync.Map // key: comma-joined candidates → value: binaryCacheEntry

type binaryCacheEntry struct {
	path string
	ok   bool
}

func firstAvailableBinary(candidates ...string) (string, bool) {
	key := strings.Join(candidates, ",")
	if v, ok := binaryCache.Load(key); ok {
		e := v.(binaryCacheEntry)
		return e.path, e.ok
	}
	for _, candidate := range candidates {
		if candidate == "" {
			continue
		}
		if path, err := exec.LookPath(candidate); err == nil {
			binaryCache.Store(key, binaryCacheEntry{path: path, ok: true})
			return path, true
		}
	}
	binaryCache.Store(key, binaryCacheEntry{ok: false})
	return "", false
}

func nonEmptyCommandLines(raw string) []string {
	lines := strings.Split(raw, "\n")
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		line = strings.TrimPrefix(line, "./")
		line = strings.TrimPrefix(line, ".\\")
		if line == "" {
			continue
		}
		out = append(out, line)
	}
	return out
}
