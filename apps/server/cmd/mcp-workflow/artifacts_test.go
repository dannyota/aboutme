package main

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func testArtifacts(t *testing.T, scope artifactScope) privateArtifacts {
	t.Helper()
	artifacts, err := openPrivateArtifacts(filepath.Join(t.TempDir(), "private"), scope)
	if err != nil {
		t.Fatal(err)
	}
	return artifacts
}

func TestArtifactsCreateReopenReplaceAndRemove(t *testing.T) {
	run := testArtifacts(t, runScope)
	for _, want := range []string{"first", "second"} {
		if writeErr := run.write(sourceName, []byte(want)); writeErr != nil {
			t.Fatal(writeErr)
		}
		reopened, err := openPrivateArtifacts(run.root, runScope)
		if err != nil {
			t.Fatal(err)
		}
		got, err := reopened.read(sourceName)
		if err != nil || string(got) != want {
			t.Fatalf("read = %q, %v", got, err)
		}
	}
	entries, err := os.ReadDir(run.root)
	if err != nil || len(entries) != 1 {
		t.Fatalf("atomic replacement left %d entries: %v", len(entries), err)
	}
	info, err := os.Lstat(filepath.Join(run.root, sourceName))
	if err != nil || info.Mode() != fs.FileMode(privateFileMode) {
		t.Fatalf("mode = %v, %v", info.Mode(), err)
	}
	if removeErr := run.remove(sourceName); removeErr != nil {
		t.Fatal(removeErr)
	}
	if present, existsErr := run.exists(sourceName); existsErr != nil || present {
		t.Fatalf("removed artifact present=%t err=%v", present, existsErr)
	}
}

func TestArtifactsRejectUnsafeEntries(t *testing.T) {
	run := testArtifacts(t, runScope)
	path := filepath.Join(run.root, candidateName)
	for name, setup := range map[string]func(t *testing.T){
		"group readable": func(t *testing.T) { writeRaw(t, path, 0o640, "value") },
		"sticky bit": func(t *testing.T) {
			writeRaw(t, path, 0o600, "value")
			if err := os.Chmod(path, 0o600|fs.ModeSticky); err != nil {
				t.Fatal(err)
			}
		},
		"symlink": func(t *testing.T) {
			writeRaw(t, filepath.Join(run.root, sourceName), 0o600, "value")
			if err := os.Symlink(sourceName, path); err != nil {
				t.Fatal(err)
			}
		},
		"fifo": func(t *testing.T) {
			if err := syscall.Mkfifo(path, 0o600); err != nil {
				t.Fatal(err)
			}
		},
		"directory": func(t *testing.T) {
			if err := os.Mkdir(path, 0o700); err != nil {
				t.Fatal(err)
			}
		},
		"oversized": func(t *testing.T) { writeRaw(t, path, 0o600, strings.Repeat("x", maxWorkflowArtifactBytes+1)) },
	} {
		t.Run(name, func(t *testing.T) {
			for _, entry := range []string{path, filepath.Join(run.root, sourceName)} {
				if err := os.RemoveAll(entry); err != nil {
					t.Fatal(err)
				}
			}
			setup(t)
			if _, err := run.read(candidateName); !errors.Is(err, errUnsafeFile) {
				t.Fatalf("read err = %v", err)
			}
			if err := run.write(candidateName, []byte("replacement")); name != "oversized" && !errors.Is(err, errUnsafeFile) {
				t.Fatalf("write over unsafe entry err = %v", err)
			}
		})
	}
}

func writeRaw(t *testing.T, path string, mode os.FileMode, value string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(value), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, mode); err != nil {
		t.Fatal(err)
	}
}

func TestArtifactsRejectWrongOwner(t *testing.T) {
	info := ownerInfo{uid: uint32(os.Getuid()) + 1}
	if validArtifactInfo(info) {
		t.Fatal("accepted a file owned by another user")
	}
	info.uid = uint32(os.Getuid())
	if !validArtifactInfo(info) {
		t.Fatal("rejected a file owned by the current user")
	}
}

type ownerInfo struct{ uid uint32 }

