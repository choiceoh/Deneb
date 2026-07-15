package server

import (
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/runtime/server/platbind"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/server/svcbind"
)

// autoProposeCalendarFromMail persists the conservative proposals derived by
// mailflow. Individual failures are logged and do not abort mail analysis.
func (s *Server) autoProposeCalendarFromMail(msg *platbind.MessageDetail, items []platbind.ActionItem, deal *platbind.DealInfo, importance string) int {
	if msg == nil {
		return 0
	}
	inputs := svcbind.CalendarProposalsFromMail(
		msg.ID,
		msg.Subject,
		msg.From,
		svcbind.DocumentAttachmentNames(msg.Attachments),
		items,
		deal,
		importance,
		time.Now(),
	)
	if len(inputs) == 0 {
		return 0
	}
	store, err := platbind.CalPropDefault()
	if err != nil {
		s.logger.Warn("mail→calendar: proposal store unavailable", "id", msg.ID, "error", err)
		return len(inputs)
	}
	created := 0
	for _, in := range inputs {
		_, was, createErr := store.CreateIfAbsent(in)
		if createErr != nil {
			s.logger.Warn("mail→calendar: propose failed", "id", msg.ID, "title", in.Title, "error", createErr)
			continue
		}
		if was {
			created++
		}
	}
	if created > 0 {
		s.logger.Info("mail→calendar: proposed calendar events from analysis", "id", msg.ID, "count", created)
	}
	return len(inputs)
}
