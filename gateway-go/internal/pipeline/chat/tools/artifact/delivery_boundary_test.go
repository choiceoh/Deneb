package artifact

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolport"
)

func writeArtifactFixture(t *testing.T, name string, data []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	return path
}

func callArtifactTool(ctx context.Context, t *testing.T, fn toolport.ToolFunc, params any) (string, error) {
	t.Helper()
	raw, err := json.Marshal(params)
	if err != nil {
		t.Fatalf("marshal params: %v", err)
	}
	return fn(ctx, raw)
}

func deliveryContext(ctx context.Context, delivery *toolport.DeliveryContext, send toolport.MediaSendFunc) context.Context {
	ctx = toolport.WithDeliveryContext(ctx, delivery)
	ctx = toolport.WithMediaSendFunc(ctx, send)
	return ctx
}

func TestResolvePathRejectsSymlinkEscapes(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "secret.txt"), []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "alias")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}

	for _, input := range []string{
		"alias/secret.txt",
		"alias/not-created-yet.txt",
		filepath.Join(link, "secret.txt"),
	} {
		got := ResolvePathWithRoots(input, root, nil)
		want, _ := filepath.Abs(root)
		if got != want {
			t.Errorf("ResolvePathWithRoots(%q) = %q, want jail root %q", input, got, want)
		}
	}

	// An explicitly curated extra root authorizes the symlink target.
	got := ResolvePathWithRoots("alias/secret.txt", root, []string{outside})
	if got != filepath.Join(root, "alias", "secret.txt") {
		t.Fatalf("curated symlink target was clamped: %q", got)
	}
}

func TestResolvePathHandlesSymlinkedRootAndMissingDescendants(t *testing.T) {
	realRoot := t.TempDir()
	parent := t.TempDir()
	rootLink := filepath.Join(parent, "workspace")
	if err := os.Symlink(realRoot, rootLink); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}

	got := ResolvePath("drafts/new/report.md", rootLink)
	want := filepath.Join(rootLink, "drafts", "new", "report.md")
	if got != want {
		t.Fatalf("missing descendant under symlinked root = %q, want %q", got, want)
	}
	canonicalRoot := evalPathForContainment(realRoot)
	if canonical := evalPathForContainment(got); canonical != filepath.Join(canonicalRoot, "drafts", "new", "report.md") {
		t.Fatalf("canonical missing descendant = %q", canonical)
	}
}

func TestPathUnderRootRejectsPrefixCollision(t *testing.T) {
	root := filepath.Join(string(filepath.Separator), "tmp", "workspace")
	for _, tc := range []struct {
		path string
		want bool
	}{
		{path: root, want: true},
		{path: filepath.Join(root, "a"), want: true},
		{path: root + "-evil", want: false},
		{path: filepath.Dir(root), want: false},
		{path: "", want: false},
	} {
		if got := pathUnderRoot(tc.path, root); got != tc.want {
			t.Errorf("pathUnderRoot(%q, %q) = %v", tc.path, root, got)
		}
	}
}

func TestProtectedPathGuardRejectsFileAndParentSymlinkAliases(t *testing.T) {
	secretDir := filepath.Join(t.TempDir(), ".ssh")
	if err := os.MkdirAll(secretDir, 0o700); err != nil {
		t.Fatal(err)
	}
	secret := filepath.Join(secretDir, "id_ed25519")
	if err := os.WriteFile(secret, []byte("PRIVATE"), 0o600); err != nil {
		t.Fatal(err)
	}
	publicDir := t.TempDir()
	fileAlias := filepath.Join(publicDir, "report.txt")
	dirAlias := filepath.Join(publicDir, "documents")
	if err := os.Symlink(secret, fileAlias); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	if err := os.Symlink(secretDir, dirAlias); err != nil {
		t.Fatal(err)
	}

	for _, alias := range []string{fileAlias, filepath.Join(dirAlias, "id_ed25519")} {
		if err := CheckProtectedPath(alias, "send"); err == nil || !strings.Contains(err.Error(), "protected") {
			t.Errorf("alias %q bypassed protected-path guard: %v", alias, err)
		}
	}
}

