package media

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
)

func newFSBackend(t *testing.T) (*fsBackend, string) {
	t.Helper()
	root := t.TempDir()
	b, err := NewFS(root)
	if err != nil {
		t.Fatalf("NewFS: %v", err)
	}
	backend, ok := b.(*fsBackend)
	if !ok {
		t.Fatalf("NewFS returned %T, want *fsBackend", b)
	}
	return backend, root
}

func closeInternalTestBody(t *testing.T, body io.Closer) {
	t.Helper()
	if err := body.Close(); err != nil {
		t.Errorf("close test body: %v", err)
	}
}

func TestNewFS_CreatesMissingRoot(t *testing.T) {
	t.Parallel()
	root := filepath.Join(t.TempDir(), "nested", "media")
	if _, err := NewFS(root); err != nil {
		t.Fatalf("NewFS(missing dir): %v", err)
	}
	info, err := os.Stat(root)
	if err != nil || !info.IsDir() {
		t.Errorf("root not created as directory: %v", err)
	}
}

func TestNewFS_Rejections(t *testing.T) {
	t.Parallel()
	if _, err := NewFS(""); err == nil {
		t.Errorf("NewFS(\"\") err = nil, want error")
	}
	file := filepath.Join(t.TempDir(), "a-file")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := NewFS(file); err == nil {
		t.Errorf("NewFS(existing file) err = nil, want error")
	}
}

func TestFS_ReRootSecondDefense(t *testing.T) {
	t.Parallel()
	b, _ := newFSBackend(t)
	for _, key := range []string{"../escape", "a/../../escape", "..", "a/../..", "../../../../etc/passwd"} {
		if _, err := b.objectPath(key); !errors.Is(err, ErrInvalidKey) {
			t.Errorf("objectPath(%q) err = %v, want ErrInvalidKey", key, err)
		}
	}
	if _, err := b.objectPath("a/ok.jpg"); err != nil {
		t.Errorf("objectPath(valid) err = %v", err)
	}
}

func TestFS_SymlinkCannotEscapeRoot(t *testing.T) {
	t.Parallel()
	b, root := newFSBackend(t)
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(root, "escape")); err != nil {
		t.Fatal(err)
	}

	outcome, err := b.Put(context.Background(), "escape/new.jpg", "image/jpeg", bytes.NewReader([]byte("x")), 1)
	if outcome != PutNotCreated || err == nil {
		t.Fatalf("Put through escaping symlink = %d, %v; want PutNotCreated + error", outcome, err)
	}
	if _, statErr := os.Stat(filepath.Join(outside, "new.jpg")); !errors.Is(statErr, fs.ErrNotExist) {
		t.Fatalf("Put reached outside root: Stat err = %v", statErr)
	}

	outsideObject := filepath.Join(outside, "existing.jpg")
	if writeErr := os.WriteFile(outsideObject, []byte("outside"), 0o600); writeErr != nil {
		t.Fatal(writeErr)
	}
	body, _, err := b.Get(context.Background(), "escape/existing.jpg")
	if err == nil {
		closeInternalTestBody(t, body)
		t.Fatal("Get through escaping symlink succeeded")
	}
	if deleteErr := b.Delete(context.Background(), "escape/existing.jpg"); deleteErr == nil {
		t.Fatal("Delete through escaping symlink succeeded")
	}
	got, err := os.ReadFile(outsideObject)
	if err != nil || string(got) != "outside" {
		t.Fatalf("outside object changed: bytes = %q, err = %v", got, err)
	}
}

