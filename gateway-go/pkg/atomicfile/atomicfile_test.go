package atomicfile_test

import (
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/pkg/atomicfile"
)

func TestWriteFile_Basic(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.txt")

	err := atomicfile.WriteFile(path, []byte("hello"), nil)
	if err != nil {
		t.Fatal(err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "hello" {
		t.Fatalf("got %q, want %q", got, "hello")
	}
}

func TestWriteFile_CreatesParentDirs(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a", "b", "c", "test.txt")

	err := atomicfile.WriteFile(path, []byte("nested"), nil)
	if err != nil {
		t.Fatal(err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "nested" {
		t.Fatalf("got %q, want %q", got, "nested")
	}
}

func TestWriteFile_OverwriteExisting(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.txt")

	if err := os.WriteFile(path, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}

	err := atomicfile.WriteFile(path, []byte("new"), nil)
	if err != nil {
		t.Fatal(err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "new" {
		t.Fatalf("got %q, want %q", got, "new")
	}
}

func TestWriteFile_Backup(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.txt")

	if err := os.WriteFile(path, []byte("original"), 0o644); err != nil {
		t.Fatal(err)
	}

	err := atomicfile.WriteFile(path, []byte("updated"), &atomicfile.Options{Backup: true})
	if err != nil {
		t.Fatal(err)
	}

	bak, err := os.ReadFile(path + ".bak")
	if err != nil {
		t.Fatal("expected .bak file:", err)
	}
	if string(bak) != "original" {
		t.Fatalf("backup got %q, want %q", bak, "original")
	}
}

func TestWriteFile_Fsync(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.txt")

	err := atomicfile.WriteFile(path, []byte("durable"), &atomicfile.Options{Fsync: true})
	if err != nil {
		t.Fatal(err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "durable" {
		t.Fatalf("got %q, want %q", got, "durable")
	}
}

func TestWriteFile_CustomPerms(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "secure.txt")

	err := atomicfile.WriteFile(path, []byte("secret"), &atomicfile.Options{Perm: 0o600})
	if err != nil {
		t.Fatal(err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("perm got %o, want %o", info.Mode().Perm(), 0o600)
	}
}

func TestWriteFile_ConcurrentSafety(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "concurrent.txt")

	const goroutines = 20
	const iterations = 50

	var wg sync.WaitGroup
	wg.Add(goroutines)

	for g := range goroutines {
		go func(id int) {
			defer wg.Done()
			for i := range iterations {
				data := []byte(filepath.Join("goroutine", string(rune('A'+id)), string(rune('0'+i%10))))
				if err := atomicfile.WriteFile(path, data, nil); err != nil {
					t.Errorf("goroutine %d iter %d: %v", id, i, err)
					return
				}
			}
		}(g)
	}

	wg.Wait()

	// File must exist and be readable (not corrupted / partial).
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) == 0 {
		t.Fatal("file is empty after concurrent writes")
	}
}

func TestWriteFile_NoLeftoverTempFiles(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "clean.txt")

	if err := atomicfile.WriteFile(path, []byte("data"), nil); err != nil {
		t.Fatal(err)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}

	for _, e := range entries {
		name := e.Name()
		if name != "clean.txt" && name != "clean.txt.lock" {
			t.Errorf("unexpected leftover file: %s", name)
		}
	}
}
