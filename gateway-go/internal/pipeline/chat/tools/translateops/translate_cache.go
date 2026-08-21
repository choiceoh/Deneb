package translateops

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"hash"
	"sync"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/core/corecache"
)

const (
	// Browser chrome / repeated article phrases benefit from a large hot set.
	// ~4k short strings is a few MB at most for a single-user gateway.
	translateCacheMaxEntries = 4000
	translateCacheTTL        = 24 * time.Hour
)

// translateTextCache stores DeepL results keyed by target lang + source text.
// Context is intentionally omitted from the key so nav/chrome phrases hit across
// pages even when the surrounding paragraph context differs.
var translateTextCache = corecache.NewLRU[[32]byte, string](translateCacheMaxEntries, translateCacheTTL)

// translateFlight collapses concurrent DeepL calls for the same miss set so a
// burst of identical browser batches (or parallel range workers) pays once.
var translateFlight translateSingleflight

type translateSingleflight struct {
	mu    sync.Mutex
	calls map[string]*translateFlightCall
}

type translateFlightCall struct {
	wg  sync.WaitGroup
	val []string
	ok  bool
}

func (g *translateSingleflight) do(key string, fn func() ([]string, bool)) ([]string, bool) {
	g.mu.Lock()
	if g.calls == nil {
		g.calls = make(map[string]*translateFlightCall)
	}
	if c, ok := g.calls[key]; ok {
		g.mu.Unlock()
		c.wg.Wait()
		return c.val, c.ok
	}
	c := &translateFlightCall{}
	c.wg.Add(1)
	g.calls[key] = c
	g.mu.Unlock()

	defer func() {
		c.wg.Done()
		g.mu.Lock()
		delete(g.calls, key)
		g.mu.Unlock()
	}()

	c.val, c.ok = fn()
	return c.val, c.ok
}

func translateCacheKey(target, text string) [32]byte {
	h := sha256.New()
	writeTranslateCacheField(h, []byte(target))
	writeTranslateCacheField(h, []byte(text))
	var key [32]byte
	h.Sum(key[:0])
	return key
}

func writeTranslateCacheField(h hash.Hash, field []byte) {
	var size [8]byte
	binary.BigEndian.PutUint64(size[:], uint64(len(field)))
	h.Write(size[:])
	h.Write(field)
}

func translateCached(target, text string) (string, bool) {
	return translateTextCache.Get(translateCacheKey(target, text))
}

func rememberTranslated(target, text, translated string) {
	if text == "" || translated == "" {
		return
	}
	translateTextCache.Put(translateCacheKey(target, text), translated)
}

func translateMissFlightKey(target string, texts []string) string {
	h := sha256.New()
	writeTranslateCacheField(h, []byte(target))
	var n [8]byte
	binary.BigEndian.PutUint64(n[:], uint64(len(texts)))
	h.Write(n[:])
	for _, text := range texts {
		writeTranslateCacheField(h, []byte(text))
	}
	return hex.EncodeToString(h.Sum(nil))
}

func resetTranslateTextCache() {
	translateTextCache.Clear()
}
