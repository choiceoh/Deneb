package web

import "testing"

// TestClassifyContentType_DocumentByURL covers the case where a download
// endpoint serves a document under a generic content type — classification must
// fall back to the URL's file extension instead of returning plain.
func TestClassifyContentType_DocumentByURL(t *testing.T) {
	cases := []struct {
		name        string
		contentType string
		url         string
		want        fetchedContentType
	}{
		{"octet-stream pdf with query", "application/octet-stream", "https://x.test/files/report.pdf?sig=abc", contentTypeDocument},
		{"text-plain csv download", "text/plain", "https://x.test/d/data.csv", contentTypeDocument},
		{"proper pdf mime", "application/pdf", "https://x.test/a", contentTypeDocument},
		{"html page wins over url", "text/html; charset=utf-8", "https://x.test/page", contentTypeHTML},
		{"no extension generic type", "application/octet-stream", "https://x.test/download?id=1", contentTypePlain},
	}
	for _, c := range cases {
		if got := classifyContentType(c.contentType, c.url); got != c.want {
			t.Errorf("%s: classifyContentType(%q, %q) = %v, want %v", c.name, c.contentType, c.url, got, c.want)
		}
	}
}

func TestDocumentName(t *testing.T) {
	cases := map[string]string{
		"https://x.test/a/b/report.pdf?x=1": "report.pdf",
		"https://x.test/data.csv":           "data.csv",
		"https://x.test/download?id=1":      "download",
		"":                                  "document",
		"https://x.test/":                   "document",
	}
	for url, want := range cases {
		if got := documentName(url); got != want {
			t.Errorf("documentName(%q) = %q, want %q", url, got, want)
		}
	}
}
