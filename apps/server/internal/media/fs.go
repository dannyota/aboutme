package media

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"syscall"
)

type fsBackend struct {
	root string
	dir  *os.Root

	indexMu sync.RWMutex
	index   []Object

	visitIndexedObjectForTest func()
}

func NewFS(dir string) (Backend, error) {
	if strings.TrimSpace(dir) == "" {
		return nil, errors.New("media: filesystem root directory is required")
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, fmt.Errorf("media: resolving filesystem root: %w", err)
	}
	if mkdirErr := os.MkdirAll(abs, 0o700); mkdirErr != nil {
		return nil, fmt.Errorf("media: creating filesystem root: %w", mkdirErr)
	}
	info, err := os.Stat(abs)
	if err != nil {
		return nil, fmt.Errorf("media: inspecting filesystem root: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("media: filesystem root %q is not a directory", abs)
	}
	rootHandle, err := os.OpenRoot(abs)
	if err != nil {
		return nil, fmt.Errorf("media: opening filesystem root: %w", err)
	}
	b := &fsBackend{root: abs, dir: rootHandle}
	if err := b.loadIndex(); err != nil {
		if closeErr := rootHandle.Close(); closeErr != nil {
			err = errors.Join(err, closeErr)
		}
		return nil, fmt.Errorf("media: indexing filesystem root: %w", err)
	}
	return b, nil
}

const tmpPattern = ".put-*.tmp"

func (b *fsBackend) objectPath(key string) (string, error) {
	p := filepath.Join(b.root, filepath.FromSlash(key))
	rel, err := filepath.Rel(b.root, p)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("%w: key escapes the storage root", ErrInvalidKey)
	}
	return p, nil
}

func (b *fsBackend) Put(ctx context.Context, key, contentType string, body io.Reader, size int64) (PutOutcome, error) {
	if err := validatePut(ctx, key, contentType, size); err != nil {
		return PutNotCreated, err
	}
	_, err := b.objectPath(key)
	if err != nil {
		return PutNotCreated, err
	}
	target := filepath.FromSlash(key)
	buf, err := readExact(body, size)
	if err != nil {
		return PutNotCreated, err
	}
	if contextErr := ctx.Err(); contextErr != nil {
		return PutNotCreated, contextErr
	}

	tmp, tmpName, err := b.createTemp()
	if err != nil {
		return PutNotCreated, fmt.Errorf("media: staging object: %w", err)
	}
	defer func() {
		if cleanupErr := b.dir.Remove(tmpName); cleanupErr != nil && !errors.Is(cleanupErr, fs.ErrNotExist) {
			return
		}
	}()
	if _, writeErr := tmp.WriteString(contentType + "\n"); writeErr != nil {
		err = writeErr
		if closeErr := tmp.Close(); closeErr != nil {
			err = errors.Join(err, closeErr)
		}
		return PutNotCreated, fmt.Errorf("media: staging object: %w", err)
	}
	if _, writeErr := tmp.Write(buf); writeErr != nil {
		err = writeErr
		if closeErr := tmp.Close(); closeErr != nil {
			err = errors.Join(err, closeErr)
		}
		return PutNotCreated, fmt.Errorf("media: staging object: %w", err)
	}
	if closeErr := tmp.Close(); closeErr != nil {
		return PutNotCreated, fmt.Errorf("media: staging object: %w", closeErr)
	}

	if mkdirErr := b.dir.MkdirAll(filepath.Dir(target), 0o700); mkdirErr != nil {
		return PutNotCreated, fmt.Errorf("media: creating object directory: %w", mkdirErr)
	}
	if linkErr := b.dir.Link(tmpName, target); linkErr != nil {
		if errors.Is(linkErr, fs.ErrExist) {
			if info, statErr := b.dir.Stat(target); statErr == nil && info.IsDir() {
				return PutNotCreated, fmt.Errorf("media: fs put %q: key names a directory", key)
			}
			return PutNotCreated, fmt.Errorf("media: fs put %q: %w", key, ErrAlreadyExists)
		}
		return PutNotCreated, fmt.Errorf("media: publishing object: %w", linkErr)
	}
	info, err := b.dir.Stat(target)
	if err != nil {
		b.insertIndex(Object{Key: key})
		return PutUnknown, fmt.Errorf("media: inspecting published object: %w", err)
	}
	b.insertIndex(Object{Key: key, UpdatedAt: info.ModTime()})
	return PutCreated, nil
}

func (b *fsBackend) createTemp() (*os.File, string, error) {
	var random [16]byte
	for range 100 {
		if _, err := io.ReadFull(rand.Reader, random[:]); err != nil {
			return nil, "", err
		}
		name := ".put-" + hex.EncodeToString(random[:]) + ".tmp"
		f, err := b.dir.OpenFile(name, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0o600)
		if err == nil {
			return f, name, nil
		}
		if !errors.Is(err, fs.ErrExist) {
			return nil, "", err
		}
	}
	return nil, "", errors.New("could not allocate a unique staging name")
}

