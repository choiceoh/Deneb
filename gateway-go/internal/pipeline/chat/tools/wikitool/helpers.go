package wikitool

import (
	"strings"

	"github.com/choiceoh/deneb/gateway-go/pkg/textutil"
)

func truncateRunes(s string, maxRunes int) string {
	return textutil.TruncateRunes(s, maxRunes, "\n... (이하 생략)")
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return strings.TrimSpace(s[:i])
	}
	return s
}
