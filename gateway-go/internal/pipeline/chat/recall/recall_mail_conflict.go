// recall_mail_conflict.go — wiki 인물 페이지와 최근 메일 From이 다를 때
// 회상에 둘 다 올리고 불일치만 표시한다. 점수는 건드리지 않는다.
// 서버가 한쪽을 이기게 하면 stale 위키가 이긴다 (위키 개선안 P2).
package recall

import (
	"context"
	"net/mail"
	"regexp"
	"strings"

	wiki "github.com/choiceoh/deneb/gateway-go/internal/domain/wikiport"
)

const (
	wikiMailConflictMarker  = "⚠ 불일치(위키와 최근 메일 From이 다름 — 둘 다 근거로 두고 서버가 한쪽을 이기게 하지 말 것)"
	wikiMailConflictPullMax = 2
)

var recallEmailRe = regexp.MustCompile(`(?i)[a-z0-9._%+\-]+@[a-z0-9.\-]+\.[a-z]{2,}`)

var recallFreemail = map[string]bool{
	"gmail.com": true, "naver.com": true, "daum.net": true, "hanmail.net": true,
	"outlook.com": true, "hotmail.com": true, "icloud.com": true, "yahoo.com": true,
}

func attachWikiMailConflicts(ctx context.Context, store *wiki.Store, evidence []recallEvidence, cue bool) []recallEvidence {
	if store == nil || len(evidence) == 0 {
		return evidence
	}
	evidence = annotateExistingWikiMailConflicts(store, evidence)
	if cue {
		evidence = pullMissingMailAnalysisConflicts(ctx, store, evidence)
	}
	return evidence
}

func annotateExistingWikiMailConflicts(store *wiki.Store, evidence []recallEvidence) []recallEvidence {
	for i := range evidence {
		if evidence[i].Kind != "wiki" || !isPersonWikiPath(evidence[i].Source) {
			continue
		}
		wikiEmails := personEmailsFromStore(store, evidence[i].Source)
		if len(wikiEmails) == 0 {
			continue
		}
		title := personTitleFromStore(store, evidence[i].Source)
		for j := range evidence {
			if i == j || evidence[j].Kind != "wiki" || !wiki.IsMailAnalysisPath(evidence[j].Source) {
				continue
			}
			mailText := evidence[j].Note + "\n" + mailAnalysisText(store, evidence[j].Source)
			addr := mailFromAddress(mailText)
			if addr == "" || !mailAboutPerson(title, mailText, addr) {
				continue
			}
			if note := personMailConflict(wikiEmails, addr); note != "" {
				evidence[i].Note = prefixConflict(evidence[i].Note, note)
				evidence[j].Note = prefixConflict(evidence[j].Note, note)
			}
		}
	}
	return evidence
}

func pullMissingMailAnalysisConflicts(ctx context.Context, store *wiki.Store, evidence []recallEvidence) []recallEvidence {
	if ctx != nil && ctx.Err() != nil {
		return evidence
	}
	pulled := 0
	for i := range evidence {
		if pulled >= wikiMailConflictPullMax {
			break
		}
		if evidence[i].Kind != "wiki" || !isPersonWikiPath(evidence[i].Source) {
			continue
		}
		if strings.Contains(evidence[i].Note, wikiMailConflictMarker) {
			continue
		}
		wikiEmails := personEmailsFromStore(store, evidence[i].Source)
		title := personTitleFromStore(store, evidence[i].Source)
		if len(wikiEmails) == 0 || title == "" {
			continue
		}
		if hasMailAnalysisForPerson(evidence, title) {
			continue
		}
		hits, err := store.Search(ctx, title, 3)
		if err != nil || len(hits) == 0 {
			continue
		}
		for _, h := range hits {
			if !wiki.IsMailAnalysisPath(h.Path) {
				continue
			}
			mailText := h.Content + "\n" + mailAnalysisText(store, h.Path)
			addr := mailFromAddress(mailText)
			if addr == "" || !mailAboutPerson(title, mailText, addr) {
				continue
			}
			note := personMailConflict(wikiEmails, addr)
			if note == "" {
				continue
			}
			evidence[i].Note = prefixConflict(evidence[i].Note, note)
			evidence = append(evidence, recallEvidence{
				Kind:   "wiki",
				Source: h.Path,
				Query:  evidence[i].Query,
				Note:   prefixConflict(strings.TrimSpace(h.Content), note),
				Score:  evidence[i].Score,
				At:     h.UpdatedAt,
			})
			pulled++
			break
		}
	}
	return evidence
}

