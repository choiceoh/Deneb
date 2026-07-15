package analyzebind

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/infra/clientauth"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/gmail"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

// fakeGmailClient implements GmailClient with function fields so each test
// can wire up exactly the behavior it needs. Deliberately duplicated from
// the sibling gmailops package's test helper of the same name — Go test
// files aren't importable across packages, and this leaf's tests only need
// GetMessage, so the fake stays small.
type fakeGmailClient struct {
	getMessageFn func(ctx context.Context, id string) (*gmail.MessageDetail, error)
}

func (f *fakeGmailClient) Search(context.Context, string, int) ([]gmail.MessageSummary, error) {
	return nil, errors.New("Search not stubbed")
}

func (f *fakeGmailClient) SearchPage(context.Context, string, string, int) ([]gmail.MessageSummary, string, error) {
	return nil, "", errors.New("SearchPage not stubbed")
}

func (f *fakeGmailClient) GetMessage(ctx context.Context, id string) (*gmail.MessageDetail, error) {
	if f.getMessageFn == nil {
		return nil, errors.New("GetMessage not stubbed")
	}
	return f.getMessageFn(ctx, id)
}

func (f *fakeGmailClient) ModifyLabels(context.Context, string, []string, []string) error {
	return errors.New("ModifyLabels not stubbed")
}

func (f *fakeGmailClient) Trash(context.Context, string) error {
	return errors.New("Trash not stubbed")
}

func authedCtx() context.Context {
	return clientauth.WithContext(context.Background(), &clientauth.Identity{
		User: &clientauth.User{ID: 42, FirstName: "Tester"},
	})
}

func reqWith(t *testing.T, method string, params any) *protocol.RequestFrame {
	t.Helper()
	req, err := protocol.NewRequestFrame("t-1", method, params)
	if err != nil {
		t.Fatalf("NewRequestFrame: %v", err)
	}
	return req
}

func decode(t *testing.T, resp *protocol.ResponseFrame, dest any) {
	t.Helper()
	if resp == nil {
		t.Fatal("nil response")
	}
	if !resp.OK {
		t.Fatalf("response not OK: code=%s message=%s", resp.Error.Code, resp.Error.Message)
	}
	if err := json.Unmarshal(resp.Payload, dest); err != nil {
		t.Fatalf("decode payload: %v (raw=%s)", err, string(resp.Payload))
	}
}
