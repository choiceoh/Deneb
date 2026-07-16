package server

import (
	"os"
	"strings"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/mailpriority"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/wiki"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/gmail"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/mailanalysis"
)

const (
	trustedSendersEnv = "DENEB_MAIL_TRUSTED_SENDERS"
	trustedDomainsEnv = "DENEB_MAIL_TRUSTED_DOMAINS"
)

// mailSenderTrustDecision gates autonomous intake. Default is analyze: only
// clear bulk/noise (noreply, newsletter, [광고], …) goes to review. Unknown
// business counterparties must still auto-analyze — the operator asked for
// spam/newsletter filtering, not a first-seen allowlist.
// Manual analysis intentionally bypasses review either way.
func (s *Server) mailSenderTrustDecision(msg *gmail.MessageDetail) mailanalysis.SenderTrustDecision {
	from := messageFrom(msg)
	email := strings.ToLower(senderEmailFromHeader(from))
	domain := emailDomain(email)
	subject := ""
	if msg != nil {
		subject = msg.Subject
	}

	if s.senderExplicitlyTrusted(email, domain) {
		return trustedSender()
	}
	if reason := bulkNoiseReason(from, subject, msg); reason != "" {
		return reviewSender(reason)
	}
	return trustedSender()
}

func (s *Server) senderExplicitlyTrusted(email, domain string) bool {
	if email == "" {
		return false
	}
	if stringSetContains(mailanalysis.OurMailDomains(), domain) {
		return true
	}
	if stringSetContains(splitEnvSet(os.Getenv(trustedSendersEnv)), email) ||
		stringSetContains(splitEnvSet(os.Getenv(trustedDomainsEnv)), domain) {
		return true
	}
	if s == nil {
		return false
	}
	if s.wikiStore != nil && s.wikiStore.ResolvePersonByEmail(email) != "" {
		return true
	}
	if s.contactsStore != nil {
		if s.contactsStore.HasEmail(email) {
			return true
		}
		if domain != "" && !wiki.IsFreemailDomain(domain) && s.contactsStore.HasDomain(domain) {
			return true
		}
	}
	if domain != "" && len(s.cpProjects.Lookup(s.wikiStore, domain)) > 0 {
		return true
	}
	return false
}

func bulkNoiseReason(from, subject string, msg *gmail.MessageDetail) string {
	if label := promoOrSpamLabel(msg); label != "" {
		return "스팸/프로모션 라벨: " + label
	}
	if ok, reason := mailpriority.IsBulkNoise(from, subject); ok {
		if email := strings.ToLower(senderEmailFromHeader(from)); email != "" {
			return reason + ": " + email
		}
		return reason
	}
	return ""
}

func promoOrSpamLabel(msg *gmail.MessageDetail) string {
	if msg == nil {
		return ""
	}
	for _, label := range msg.Labels {
		switch strings.ToUpper(strings.TrimSpace(label)) {
		case "SPAM", "CATEGORY_PROMOTIONS":
			return label
		}
	}
	return ""
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
