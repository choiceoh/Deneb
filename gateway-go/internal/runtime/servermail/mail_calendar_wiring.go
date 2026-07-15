package servermail

import (
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/platform/calprop"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/gmail"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/mailanalysis"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/mailflow"
)

// autoProposeCalendarFromMail persists the conservative proposals derived by
// mailflow. Individual failures are logged and do not abort mail analysis.
func (m *Manager) autoProposeCalendarFromMail(msg *gmail.MessageDetail, items []mailanalysis.ActionItem, deal *mailanalysis.DealInfo, importance string) int {
	if msg == nil {
		return 0
	}
	inputs := mailflow.CalendarProposalsFromMail(
		msg.ID,
		msg.Subject,
		msg.From,
		mailflow.DocumentAttachmentNames(msg.Attachments),
		items,
		deal,
		importance,
		time.Now(),
	)
	if len(inputs) == 0 {
		return 0
	}
	store, err := calprop.Default()
	if err != nil {
		m.Host.Logger().Warn("mail→calendar: proposal store unavailable", "id", msg.ID, "error", err)
		return len(inputs)
	}
	created := 0
	for _, in := range inputs {
		_, was, createErr := store.CreateIfAbsent(in)
		if createErr != nil {
			m.Host.Logger().Warn("mail→calendar: propose failed", "id", msg.ID, "title", in.Title, "error", createErr)
			continue
		}
		if was {
			created++
		}
	}
	if created > 0 {
		m.Host.Logger().Info("mail→calendar: proposed calendar events from analysis", "id", msg.ID, "count", created)
	}
	return len(inputs)
}
