package server

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/skills"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/workfeed"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/lanewatch"
)

// lane_liveness.go — which lanes the liveness watch reads, and how long each
// may be quiet.
//
// The seed set is not a survey of the gateway; it is the 2026-08-26 runtime
// review's findings turned into standing checks. Every lane here is one that
// HAD gone quiet without anyone noticing. Adding a lane is cheap — a name, a
// silence budget, and a read — so the set should grow as more silent failures
// are found, not be guessed at up front.
func (s *Server) laneLivenessWatch() *lanewatch.Watch {
	var lanes []lanewatch.Lane

	// Skill catalog suppression. A bundled skill deleted from the app becomes a
	// tombstone and then vanishes from every surface — kb-interview swallowed
	// two exact operator triggers for five weeks that way, and the loop's own
	// evolution-proposal / skill-factory were found suppressed without intent.
	// Zero is the healthy reading here, so ANY tombstone is the finding: this
	// lane reports "worked" when the list is empty and silence when it is not.
	lanes = append(lanes, lanewatch.Lane{
		Name:       "skill-catalog-suppression",
		MaxSilence: 0, // report on the first check that finds one
		Read: func(context.Context) (lanewatch.Reading, error) {
			deleted := skills.DeletedSkillNamesSorted()
			if len(deleted) == 0 {
				return lanewatch.Reading{Worked: 1}, nil
			}
			return lanewatch.Reading{
				Detail: fmt.Sprintf("억제된 스킬 %d개: %v — miniapp.skills.restore 로 복구", len(deleted), deleted),
			}, nil
		},
	})

	if s.genesisTracker != nil {
		tracker := s.genesisTracker
		// Skill evolution. Read evolved=0 on 2026-08-26 because one unusable
		// validation case had disabled the behavioral gate — and zero looked
		// exactly like "no skill needed changing". Two weeks is generous for a
		// lane that should produce something most weeks.
		lanes = append(lanes, lanewatch.Lane{
			Name:       "skill-evolve",
			MaxSilence: 14 * 24 * time.Hour,
			Read: func(context.Context) (lanewatch.Reading, error) {
				h := tracker.EvolutionHealth()
				worked := h.Evolves7d + h.EvolveRejected7d
				// A rejection IS work: the gate ran and said no. Only both at
				// zero means the lane never executed.
				return lanewatch.Reading{
					Worked: worked,
					Detail: fmt.Sprintf("7일 진화 %d · 기각 %d", h.Evolves7d, h.EvolveRejected7d),
				}, nil
			},
		})
	}

	return lanewatch.New(s.logger, nil, lanes...)
}

// postLaneLivenessCard surfaces findings where a person actually looks. The
// journal line is the floor; a lane that went quiet is precisely the thing
// nobody greps for, because they do not know to look.
func (s *Server) postLaneLivenessCard(findings []lanewatch.Finding) {
	if len(findings) == 0 {
		return
	}
	nf := s.nativeWorkFeedStore()
	if nf == nil {
		return
	}
	lines := make([]string, 0, len(findings))
	for _, f := range findings {
		lines = append(lines, "- "+f.String())
	}
	if _, err := nf.Append(workfeed.Item{
		Source:  "lane-liveness",
		Title:   fmt.Sprintf("레인 %d개가 조용합니다", len(findings)),
		Summary: "자율 레인 라이브니스 점검",
		Body: "아래 레인이 정상 범위보다 오래 아무 일도 하지 않았습니다. 조용한 것과 고장난 것은 로그로 구별되지 않으므로, 각 항목을 한 번씩 확인해 주세요.\n\n" +
			strings.Join(lines, "\n"),
		Status: "unread",
	}); err != nil {
		s.logger.Warn("lane-liveness 카드 생성 실패", "findings", len(findings), "error", err)
	}
}
