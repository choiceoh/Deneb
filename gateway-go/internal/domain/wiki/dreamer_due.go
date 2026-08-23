// dreamer_due.go — close dues that have been stale long enough that the
// diary no longer mentions them. verify.go's stale_deadline finding is
// advisory and re-reported every cycle (2026-08-23: 50 findings, 36 with
// count≥50). This pass actually clears the field so morning-letter "임박
// 마감" stops listing ghosts.
package wiki

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	staleDueCloseAfterDays = 7
	staleDueCloseCap       = 15
	staleDueAlertLimit     = 5
)

type staleDueCandidate struct {
	path  string
	title string
	due   string
	days  int
}

type staleDueResult struct {
	closed int
	alert  []string
}

// closeStaleDues clears frontmatter due on pages overdue by
// staleDueCloseAfterDays whose title, project folder, or frozen code is not
// mentioned in this cycle's diary. Cap per cycle so a first run after deploy
// cannot rewrite the whole wiki. Returns how many dues were cleared and the
// top-N alert lines (closed first, then remaining).
func (wd *WikiDreamer) closeStaleDues(diary string) staleDueResult {
	if wd.store == nil {
		return staleDueResult{}
	}
	relPaths, err := wd.store.ListPages("")
	if err != nil {
		return staleDueResult{}
	}
	today := time.Now()
	var closable, remaining []staleDueCandidate
	for _, rp := range relPaths {
		rp = filepath.ToSlash(rp)
		page, rerr := wd.store.ReadPage(rp)
		if rerr != nil || page == nil || page.Meta.Archived {
			continue
		}
		due := strings.TrimSpace(page.Meta.Due)
		if due == "" {
			continue
		}
		dueTime, perr := time.Parse("2006-01-02", due)
		if perr != nil {
			continue
		}
		days := int(today.Sub(dueTime).Hours() / 24)
		if days < staleDueCloseAfterDays {
			continue
		}
		title := page.Meta.Title
		if title == "" {
			title = strings.TrimSuffix(filepath.Base(rp), ".md")
		}
		cand := staleDueCandidate{path: rp, title: title, due: due, days: days}
		if diaryMentionsPage(diary, title, rp, page.Meta.Code) {
			remaining = append(remaining, cand)
			continue
		}
		closable = append(closable, cand)
	}
	sort.Slice(closable, func(i, j int) bool { return closable[i].days > closable[j].days })
	sort.Slice(remaining, func(i, j int) bool { return remaining[i].days > remaining[j].days })

	closed := make([]staleDueCandidate, 0, staleDueCloseCap)
	for i, cand := range closable {
		if i >= staleDueCloseCap {
			remaining = append(remaining, closable[i:]...)
			break
		}
		if !wd.clearStaleDue(cand) {
			remaining = append(remaining, cand)
			continue
		}
		closed = append(closed, cand)
	}

	alert := make([]string, 0, staleDueAlertLimit)
	for _, c := range closed {
		if len(alert) >= staleDueAlertLimit {
			break
		}
		alert = append(alert, fmt.Sprintf("해제 %q (기한 %s, %d일)", c.title, c.due, c.days))
	}
	for _, c := range remaining {
		if len(alert) >= staleDueAlertLimit {
			break
		}
		alert = append(alert, fmt.Sprintf("유지 %q (기한 %s, %d일)", c.title, c.due, c.days))
	}
	return staleDueResult{closed: len(closed), alert: alert}
}

func (wd *WikiDreamer) clearStaleDue(cand staleDueCandidate) bool {
	note := fmt.Sprintf("- 기한 해제: %s (%d일 경과, 이번 일지 무언급)", cand.due, cand.days)
	err := wd.store.UpdatePage(cand.path, func(page *Page) (*Page, error) {
		if page == nil || strings.TrimSpace(page.Meta.Due) != cand.due {
			return nil, nil
		}
		page.Meta.Due = ""
		page.Body = strings.TrimRight(page.Body, "\n") + "\n\n" + note + "\n"
		page.Meta.Updated = time.Now().Format("2006-01-02")
		return page, nil
	})
	if err != nil {
		if wd.logger != nil {
			wd.logger.Warn("wiki-dream: stale due close failed", "path", cand.path, "error", err)
		}
		return false
	}
	if wd.logger != nil {
		wd.logger.Info("wiki-dream: stale due closed",
			"path", cand.path, "due", cand.due, "days", cand.days)
	}
	return true
}

func diaryMentionsPage(diary, title, path, code string) bool {
	if strings.TrimSpace(diary) == "" {
		return false
	}
	lower := strings.ToLower(diary)
	if title != "" && strings.Contains(lower, strings.ToLower(title)) {
		return true
	}
	if name, ok := ProjectNameOf(path); ok && name != "" && strings.Contains(lower, strings.ToLower(name)) {
		return true
	}
	if code != "" && strings.Contains(lower, strings.ToLower(code)) {
		return true
	}
	return false
}
