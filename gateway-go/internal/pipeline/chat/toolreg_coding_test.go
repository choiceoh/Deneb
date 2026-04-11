package chat

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	chattools "github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/tools"
	"github.com/choiceoh/deneb/gateway-go/internal/testutil"
)

func TestToolDiff(t *testing.T) {
	// diffFiles mode doesn't require git — test it directly.
	dir := t.TempDir()

	t.Run("files mode identical", func(t *testing.T) {
		path1 := filepath.Join(dir, "a.txt")
		path2 := filepath.Join(dir, "b.txt")
		os.WriteFile(path1, []byte("same content"), 0o644)
		os.WriteFile(path2, []byte("same content"), 0o644)

		fn := chattools.ToolDiff(dir)
		input, _ := json.Marshal(map[string]any{
			"mode": "files",
			"path": path1,
			"ref2": path2,
		})
		result := testutil.Must(fn(context.Background(), input))
		if !strings.Contains(result, "identical") {
			t.Errorf("expected identical message, got: %s", result)
		}
	})

	t.Run("files mode different", func(t *testing.T) {
		path1 := filepath.Join(dir, "c.txt")
		path2 := filepath.Join(dir, "d.txt")
		os.WriteFile(path1, []byte("line1\nline2\n"), 0o644)
		os.WriteFile(path2, []byte("line1\nline3\n"), 0o644)

		fn := chattools.ToolDiff(dir)
		input, _ := json.Marshal(map[string]any{
			"mode": "files",
			"path": path1,
			"ref2": path2,
		})
		result := testutil.Must(fn(context.Background(), input))
		if !strings.Contains(result, "line2") || !strings.Contains(result, "line3") {
			t.Errorf("expected diff content, got: %s", result)
		}
	})

	t.Run("files mode missing path", func(t *testing.T) {
		fn := chattools.ToolDiff(dir)
		input, _ := json.Marshal(map[string]any{
			"mode": "files",
			"path": "",
			"ref2": "",
		})
		_, err := fn(context.Background(), input)
		if err == nil {
			t.Fatal("expected error for missing paths")
		}
	})

	t.Run("unknown mode", func(t *testing.T) {
		fn := chattools.ToolDiff(dir)
		input, _ := json.Marshal(map[string]any{"mode": "bogus"})
		_, err := fn(context.Background(), input)
		if err == nil {
			t.Fatal("expected error for unknown mode")
		}
	})
}
