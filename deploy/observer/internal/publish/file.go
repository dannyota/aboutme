package publish

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/dannyota/aboutme/deploy/observer/internal/document"
)

// FilePublisher atomically replaces deployment.json in one existing directory.
type FilePublisher struct {
	directory string
	syncFile  func(*os.File) error
	syncDir   func(*os.File) error
	rename    func(string, string) error
}

// NewFilePublisher returns a publisher for the host's public directory.
func NewFilePublisher(directory string) *FilePublisher {
	return &FilePublisher{
		directory: directory,
		syncFile:  (*os.File).Sync,
		syncDir:   (*os.File).Sync,
		rename:    os.Rename,
	}
}

// Put fsyncs a complete temporary file before rename. A directory fsync error
// after rename is reported even though the complete new document is visible.
func (p *FilePublisher) Put(ctx context.Context, body []byte) (retErr error) {
	if len(body) > document.MaxBytes {
		return errors.New("document exceeds size limit")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	dir, err := os.Open(filepath.Clean(p.directory))
	if err != nil {
		return fmt.Errorf("open output directory: %w", err)
	}
	defer func() {
		if closeErr := dir.Close(); closeErr != nil {
			retErr = errors.Join(retErr, fmt.Errorf("close output directory: %w", closeErr))
		}
	}()
	f, err := os.CreateTemp(p.directory, ".deployment.*")
	if err != nil {
		return fmt.Errorf("create document temporary file: %w", err)
	}
	closed, renamed := false, false
	defer func() {
		if !closed {
			if closeErr := f.Close(); closeErr != nil {
				retErr = errors.Join(retErr, fmt.Errorf("close document temporary file: %w", closeErr))
			}
		}
		if !renamed {
			if removeErr := os.Remove(f.Name()); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
				retErr = errors.Join(retErr, fmt.Errorf("remove document temporary file: %w", removeErr))
			}
		}
	}()
	n, err := f.Write(body)
	if err != nil {
		return fmt.Errorf("write document: %w", err)
	}
	if n != len(body) {
		return io.ErrShortWrite
	}
	if err := f.Chmod(0o644); err != nil {
		return fmt.Errorf("set document permissions: %w", err)
	}
	if err := p.syncFile(f); err != nil {
		return fmt.Errorf("sync document: %w", err)
	}
	closeErr := f.Close()
	closed = true
	if closeErr != nil {
		return fmt.Errorf("close document: %w", closeErr)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := p.rename(f.Name(), filepath.Join(p.directory, "deployment.json")); err != nil {
		return fmt.Errorf("replace document: %w", err)
	}
	renamed = true
	if err := p.syncDir(dir); err != nil {
		return fmt.Errorf("sync output directory after replace: %w", err)
	}
	return nil
}
