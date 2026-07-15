package document

import (
	"context"
	"net/url"
	"os"
	"path"
	"strings"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/filestore"
)

const fetchedWebDocArchiveMaxBytes = 25 * 1024 * 1024

// ArchiveFetchedWebDoc best-effort saves a fetched binary web document into the
// user file store at /web/<host>/<name>. Non-fatal by design. Disable with
// DENEB_ARCHIVE_FETCHED_DOCS=0.
func ArchiveFetchedWebDoc(ctx context.Context, rawURL string, data []byte) {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("DENEB_ARCHIVE_FETCHED_DOCS"))) {
	case "0", "false", "no", "off":
		return
	}
	if len(data) == 0 || len(data) > fetchedWebDocArchiveMaxBytes {
		return
	}
	store, err := filestore.DefaultLocalStore()
	if err != nil || store == nil {
		return
	}
	_, _ = store.Put(ctx, fetchedWebDocVPath(rawURL), data, true)
}

func fetchedWebDocVPath(rawURL string) string {
	host := "web"
	if u, err := url.Parse(rawURL); err == nil && u.Host != "" {
		host = u.Host
	}
	return path.Join("/web", host, fetchedWebDocName(rawURL))
}

func fetchedWebDocName(url string) string {
	name := url
	if idx := strings.LastIndex(name, "/"); idx >= 0 {
		name = name[idx+1:]
	}
	if idx := strings.Index(name, "?"); idx >= 0 {
		name = name[:idx]
	}
	if name == "" {
		return "document"
	}
	return name
}