func TestReadMediaFileBoundaryAndProtection(t *testing.T) {
	path := writeArtifactFixture(t, "audio.bin", []byte("12345"))
	data, err := readMediaFile(path, 5)
	if err != nil || string(data) != "12345" {
		t.Fatalf("exact cap read = %q, %v", data, err)
	}
	if _, err := readMediaFile(path, 4); err == nil || !strings.Contains(err.Error(), "파일이 너무 큽니다") {
		t.Fatalf("over cap err = %v", err)
	}
	if _, err := readMediaFile(filepath.Dir(path), 100); err == nil || !strings.Contains(err.Error(), "디렉토리") {
		t.Fatalf("directory err = %v", err)
	}
	if _, err := readMediaFile("  ", 100); err == nil || !strings.Contains(err.Error(), "path가 필요") {
		t.Fatalf("blank path err = %v", err)
	}

	protected := filepath.Join(t.TempDir(), ".env.production")
	if err := os.WriteFile(protected, []byte("TOKEN=secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(t.TempDir(), "recording.wav")
	if err := os.Symlink(protected, alias); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	if _, err := readMediaFile(alias, 100); err == nil || !strings.Contains(err.Error(), "protected") {
		t.Fatalf("protected media alias err = %v", err)
	}
}

func TestToolSendFileForwardsExactDeliveryContract(t *testing.T) {
	t.Setenv("DENEB_ARCHIVE_SENT_FILES", "0")
	path := writeArtifactFixture(t, "pixel.png", []byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a, 1, 2, 3})
	delivery := &toolport.DeliveryContext{Channel: "telegram", To: "chat-42", AccountID: "primary", ThreadID: "thread-7"}
	type sentCall struct {
		delivery *toolport.DeliveryContext
		path     string
		kind     string
		caption  string
		silent   bool
	}
	var got sentCall
	ctx := deliveryContext(context.Background(), delivery, func(_ context.Context, d *toolport.DeliveryContext, p, kind, caption string, silent bool) error {
		got = sentCall{delivery: d, path: p, kind: kind, caption: caption, silent: silent}
		return nil
	})

	out, err := callArtifactTool(ctx, t, ToolSendFile(), map[string]any{
		"file_path": path,
		"caption":   "정확한 캡션",
		"silent":    true,
	})
	if err != nil {
		t.Fatalf("ToolSendFile: %v", err)
	}
	if got.delivery != delivery || got.path != path || got.kind != "photo" || got.caption != "정확한 캡션" || !got.silent {
		t.Fatalf("send callback mismatch: %+v", got)
	}
	if !strings.Contains(out, "File sent: pixel.png (photo, 11 bytes)") {
		t.Fatalf("result = %q", out)
	}
}

func TestToolSendFileExplicitTypeIgnoresDetectedMediaType(t *testing.T) {
	t.Setenv("DENEB_ARCHIVE_SENT_FILES", "off")
	path := writeArtifactFixture(t, "looks.png", []byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a})
	var gotType string
	ctx := deliveryContext(context.Background(), &toolport.DeliveryContext{Channel: "slack", To: "C1"},
		func(_ context.Context, _ *toolport.DeliveryContext, _, mediaType, _ string, _ bool) error {
			gotType = mediaType
			return nil
		})
	_, err := callArtifactTool(ctx, t, ToolSendFile(), map[string]any{"file_path": path, "type": "document"})
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	if gotType != "document" {
		t.Fatalf("explicit type replaced by %q", gotType)
	}
}

