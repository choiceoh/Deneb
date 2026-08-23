// person_dossier.go — miniapp.person.dossier RPC handler.
//
// The per-person business dossier join the people index deliberately does not
// do (docs/research/improvement-ideas.md 4.8): one person's recent mail
// rollup, phone-side call/notification history (phoneledger, matched by
// name), and wiki pages that mention them (full-text search — the same
// recall the agent uses). Mail needs an address; calls need a name; the wiki
// identity resolves an 인물 page from either. Every source degrades
// independently so a missing Gmail config or an empty ledger never blanks
// the whole dossier.

package knowledge

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/core/rpcerr"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/contacts"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/phoneledger"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/handler/minibind"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/rpcutil"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

const (
	// dossierMailWindowDays bounds the mail rollup; the people index uses 30,
	// a dossier drill-in earns a deeper look.
	dossierMailWindowDays = 90
	dossierMailScanMax    = 25
	dossierSubjectsMax    = 8
	dossierCallsMax       = 10
	// dossierCallBudgetRunes: full 30-day ledger tail budget; the name filter
	// narrows after the read.
	dossierCallBudgetRunes = 60_000
	dossierWikiRefsMax     = 10
)

// PersonDossierOut is one person's cross-source dossier. Sections are
// independently optional — an email-only person has no calls, a wiki-only
// person has no mail.
//
//deneb:wire
type PersonDossierOut struct {
	Email       string `json:"email"`
	Name        string `json:"name,omitempty"`
	WikiPath    string `json:"wikiPath,omitempty"`
	WikiSummary string `json:"wikiSummary,omitempty"`
	// Mail rollup over the window (0 when no address / Gmail unavailable).
	MailCount      int      `json:"mailCount"`
	LastMailAt     string   `json:"lastMailAt,omitempty"` // ISO 8601
	RecentSubjects []string `json:"recentSubjects,omitempty"`
	// Calls: phoneledger entries (통화·메시지 알림) mentioning the person,
	// newest first. Empty without a resolvable name or a ledger dir.
	Calls []PersonDossierCallOut `json:"calls,omitempty"`
	// WikiRefs: wiki pages mentioning the person (full-text search), the
	// 인물 page itself excluded.
	WikiRefs []PersonDossierWikiRefOut `json:"wikiRefs,omitempty"`
}

//deneb:wire
type PersonDossierCallOut struct {
	TS     string `json:"ts"`
	Type   string `json:"type,omitempty"`
	Source string `json:"source,omitempty"`
	Text   string `json:"text"`
}

//deneb:wire
type PersonDossierWikiRefOut struct {
	Path    string `json:"path"`
	Summary string `json:"summary,omitempty"` // matching snippet, rune-truncated
}

func personDossier(deps PeopleDeps) rpcutil.HandlerFunc {
	type params struct {
		Email string `json:"email,omitempty"`
		Name  string `json:"name,omitempty"`
	}
	return minibind.BindOptional[params](func(ctx context.Context, req *protocol.RequestFrame, p params) *protocol.ResponseFrame {
		email := strings.ToLower(strings.TrimSpace(p.Email))
		name := strings.TrimSpace(p.Name)
		if email == "" && len([]rune(name)) < 2 {
			return rpcerr.MissingParam("email or name").Response(req.ID)
		}

		out := PersonDossierOut{Email: email, Name: name}

		// Wiki identity first: an 인물 page match resolves the canonical name
		// (page title), which the call matcher below needs.
		people := loadWikiPeople(deps.WikiStore)
		if idx, ok := matchWikiPerson(people, email, name); ok {
			out.WikiPath = people[idx].path
			out.WikiSummary = people[idx].summary
			if people[idx].title != "" {
				out.Name = people[idx].title
			}
		}
		if out.Name == "" {
			out.Name = name
		}

		if email != "" && deps.Client != nil {
			if client, err := deps.Client(); err == nil {
				fillDossierMail(ctx, client, email, &out)
			}
		}
		if out.Name != "" && deps.PhoneLedgerDir != "" {
			fillDossierCalls(deps.PhoneLedgerDir, contacts.NormalizePersonName(out.Name), &out)
		}
		fillDossierWikiRefs(ctx, deps.WikiStore, out.Name, out.WikiPath, &out)

		return rpcutil.RespondOK(req.ID, map[string]any{"dossier": out})
	})
}

