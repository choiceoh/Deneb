package web

import (
	"fmt"
	"strings"
	"testing"
)

func TestExtractJSONLD_PreservesBlockPrecedenceAndArrayShapes(t *testing.T) {
	html := `<script type="application/ld+json">not json</script>
<script type="application/ld+json">[
  {
    "@type": "NewsArticle",
    "name": "Array Title",
    "description": "Array Description",
    "datePublished": "2026-01-02",
    "author": [{"name": "Array Author"}, {"name": "Ignored Author"}],
    "publisher": {"name": "Array Publisher"},
    "wordCount": 100
  },
  {"name": "Ignored Array Item"}
]</script>
<script type="application/ld+json">{
  "@type": "Product",
  "headline": "Later Title",
  "description": "Later Description",
  "datePublished": "2026-02-03",
  "author": "Later Author",
  "publisher": {"name": "Later Publisher"},
  "wordCount": 250
}</script>`

	meta := &webFetchMeta{}
	extractJSONLD(html, meta)

	if meta.Title != "Array Title" || meta.Description != "Array Description" ||
		meta.Published != "2026-01-02" || meta.Author != "Array Author" ||
		meta.SiteName != "Array Publisher" || meta.OGType != "ld:NewsArticle" {
		t.Fatalf("first valid block did not retain metadata precedence: %+v", meta)
	}
	if meta.WordCount != 250 {
		t.Fatalf("WordCount = %d, want later positive value 250", meta.WordCount)
	}
}

func TestExtractJSONLD_PreservesExistingFieldsAndFiveBlockLimit(t *testing.T) {
	meta := &webFetchMeta{
		Title:       "Existing Title",
		Description: "Existing Description",
		Published:   "Existing Date",
		Author:      "Existing Author",
		SiteName:    "Existing Publisher",
		OGType:      "article",
	}
	var blocks []string
	for wordCount := 1; wordCount <= 6; wordCount++ {
		blocks = append(blocks, fmt.Sprintf(
			`<script type="application/ld+json">{"headline":"Title %d","wordCount":%d}</script>`,
			wordCount, wordCount,
		))
	}

	extractJSONLD(strings.Join(blocks, "\n"), meta)

	if meta.Title != "Existing Title" || meta.Description != "Existing Description" ||
		meta.Published != "Existing Date" || meta.Author != "Existing Author" ||
		meta.SiteName != "Existing Publisher" || meta.OGType != "article" {
		t.Fatalf("existing metadata was overwritten: %+v", meta)
	}
	if meta.WordCount != 5 {
		t.Fatalf("WordCount = %d, want fifth block value 5 (sixth must not be scanned)", meta.WordCount)
	}
}
