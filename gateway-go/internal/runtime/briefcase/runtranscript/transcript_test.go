package runtranscript_test

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chatport"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/briefcase"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/briefcase/runtranscript"
)

func TestRunTranscriptPreservesSameSessionHistoryThroughPolaris(t *testing.T) {
	ambientHome := t.TempDir()
	t.Setenv("HOME", ambientHome)
	root, err := briefcase.NewRunRoot(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	paths, err := root.Paths()
	if err != nil {
		t.Fatal(err)
	}

	transcript, err := runtranscript.NewRunTranscript(runtranscript.Paths{Root: paths.Root, State: paths.State}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer transcript.Close()
	bridge := transcript.Bridge()
	if bridge == nil {
		t.Fatal("RunTranscript returned a nil Polaris bridge")
	}

	const sessionKey = "bench:case:memory-assisted:nonce"
	if err := bridge.Append(sessionKey, chatport.NewTextChatMessage("user", "remember alpha", 1_000)); err != nil {
		t.Fatal(err)
	}
	if err := bridge.Append(sessionKey, chatport.NewTextChatMessage("assistant", "alpha is retained", 2_000)); err != nil {
		t.Fatal(err)
	}
	if err := bridge.Append("bench:other", chatport.NewTextChatMessage("user", "must stay isolated", 3_000)); err != nil {
		t.Fatal(err)
	}

	assembled, err := bridge.AssembleContext(sessionKey, 8_192, 24, discardTestLogger())
	if err != nil {
		t.Fatal(err)
	}
	if assembled.TotalMessages != 2 || len(assembled.Messages) != 2 {
		t.Fatalf("assembled context = %+v, want two same-session messages", assembled)
	}
	got := make([]string, len(assembled.Messages))
	for i, msg := range assembled.Messages {
		if err := json.Unmarshal(msg.Content.Bytes(), &got[i]); err != nil {
			t.Fatalf("decode assembled message %d: %v", i, err)
		}
	}
	if strings.Join(got, "|") != "remember alpha|alpha is retained" {
		t.Fatalf("assembled messages = %q", got)
	}

	for _, dir := range []string{
		filepath.Join(paths.State, "transcripts"),
		filepath.Join(paths.State, "polaris"),
		filepath.Join(paths.State, "polaris", "messages"),
		filepath.Join(paths.State, "polaris", "summaries"),
	} {
		info, err := os.Stat(dir)
		if err != nil {
			t.Fatalf("stat %s: %v", dir, err)
		}
		if !info.IsDir() || info.Mode().Perm() != 0o700 {
			t.Fatalf("directory %s mode = %v", dir, info.Mode())
		}
	}
	if _, err := os.Stat(filepath.Join(ambientHome, ".deneb")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("RunTranscript touched ambient HOME: %v", err)
	}
}

func TestRunTranscriptPersistsHistoryWithinSameRunRoot(t *testing.T) {
	root, err := briefcase.NewRunRoot(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	paths, _ := root.Paths()

	first, err := runtranscript.NewRunTranscript(runtranscript.Paths{Root: paths.Root, State: paths.State}, nil)
	if err != nil {
		t.Fatal(err)
	}
	const sessionKey = "bench:resume"
	if err := first.Bridge().Append(sessionKey, chatport.NewTextChatMessage("user", "persisted turn", 1_000)); err != nil {
		t.Fatal(err)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	if err := first.Close(); err != nil {
		t.Fatalf("idempotent close: %v", err)
	}

	second, err := runtranscript.NewRunTranscript(runtranscript.Paths{Root: paths.Root, State: paths.State}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	assembled, err := second.Bridge().AssembleContext(sessionKey, 8_192, 24, discardTestLogger())
	if err != nil {
		t.Fatal(err)
	}
	if assembled.TotalMessages != 1 || len(assembled.Messages) != 1 || !strings.Contains(assembled.Messages[0].Content.String(), "persisted turn") {
		t.Fatalf("reopened context = %+v", assembled)
	}
}

func TestRunTranscriptFailsClosedWhenPolarisAppendFails(t *testing.T) {
	root, err := briefcase.NewRunRoot(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	paths, err := root.Paths()
	if err != nil {
		t.Fatal(err)
	}

	transcript, err := runtranscript.NewRunTranscript(runtranscript.Paths{Root: paths.Root, State: paths.State}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer transcript.Close()

	const sessionKey = "strict-persistence"
	messagePath := filepath.Join(paths.State, "polaris", "messages", sessionKey+".jsonl")
	if err := os.Mkdir(messagePath, 0o700); err != nil {
		t.Fatalf("block Polaris append path: %v", err)
	}
	err = transcript.Bridge().Append(sessionKey, chatport.NewTextChatMessage("user", "must fail closed", 1_000))
	if err == nil || !strings.Contains(err.Error(), "strict dual-write append failed") {
		t.Fatalf("Append error = %v, want strict Polaris persistence failure", err)
	}
}

func discardTestLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestRunTranscriptRejectsNonRunRootStateAndSymlinks(t *testing.T) {
	root, err := briefcase.NewRunRoot(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	paths, _ := root.Paths()

	outside := t.TempDir()
	forged := paths
	forged.State = outside
	if _, err := runtranscript.NewRunTranscript(runtranscript.Paths{Root: forged.Root, State: forged.State}, nil); err == nil || !strings.Contains(err.Error(), "not owned") {
		t.Fatalf("forged State error = %v", err)
	}

	link := filepath.Join(paths.State, "transcripts")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	if _, err := runtranscript.NewRunTranscript(runtranscript.Paths{Root: paths.Root, State: paths.State}, nil); err == nil || !strings.Contains(err.Error(), "real directory") {
		t.Fatalf("symlink transcript error = %v", err)
	}
	if _, err := os.Stat(filepath.Join(outside, "polaris")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("helper touched outside state: %v", err)
	}
}
