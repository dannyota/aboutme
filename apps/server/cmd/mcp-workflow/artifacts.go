package main

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"
)

const (
	privateDirectoryMode     = 0o700
	privateFileMode          = 0o600
	maxWorkflowArtifactBytes = 4 << 20
)

// Artifact names. The control root holds durable one-shot state that outlives
// a run; the run root holds disposable owner content and evidence. See
// docs/design/mcp-owner-workflow.md#inputs-and-private-runtime.
const (
	lockName            = "workflow.lock"
	createIntentName    = "create-intent.json"
	completionName      = "owner-vi-complete.json"
	revocationName      = "owner-vi-revocation.json"
	sourceName          = "source.json"
	candidateName       = "candidate.json"
	candidateReviewName = "candidate-review.json"
	evidenceName        = "evidence.json"
)

type artifactScope int

const (
	controlScope artifactScope = iota + 1
	runScope
)

// privateArtifacts is the only filesystem boundary for workflow state. Names
// are constants at call sites, so untrusted identifiers never become paths.
type privateArtifacts struct {
	root  string
	scope artifactScope
}

type workflowLock struct{ file *os.File }

func openPrivateArtifacts(root string, scope artifactScope) (privateArtifacts, error) {
	if root == "" || !filepath.IsAbs(root) || filepath.Clean(root) != root || (scope != controlScope && scope != runScope) {
		return privateArtifacts{}, errUnsafeFile
	}
	if err := os.Mkdir(root, privateDirectoryMode); err != nil && !errors.Is(err, fs.ErrExist) { //nolint:gosec // G703: root is an absolute, cleaned launcher path checked above.
		return privateArtifacts{}, errUnsafeFile
	}
	artifacts := privateArtifacts{root: root, scope: scope}
	if !artifacts.validRoot() {
		return privateArtifacts{}, errUnsafeFile
	}
	return artifacts, nil
}

func (a privateArtifacts) allows(name string) bool {
	switch a.scope {
	case controlScope:
		return name == createIntentName || name == completionName || name == revocationName || name == lockName
	case runScope:
		return name == sourceName || name == candidateName || name == candidateReviewName || name == evidenceName
	default:
		return false
	}
}

// write atomically replaces name through a synced temporary file and then
// syncs the directory. An existing entry must already be a safe regular file.
func (a privateArtifacts) write(name string, data []byte) error {
	if !a.validRoot() || !a.allows(name) || name == lockName || len(data) > maxWorkflowArtifactBytes {
		return errUnsafeFile
	}
	if _, err := a.exists(name); err != nil {
		return err
	}
	if err := atomicPrivateWrite(a.root, name, data); err != nil {
		return errUnsafeFile
	}
	return syncDirectory(a.root)
}

// writeExclusiveTemp writes and syncs the temporary file. Tests replace it to
// prove a failed write leaves no final name.
var writeExclusiveTemp = func(file *os.File, data []byte) error {
	if _, err := file.Write(data); err != nil {
		return err
	}
	return file.Sync()
}

// createExclusive publishes name only when it does not exist. It writes and
// syncs a same-directory temporary file, then link(2)s it to the final name,
// which fails if the name exists, so a reader never sees a partial file and
// an existing intent is never replaced. Every path removes the temporary.
func (a privateArtifacts) createExclusive(name string, data []byte) (err error) {
	if !a.validRoot() || !a.allows(name) || name == lockName || len(data) > maxWorkflowArtifactBytes {
		return errUnsafeFile
	}
	temporary, err := os.CreateTemp(a.root, "."+name+"-")
	if err != nil {
		return errUnsafeFile
	}
	temporaryName := temporary.Name()
	closed := false
	defer func() {
		if !closed {
			if closeErr := temporary.Close(); closeErr != nil && err == nil {
				err = errUnsafeFile
			}
		}
		if removeErr := os.Remove(temporaryName); removeErr != nil && !errors.Is(removeErr, fs.ErrNotExist) && err == nil {
			err = errUnsafeFile
		}
		if syncErr := syncDirectory(a.root); syncErr != nil && err == nil {
			err = syncErr
		}
	}()
	if chmodErr := temporary.Chmod(privateFileMode); chmodErr != nil {
		return errUnsafeFile
	}
	if writeErr := writeExclusiveTemp(temporary, data); writeErr != nil {
		return errUnsafeFile
	}
	closed = true
	if closeErr := temporary.Close(); closeErr != nil {
		return errUnsafeFile
	}
	if linkErr := os.Link(temporaryName, filepath.Join(a.root, name)); linkErr != nil {
		return errUnsafeFile
	}
	return nil
}

func (a privateArtifacts) read(name string) (data []byte, err error) {
	if !a.validRoot() || !a.allows(name) || name == lockName {
		return nil, errUnsafeFile
	}
	file, err := os.OpenFile(filepath.Join(a.root, name), os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, errUnsafeFile
	}
	defer func() {
		if closeErr := file.Close(); closeErr != nil {
			data, err = nil, errUnsafeFile
		}
	}()
	info, err := file.Stat()
	if err != nil || !validArtifactInfo(info) || info.Size() > maxWorkflowArtifactBytes {
		return nil, errUnsafeFile
	}
	data, err = io.ReadAll(io.LimitReader(file, maxWorkflowArtifactBytes+1))
	if err != nil || len(data) > maxWorkflowArtifactBytes {
		return nil, errUnsafeFile
	}
	return data, nil
}

