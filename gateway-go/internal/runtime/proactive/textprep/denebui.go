package textprep

import "github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/denebui"

// Fence helpers re-exported so proactive parent stays under the fanout soft bar.
var (
	HasFence             = denebui.HasFence
	CollapsedReportFence = denebui.CollapsedReportFence
	ReplaceFences        = denebui.ReplaceFences
	PlainText            = denebui.PlainText
	IsFenceOpenLine      = denebui.IsFenceOpenLine
	StripHTMLAnswers     = denebui.StripHTMLAnswers
)
