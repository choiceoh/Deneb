package document

import "github.com/choiceoh/deneb/gateway-go/pkg/textutil"

func truncate(s string, maxRunes int) string {
	return textutil.TruncateRunes(s, maxRunes, "...")
}

func formatBytes(b int64) string {
	return textutil.FormatBytes(b)
}
