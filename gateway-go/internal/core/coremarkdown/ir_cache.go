package coremarkdown

import (
	"encoding/json"

	"github.com/choiceoh/deneb/gateway-go/internal/core/corecache"
)

const cacheMaxEntries = 128

var cache = corecache.NewLRU[uint64, json.RawMessage](cacheMaxEntries, 0)

func fnv1a64(s string) uint64 {
	const offset64 = 14695981039346656037
	const prime64 = 1099511628211
	h := uint64(offset64)
	for i := 0; i < len(s); i++ {
		h ^= uint64(s[i])
		h *= prime64
	}
	return h
}
