package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestSecureFile(t *testing.T) {
	directory := t.TempDir()
	if err := atomicPrivateWrite(directory, "private", []byte("value")); err != nil {
		t.Fatal(err)
	}
	info, err := os.Lstat(directory + "/private")
	if err != nil {
		t.Fatal(err)
	}
	if !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %v, want regular 0600", info.Mode())
	}
	contents, err := readPrivateFile(directory, "private")
	if err != nil || string(contents) != "value" {
		t.Fatalf("readPrivateFile = %q, %v", contents, err)
	}
	owner, err := privateFileOwner(os.Getuid())
	if err != nil {
		t.Fatal(err)
	}
	if validPrivateFileInfo(info, owner+1) {
		t.Fatal("private file accepted a different owner")
	}
	if err := atomicPrivateWrite(directory, "../escape", []byte("value")); !errors.Is(err, errUnsafeFile) {
		t.Fatalf("unsafe name error = %v", err)
	}
}

func TestPrivateFileOwnerRejectsInvalidUID(t *testing.T) {
	if _, err := privateFileOwner(-1); !errors.Is(err, errUnsafeFile) {
		t.Fatalf("privateFileOwner(-1) error = %v, want %v", err, errUnsafeFile)
	}
}

func TestReadPrivateFileRejectsUnsafeDescriptorsAndBound(t *testing.T) {
	directory := t.TempDir()
	if err := os.Symlink("/etc/passwd", filepath.Join(directory, "link")); err != nil {
		t.Fatal(err)
	}
	if _, err := readPrivateFile(directory, "link"); !errors.Is(err, errUnsafeFile) {
		t.Fatalf("symlink error = %v, want %v", err, errUnsafeFile)
	}
	if err := os.WriteFile(filepath.Join(directory, "wide"), []byte("value"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(filepath.Join(directory, "wide"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := readPrivateFile(directory, "wide"); !errors.Is(err, errUnsafeFile) {
		t.Fatalf("wide mode error = %v, want %v", err, errUnsafeFile)
	}
	if err := os.WriteFile(filepath.Join(directory, "large"), []byte(strings.Repeat("x", maxPrivateFileBytes+1)), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := readPrivateFile(directory, "large"); !errors.Is(err, errUnsafeFile) {
		t.Fatalf("large file error = %v, want %v", err, errUnsafeFile)
	}
}

func TestReadPrivateFileRejectsFIFOBeforeWriter(t *testing.T) {
	directory := t.TempDir()
	if err := syscall.Mkfifo(filepath.Join(directory, "fifo"), 0o600); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		_, err := readPrivateFile(directory, "fifo")
		done <- err
	}()
	select {
	case err := <-done:
		if !errors.Is(err, errUnsafeFile) {
			t.Fatalf("FIFO error = %v, want %v", err, errUnsafeFile)
		}
	case <-time.After(time.Second):
		t.Fatal("FIFO read waited for a writer")
	}
}
