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
}

// ContactsMethods returns the miniapp.contacts.* handler map, or nil when no store
// factory is provided so method_registry can register conditionally.
func ContactsMethods(deps ContactsDeps) map[string]rpcutil.HandlerFunc {
	if deps.Store == nil {
		return nil
	}
	return map[string]rpcutil.HandlerFunc{
		"miniapp.contacts.list":  contactsList(deps),
		"miniapp.contacts.dedup": contactsDedup(deps),
	}
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
