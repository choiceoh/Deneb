// contacts.go — miniapp.contacts.* RPC handlers.
//
// Exposes the device address-book mirror (contacts.json, synced via
// miniapp.capture.contacts) as a read-only list for the native 전체 연락처 browser.
// Distinct from miniapp.people.list — that is the Gmail-counterparty + 인물-wiki
// directory ranked by message volume ("who's writing me a lot"); this is the raw,
// complete address book, which the client sections alphabetically (ㄱㄴㄷ). The
// client owns filtering/sorting, so this returns the whole list unsorted.
// UNAVAILABLE when the contacts store isn't configured.

package contactsapi

import (
	"context"

	"github.com/choiceoh/deneb/gateway-go/internal/core/rpcerr"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/contacts"
	miniappcontract "github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/handler/handlerminiapp/contract"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/handler/minibind"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/rpcutil"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

type ContactRow = miniappcontract.ContactRow

// ContactsDeps holds the lazy contacts-store factory. Same UNAVAILABLE-per-call
// pattern as the other domains: an unconfigured store surfaces the right error
// instead of crashing the gateway at boot.
type ContactsDeps struct {
	Store func() (*contacts.Store, error)
	// Adjudicator resolves dedup ambiguous pairs with an LLM. Optional: when nil
	// (no model configured), miniapp.contacts.adjudicate is not registered — the
	// deterministic miniapp.contacts.dedup still is.
	Adjudicator contacts.Adjudicator
}

// ContactsMethods returns the miniapp.contacts.* handler map, or nil when no store
// factory is provided so method_registry can register conditionally.
func ContactsMethods(deps ContactsDeps) map[string]rpcutil.HandlerFunc {
	if deps.Store == nil {
		return nil
	}
	methods := map[string]rpcutil.HandlerFunc{
		"miniapp.contacts.list":  contactsList(deps),
		"miniapp.contacts.dedup": contactsDedup(deps),
	}
	if deps.Adjudicator != nil {
		methods["miniapp.contacts.adjudicate"] = contactsAdjudicate(deps)
	}
	return methods
}

func contactsList(deps ContactsDeps) rpcutil.HandlerFunc {
	type out struct {
		Contacts []ContactRow `json:"contacts"`
		Count    int          `json:"count"`
	}
	return minibind.Authenticated(func(ctx context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		store, err := deps.Store()
		if err != nil {
			return rpcerr.Unavailable("contacts store unavailable").Response(req.ID)
		}
		all := store.All()
		rows := make([]ContactRow, 0, len(all))
		for _, c := range all {
			rows = append(rows, ContactRow{Name: c.Name, Phones: c.Phones, Emails: c.Emails, Org: c.Org})
		}
		return rpcutil.RespondOK(req.ID, out{Contacts: rows, Count: len(rows)})
	})
}

// contactsDedup runs the DETERMINISTIC dedup pass over the mirror and returns the
// safe merge groups (each with the cleanest name + the union of phones/emails) so
// the client can preview the cleanup. It never mutates and never calls the model:
// the ambiguous pairs are only counted here — the LLM/operator review is a
// separate, slower pass — so this stays a fast, synchronous RPC.
func contactsDedup(deps ContactsDeps) rpcutil.HandlerFunc {
	type mergeOut struct {
		Canonical string   `json:"canonical"`
		Names     []string `json:"names"`
		Phones    []string `json:"phones"`
		Emails    []string `json:"emails"`
	}
	type out struct {
		Total     int        `json:"total"`     // address-book entries in
		Distinct  int        `json:"distinct"`  // people after the safe merges
		Ambiguous int        `json:"ambiguous"` // pairs left for AI/operator review
		Merges    []mergeOut `json:"merges"`
	}
	return minibind.Authenticated(func(ctx context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		store, err := deps.Store()
		if err != nil {
			return rpcerr.Unavailable("contacts store unavailable").Response(req.ID)
		}
		all := store.All()
		res := contacts.Dedup(all)
		merges := make([]mergeOut, 0, len(res.Merges))
		for _, g := range res.Merges {
			m := mergeOut{Canonical: g.Canonical}
			seenP := map[string]bool{}
			seenE := map[string]bool{}
			for _, idx := range g.Members {
				m.Names = append(m.Names, all[idx].Name)
				for _, p := range all[idx].Phones {
					if !seenP[p] {
						seenP[p] = true
						m.Phones = append(m.Phones, p)
					}
				}
				for _, e := range all[idx].Emails {
					if !seenE[e] {
						seenE[e] = true
						m.Emails = append(m.Emails, e)
					}
				}
			}
			merges = append(merges, m)
		}
		return rpcutil.RespondOK(req.ID, out{
			Total:     res.Total,
			Distinct:  res.Distinct,
			Ambiguous: len(res.Ambiguous),
			Merges:    merges,
		})
	})
}

// maxAdjudicatePairs bounds one adjudicate call so the request stays within the
// client's timeout (each pair costs a slice of an LLM batch). The client works
// through the ambiguous list in chunks rather than one multi-minute request.
const maxAdjudicatePairs = 32

// contactsAdjudicate rules on a bounded batch of ambiguous pairs with the LLM,
// returning one verdict per pair ("same"/"diff"/"unsure"). Stateless: the client
// sends the pairs it got from miniapp.contacts.dedup and applies the verdicts.
// Only registered when an Adjudicator is wired.
func contactsAdjudicate(deps ContactsDeps) rpcutil.HandlerFunc {
	type partyIn struct {
		Name   string   `json:"name"`
		Org    string   `json:"org"`
		Phones []string `json:"phones"`
		Emails []string `json:"emails"`
	}
	type pairIn struct {
		A      partyIn `json:"a"`
		B      partyIn `json:"b"`
		Shared string  `json:"shared"`
	}
	type params struct {
		Pairs []pairIn `json:"pairs"`
	}
	type out struct {
		Verdicts []string `json:"verdicts"`
	}
	return minibind.Bind(func(ctx context.Context, req *protocol.RequestFrame, p params) *protocol.ResponseFrame {
		if len(p.Pairs) > maxAdjudicatePairs {
			return rpcerr.InvalidRequest("too many pairs (max 32 per call)").Response(req.ID)
		}
		if len(p.Pairs) == 0 {
			return rpcutil.RespondOK(req.ID, out{Verdicts: []string{}})
		}
		cs := make([]contacts.Contact, 0, 2*len(p.Pairs))
		pairs := make([]contacts.AmbiguousPair, len(p.Pairs))
		for i, pr := range p.Pairs {
			aIdx, bIdx := len(cs), len(cs)+1
			cs = append(
				cs,
				contacts.Contact{Name: pr.A.Name, Org: pr.A.Org, Phones: pr.A.Phones, Emails: pr.A.Emails},
				contacts.Contact{Name: pr.B.Name, Org: pr.B.Org, Phones: pr.B.Phones, Emails: pr.B.Emails},
			)
			pairs[i] = contacts.AmbiguousPair{A: aIdx, B: bIdx, Shared: pr.Shared}
		}
		verdicts, err := deps.Adjudicator.Adjudicate(ctx, cs, pairs)
		if err != nil {
			return rpcerr.Unavailable("contacts adjudication unavailable").Response(req.ID)
		}
		res := make([]string, len(pairs))
		for i := range res {
			res[i] = "unsure"
			if i < len(verdicts) {
				res[i] = verdictString(verdicts[i])
			}
		}
		return rpcutil.RespondOK(req.ID, out{Verdicts: res})
	})
}

func verdictString(v contacts.Verdict) string {
	switch v {
	case contacts.VerdictSame:
		return "same"
	case contacts.VerdictDifferent:
		return "diff"
	default:
		return "unsure"
	}
}
