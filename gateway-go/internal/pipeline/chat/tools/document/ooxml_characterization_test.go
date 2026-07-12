package document

import (
	"strings"
	"testing"
)

func TestExtractOOXMLText_PreservesNestedTableAndBoundaryOrdering(t *testing.T) {
	xml := `<document><body>
<p>ignored outside text node<t>before</t></p>
<tbl><tr><tc>
  <p><t>outer</t></p>
  <tbl><tr><tc><p><t>inner</t></p></tc></tr></tbl>
  <p><t>tail</t></p>
</tc></tr></tbl>
<p><t>after</t></p>
</body></document>`

	got := strings.TrimSpace(extractOOXMLText(strings.NewReader(xml)))
	want := "before\n\n| outer inner tail |\n| --- |\nafter"
	if got != want {
		t.Fatalf("OOXML boundary rendering:\n got: %q\nwant: %q", got, want)
	}
}

func TestExtractOOXMLText_PreservesDecodedPrefixOnMalformedTable(t *testing.T) {
	xml := `<document><p><t>prefix</t></p><tbl><tr><tc><p><t>unfinished cell</t>`
	got := extractOOXMLText(strings.NewReader(xml))
	if got != "prefix\n" {
		t.Fatalf("malformed table result = %q, want only completed prefix", got)
	}
}