func (b *fsBackend) Get(ctx context.Context, key string) (io.ReadCloser, string, error) {
	if err := validateKey(key); err != nil {
		return nil, "", err
	}
	if err := ctx.Err(); err != nil {
		return nil, "", err
	}
	_, err := b.objectPath(key)
	if err != nil {
		return nil, "", err
	}
	f, err := b.dir.Open(filepath.FromSlash(key))
	if err != nil {
		if isAbsent(err) {
			return nil, "", ErrNotFound
		}
		return nil, "", fmt.Errorf("media: fs get %q: %w", key, err)
	}
	info, err := f.Stat()
	if err != nil {
		if closeErr := f.Close(); closeErr != nil {
			err = errors.Join(err, closeErr)
		}
		return nil, "", fmt.Errorf("media: fs get %q: %w", key, err)
	}
	if info.IsDir() {
		if closeErr := f.Close(); closeErr != nil {
			return nil, "", fmt.Errorf("media: fs get %q: close directory: %w", key, closeErr)
		}
		return nil, "", ErrNotFound
	}
	reader := bufio.NewReader(f)
	contentType, err := reader.ReadString('\n')
	if err != nil || len(contentType) > maxContentTypeBytes+1 {
		if closeErr := f.Close(); closeErr != nil {
			return nil, "", fmt.Errorf("media: fs get %q: close corrupt object: %w", key, closeErr)
		}
		return nil, "", fmt.Errorf("media: fs get %q: stored object header is corrupt", key)
	}
	return readCloser{Reader: reader, Closer: f}, strings.TrimSuffix(contentType, "\n"), nil
}

type readCloser struct {
	io.Reader
	io.Closer
}

func (b *fsBackend) Delete(ctx context.Context, key string) error {
	if err := validateKey(key); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	_, err := b.objectPath(key)
	if err != nil {
		return err
	}
	target := filepath.FromSlash(key)
	info, err := b.dir.Stat(target)
	if err != nil {
		if isAbsent(err) {
			return ErrNotFound
		}
		return fmt.Errorf("media: fs delete %q: %w", key, err)
	}
	if info.IsDir() {
		return ErrNotFound
	}
	if err := b.dir.Remove(target); err != nil {
		if isAbsent(err) {
			return ErrNotFound
		}
		return fmt.Errorf("media: fs delete %q: %w", key, err)
	}
	b.deleteIndex(key)
	return nil
}

// ENOTDIR is the shadowing case that S3 answers with NoSuchKey.
func isAbsent(err error) bool {
	return errors.Is(err, fs.ErrNotExist) || errors.Is(err, syscall.ENOTDIR)
}

func (b *fsBackend) loadIndex() error {
	objects := make([]Object, 0)
	err := fs.WalkDir(b.dir.FS(), ".", func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == "." || entry.IsDir() {
			return nil
		}
		if entry.Type()&fs.ModeSymlink != 0 {
			return nil
		}
		matched, err := filepath.Match(tmpPattern, path)
		if err != nil {
			return err
		}
		if matched {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		objects = append(objects, Object{Key: filepath.ToSlash(path), UpdatedAt: info.ModTime()})
		return nil
	})
	if err != nil {
		return err
	}
	sort.Slice(objects, func(i, j int) bool { return objects[i].Key < objects[j].Key })
	b.index = objects
	return nil
}

func (b *fsBackend) insertIndex(object Object) {
	b.indexMu.Lock()
	defer b.indexMu.Unlock()
	position := sort.Search(len(b.index), func(i int) bool { return b.index[i].Key >= object.Key })
	if position < len(b.index) && b.index[position].Key == object.Key {
		b.index[position] = object
		return
	}
	b.index = append(b.index, Object{})
	copy(b.index[position+1:], b.index[position:])
	b.index[position] = object
}

func (b *fsBackend) deleteIndex(key string) {
	b.indexMu.Lock()
	defer b.indexMu.Unlock()
	position := sort.Search(len(b.index), func(i int) bool { return b.index[i].Key >= key })
	if position == len(b.index) || b.index[position].Key != key {
		return
	}
	copy(b.index[position:], b.index[position+1:])
	b.index[len(b.index)-1] = Object{}
	b.index = b.index[:len(b.index)-1]
}

func (b *fsBackend) ListPage(ctx context.Context, prefix, cursor string, limit int) ([]Object, string, error) {
	if err := validateListPage(ctx, prefix, cursor, limit); err != nil {
		return nil, "", err
	}
	startKey := prefix
	exclusive := false
	if cursor != "" && cursor >= startKey {
		startKey = cursor
		exclusive = true
	}

	b.indexMu.RLock()
	defer b.indexMu.RUnlock()
	start := sort.Search(len(b.index), func(i int) bool { return b.index[i].Key >= startKey })
	if exclusive && start < len(b.index) && b.index[start].Key == cursor {
		start++
	}
	window := make([]Object, 0, limit+1)
	for i := start; i < len(b.index) && len(window) < limit+1; i++ {
		if err := ctx.Err(); err != nil {
			return nil, "", fmt.Errorf("media: fs list %q: %w", prefix, err)
		}
		if !strings.HasPrefix(b.index[i].Key, prefix) {
			break
		}
		if b.visitIndexedObjectForTest != nil {
			b.visitIndexedObjectForTest()
		}
		window = append(window, b.index[i])
	}
	objects := append([]Object(nil), window[:min(limit, len(window))]...)
	nextCursor := ""
	if len(window) > limit {
		nextCursor = objects[len(objects)-1].Key
	}
	return objects, nextCursor, nil
}
