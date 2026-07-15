package toolwire

import (
	"context"

	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolwire/attach"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolwire/wire"
)

func IsExtractableDocument(mimeType, filename string) bool {
	return attach.IsExtractableDocument(mimeType, filename)
}

func ExtractDocumentText(ctx context.Context, data []byte, filename, mimeType string) (string, bool) {
	return attach.ExtractDocumentText(ctx, data, filename, mimeType)
}

func BuildNotebookGrounding(d *wire.NotebookDeps, notebookID string) (string, bool) {
	return attach.BuildNotebookGrounding(d, notebookID)
}
