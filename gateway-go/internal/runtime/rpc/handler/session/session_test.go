package session

import (
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/rpctest"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/session"
)

var callMethod = rpctest.Call

// ---------------------------------------------------------------------------
// Methods (session management)
// ---------------------------------------------------------------------------

func TestSessionsPatch_missingKey(t *testing.T) {
	m := Methods(Deps{})
	resp := callMethod(m, "sessions.patch", map[string]any{})
	if resp == nil || resp.Error == nil {
		t.Fatal("expected error for missing key")
	}
}

func TestSessionsReset_missingKey(t *testing.T) {
	m := Methods(Deps{})
	resp := callMethod(m, "sessions.reset", map[string]any{})
	if resp == nil || resp.Error == nil {
		t.Fatal("expected error for missing key")
	}
}

// ---------------------------------------------------------------------------
// ExecMethods
// ---------------------------------------------------------------------------

// ---------------------------------------------------------------------------
// CRUDMethods (sessions.delete transcript parity)
// ---------------------------------------------------------------------------

type fakeTranscriptDeleter struct {
	deleted []string
}

func (f *fakeTranscriptDeleter) Delete(sessionKey string) error {
	f.deleted = append(f.deleted, sessionKey)
	return nil
}

// TestSessionsDelete_RemovesTranscript guards the zombie-session fix: the
// generic sessions.delete used to clear only the in-memory row, so the
// surviving .jsonl resurrected the session at the next startup restore.
// It must delete the transcript like miniapp.sessions.delete does.
func TestSessionsDelete_RemovesTranscript(t *testing.T) {
	tr := &fakeTranscriptDeleter{}
	m := CRUDMethods(Deps{
		Sessions:    session.NewManager(),
		Transcripts: func() (TranscriptDeleter, error) { return tr, nil },
	})
	resp := callMethod(m, "sessions.delete", map[string]any{"key": "client:main:abc"})
	if resp == nil || resp.Error != nil {
		t.Fatalf("sessions.delete failed: %+v", resp)
	}
	if len(tr.deleted) != 1 || tr.deleted[0] != "client:main:abc" {
		t.Errorf("transcript not deleted: %v", tr.deleted)
	}
}