func (ownerInfo) Name() string       { return "owned" }
func (ownerInfo) Size() int64        { return 1 }
func (ownerInfo) Mode() fs.FileMode  { return privateFileMode }
func (ownerInfo) ModTime() time.Time { return time.Time{} }
func (ownerInfo) IsDir() bool        { return false }
func (i ownerInfo) Sys() any         { return &syscall.Stat_t{Uid: i.uid} }

func TestArtifactsRejectNamesOutsideScopeAndReplacedRoot(t *testing.T) {
	run := testArtifacts(t, runScope)
	control := testArtifacts(t, controlScope)
	for _, name := range []string{"unapproved.json", "../source.json", createIntentName, completionName, lockName} {
		if err := run.write(name, []byte("{}")); !errors.Is(err, errUnsafeFile) {
			t.Fatalf("run scope accepted %q: %v", name, err)
		}
	}
	for _, name := range []string{sourceName, candidateName, evidenceName, lockName} {
		if err := control.write(name, []byte("{}")); !errors.Is(err, errUnsafeFile) {
			t.Fatalf("control scope accepted %q: %v", name, err)
		}
	}
	if err := os.Remove(run.root); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(t.TempDir(), run.root); err != nil {
		t.Fatal(err)
	}
	if err := run.write(sourceName, []byte("{}")); !errors.Is(err, errUnsafeFile) {
		t.Fatalf("replaced root err = %v", err)
	}
}

