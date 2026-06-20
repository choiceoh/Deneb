// contacts.go — miniapp.contacts.* RPC handlers.
//
// Exposes the device address-book mirror (contacts.json, synced via
// miniapp.capture.contacts) as a read-only list for the native 전체 연락처 browser.
// Distinct from miniapp.people.list — that is the Gmail-counterparty + 인물-wiki
// directory ranked by message volume ("who's writing me a lot"); this is the raw,
// complete address book, which the client sections alphabetically (ㄱㄴㄷ). The
// client owns filtering/sorting, so this returns the whole list unsorted.
// UNAVAILABLE when the contacts store isn't configured.

package handlerminiapp

import (
	"context"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/contacts"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/rpcerr"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/rpcutil"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

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
		"miniapp.contacts.list": contactsList(deps),
	}
}

// ContactRow is one address-book entry on the wire (mirrors contacts.Contact so the
// handler owns the JSON shape independently of the domain type).
//
//deneb:wire
type ContactRow struct {
	Name   string   `json:"name"`
	Phones []string `json:"phones,omitempty"`
	Emails []string `json:"emails,omitempty"`
	Org    string   `json:"org,omitempty"`
}

func contactsList(deps ContactsDeps) rpcutil.HandlerFunc {
	type out struct {
		Contacts []ContactRow `json:"contacts"`
		Count    int          `json:"count"`
	}
	return func(ctx context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		if errResp := requireAuth(ctx, req.ID); errResp != nil {
			return errResp
		}
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
	}
}
