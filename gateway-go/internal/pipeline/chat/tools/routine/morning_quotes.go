package routine

import (
	"fmt"
	"regexp"
	"strings"
)

// Quote-version grouping for the morning letter. Kia AL 광주/이천/충주
// (and similar site runs) arrive as separate subjects; the raw mail list
// stays, and this card only appears when one site has two or more hits.
var morningQuoteKeyword = regexp.MustCompile(`견적|가견적|재산정|재요청|입찰`)

var morningQuoteSites = []string{"광주", "이천", "충주", "덕평", "캐노피"}

var morningQuoteCompanies = []string{"기아", "현대"}

type morningQuoteGroup struct {
	Site  string
	Items []string
}

func writeMorningQuoteGroups(b *strings.Builder, email emailData) {
	if !email.OK {
		return
	}
	groups := groupMorningQuoteMails(email.Messages)
	for _, group := range groups {
		fmt.Fprintf(b, "  <card>\n    <row><icon name=\"mail\" size=\"16\"/><text style=\"caption\">견적 묶음 · %s · %d건</text></row>\n", morningRaw(group.Site), len(group.Items))
		writeMorningList(b, group.Items, 6)
		b.WriteString("  </card>\n")
	}
}

func groupMorningQuoteMails(messages []emailEntry) []morningQuoteGroup {
	bySite := make(map[string][]string)
	order := make([]string, 0, 4)
	seenSite := make(map[string]bool)

	for _, msg := range messages {
		subj := strings.TrimSpace(msg.Subject)
		if !morningQuoteKeyword.MatchString(subj) {
			continue
		}
		sites := quoteSitesForSubject(subj)
		if len(sites) == 0 {
			continue
		}
		line := emailSenderName(msg.From) + " — " + morningPlain(subj, 90)
		for _, site := range sites {
			if !seenSite[site] {
				seenSite[site] = true
				order = append(order, site)
			}
			bySite[site] = appendUniqueQuoteLine(bySite[site], line)
		}
	}

	out := make([]morningQuoteGroup, 0, len(order))
	for _, site := range order {
		if len(bySite[site]) < 2 {
			continue
		}
		out = append(out, morningQuoteGroup{Site: site, Items: bySite[site]})
	}
	return out
}

func quoteSitesForSubject(subj string) []string {
	var sites []string
	for _, site := range morningQuoteSites {
		if strings.Contains(subj, site) {
			sites = append(sites, site)
		}
	}
	if len(sites) > 0 {
		return sites
	}
	for _, co := range morningQuoteCompanies {
		if strings.Contains(subj, co) {
			sites = append(sites, co)
		}
	}
	return sites
}

func appendUniqueQuoteLine(items []string, line string) []string {
	for _, existing := range items {
		if existing == line {
			return items
		}
	}
	return append(items, line)
}
