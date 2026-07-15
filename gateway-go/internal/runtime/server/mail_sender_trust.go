package server

import (
	"os"
	"strings"

	"github.com/choiceoh/deneb/gateway-go/internal/platform/gmail"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/mailanalysis"
)

const (
	trustedSendersEnv = "DENEB_MAIL_TRUSTED_SENDERS"
	trustedDomainsEnv = "DENEB_MAIL_TRUSTED_DOMAINS"
)

// mailSenderTrustDecision is the deterministic autonomous-intake allowlist.
// Manual analysis intentionally bypasses it: review means "ask the operator",
// not "this message can never be analyzed".
func (s *Server) mailSenderTrustDecision(msg *gmail.MessageDetail) mailanalysis.SenderTrustDecision {
	email := strings.ToLower(senderEmailFromHeader(messageFrom(msg)))
	if email == "" {
		return reviewSender("발신자 주소를 확인할 수 없음")
	}
	domain := emailDomain(email)

	if stringSetContains(mailanalysis.OurMailDomains(), domain) {
		return trustedSender()
	}
	if stringSetContains(splitEnvSet(os.Getenv(trustedSendersEnv)), email) ||
		stringSetContains(splitEnvSet(os.Getenv(trustedDomainsEnv)), domain) {
		return trustedSender()
	}
	if s != nil && s.wikiStore != nil && s.wikiStore.ResolvePersonByEmail(email) != "" {
		return trustedSender()
	}
	if s != nil && domain != "" && len(s.cpProjects.Lookup(s.wikiStore, domain)) > 0 {
		return trustedSender()
	}
	return reviewSender("미확인 발신자: " + email)
}

func messageFrom(msg *gmail.MessageDetail) string {
	if msg == nil {
		return ""
	}
	return msg.From
}

func emailDomain(email string) string {
	if i := strings.LastIndexByte(email, '@'); i >= 0 && i+1 < len(email) {
		return strings.ToLower(strings.TrimSpace(email[i+1:]))
	}
	return ""
}

func splitEnvSet(raw string) []string {
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if part = strings.ToLower(strings.TrimSpace(part)); part != "" {
			out = append(out, part)
		}
	}
	return out
}

func stringSetContains(values []string, want string) bool {
	want = strings.ToLower(strings.TrimSpace(want))
	if want == "" {
		return false
	}
	for _, value := range values {
		if strings.ToLower(strings.TrimSpace(value)) == want {
			return true
		}
	}
	return false
}

func trustedSender() mailanalysis.SenderTrustDecision {
	return mailanalysis.SenderTrustDecision{Disposition: mailanalysis.SenderTrusted}
}

func reviewSender(reason string) mailanalysis.SenderTrustDecision {
	return mailanalysis.SenderTrustDecision{Disposition: mailanalysis.SenderReview, Reason: reason}
}