// matchWikiPerson finds the 인물 page for an address (연락처 email, strong)
// or a normalized display name — the same matching rules mergeWikiPeople
// applies to the index rows, factored out so both surfaces agree.
func matchWikiPerson(people []wikiPerson, email, name string) (int, bool) {
	if len(people) == 0 {
		return 0, false
	}
	if email != "" {
		for i, wp := range people {
			for _, e := range wp.emails {
				if e == email {
					return i, true
				}
			}
		}
	}
	key := contacts.NormalizePersonName(name)
	if len([]rune(key)) < 2 {
		return 0, false
	}
	for i, wp := range people {
		if contacts.NormalizePersonName(wp.title) == key {
			return i, true
		}
	}
	return 0, false
}

func fillDossierMail(ctx context.Context, client PeopleClient, email string, out *PersonDossierOut) {
	query := "from:" + email + " newer_than:" + strconv.Itoa(dossierMailWindowDays) + "d"
	msgs, err := client.Search(ctx, query, dossierMailScanMax)
	if err != nil || len(msgs) == 0 {
		return
	}
	out.MailCount = len(msgs)
	for _, m := range msgs {
		if when := parseMessageTime(m.Date); when.After(parseMessageTime(out.LastMailAt)) {
			out.LastMailAt = when.UTC().Format(time.RFC3339)
		}
	}
	// Newest-first subjects (Search returns newest-first), deduped, capped.
	seen := map[string]bool{}
	for _, m := range msgs {
		subject := truncateRunes(strings.TrimSpace(m.Subject), maxPeopleSubjectPreview)
		if subject == "" || seen[subject] {
			continue
		}
		seen[subject] = true
		out.RecentSubjects = append(out.RecentSubjects, subject)
		if len(out.RecentSubjects) >= dossierSubjectsMax {
			break
		}
	}
}

func fillDossierCalls(ledgerDir, nameKey string, out *PersonDossierOut) {
	tail, err := phoneledger.ReadTail(ledgerDir, nil, dossierCallBudgetRunes)
	if err != nil || tail == nil {
		return
	}
	var calls []PersonDossierCallOut
	for i := len(tail.Entries) - 1; i >= 0 && len(calls) < dossierCallsMax; i-- {
		e := tail.Entries[i]
		source := contacts.NormalizePersonName(e.Source)
		if source != nameKey && !strings.Contains(contacts.NormalizePersonName(e.Text), nameKey) {
			continue
		}
		calls = append(calls, PersonDossierCallOut{
			TS: e.TS, Type: e.Type, Source: e.Source,
			Text: truncateRunes(e.Text, 120),
		})
	}
	out.Calls = calls
}

func fillDossierWikiRefs(ctx context.Context, storeFn func() (MemorySearcher, error), name, ownPath string, out *PersonDossierOut) {
	if len([]rune(name)) < 2 || storeFn == nil {
		return
	}
	store, err := storeFn()
	if err != nil || store == nil {
		return
	}
	results, err := store.Search(ctx, name, dossierWikiRefsMax*2)
	if err != nil {
		return
	}
	for _, r := range results {
		if len(out.WikiRefs) >= dossierWikiRefsMax {
			break
		}
		if r.Path == ownPath || r.Path == "" {
			continue
		}
		out.WikiRefs = append(out.WikiRefs, PersonDossierWikiRefOut{
			Path:    r.Path,
			Summary: truncateRunes(strings.TrimSpace(r.Content), 120),
		})
	}
}
