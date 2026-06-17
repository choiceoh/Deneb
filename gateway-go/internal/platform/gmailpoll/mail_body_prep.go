package gmailpoll

import "github.com/choiceoh/deneb/gateway-go/internal/platform/mailbody"

func cleanMailBodyForAnalysis(body string) string {
	return mailbody.CleanForAnalysis(body)
}
