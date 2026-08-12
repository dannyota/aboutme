package media

import (
	"bufio"
	"container/heap"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

// fsBackend is the rooted filesystem implementation of Backend: native
// development and every unit test (D10). It has no credentials.
//
// On-disk layout: each object lives at root/<key> as one file whose first
// line is the stored content type followed by the raw object bytes, so an
// object and its content type commit atomically in one hard link. Writes
// stage a temp file at the root (".put-*.tmp", invisible to ListPage) and
// publish it with os.Link, which fails on an existing name — the same
// create-only guarantee the S3 backend gets from If-None-Match, with no
// window in which a reader can observe a partial object.
//
// Divergence from S3, documented and unreachable through D11 keys: the
// filesystem cannot store both "k" and "k/child", because "k" occupies the
// path "k/child" would need as a directory. Such a Put fails as a proved
// non-create; Get and Delete on the shadowed deeper path behave exactly
// like S3 (absent).
type fsBackend struct {
	root string
	dir  *os.Root
}

// NewFS returns the filesystem backend rooted at dir: native development
// and every unit test. It refuses any key that escapes dir after cleaning.
func NewFS(dir string) (Backend, error) {
	if strings.TrimSpace(dir) == "" {
		return nil, errors.New("media: filesystem root directory is required")
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, fmt.Errorf("media: resolving filesystem root: %w", err)
	}
	if err := os.MkdirAll(abs, 0o700); err != nil {
		return nil, fmt.Errorf("media: creating filesystem root: %w", err)
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
	return &fsBackend{root: abs, dir: rootHandle}, nil
}

// tmpPattern names in-flight Put staging files at the root. ListPage skips
// root-level names matching it so a crash-orphaned staging file can never
// surface as an object.
const tmpPattern = ".put-*.tmp"

// objectPath maps a validated key to its absolute path under root. The
// re-root check is a second defense behind validateKey (the
// canonicalization rule): even a hypothetical grammar bypass cannot name a
// path outside root.
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
	// Prove the body's EOF sits exactly at size before touching the disk,
	// so a short or long body leaves no partial object and no residue.
	buf, err := readExact(body, size)
	if err != nil {
		return PutNotCreated, err
	}
	if err := ctx.Err(); err != nil {
		return PutNotCreated, err
	}

	tmp, tmpName, err := b.createTemp()
	if err != nil {
		return PutNotCreated, fmt.Errorf("media: staging object: %w", err)
	}
	defer b.dir.Remove(tmpName) // Always our own file; on success it is the spare link name.
	if _, err := tmp.WriteString(contentType + "\n"); err != nil {
		tmp.Close()
		return PutNotCreated, fmt.Errorf("media: staging object: %w", err)
	}
	if _, err := tmp.Write(buf); err != nil {
		tmp.Close()
		return PutNotCreated, fmt.Errorf("media: staging object: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return PutNotCreated, fmt.Errorf("media: staging object: %w", err)
	}

	if err := b.dir.MkdirAll(filepath.Dir(target), 0o700); err != nil {
		return PutNotCreated, fmt.Errorf("media: creating object directory: %w", err)
	}
	// os.Link is atomic create-only: it can never replace an existing
	// object, and a reader can only ever observe the complete file.
	if err := b.dir.Link(tmpName, target); err != nil {
		if errors.Is(err, fs.ErrExist) {
			if info, statErr := b.dir.Stat(target); statErr == nil && info.IsDir() {
				// The name is a directory for deeper keys, not an object:
				// the mirror image of the documented "k"/"k/child"
				// divergence, equally unreachable through D11 keys.
				return PutNotCreated, fmt.Errorf("media: fs put %q: key names a directory", key)
			}
			return PutNotCreated, fmt.Errorf("media: fs put %q: %w", key, ErrAlreadyExists)
		}
		return PutNotCreated, fmt.Errorf("media: publishing object: %w", err)
	}
	return PutCreated, nil
}

// createTemp opens an exclusive root-relative staging file. os.CreateTemp
// accepts only a path string and could follow a swapped root path; using the
// pinned os.Root handle keeps even staging I/O inside the original root.
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
		f.Close()
		return nil, "", fmt.Errorf("media: fs get %q: %w", key, err)
	}
	if info.IsDir() {
		f.Close()
		return nil, "", ErrNotFound
	}
	reader := bufio.NewReader(f)
	contentType, err := reader.ReadString('\n')
	if err != nil || len(contentType) > maxContentTypeBytes+1 {
		f.Close()
		return nil, "", fmt.Errorf("media: fs get %q: stored object header is corrupt", key)
	}
	return readCloser{Reader: reader, Closer: f}, strings.TrimSuffix(contentType, "\n"), nil
}

