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

var writeExclusiveTemp = func(file *os.File, data []byte) error {
	if _, err := file.Write(data); err != nil {
		return err
	}
	return file.Sync()
}

// link(2) fails when the target name exists, so a reader never sees a partial file.
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

func removeOwnerContent(run privateArtifacts) error {
	for _, name := range []string{sourceName, candidateName, candidateReviewName} {
		if err := run.remove(name); err != nil {
			return err
		}
	}
	return nil
}

var errRunEntries = fmt.Errorf("%w: run entries", errWorkflowBlocked)

const syntheticLoginName = "synthetic-login.env"

const maxRunEntries = 6

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

func validSyntheticLogin(path string) bool {
	info, err := os.Lstat(path)
	return err == nil && validArtifactInfo(info)
}