func TestToolSendFileValidationRejectsBeforeInvokingCallback(t *testing.T) {
	t.Setenv("DENEB_ARCHIVE_SENT_FILES", "0")
	path := writeArtifactFixture(t, "large.bin", []byte("123456"))
	var calls atomic.Int32
	send := func(context.Context, *toolport.DeliveryContext, string, string, string, bool) error {
		calls.Add(1)
		return nil
	}
	validDelivery := &toolport.DeliveryContext{Channel: "telegram", To: "chat"}

	tests := []struct {
		name   string
		ctx    context.Context
		params map[string]any
		want   string
	}{
		{name: "blank path", ctx: deliveryContext(context.Background(), validDelivery, send), params: map[string]any{}, want: "file_path is required"},
		{name: "missing file", ctx: deliveryContext(context.Background(), validDelivery, send), params: map[string]any{"file_path": filepath.Join(t.TempDir(), "missing")}, want: "file not found"},
		{name: "directory", ctx: deliveryContext(context.Background(), validDelivery, send), params: map[string]any{"file_path": t.TempDir()}, want: "path is a directory"},
		{name: "size cap", ctx: toolport.WithMaxUploadBytes(deliveryContext(context.Background(), validDelivery, send), 5), params: map[string]any{"file_path": path}, want: "file too large"},
		{name: "no callback", ctx: toolport.WithDeliveryContext(context.Background(), validDelivery), params: map[string]any{"file_path": path}, want: "channel not connected"},
		{name: "nil delivery", ctx: toolport.WithMediaSendFunc(context.Background(), send), params: map[string]any{"file_path": path}, want: "no active delivery target"},
		{name: "empty channel", ctx: deliveryContext(context.Background(), &toolport.DeliveryContext{To: "chat"}, send), params: map[string]any{"file_path": path}, want: "no active delivery target"},
		{name: "empty recipient", ctx: deliveryContext(context.Background(), &toolport.DeliveryContext{Channel: "telegram"}, send), params: map[string]any{"file_path": path}, want: "no active delivery target"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			before := calls.Load()
			_, err := callArtifactTool(tc.ctx, t, ToolSendFile(), tc.params)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want %q", err, tc.want)
			}
			if calls.Load() != before {
				t.Fatalf("validation failure invoked send callback")
			}
		})
	}
}