// remove deletes a safe regular file and syncs the directory. A missing file
// is already removed.
func (a privateArtifacts) remove(name string) error {
	exists, err := a.exists(name)
	if err != nil || !exists {
		return err
	}
	if removeErr := os.Remove(filepath.Join(a.root, name)); removeErr != nil {
		return errUnsafeFile
	}
	return syncDirectory(a.root)
}

func (a privateArtifacts) exists(name string) (bool, error) {
	if !a.validRoot() || !a.allows(name) {
		return false, errUnsafeFile
	}
	info, err := os.Lstat(filepath.Join(a.root, name))
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil || !validArtifactInfo(info) {
		return false, errUnsafeFile
	}
	return true, nil
}

// lock takes the exclusive, non-blocking control lock that serializes every
// run and recovery against one control root.
func (a privateArtifacts) lock() (*workflowLock, error) {
	if a.scope != controlScope || !a.validRoot() {
		return nil, errUnsafeFile
	}
	file, err := os.OpenFile(filepath.Join(a.root, lockName), os.O_CREATE|os.O_RDWR|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, privateFileMode)
	if err != nil {
		return nil, errUnsafeFile
	}
	info, err := file.Stat()
	if err != nil || !validArtifactInfo(info) || syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB) != nil { //nolint:gosec // a file descriptor always fits in int on Linux.
		if closeErr := file.Close(); closeErr != nil {
			return nil, errUnsafeFile
		}
		return nil, errUnsafeFile
	}
	return &workflowLock{file: file}, nil
}

func (l *workflowLock) close() error {
	if l == nil || l.file == nil {
		return errUnsafeFile
	}
	file := l.file
	l.file = nil
	unlockErr := syscall.Flock(int(file.Fd()), syscall.LOCK_UN) //nolint:gosec // a file descriptor always fits in int on Linux.
	if err := file.Close(); err != nil || unlockErr != nil {
		return errUnsafeFile
	}
	return nil
}

func validArtifactInfo(info fs.FileInfo) bool {
	return info.Mode().IsRegular() && info.Mode() == fs.FileMode(privateFileMode) && ownedByCurrentUser(info)
}

func (a privateArtifacts) validRoot() bool {
	return validPrivateDirectory(a.root)
}

func validPrivateDirectory(path string) bool {
	if path == "" || !filepath.IsAbs(path) {
		return false
	}
	info, err := os.Lstat(path) //nolint:gosec // G703: callers pass only launcher roots or their fixed browser child; lstat never follows a link.
	return err == nil && info.IsDir() && info.Mode() == fs.ModeDir|fs.FileMode(privateDirectoryMode) && ownedByCurrentUser(info)
}

func ownedByCurrentUser(info fs.FileInfo) bool {
	stat, ok := info.Sys().(*syscall.Stat_t)
	owner, err := privateFileOwner(os.Getuid())
	return ok && err == nil && stat.Uid == owner
}

func syncDirectory(path string) (err error) {
	directory, err := os.Open(path) //nolint:gosec // path is a validated private workflow root.
	if err != nil {
		return errUnsafeFile
	}
	defer func() {
		if closeErr := directory.Close(); closeErr != nil && err == nil {
			err = errUnsafeFile
		}
	}()
	if syncErr := directory.Sync(); syncErr != nil {
		return errUnsafeFile
	}
	return nil
}

// removeOwnerContent deletes the run's owner-bearing handoff files. Evidence
// stays.
func removeOwnerContent(run privateArtifacts) error {
	for _, name := range []string{sourceName, candidateName, candidateReviewName} {
		if err := run.remove(name); err != nil {
			return err
		}
	}
	return nil
}

// errRunEntries names a run root holding an entry outside the allowed set.
var errRunEntries = fmt.Errorf("%w: run entries", errWorkflowBlocked)

// syntheticLoginName is the local proof's generated login file. The runner
// only checks its metadata and never opens it.
const syntheticLoginName = "synthetic-login.env"

// maxRunEntries bounds the run-root listing: four handoff and evidence files,
// the browser directory, and the local synthetic login file.
const maxRunEntries = 6

// validateRunEntries rejects any run-root entry other than the handoff files,
// evidence, and the real browser directory, such as a stray credential file,
// a symbolic link, or a leftover temporary file. Local mode alone also admits
// the synthetic login file when it is a regular mode-0600 file owned by the
// current user. The read is bounded.
func validateRunEntries(run privateArtifacts, browserRoot string, local bool) (err error) {
	if !run.validRoot() || browserRoot != filepath.Join(run.root, browserDirectory) || !validPrivateDirectory(browserRoot) {
		return errRunEntries
	}
	directory, err := os.Open(run.root) //nolint:gosec // run.root is a validated private workflow root.
	if err != nil {
		return errRunEntries
	}
	defer func() {
		if closeErr := directory.Close(); closeErr != nil && err == nil {
			err = errRunEntries
		}
	}()
	entries, readErr := directory.ReadDir(maxRunEntries + 1)
	if (readErr != nil && !errors.Is(readErr, io.EOF)) || len(entries) > maxRunEntries {
		return errRunEntries
	}
	for _, entry := range entries {
		switch {
		case entry.Name() == browserDirectory && entry.Type() == fs.ModeDir:
		case run.allows(entry.Name()) && entry.Type().IsRegular():
		case local && entry.Name() == syntheticLoginName && validSyntheticLogin(filepath.Join(run.root, syntheticLoginName)):
		default:
			return errRunEntries
		}
	}
	return nil
}

// validSyntheticLogin checks metadata only, through lstat.
func validSyntheticLogin(path string) bool {
	info, err := os.Lstat(path)
	return err == nil && validArtifactInfo(info)
}
