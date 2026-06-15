package tools

import (
	"context"
	"strings"
	"testing"
)

// gmailRead with neither a message_id nor a query must fail fast (before touching
// the client) with guidance steering the model away from guessing opaque IDs —
// the recurring 404 "not found" / 400 "Invalid id" source in the mail-analysis loop.
func TestGmailRead_RequiresIDorQuery(t *testing.T) {
	_, err := gmailRead(context.Background(), nil, GmailParams{})
	if err == nil {
		t.Fatal("expected error when both message_id and query are empty")
	}
	if !strings.Contains(err.Error(), "message_id 또는 query") {
		t.Errorf("error should guide toward query/exact-ID, got: %v", err)
	}
}