// readCloser pairs the header-consumed buffered reader with the file's
// Close.
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
	// Empty parent directories are retained: they are invisible to
	// ListPage and pruning them races concurrent Puts for nothing.
	return nil
}

// isAbsent reports the errors that all mean "no object at this key": the
// file is missing, or some path segment is a regular file so the deeper
// path cannot exist (ENOTDIR — the shadowing case S3 answers with
// NoSuchKey).
func isAbsent(err error) bool {
	return errors.Is(err, fs.ErrNotExist) || errors.Is(err, syscall.ENOTDIR)
}

// fsListQueue orders a directory by its key prefix (including the slash) and
// an object by its complete key. Expanding the smallest directory prefix first
// produces the same full-key byte order as S3 without retaining every object.
type fsListQueue []fsListCandidate

type fsListCandidate struct {
	sortKey string
	path    string
	entry   fs.DirEntry
	isDir   bool
}

func (q fsListQueue) Len() int           { return len(q) }
func (q fsListQueue) Less(i, j int) bool { return q[i].sortKey < q[j].sortKey }
func (q fsListQueue) Swap(i, j int)      { q[i], q[j] = q[j], q[i] }

func (q *fsListQueue) Push(value any) {
	*q = append(*q, value.(fsListCandidate))
}

func (q *fsListQueue) Pop() any {
	old := *q
	last := len(old) - 1
	value := old[last]
	old[last] = fsListCandidate{}
	*q = old[:last]
	return value
}

func (b *fsBackend) ListPage(ctx context.Context, prefix, cursor string, limit int) ([]Object, string, error) {
	if err := validateListPage(ctx, prefix, cursor, limit); err != nil {
		return nil, "", err
	}
	type entry struct {
		key  string
		info fs.FileInfo
	}
	entries := make([]entry, 0, limit+1)
	queue := make(fsListQueue, 0, limit+1)
	enqueueDirectory := func(dir string) error {
		dirEntries, err := fs.ReadDir(b.dir.FS(), dir)
		if err != nil {
			// A directory or file removed mid-walk is not an error for a
			// point-in-time listing.
			if errors.Is(err, fs.ErrNotExist) {
				return nil
			}
			return err
		}
		for _, dirEntry := range dirEntries {
			if err := ctx.Err(); err != nil {
				return err
			}
			// The backend never creates symlinks. Ignoring any pre-existing one
			// prevents local filesystem aliases from becoming object aliases.
			if dirEntry.Type()&fs.ModeSymlink != 0 {
				continue
			}
			key := dirEntry.Name()
			if dir != "." {
				key = dir + "/" + key
			}
			// Skip in-flight/crashed root staging files; they are not objects.
			if dir == "." {
				if matched, _ := filepath.Match(tmpPattern, key); matched {
					continue
				}
			}
			candidate := fsListCandidate{sortKey: key, path: key, entry: dirEntry}
			if dirEntry.IsDir() {
				candidate.sortKey += "/"
				candidate.isDir = true
			}
			heap.Push(&queue, candidate)
		}
		return nil
	}
	if err := enqueueDirectory("."); err != nil {
		return nil, "", fmt.Errorf("media: fs list %q: %w", prefix, err)
	}
	for queue.Len() > 0 && len(entries) < limit+1 {
		if err := ctx.Err(); err != nil {
			return nil, "", fmt.Errorf("media: fs list %q: %w", prefix, err)
		}
		candidate := heap.Pop(&queue).(fsListCandidate)
		if candidate.isDir {
			dirPrefix := candidate.sortKey
			prefixIntersects := strings.HasPrefix(prefix, dirPrefix) || strings.HasPrefix(dirPrefix, prefix)
			cursorInside := strings.HasPrefix(cursor, dirPrefix)
			if prefixIntersects && (cursor == "" || cursor < dirPrefix || cursorInside) {
				if err := enqueueDirectory(candidate.path); err != nil {
					return nil, "", fmt.Errorf("media: fs list %q: %w", prefix, err)
				}
			}
			continue
		}
		key := candidate.path
		if !strings.HasPrefix(key, prefix) || (cursor != "" && key <= cursor) {
			continue
		}
		info, err := candidate.entry.Info()
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				continue
			}
			return nil, "", fmt.Errorf("media: fs list %q: %w", prefix, err)
		}
		entries = append(entries, entry{key: key, info: info})
	}
	objects := make([]Object, 0, min(limit, len(entries)))
	for _, e := range entries[:min(limit, len(entries))] {
		objects = append(objects, Object{Key: e.key, UpdatedAt: e.info.ModTime()})
	}
	nextCursor := ""
	if len(entries) > limit {
		nextCursor = objects[len(objects)-1].Key
	}
	return objects, nextCursor, nil
}