func TestFS_NoResidueAfterFailedPut(t *testing.T) {
	t.Parallel()
	b, root := newFSBackend(t)
	outcome, err := b.Put(context.Background(), "a/short.jpg", "image/jpeg", bytes.NewReader([]byte("123")), 10)
	if outcome != PutNotCreated || err == nil {
		t.Fatalf("Put outcome = %d, err = %v, want PutNotCreated + error", outcome, err)
	}
	var files []string
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			files = append(files, path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 0 {
		t.Errorf("residue after failed Put: %v", files)
	}
}

func TestFS_TempResidueFromCrashIsInvisible(t *testing.T) {
	t.Parallel()
	b, root := newFSBackend(t)
	if err := os.WriteFile(filepath.Join(root, ".put-crashed.tmp"), []byte("image/jpeg\npartial"), 0o600); err != nil {
		t.Fatal(err)
	}
	b.mustPutForTest(t, "a/real.jpg")
	objects, next, err := b.ListPage(context.Background(), "", "", 100)
	if err != nil {
		t.Fatalf("ListPage: %v", err)
	}
	if next != "" || len(objects) != 1 || objects[0].Key != "a/real.jpg" {
		t.Errorf("ListPage = %v, %q; want only a/real.jpg", objects, next)
	}
}

func TestFS_ListPageStopsAtBound(t *testing.T) {
	t.Parallel()
	b, root := newFSBackend(t)
	b.mustPutForTest(t, "a/one.jpg")
	b.mustPutForTest(t, "a/two.jpg")

	blocked := filepath.Join(root, "z-blocked")
	if err := os.Mkdir(blocked, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(blocked, "unreachable.jpg"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(blocked, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if chmodErr := os.Chmod(blocked, 0o700); chmodErr != nil {
			t.Errorf("restore blocked directory permissions: %v", chmodErr)
		}
	})

	objects, next, err := b.ListPage(context.Background(), "", "", 1)
	if err != nil {
		t.Fatalf("ListPage scanned beyond its bounded window: %v", err)
	}
	if len(objects) != 1 || objects[0].Key != "a/one.jpg" || next != "a/one.jpg" {
		t.Errorf("ListPage = %v, %q; want first object and continuation cursor", objects, next)
	}
}

func TestFS_ListPageWideDirectoryUsesBoundedIndexWork(t *testing.T) {
	t.Parallel()
	b, _ := newFSBackend(t)
	for i := range 10_000 {
		b.mustPutForTest(t, fmt.Sprintf("wide/%05d.jpg", i))
	}

	var visits atomic.Int64
	b.visitIndexedObjectForTest = func() { visits.Add(1) }
	objects, next, err := b.ListPage(context.Background(), "wide/", "wide/04999.jpg", 1)
	if err != nil {
		t.Fatalf("ListPage: %v", err)
	}
	if len(objects) != 1 || objects[0].Key != "wide/05000.jpg" || next != "wide/05000.jpg" {
		t.Fatalf("ListPage = %v, %q; want wide/05000.jpg and continuation", objects, next)
	}
	if got := visits.Load(); got > 2 {
		t.Fatalf("ListPage visited %d index records, want at most limit+1", got)
	}
}

func TestFS_ListPageUsesFullKeyLexicographicOrder(t *testing.T) {
	t.Parallel()
	b, _ := newFSBackend(t)
	for _, key := range []string{"a/z.jpg", "a!.jpg", "a0.jpg"} {
		b.mustPutForTest(t, key)
	}

	var got []string
	cursor := ""
	for range 4 {
		objects, next, err := b.ListPage(context.Background(), "", cursor, 1)
		if err != nil {
			t.Fatalf("ListPage(cursor %q): %v", cursor, err)
		}
		for _, object := range objects {
			got = append(got, object.Key)
		}
		if next == "" {
			break
		}
		cursor = next
	}
	want := []string{"a!.jpg", "a/z.jpg", "a0.jpg"}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("paginated keys = %v, want full-key order %v with no skipped sibling", got, want)
	}
}

func (b *fsBackend) mustPutForTest(t *testing.T, key string) {
	t.Helper()
	outcome, err := b.Put(context.Background(), key, "image/jpeg", bytes.NewReader([]byte("x")), 1)
	if outcome != PutCreated || err != nil {
		t.Fatalf("Put(%q) = %d, %v", key, outcome, err)
	}
}

func TestFS_ContentTypeSurvivesReopen(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	b1, err := NewFS(root)
	if err != nil {
		t.Fatal(err)
	}
	outcome, err := b1.Put(context.Background(), "a/b.png", "image/png", bytes.NewReader([]byte("pngbytes")), 8)
	if outcome != PutCreated || err != nil {
		t.Fatalf("Put = %d, %v", outcome, err)
	}
	b2, err := NewFS(root)
	if err != nil {
		t.Fatal(err)
	}
	body, contentType, err := b2.Get(context.Background(), "a/b.png")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	defer closeInternalTestBody(t, body)
	raw, err := io.ReadAll(body)
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != "pngbytes" || contentType != "image/png" {
		t.Errorf("Get = %q, %q; want pngbytes, image/png", raw, contentType)
	}
}

func TestFS_PutUnderFileSegmentFails(t *testing.T) {
	t.Parallel()
	b, _ := newFSBackend(t)
	b.mustPutForTest(t, "leaf")
	outcome, err := b.Put(context.Background(), "leaf/child.jpg", "image/jpeg", bytes.NewReader([]byte("x")), 1)
	if outcome != PutNotCreated || err == nil {
		t.Errorf("Put(under file) outcome = %d, err = %v, want PutNotCreated + error", outcome, err)
	}
}

func TestFS_GetDirectoryIsNotFound(t *testing.T) {
	t.Parallel()
	b, _ := newFSBackend(t)
	b.mustPutForTest(t, "dir/leaf.jpg")
	if _, _, err := b.Get(context.Background(), "dir"); !errors.Is(err, ErrNotFound) {
		t.Errorf("Get(directory) err = %v, want ErrNotFound", err)
	}
}

func TestFS_DeleteDirectoryIsNotFound(t *testing.T) {
	t.Parallel()
	b, _ := newFSBackend(t)
	b.mustPutForTest(t, "dir/leaf.jpg")
	if err := b.Delete(context.Background(), "dir"); !errors.Is(err, ErrNotFound) {
		t.Errorf("Delete(directory) err = %v, want ErrNotFound", err)
	}
	if _, _, err := b.Get(context.Background(), "dir/leaf.jpg"); err != nil {
		t.Errorf("Get(child) after Delete(directory) error = %v, want nil", err)
	}
}