func TestArtifactsRejectUnsafeRoot(t *testing.T) {
	base := t.TempDir()
	loose := filepath.Join(base, "loose")
	if err := os.Mkdir(loose, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(loose, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, root := range []string{"", "relative", loose, base + "/./private"} {
		if _, err := openPrivateArtifacts(root, runScope); !errors.Is(err, errUnsafeFile) {
			t.Fatalf("root %q err = %v", root, err)
		}
	}
}

func TestArtifactsCreateExclusiveNeverReplaces(t *testing.T) {
	control := testArtifacts(t, controlScope)
	if err := control.createExclusive(createIntentName, []byte("first")); err != nil {
		t.Fatal(err)
	}
	if err := control.createExclusive(createIntentName, []byte("second")); !errors.Is(err, errUnsafeFile) {
		t.Fatalf("second create err = %v", err)
	}
	if got, err := control.read(createIntentName); err != nil || string(got) != "first" {
		t.Fatalf("intent = %q, %v", got, err)
	}
}

func TestArtifactsControlLockIsExclusive(t *testing.T) {
	control := testArtifacts(t, controlScope)
	lock, err := control.lock()
	if err != nil {
		t.Fatal(err)
	}
	if second, secondErr := control.lock(); secondErr == nil {
		if closeErr := second.close(); closeErr != nil {
			t.Fatal(closeErr)
		}
		t.Fatal("second lock acquired while the first is held")
	}
	if closeErr := lock.close(); closeErr != nil {
		t.Fatal(closeErr)
	}
	again, err := control.lock()
	if err != nil {
		t.Fatal(err)
	}
	if closeErr := again.close(); closeErr != nil {
		t.Fatal(closeErr)
	}
	if _, runLockErr := testArtifacts(t, runScope).lock(); !errors.Is(runLockErr, errUnsafeFile) {
		t.Fatal("run scope granted a control lock")
	}
}

func TestArtifactsRejectExtraRunEntriesBeforeAnyWork(t *testing.T) {
	for name, setup := range map[string]func(t *testing.T, root string){
		"credential file": func(t *testing.T, root string) { writeRaw(t, filepath.Join(root, "login.env"), 0o600, "x") },
		"temporary file":  func(t *testing.T, root string) { writeRaw(t, filepath.Join(root, ".source.json-1"), 0o600, "x") },
		"symlink": func(t *testing.T, root string) {
			if err := os.Symlink("/etc/passwd", filepath.Join(root, sourceName)); err != nil {
				t.Fatal(err)
			}
		},
		"nested directory": func(t *testing.T, root string) {
			if err := os.Mkdir(filepath.Join(root, "extra"), 0o700); err != nil {
				t.Fatal(err)
			}
		},
	} {
		t.Run(name, func(t *testing.T) {
			h := newWorkflowHarness(t, modeProduction, false)
			if err := validateRunEntries(h.config.Run, h.config.BrowserRoot, false); err != nil {
				t.Fatalf("clean run root rejected: %v", err)
			}
			setup(t, h.config.Run.root)
			if _, runErr := h.run(); !errors.Is(runErr, errWorkflowBlocked) || h.connects != 0 {
				t.Fatalf("run err = %v connects = %d", runErr, h.connects)
			}
		})
	}
}

func TestArtifactsSyntheticLoginIsLocalOnlyAndMetadataChecked(t *testing.T) {
	for name, test := range map[string]struct {
		mode    string
		setup   func(t *testing.T, path string)
		allowed bool
	}{
		"local regular 0600":   {modeLocal, func(t *testing.T, path string) { writeRaw(t, path, 0o600, "x") }, true},
		"local group readable": {modeLocal, func(t *testing.T, path string) { writeRaw(t, path, 0o640, "x") }, false},
		"local symlink": {modeLocal, func(t *testing.T, path string) {
			if err := os.Symlink("/etc/hostname", path); err != nil {
				t.Fatal(err)
			}
		}, false},
		"local directory": {modeLocal, func(t *testing.T, path string) {
			if err := os.Mkdir(path, 0o700); err != nil {
				t.Fatal(err)
			}
		}, false},
		"production regular 0600": {modeProduction, func(t *testing.T, path string) { writeRaw(t, path, 0o600, "x") }, false},
	} {
		t.Run(name, func(t *testing.T) {
			h := newWorkflowHarness(t, test.mode, false)
			test.setup(t, filepath.Join(h.config.Run.root, syntheticLoginName))
			err := validateRunEntries(h.config.Run, h.config.BrowserRoot, h.config.local())
			if (err == nil) != test.allowed {
				t.Fatalf("err = %v, allowed = %t", err, test.allowed)
			}
			if !test.allowed {
				if _, runErr := h.run(); !errors.Is(runErr, errWorkflowBlocked) || h.connects != 0 {
					t.Fatalf("run err = %v connects = %d", runErr, h.connects)
				}
			}
		})
	}
}

func TestArtifactsLocalRunCompletesBesideSyntheticLogin(t *testing.T) {
	h := newWorkflowHarness(t, modeLocal, true)
	path := filepath.Join(h.config.Run.root, syntheticLoginName)
	writeRaw(t, path, 0o600, "synthetic")
	if _, runErr := h.run(); runErr != nil {
		t.Fatal(runErr)
	}
	if info, err := os.Lstat(path); err != nil || info.Mode() != fs.FileMode(privateFileMode) {
		t.Fatal("runner changed the synthetic login file")
	}
}

func TestArtifactsCreateExclusiveFailureLeavesNoFinalOrTemporaryName(t *testing.T) {
	control := testArtifacts(t, controlScope)
	original := writeExclusiveTemp
	writeExclusiveTemp = func(file *os.File, data []byte) error {
		if _, err := file.Write(data[:len(data)/2]); err != nil {
			return err
		}
		return errFakeLost
	}
	t.Cleanup(func() { writeExclusiveTemp = original })
	if failedErr := control.createExclusive(createIntentName, []byte("complete payload")); !errors.Is(failedErr, errUnsafeFile) {
		t.Fatalf("err = %v", failedErr)
	}
	entries, readErr := os.ReadDir(control.root)
	if readErr != nil || len(entries) != 0 {
		t.Fatalf("failed create left %d entries: %v", len(entries), readErr)
	}
	writeExclusiveTemp = original
	if createErr := control.createExclusive(createIntentName, []byte("complete payload")); createErr != nil {
		t.Fatal(createErr)
	}
	if got, intentErr := control.read(createIntentName); intentErr != nil || string(got) != "complete payload" {
		t.Fatalf("intent = %q, %v", got, intentErr)
	}
	entries, readErr = os.ReadDir(control.root)
	if readErr != nil || len(entries) != 1 {
		t.Fatalf("successful create left %d entries: %v", len(entries), readErr)
	}
}
