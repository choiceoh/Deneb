// archive.go — persist fetched binary documents into the user file store so a
// fetched source survives the turn and is browsable/recallable later, instead of
// being silently dropped and re-fetched next time. Only the contentTypeDocument
// branch of processFetchedContent calls this (PDF/Office/CSV), so ordinary HTML
// page browsing is never archived — the store stays signal, not noise.
package web

import (
	"context"
	"net/url"
	"os"
	"path"
	"strings"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/filestore"
)

// fetchedDocArchiveMaxBytes caps the size of a fetched document copied into the
// store; an oversized payload is still extracted/returned, just not archived.
const fetchedDocArchiveMaxBytes = 25 * 1024 * 1024

// archiveFetchedDocument best-effort saves a fetched binary document into the
// user file store at /web/<host>/<name>. Non-fatal by design: any failure (no
// store, empty/oversized payload, write error) is swallowed — the fetch already
// succeeded and the caller still gets the extracted text. Disable with
// DENEB_ARCHIVE_FETCHED_DOCS=0.
func archiveFetchedDocument(ctx context.Context, rawURL string, data []byte) {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("DENEB_ARCHIVE_FETCHED_DOCS"))) {
	case "0", "false", "no", "off":
		return
	}
	if len(data) == 0 || len(data) > fetchedDocArchiveMaxBytes {
		return
	}
	store, err := filestore.DefaultLocalStore()
	if err != nil || store == nil {
		return
	}
	_, _ = store.Put(ctx, fetchedDocVPath(rawURL), data, true)
}

// fetchedDocVPath maps a fetched document URL to its store path
// /web/<host>/<name>, reusing documentName for the trailing filename.
func fetchedDocVPath(rawURL string) string {
	host := "web"
	if u, err := url.Parse(rawURL); err == nil && u.Host != "" {
		host = u.Host
	}
	return path.Join("/web", host, documentName(rawURL))
}
