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

	// Order matters, and the axis is how DIRECTLY the sender was vouched for.
	// Two kinds of evidence name this exact address — what the operator
	// configured, and a record somebody saved of the address itself — and both
	// outrank the noise test. Domain-level inference does not, because a
	// newsletter address stays a newsletter address no matter what else that
	// domain sends.
	//
	// Ranking domain inference above the noise test made the gate unreachable
	// for exactly the senders that needed it, and did so through a loop that
	// sustained itself: ActiveCounterpartyDomains is built from the sender-domain
	// tags of project-linked mail analyses, so analyzing a newsletter once wrote
	// its own domain into the counterparty set, which granted the sender trust,
	// which kept it being analyzed. Measured 2026-08-25 in the live corpus:
	// no-reply@plaud.ai (45 pages, trusted purely by that loop) and
	// newsletteradmin@yulchon.com (8 pages, trusted because the firm's domain is
	// in contacts) both matched machineSenderRe and were analyzed anyway. Neither
	// address is itself recorded anywhere.
	if s.senderOperatorTrusted(email, domain) || s.senderAddressRecorded(email) {
		return trustedSender()
	}
	// The sender's own address, and an ad tag in the subject, are properties of
	// the mail itself — no inference about the domain overrides them.
	if reason := selfDeclaredNoiseReason(from, subject); reason != "" {
		return reviewSender(reason)
	}
	// A Gmail SPAM/PROMOTIONS label is a third party's guess, and it lands on
	// real counterparty mail often enough that any sign we know this domain
	// should outrank it.
	if label := promoOrSpamLabel(msg); label != "" && !s.senderDomainInferred(domain) {
		return reviewSender("스팸/프로모션 라벨: " + label)
	}
	return trustedSender()
}

// senderOperatorTrusted reports the trust the operator stated directly: our own
// mail domains and the trusted-sender/domain env lists.
func (s *Server) senderOperatorTrusted(email, domain string) bool {
	if email == "" {
		return false
	}
	if stringSetContains(mailanalysis.OurMailDomains(), domain) {
		return true
	}
	return stringSetContains(splitEnvSet(os.Getenv(trustedSendersEnv)), email) ||
		stringSetContains(splitEnvSet(os.Getenv(trustedDomainsEnv)), domain)
}

// senderAddressRecorded reports whether this exact address is on file — a wiki
// person page or a contact entry that lists it. Somebody deliberately saved the
// address, so it beats the noise test even when it reads like a machine sender
// (a vendor's no-reply order-confirmation address is the real case).
func (s *Server) senderAddressRecorded(email string) bool {
	if s == nil || email == "" {
		return false
	}
	if s.wikiStore != nil && s.wikiStore.ResolvePersonByEmail(email) != "" {
		return true
	}
	return s.contactsStore != nil && s.contactsStore.HasEmail(email)
}

// senderDomainInferred reports whether we have a standing relationship with the
// sender's DOMAIN: a contact there, or recent project mail with it. This is what
// keeps unknown business counterparties auto-analyzing. Nothing here names the
// sender, so it outranks only a third-party spam label, never the address itself.
func (s *Server) senderDomainInferred(domain string) bool {
	if s == nil || domain == "" {
		return false
	}
	if s.contactsStore != nil && !wiki.IsFreemailDomain(domain) && s.contactsStore.HasDomain(domain) {
		return true
	}
	return len(s.cpProjects.Lookup(s.wikiStore, domain)) > 0
}

// selfDeclaredNoiseReason names the noise the mail declares about itself — a
// machine-sender address, or an ad tag / unsubscribe line in the subject.
func selfDeclaredNoiseReason(from, subject string) string {
	ok, reason := mailpriority.IsBulkNoise(from, subject)
	if !ok {
		return ""
	}
	if email := strings.ToLower(senderEmailFromHeader(from)); email != "" {
		return reason + ": " + email
	}
	return reason
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