func hasMailAnalysisForPerson(evidence []recallEvidence, title string) bool {
	for _, ev := range evidence {
		if ev.Kind == "wiki" && wiki.IsMailAnalysisPath(ev.Source) && strings.Contains(ev.Note, title) {
			return true
		}
	}
	return false
}

func isPersonWikiPath(path string) bool {
	return strings.HasPrefix(filepathToSlash(path), "인물/")
}

func filepathToSlash(path string) string {
	return strings.ReplaceAll(path, "\\", "/")
}

func personEmailsFromStore(store *wiki.Store, path string) []string {
	page, err := store.ReadPage(path)
	if err != nil || page == nil {
		return nil
	}
	out := append([]string(nil), page.Meta.Emails...)
	out = append(out, extractEmails(page.Body)...)
	return dedupeLower(out)
}

func personTitleFromStore(store *wiki.Store, path string) string {
	page, err := store.ReadPage(path)
	if err != nil || page == nil {
		return ""
	}
	return strings.TrimSpace(page.Meta.Title)
}

func mailAnalysisText(store *wiki.Store, path string) string {
	page, err := store.ReadPage(path)
	if err != nil || page == nil {
		return ""
	}
	return page.Body
}

func mailAboutPerson(title, mailText, mailFrom string) bool {
	name := strings.TrimSpace(title)
	if len([]rune(name)) < 2 {
		return false
	}
	return strings.Contains(mailText, name) || strings.Contains(mailFrom, name)
}

func prefixConflict(note, conflict string) string {
	note = strings.TrimSpace(note)
	if strings.Contains(note, wikiMailConflictMarker) {
		return note
	}
	if note == "" {
		return conflict
	}
	return conflict + " | " + note
}

func personMailConflict(wikiEmails []string, mailFrom string) string {
	addr := parseMailAddress(mailFrom)
	if addr == "" {
		return ""
	}
	want := map[string]bool{}
	wikiDom := map[string]bool{}
	var listed []string
	for _, e := range wikiEmails {
		e = strings.ToLower(strings.TrimSpace(e))
		if e == "" || want[e] {
			continue
		}
		want[e] = true
		listed = append(listed, e)
		if d := emailDomain(e); d != "" && !recallFreemail[d] {
			wikiDom[d] = true
		}
	}
	if len(want) == 0 || want[addr] {
		return ""
	}
	if d := emailDomain(addr); d != "" && wikiDom[d] {
		return ""
	}
	return wikiMailConflictMarker + " 위키 " + strings.Join(listed, ", ") + " · 메일 " + addr
}

func parseMailAddress(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if addr, err := mail.ParseAddress(raw); err == nil && addr.Address != "" {
		return strings.ToLower(strings.TrimSpace(addr.Address))
	}
	if found := extractEmails(raw); len(found) > 0 {
		return found[0]
	}
	return ""
}

func extractEmails(s string) []string {
	if s == "" {
		return nil
	}
	return dedupeLower(recallEmailRe.FindAllString(s, -1))
}

func mailFromAddress(s string) string {
	for _, line := range strings.Split(s, "\n") {
		trim := strings.TrimSpace(line)
		lower := strings.ToLower(trim)
		if strings.HasPrefix(lower, "> from:") || strings.HasPrefix(lower, "from:") ||
			strings.Contains(lower, "**발신**") || strings.HasPrefix(lower, "발신") {
			if addr := parseMailAddress(trim); addr != "" {
				return addr
			}
		}
	}
	if found := extractEmails(s); len(found) > 0 {
		return found[0]
	}
	return ""
}

func emailDomain(addr string) string {
	at := strings.LastIndex(addr, "@")
	if at < 0 || at+1 >= len(addr) {
		return ""
	}
	return strings.ToLower(addr[at+1:])
}

func dedupeLower(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range in {
		s = strings.ToLower(strings.TrimSpace(s))
		if s == "" || seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	return out
}
