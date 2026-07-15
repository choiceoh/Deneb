package attach

import (
	"context"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/tooldeps"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/tools/document"
	notebooktool "github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/tools/notebook"
)

// IsExtractableDocument reports whether mime/filename look like a document the
// extractors can read. Re-exported so chat attachment prep does not import
// tools/document.
func IsExtractableDocument(mimeType, filename string) bool {
	return document.IsExtractableDocument(mimeType, filename)
}

// ExtractDocumentText extracts text from a document attachment. Re-exported so
// chat attachment prep does not import tools/document.
func ExtractDocumentText(ctx context.Context, data []byte, filename, mimeType string) (string, bool) {
	return document.ExtractDocumentText(ctx, data, filename, mimeType)
}

// BuildNotebookGrounding renders a bound notebook as grounding text for the
// turn. Re-exported so chat run_exec does not import tools/notebook.
func BuildNotebookGrounding(d *tooldeps.NotebookDeps, notebookID string) (string, bool) {
	return notebooktool.BuildNotebookGrounding(d, notebookID)
}
