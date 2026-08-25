// identity_reviewed.go — records the operator's answer to a person-identity
// question so the wiki verify scans stop asking.
//
// Sibling of deadline_done.go, and the same contract: stamp the EVIDENCE that
// was judged, never a bare flag. The homonym scan records the company domains
// it saw; the duplicate-name scan records the peer pages. A new domain or a new
// same-name page is therefore not covered by the old decision and surfaces once
// more, which is the difference between "settled" and "silenced".
package server

import (
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/workfeed"
)

// identityReviewedActionPrefix carries the scan kind and the pages the decision
// covers: "identity_reviewed:<kind>:<path>|<path>…".
const identityReviewedActionPrefix = "identity_reviewed:"

// markIdentityReviewed stamps identity_reviewed on every page the answered
// question spans. Best-effort and fire-and-forget (the feed action already
// succeeded); a page that fails to write simply gets asked again.
func (s *Server) markIdentityReviewed(_ workfeed.Item, actionID string) {
	if s.wikiStore == nil {
		return
	}
	rest := strings.TrimSpace(strings.TrimPrefix(actionID, identityReviewedActionPrefix))
	kind, pathList, ok := strings.Cut(rest, ":")
	if !ok {
		return
	}
	var paths []string
	for _, p := range strings.Split(pathList, "|") {
		if p = strings.TrimSpace(p); p != "" {
			paths = append(paths, p)
		}
	}
	if len(paths) == 0 {
		return
	}
	for _, path := range paths {
		evidence := s.identityAckEvidence(kind, path, paths)
		if len(evidence) == 0 {
			continue
		}
		if err := s.stampIdentityReviewed(path, evidence); err != nil {
			s.logger.Warn("identity reviewed 기록 실패", "path", path, "error", err)
		}
	}
	s.logger.Info("identity reviewed: 운영자 확인", "kind", kind, "pages", len(paths))
}

// identityAckEvidence recomputes what this decision covers from the page's
// CURRENT state — stamping the card's stale evidence would silence a signal the
// operator never saw.
func (s *Server) identityAckEvidence(kind, path string, group []string) []string {
	switch kind {
	case "person-duplicate":
		var out []string
		for _, peer := range group {
			if peer != path {
				out = append(out, "dup:"+peer)
			}
		}
		return out
	default: // homonym
		return s.wikiStore.PersonCompanyDomains(path)
	}
}

// stampIdentityReviewed merges evidence into the page's recorded decision.
func (s *Server) stampIdentityReviewed(path string, evidence []string) error {
	page, err := s.wikiStore.ReadPage(path)
	if err != nil || page == nil {
		return err
	}
	seen := map[string]bool{}
	for _, v := range page.Meta.IdentityReviewed {
		seen[strings.ToLower(strings.TrimSpace(v))] = true
	}
	changed := false
	for _, e := range evidence {
		key := strings.ToLower(strings.TrimSpace(e))
		if key == "" || seen[key] {
			continue
		}
		seen[key] = true
		page.Meta.IdentityReviewed = append(page.Meta.IdentityReviewed, key)
		changed = true
	}
	if !changed {
		return nil
	}
	page.Meta.Updated = time.Now().Format("2006-01-02")
	return s.wikiStore.WritePage(path, page)
}
