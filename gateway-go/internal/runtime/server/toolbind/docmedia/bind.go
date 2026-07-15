package docmedia

import (
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/tools"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/tools/artifact"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/tools/document"
)

var (
	OCRImage              = document.OCRImage
	ExtractAttachmentText = document.ExtractAttachmentText
	ExtractDocumentText   = document.ExtractDocumentText
	TranscribeAudio       = artifact.TranscribeAudio
	TranslateSegments     = tools.TranslateSegments
)

type LocalAIFunc = tools.LocalAIFunc
