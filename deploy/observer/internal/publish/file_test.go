package publish

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/dannyota/aboutme/deploy/observer/internal/document"
)

func TestFilePublisherAtomicReplace(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "deployment.json")
	if err := os.WriteFile(path, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	old, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if closeErr := old.Close(); closeErr != nil {
			t.Error(closeErr)
		}
	})
	p := NewFilePublisher(dir)
	p.rename = func(from, to string) error {
		body, readErr := os.ReadFile(to)
		if readErr != nil || string(body) != "old" {
			t.Fatal("previous document changed before rename")
		}
		if filepath.Dir(from) != dir || to != path {
			t.Fatal("temporary file is not beside deployment.json")
		}
		return os.Rename(from, to)
	}
	if err = p.Put(t.Context(), []byte("new")); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(path)
	if err != nil || string(body) != "new" {
		t.Fatalf("replacement: %q, %v", body, err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o644 {
		t.Fatalf("permissions: %v, %v", info, err)
	}
	oldBody := make([]byte, 3)
	if _, err := old.Read(oldBody); err != nil || string(oldBody) != "old" {
		t.Fatal("replacement modified the open previous document")
	}
	assertNoTemporary(t, dir)
}

func TestFilePublisherFailureKeepsPrevious(t *testing.T) {
	for _, failure := range []string{"file sync", "rename", "oversize", "canceled"} {
		t.Run(failure, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "deployment.json")
			if err := os.WriteFile(path, []byte("old"), 0o644); err != nil {
				t.Fatal(err)
			}
			p := NewFilePublisher(dir)
			body := []byte("new")
			ctx := t.Context()
			switch failure {
			case "file sync":
				p.syncFile = func(*os.File) error { return errors.New("sync failed") }
			case "rename":
				p.rename = func(string, string) error { return errors.New("rename failed") }
			case "oversize":
				body = bytes.Repeat([]byte("x"), document.MaxBytes+1)
			case "canceled":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}
			if err := p.Put(ctx, body); err == nil {
				t.Fatal("expected failure")
			}
			got, err := os.ReadFile(path)
			if err != nil || string(got) != "old" {
				t.Fatal("failed publication changed previous document")
			}
			assertNoTemporary(t, dir)
		})
	}
}

func TestFilePublisherDirectorySyncFailure(t *testing.T) {
	dir := t.TempDir()
	p := NewFilePublisher(dir)
	p.syncDir = func(*os.File) error { return errors.New("directory sync failed") }
	if err := p.Put(t.Context(), []byte("complete")); err == nil {
		t.Fatal("directory sync failure was ignored")
	}
	body, err := os.ReadFile(filepath.Join(dir, "deployment.json"))
	if err != nil || string(body) != "complete" {
		t.Fatal("document is incomplete after rename")
	}
	assertNoTemporary(t, dir)
}

func TestFilePublisherMissingDirectory(t *testing.T) {
	if err := NewFilePublisher(filepath.Join(t.TempDir(), "missing")).Put(t.Context(), []byte("new")); err == nil {
		t.Fatal("accepted missing output directory")
	}
}

func TestFilePublisherSizeLimit(t *testing.T) {
	dir := t.TempDir()
	if err := NewFilePublisher(dir).Put(t.Context(), bytes.Repeat([]byte("x"), document.MaxBytes)); err != nil {
		t.Fatal(err)
	}
}

func assertNoTemporary(t *testing.T, dir string) {
	t.Helper()
	paths, err := filepath.Glob(filepath.Join(dir, ".deployment.*"))
	if err != nil || len(paths) != 0 {
		t.Fatalf("temporary files remain: %v, %v", paths, err)
	}
}