func TestToolSendFileRejectsProtectedSymlinkBeforeSend(t *testing.T) {
	secretDir := filepath.Join(t.TempDir(), ".deneb", "credentials")
	if err := os.MkdirAll(secretDir, 0o700); err != nil {
		t.Fatal(err)
	}
	secret := filepath.Join(secretDir, "mail.json")
	if err := os.WriteFile(secret, []byte(`{"token":"secret"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(t.TempDir(), "mail-export.json")
	if err := os.Symlink(secret, alias); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	var called bool
	ctx := deliveryContext(context.Background(), &toolport.DeliveryContext{Channel: "x", To: "y"},
		func(context.Context, *toolport.DeliveryContext, string, string, string, bool) error {
			called = true
			return nil
		})
	_, err := callArtifactTool(ctx, t, ToolSendFile(), map[string]any{"file_path": alias})
	if err == nil || !strings.Contains(err.Error(), "protected") {
		t.Fatalf("protected symlink err = %v", err)
	}
	if called {
		t.Fatal("protected symlink reached send callback")
	}
}

func TestToolSendFileReportsExternalFailureWithoutSuccessClaim(t *testing.T) {
	t.Setenv("DENEB_ARCHIVE_SENT_FILES", "0")
	path := writeArtifactFixture(t, "report.pdf", []byte("%PDF-1.7"))
	wantErr := errors.New("remote upload rejected")
	ctx := deliveryContext(context.Background(), &toolport.DeliveryContext{Channel: "teams", To: "room"},
		func(context.Context, *toolport.DeliveryContext, string, string, string, bool) error { return wantErr })
	out, err := callArtifactTool(ctx, t, ToolSendFile(), map[string]any{"file_path": path})
	if err == nil || !strings.Contains(err.Error(), wantErr.Error()) || !strings.Contains(err.Error(), "was not confirmed") {
		t.Fatalf("send failure = out %q, err %v", out, err)
	}
	if strings.Contains(out, "File sent") {
		t.Fatalf("failure claimed success: %q", out)
	}
}

func TestToolSendFilePropagatesCanceledContextToCallback(t *testing.T) {
	t.Setenv("DENEB_ARCHIVE_SENT_FILES", "0")
	path := writeArtifactFixture(t, "report.txt", []byte("report"))
	parent, cancel := context.WithCancel(context.Background())
	cancel()
	callbackSawCanceled := false
	ctx := deliveryContext(parent, &toolport.DeliveryContext{Channel: "mail", To: "user"},
		func(ctx context.Context, _ *toolport.DeliveryContext, _ string, _ string, _ string, _ bool) error {
			callbackSawCanceled = errors.Is(ctx.Err(), context.Canceled)
			return ctx.Err()
		})
	_, err := callArtifactTool(ctx, t, ToolSendFile(), map[string]any{"file_path": path})
	if !callbackSawCanceled {
		t.Fatal("callback did not receive canceled context")
	}
	if err == nil || !strings.Contains(err.Error(), "context canceled") {
		t.Fatalf("cancellation err = %v", err)
	}
}

func TestToolSendFileConcurrentCallsKeepParametersIsolated(t *testing.T) {
	t.Setenv("DENEB_ARCHIVE_SENT_FILES", "0")
	const count = 24
	root := t.TempDir()
	inputs := make([]json.RawMessage, count)
	for i := 0; i < count; i++ {
		name := fmt.Sprintf("report-%02d.txt", i)
		if err := os.WriteFile(filepath.Join(root, name), []byte(name), 0o600); err != nil {
			t.Fatal(err)
		}
		var err error
		inputs[i], err = json.Marshal(map[string]any{
			"file_path": filepath.Join(root, name),
			"caption":   "caption-" + name,
		})
		if err != nil {
			t.Fatalf("marshal input %d: %v", i, err)
		}
	}
	var (
		mu   sync.Mutex
		seen = make(map[string]string, count)
	)
	send := func(_ context.Context, _ *toolport.DeliveryContext, path, _, caption string, _ bool) error {
		mu.Lock()
		seen[filepath.Base(path)] = caption
		mu.Unlock()
		return nil
	}
	ctx := deliveryContext(context.Background(), &toolport.DeliveryContext{Channel: "telegram", To: "chat"}, send)

	var wg sync.WaitGroup
	errs := make(chan error, count)
	for i := 0; i < count; i++ {

		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := ToolSendFile()(ctx, inputs[i])
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent send: %v", err)
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if len(seen) != count {
		t.Fatalf("saw %d callbacks, want %d", len(seen), count)
	}
	for name, caption := range seen {
		if caption != "caption-"+name {
			t.Fatalf("parameter contamination: %s got %q", name, caption)
		}
	}
}

func TestDetectMediaTypeReturnsTypeFromMagicAndVoiceExtension(t *testing.T) {
	fixtures := []struct {
		name string
		data []byte
		want string
	}{
		{name: "image.png", data: []byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a}, want: "photo"},
		{name: "image.jpg", data: []byte{0xff, 0xd8, 0xff, 0xe0}, want: "photo"},
		{name: "note.ogg", data: []byte("OggS-data"), want: "voice"},
		{name: "note.opus", data: []byte("OggS-data"), want: "voice"},
		{name: "music.bin", data: []byte("fLaC-data"), want: "audio"},
		{name: "video.webm", data: []byte{0x1a, 0x45, 0xdf, 0xa3}, want: "video"},
		{name: "unknown.bin", data: []byte("plain text"), want: "document"},
		{name: "empty.bin", data: nil, want: "document"},
	}
	for _, tc := range fixtures {
		t.Run(tc.name, func(t *testing.T) {
			path := writeArtifactFixture(t, tc.name, tc.data)
			if got := detectMediaType(path); got != tc.want {
				t.Fatalf("detectMediaType = %q, want %q", got, tc.want)
			}
		})
	}
	if got := detectMediaType(filepath.Join(t.TempDir(), "missing")); got != "document" {
		t.Fatalf("missing file type = %q", got)
	}
}

func TestFormatWatchDurationClampsExternalNegativeValues(t *testing.T) {
	for _, tc := range []struct {
		seconds int
		want    string
	}{
		{seconds: -1, want: "0:00"},
		{seconds: 0, want: "0:00"},
		{seconds: 65, want: "1:05"},
		{seconds: 3_661, want: "1:01:01"},
	} {
		if got := formatWatchDuration(tc.seconds); got != tc.want {
			t.Errorf("formatWatchDuration(%d) = %q, want %q", tc.seconds, got, tc.want)
		}
	}
}

func TestDeliverRenderedImageStateTransitions(t *testing.T) {
	path := writeArtifactFixture(t, "chart.png", []byte("png"))
	if err := deliverRenderedImage(context.Background(), path, "caption"); err == nil {
		t.Fatal("delivery without channel unexpectedly succeeded")
	}

	delivery := &toolport.DeliveryContext{Channel: "telegram", To: "chat"}
	var calls int
	ctx := deliveryContext(context.Background(), delivery,
		func(_ context.Context, got *toolport.DeliveryContext, gotPath, kind, caption string, silent bool) error {
			calls++
			if got != delivery || gotPath != path || kind != "photo" || caption != "caption" || silent {
				return fmt.Errorf("bad delivery payload")
			}
			return nil
		})
	if err := deliverRenderedImage(ctx, path, "caption"); err != nil {
		t.Fatalf("deliverRenderedImage: %v", err)
	}
	if calls != 1 {
		t.Fatalf("calls = %d", calls)
	}
}

func TestFinishRenderedImageSuccessAndFailureStates(t *testing.T) {
	t.Setenv("DENEB_ARCHIVE_SENT_FILES", "0")
	path := writeArtifactFixture(t, "chart.png", []byte("png"))
	if out := finishRenderedImage(context.Background(), path, "차트", false, "", "제목"); !strings.Contains(out, "send_file") || strings.Contains(out, "전송 완료") {
		t.Fatalf("manual state = %q", out)
	}

	delivery := &toolport.DeliveryContext{Channel: "telegram", To: "chat"}
	var caption string
	ctx := deliveryContext(context.Background(), delivery,
		func(_ context.Context, _ *toolport.DeliveryContext, _, _, gotCaption string, _ bool) error {
			caption = gotCaption
			return nil
		})
	out := finishRenderedImage(ctx, path, "차트", true, "", "기본 제목")
	if !strings.Contains(out, "전송 완료") || caption != "기본 제목" {
		t.Fatalf("success state = %q, caption %q", out, caption)
	}

	failCtx := deliveryContext(context.Background(), delivery,
		func(context.Context, *toolport.DeliveryContext, string, string, string, bool) error {
			return errors.New("channel down")
		})
	out = finishRenderedImage(failCtx, path, "다이어그램", true, "cap", "title")
	if !strings.Contains(out, "자동 전송 실패") || !strings.Contains(out, "send_file") || strings.Contains(out, "전송 완료") {
		t.Fatalf("failure state = %q", out)
	}
}

func TestDeliveryCallbackObservesBoundedSendTimeoutDeadline(t *testing.T) {
	path := writeArtifactFixture(t, "x.txt", []byte("x"))
	ctx := deliveryContext(context.Background(), &toolport.DeliveryContext{Channel: "x", To: "y"},
		func(ctx context.Context, _ *toolport.DeliveryContext, _ string, _ string, _ string, _ bool) error {
			deadline, ok := ctx.Deadline()
			if !ok || time.Until(deadline) <= 0 || time.Until(deadline) > 61*time.Second {
				return fmt.Errorf("missing bounded send deadline")
			}
			return nil
		})
	if _, err := callArtifactTool(ctx, t, ToolSendFile(), map[string]any{"file_path": path}); err != nil {
		t.Fatalf("send deadline: %v", err)
	}
}
