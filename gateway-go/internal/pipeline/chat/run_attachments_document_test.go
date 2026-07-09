package chat

import "testing"

// TestHasDocumentAttachment covers the hard anchor the deliverable auto-publish
// gates on: a document was ingested this turn. Ordinary chat (no attachment, or an
// image/audio one) must read false so it can never trip the safety net.
func TestHasDocumentAttachment(t *testing.T) {
	cases := []struct {
		name string
		atts []ChatAttachment
		want bool
	}{
		{"none", nil, false},
		{"image only", []ChatAttachment{{Type: "image", MimeType: "image/png"}}, false},
		{"already-extracted document_text", []ChatAttachment{{Type: "document_text", Name: "계약서"}}, true},
		{"docx by office mime (the RFP case)", []ChatAttachment{{
			MimeType: "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
			Name:     "당진_솔라빌리지_사업개요.docx",
		}}, true},
		{"pdf by name", []ChatAttachment{{Name: "report.pdf"}}, true},
		{"image + doc mixed", []ChatAttachment{
			{Type: "image", MimeType: "image/jpeg"},
			{Name: "terms.pdf"},
		}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := hasDocumentAttachment(tc.atts); got != tc.want {
				t.Errorf("hasDocumentAttachment(%s) = %v, want %v", tc.name, got, tc.want)
			}
		})
	}
}
