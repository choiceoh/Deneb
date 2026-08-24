package modelpicker

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/infra/config"
)

// TestEveryPickerRoleIsPersistable binds the third list. A role the picker
// offers but PersistRoleModel does not map has a working button that fails on
// save — the exact shape of every past drift.
func TestEveryPickerRoleIsPersistable(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	for _, r := range pickerRoles {
		t.Run(string(r.role), func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "deneb.json")
			if err := config.PersistRoleModel(path, string(r.role), "provider/model", logger); err != nil {
				t.Fatalf("PersistRoleModel(%q): %v", r.role, err)
			}
		})
	}
}

// nativeRoleEnum matches one ModelRole entry: NAME("wire", "label", "desc").
var nativeRoleEnum = regexp.MustCompile(`(?m)^\s{4}[A-Z_]+\("([a-z0-9_]+)",`)

// TestNativePickerEnumMatchesPickerRoles binds the fourth list. The native enum
// is the only one that cannot derive from pickerRoles at compile time, so it is
// checked by reading the file: a role added on one side and not the other is a
// build-green feature that does not work on the phone.
func TestNativePickerEnumMatchesPickerRoles(t *testing.T) {
	path := filepath.Join("..", "..", "..", "..",
		"client-android", "app", "composeApp", "src", "commonMain",
		"kotlin", "ai", "deneb", "deneb", "ConfigModelTab.kt")
	data, err := os.ReadFile(path)
	if err != nil {
		// A moved file must fail loudly rather than silently stop checking.
		t.Fatalf("read native picker %s: %v", path, err)
	}

	start := regexp.MustCompile(`private enum class ModelRole\(`).FindIndex(data)
	if start == nil {
		t.Fatal("ModelRole enum not found; update this test with the new declaration")
	}
	body := data[start[1]:]
	if end := regexp.MustCompile(`(?m)^\}`).FindIndex(body); end != nil {
		body = body[:end[0]]
	}

	var native []string
	for _, m := range nativeRoleEnum.FindAllSubmatch(body, -1) {
		native = append(native, string(m[1]))
	}
	if len(native) == 0 {
		t.Fatal("parsed no wire keys from the ModelRole enum; the declaration shape changed")
	}

	want := make([]string, 0, len(pickerRoles))
	for _, r := range pickerRoles {
		want = append(want, string(r.role))
	}
	if len(native) != len(want) {
		t.Fatalf("native ModelRole = %v, gateway pickerRoles = %v", native, want)
	}
	for i := range want {
		if native[i] != want[i] {
			t.Fatalf("role %d: native %q, gateway %q (full: native=%v gateway=%v)", i, native[i], want[i], native, want)
		}
	}
}

// TestPickerWriteGateAcceptsExactlyPickerRoles keeps the write gate from
// re-growing its own list.
func TestPickerWriteGateAcceptsExactlyPickerRoles(t *testing.T) {
	for _, r := range pickerRoles {
		if !isPickerRole(string(r.role)) {
			t.Errorf("write gate rejects offered role %q", r.role)
		}
	}
	for _, role := range []string{"", "main2", "analysis", "chatbot", "nonsense"} {
		if isPickerRole(role) {
			t.Errorf("write gate accepts unoffered role %q", role)
		}
	}
}
