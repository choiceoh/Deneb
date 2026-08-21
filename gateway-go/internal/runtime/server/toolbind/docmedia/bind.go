package docmedia

import (
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/tools/artifact"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/tools/document"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/tools/recallops"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/tools/translateops"
)

var (
	OCRImage              = document.OCRImage
	ExtractAttachmentText = document.ExtractAttachmentText
	ExtractDocumentText   = document.ExtractDocumentText
	TranscribeAudio       = artifact.TranscribeAudio
	TranslateSegments     = translateops.TranslateSegments
)

type LocalAIFunc = recallops.LocalAIFunc
