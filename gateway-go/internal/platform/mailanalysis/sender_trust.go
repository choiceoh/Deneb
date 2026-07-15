package mailanalysis

import (
	"fmt"
	"strings"

	"github.com/choiceoh/deneb/gateway-go/internal/platform/gmail"
)

// SenderTrustDisposition controls whether autonomous intake may read and
// analyze a message body. Manual analysis remains available for review items.
type SenderTrustDisposition string

const (
	SenderTrusted SenderTrustDisposition = "trusted"
	SenderReview  SenderTrustDisposition = "review"
)

// SenderTrustDecision is a deterministic intake decision. Reason is persisted
// for review items so the operator can see why autonomous analysis stopped.
type SenderTrustDecision struct {
	Disposition SenderTrustDisposition
	Reason      string
}

func (s *Service) senderTrustDecision(msg *gmail.MessageDetail) SenderTrustDecision {
	if s == nil || s.cfg.SenderTrustFn == nil {
		return SenderTrustDecision{Disposition: SenderTrusted}
	}
	decision := s.cfg.SenderTrustFn(messageMetadataOnly(msg))
	if decision.Disposition != SenderReview {
		decision.Disposition = SenderTrusted
		decision.Reason = ""
		return decision
	}
	decision.Reason = oneLine(decision.Reason)
	if decision.Reason == "" {
		decision.Reason = "미확인 발신자"
	}
	return decision
}

func (s *Service) recordSenderReview(msg *gmail.MessageDetail, decision SenderTrustDecision) error {
	if s.cfg.OnSenderReview == nil {
		return fmt.Errorf("sender review sink is required")
	}
	return s.cfg.OnSenderReview(messageMetadataOnly(msg), decision)
}

// messageMetadataOnly deliberately excludes bodies, attachment names/bytes,
// and large-file links. Both the trust decider and review sink receive this
// shape, making the metadata-only boundary explicit rather than conventional.
func messageMetadataOnly(msg *gmail.MessageDetail) *gmail.MessageDetail {
	if msg == nil {
		return nil
	}
	return &gmail.MessageDetail{
		ID:              msg.ID,
		ThreadID:        msg.ThreadID,
		From:            msg.From,
		To:              msg.To,
		CC:              msg.CC,
		Subject:         msg.Subject,
		Date:            msg.Date,
		Labels:          append([]string(nil), msg.Labels...),
		MessageIDHeader: msg.MessageIDHeader,
		References:      append([]string(nil), msg.References...),
	}
}

func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }
