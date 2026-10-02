package remote

import (
	"os"
	"path/filepath"
	"testing"
)

// TestCopyFileSameSourceAndDestination guards against a real incident: calling CopyFile(p, p, ...)
// used to truncate p to empty before reading it, destroying the file it was asked to preserve.
// This happens whenever a caller names an artifact already in the store by its own store path
// (e.g. deploy.buildLayer given a binary that is itself already content-addressed at its
// destination), not just when a literal duplicate path is passed deliberately.
func TestCopyFileSameSourceAndDestination(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "artifact")
	want := []byte("some real binary content, not empty")
	if err := os.WriteFile(path, want, 0644); err != nil {
		t.Fatal(err)
	}

	if err := CopyFile(path, path, 0644); err != nil {
		t.Fatalf("CopyFile(path, path): %v", err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(want) {
		t.Fatalf("content changed after self-copy: got %q, want %q", got, want)
	}
}

func TestCopyFileDifferentPaths(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	dst := filepath.Join(dir, "nested", "dst")
	want := []byte("payload")
	if err := os.WriteFile(src, want, 0644); err != nil {
		t.Fatal(err)
	}

	if err := CopyFile(src, dst, 0644); err != nil {
		t.Fatalf("CopyFile: %v", err)
	}

	got, err := os.ReadFile(dst)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(want) {
		t.Fatalf("got %q, want %q", got, want)
	}
}
