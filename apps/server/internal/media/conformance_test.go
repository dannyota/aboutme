package media_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"danny.vn/aboutme/apps/server/internal/media"
	"danny.vn/aboutme/apps/server/internal/media/mediatest"
)

type backendCase struct {
	name string
	new  func(t *testing.T) media.Backend
}

func backendCases() []backendCase {
	return []backendCase{
		{
			name: "fs",
			new: func(t *testing.T) media.Backend {
				t.Helper()
				b, err := media.NewFS(t.TempDir())
				if err != nil {
					t.Fatalf("NewFS: %v", err)
				}
				return b
			},
		},
		{
			name: "s3",
			new: func(t *testing.T) media.Backend {
				t.Helper()
				cfg := mediatest.RequireTestS3(t)
				b, err := media.NewS3(context.Background(), cfg)
				if err != nil {
					t.Fatalf("NewS3: %v", err)
				}
				return b
			},
		},
	}
}

func namespace(t *testing.T) string {
	t.Helper()
	var raw [8]byte
	if _, err := rand.Read(raw[:]); err != nil {
		t.Fatalf("namespace randomness: %v", err)
	}
	return "t" + hex.EncodeToString(raw[:])
}

func checkPutPair(t *testing.T, outcome media.PutOutcome, err error) {
	t.Helper()
	switch outcome {
	case media.PutCreated:
		if err != nil {
			t.Fatalf("invalid pair: PutCreated with err = %v", err)
		}
	case media.PutNotCreated, media.PutUnknown:
		if err == nil {
			t.Fatalf("invalid pair: outcome %d with nil error", outcome)
		}
	default:
		t.Fatalf("invalid PutOutcome %d (err = %v)", outcome, err)
	}
	if errors.Is(err, media.ErrAlreadyExists) && outcome != media.PutNotCreated {
		t.Fatalf("ErrAlreadyExists must be PutNotCreated, got outcome %d", outcome)
	}
}

func mustPut(t *testing.T, b media.Backend, key, contentType string, body []byte) {
	t.Helper()
	outcome, err := b.Put(context.Background(), key, contentType, bytes.NewReader(body), int64(len(body)))
	checkPutPair(t, outcome, err)
	if outcome != media.PutCreated {
		t.Fatalf("Put(%q) outcome = %d, err = %v, want PutCreated", key, outcome, err)
	}
}

func mustGet(t *testing.T, b media.Backend, key string) ([]byte, string) {
	t.Helper()
	body, contentType, err := b.Get(context.Background(), key)
	if err != nil {
		t.Fatalf("Get(%q): %v", key, err)
	}
	defer closeExternalTestBody(t, body)
	raw, err := io.ReadAll(body)
	if err != nil {
		t.Fatalf("Get(%q) read body: %v", key, err)
	}
	return raw, contentType
}

func closeExternalTestBody(t *testing.T, body io.Closer) {
	t.Helper()
	if err := body.Close(); err != nil {
		t.Errorf("close test body: %v", err)
	}
}

func copyExternalTestBody(t *testing.T, destination io.Writer, source io.Reader) {
	t.Helper()
	if _, err := io.Copy(destination, source); err != nil {
		t.Errorf("copy test body: %v", err)
	}
}

func writeExternalTestBody(t *testing.T, destination io.Writer, body string) {
	t.Helper()
	if _, err := io.WriteString(destination, body); err != nil {
		t.Errorf("write test body: %v", err)
	}
}

func TestConformance_PutGetRoundTrip(t *testing.T) {
	t.Parallel()
	for _, bc := range backendCases() {
		t.Run(bc.name, func(t *testing.T) {
			t.Parallel()
			b := bc.new(t)
			key := namespace(t) + "/dir/photo-roundtrip.jpg"
			want := []byte("jpeg-bytes\x00\xff binary payload")

			mustPut(t, b, key, "image/jpeg", want)

			got, contentType := mustGet(t, b, key)
			if !bytes.Equal(got, want) {
				t.Errorf("Get body = %q, want %q", got, want)
			}
			if contentType != "image/jpeg" {
				t.Errorf("Get contentType = %q, want %q", contentType, "image/jpeg")
			}
		})
	}
}

func TestConformance_ZeroByteObject(t *testing.T) {
	t.Parallel()
	for _, bc := range backendCases() {
		t.Run(bc.name, func(t *testing.T) {
			t.Parallel()
			b := bc.new(t)
			key := namespace(t) + "/empty.png"
			mustPut(t, b, key, "image/png", nil)
			got, contentType := mustGet(t, b, key)
			if len(got) != 0 {
				t.Errorf("Get body = %q, want empty", got)
			}
			if contentType != "image/png" {
				t.Errorf("Get contentType = %q, want %q", contentType, "image/png")
			}
		})
	}
}

func TestConformance_GetAbsentIsNotFound(t *testing.T) {
	t.Parallel()
	for _, bc := range backendCases() {
		t.Run(bc.name, func(t *testing.T) {
			t.Parallel()
			b := bc.new(t)
			_, _, err := b.Get(context.Background(), namespace(t)+"/absent.jpg")
			if !errors.Is(err, media.ErrNotFound) {
				t.Errorf("Get(absent) err = %v, want exactly ErrNotFound", err)
			}
		})
	}
}

func TestConformance_DeleteAbsentIsNotFound(t *testing.T) {
	t.Parallel()
	for _, bc := range backendCases() {
		t.Run(bc.name, func(t *testing.T) {
			t.Parallel()
			b := bc.new(t)
			if err := b.Delete(context.Background(), namespace(t)+"/absent.jpg"); !errors.Is(err, media.ErrNotFound) {
				t.Errorf("Delete(absent) err = %v, want exactly ErrNotFound", err)
			}
		})
	}
}

func TestConformance_DeleteThenGet(t *testing.T) {
	t.Parallel()
	for _, bc := range backendCases() {
		t.Run(bc.name, func(t *testing.T) {
			t.Parallel()
			b := bc.new(t)
			key := namespace(t) + "/delete-me.jpg"
			mustPut(t, b, key, "image/jpeg", []byte("bytes"))
			if err := b.Delete(context.Background(), key); err != nil {
				t.Fatalf("Delete: %v", err)
			}
			if _, _, err := b.Get(context.Background(), key); !errors.Is(err, media.ErrNotFound) {
				t.Errorf("Get after Delete err = %v, want exactly ErrNotFound", err)
			}
			if err := b.Delete(context.Background(), key); !errors.Is(err, media.ErrNotFound) {
				t.Errorf("second Delete err = %v, want exactly ErrNotFound", err)
			}
		})
	}
}

func TestConformance_SecondPutIsRejected(t *testing.T) {
	t.Parallel()
	for _, bc := range backendCases() {
		t.Run(bc.name, func(t *testing.T) {
			t.Parallel()
			b := bc.new(t)
			key := namespace(t) + "/create-only.jpg"
			original := []byte("original bytes")
			mustPut(t, b, key, "image/jpeg", original)

			replacement := []byte("attacker replacement, longer than original")
			outcome, err := b.Put(context.Background(), key, "image/png", bytes.NewReader(replacement), int64(len(replacement)))
			checkPutPair(t, outcome, err)
			if outcome != media.PutNotCreated || !errors.Is(err, media.ErrAlreadyExists) {
				t.Fatalf("second Put outcome = %d, err = %v, want PutNotCreated + ErrAlreadyExists", outcome, err)
			}

			got, contentType := mustGet(t, b, key)
			if !bytes.Equal(got, original) {
				t.Errorf("bytes after rejected Put = %q, want original %q", got, original)
			}
			if contentType != "image/jpeg" {
				t.Errorf("contentType after rejected Put = %q, want original %q", contentType, "image/jpeg")
			}
		})
	}
}

func TestConformance_ConcurrentSameKey(t *testing.T) {
	t.Parallel()
	for _, bc := range backendCases() {
		t.Run(bc.name, func(t *testing.T) {
			t.Parallel()
			b := bc.new(t)
			key := namespace(t) + "/race.jpg"
			bodies := [][]byte{[]byte("writer-zero body"), []byte("writer-one body!")}

			outcomes := make([]media.PutOutcome, 2)
			errs := make([]error, 2)
			var wg sync.WaitGroup
			for i := range 2 {
				wg.Add(1)
				go func() {
					defer wg.Done()
					outcomes[i], errs[i] = b.Put(context.Background(), key, "image/jpeg", bytes.NewReader(bodies[i]), int64(len(bodies[i])))
				}()
			}
			wg.Wait()

			for i := range 2 {
				checkPutPair(t, outcomes[i], errs[i])
			}
			created := 0
			winner := -1
			for i := range 2 {
				switch outcomes[i] {
				case media.PutCreated:
					created++
					winner = i
				case media.PutNotCreated:
				default:
					t.Fatalf("writer %d outcome = %d (err %v); concurrent local/test-service writes must be proved", i, outcomes[i], errs[i])
				}
			}
			if created != 1 {
				t.Fatalf("PutCreated count = %d, want exactly 1 (outcomes %v, errs %v)", created, outcomes, errs)
			}
			got, _ := mustGet(t, b, key)
			if !bytes.Equal(got, bodies[winner]) {
				t.Errorf("stored bytes = %q, want winner %d's body %q (loser must not overwrite)", got, winner, bodies[winner])
			}
		})
	}
}

func invalidKeys() []string {
	return []string{
		"",
		"/leading",
		"trailing/",
		"a//b",
		"//",
		".",
		"..",
		"./a",
		"../a",
		"a/.",
		"a/..",
		"a/./b",
		"a/../b",
		`a\b`,
		"a\x00b",
		"a\x07b",
		"\xff\xfe/binary",
		strings.Repeat("k", 1025),
	}
}

func TestConformance_InvalidKeysRejectedBeforeIO(t *testing.T) {
	t.Parallel()
	for _, bc := range backendCases() {
		t.Run(bc.name, func(t *testing.T) {
			t.Parallel()
			b := bc.new(t)
			canceled, cancel := context.WithCancel(context.Background())
			cancel()
			for _, key := range invalidKeys() {
				outcome, err := b.Put(canceled, key, "image/jpeg", bytes.NewReader([]byte("x")), 1)
				checkPutPair(t, outcome, err)
				if outcome != media.PutNotCreated || !errors.Is(err, media.ErrInvalidKey) {
					t.Errorf("Put(%q) outcome = %d, err = %v, want PutNotCreated + ErrInvalidKey before I/O", key, outcome, err)
				}
				if _, _, err := b.Get(canceled, key); !errors.Is(err, media.ErrInvalidKey) {
					t.Errorf("Get(%q) err = %v, want ErrInvalidKey before I/O", key, err)
				}
				if err := b.Delete(canceled, key); !errors.Is(err, media.ErrInvalidKey) {
					t.Errorf("Delete(%q) err = %v, want ErrInvalidKey before I/O", key, err)
				}
			}
		})
	}
}

func TestConformance_ContentTypeValidated(t *testing.T) {
	t.Parallel()
	for _, bc := range backendCases() {
		t.Run(bc.name, func(t *testing.T) {
			t.Parallel()
			b := bc.new(t)
			canceled, cancel := context.WithCancel(context.Background())
			cancel()
			for _, contentType := range []string{"", "image/jpeg\nX-Injected: 1", "image/jpeg\r", "image/" + strings.Repeat("x", 300)} {
				outcome, err := b.Put(canceled, namespace(t)+"/ct.jpg", contentType, bytes.NewReader([]byte("x")), 1)
				checkPutPair(t, outcome, err)
				if outcome != media.PutNotCreated || err == nil {
					t.Errorf("Put with contentType %q outcome = %d, err = %v, want PutNotCreated + error", contentType, outcome, err)
				}
			}
		})
	}
}

func TestConformance_ExactSize(t *testing.T) {
	t.Parallel()
	for _, bc := range backendCases() {
		t.Run(bc.name, func(t *testing.T) {
			t.Parallel()
			b := bc.new(t)
			ns := namespace(t)

			cases := []struct {
				name string
				body []byte
				size int64
			}{
				{"shorter", []byte("12345"), 10},
				{"longer", []byte("1234567890"), 5},
				{"negative", []byte(""), -1},
				{"over-budget", []byte("x"), media.MaxObjectBytes + 1},
			}
			for _, tc := range cases {
				key := ns + "/" + tc.name + ".jpg"
				outcome, err := b.Put(context.Background(), key, "image/jpeg", bytes.NewReader(tc.body), tc.size)
				checkPutPair(t, outcome, err)
				if outcome != media.PutNotCreated || err == nil {
					t.Errorf("%s: Put outcome = %d, err = %v, want PutNotCreated + error", tc.name, outcome, err)
				}
				if _, _, err := b.Get(context.Background(), key); !errors.Is(err, media.ErrNotFound) {
					t.Errorf("%s: Get after failed Put err = %v, want exactly ErrNotFound (no partial object)", tc.name, err)
				}
			}

			atLimit := bytes.Repeat([]byte("a"), media.MaxObjectBytes)
			mustPut(t, b, ns+"/at-limit.jpg", "image/jpeg", atLimit)
			got, _ := mustGet(t, b, ns+"/at-limit.jpg")
			if !bytes.Equal(got, atLimit) {
				t.Errorf("at-limit object corrupted: got %d bytes", len(got))
			}
		})
	}
}

func TestConformance_CancelBeforeDispatchIsNotCreated(t *testing.T) {
	t.Parallel()
	for _, bc := range backendCases() {
		t.Run(bc.name, func(t *testing.T) {
			t.Parallel()
			b := bc.new(t)
			key := namespace(t) + "/canceled.jpg"
			canceled, cancel := context.WithCancel(context.Background())
			cancel()
			outcome, err := b.Put(canceled, key, "image/jpeg", bytes.NewReader([]byte("x")), 1)
			checkPutPair(t, outcome, err)
			if outcome != media.PutNotCreated || !errors.Is(err, context.Canceled) {
				t.Fatalf("Put outcome = %d, err = %v, want PutNotCreated + context.Canceled", outcome, err)
			}
			if _, _, err := b.Get(context.Background(), key); !errors.Is(err, media.ErrNotFound) {
				t.Errorf("Get after pre-dispatch cancel err = %v, want exactly ErrNotFound", err)
			}
		})
	}
}

type cancelAtEOFReader struct {
	reader *bytes.Reader
	cancel context.CancelFunc
}

func (r *cancelAtEOFReader) Read(p []byte) (int, error) {
	n, err := r.reader.Read(p)
	if err == io.EOF {
		r.cancel()
	}
	return n, err
}

func TestConformance_CancelAfterBodyBeforeIOIsNotCreated(t *testing.T) {
	t.Parallel()
	for _, bc := range backendCases() {
		t.Run(bc.name, func(t *testing.T) {
			t.Parallel()
			b := bc.new(t)
			key := namespace(t) + "/canceled-after-body.jpg"
			ctx, cancel := context.WithCancel(context.Background())
			body := &cancelAtEOFReader{reader: bytes.NewReader([]byte("x")), cancel: cancel}

			outcome, err := b.Put(ctx, key, "image/jpeg", body, 1)
			checkPutPair(t, outcome, err)
			if outcome != media.PutNotCreated || !errors.Is(err, context.Canceled) {
				t.Fatalf("Put outcome = %d, err = %v, want PutNotCreated + context.Canceled", outcome, err)
			}
			if _, _, err := b.Get(context.Background(), key); !errors.Is(err, media.ErrNotFound) {
				t.Errorf("Get after pre-I/O cancel err = %v, want exactly ErrNotFound", err)
			}
		})
	}
}

func TestConformance_ListPage(t *testing.T) {
	t.Parallel()
	for _, bc := range backendCases() {
		t.Run(bc.name, func(t *testing.T) {
			t.Parallel()
			b := bc.new(t)
			ns := namespace(t)
			before := time.Now().Add(-time.Hour)

			var want []string
			for i := range 5 {
				key := fmt.Sprintf("%s/p/obj-%d.jpg", ns, i)
				want = append(want, key)
				mustPut(t, b, key, "image/jpeg", []byte{byte(i)})
			}
			neighbor := ns + "/q/other.jpg"
			mustPut(t, b, neighbor, "image/jpeg", []byte("n"))

			after := time.Now().Add(time.Hour)

			for _, prefix := range []string{ns + "/p", ns + "/p/"} {
				var got []string
				cursor := ""
				pages := 0
				for {
					objects, next, err := b.ListPage(context.Background(), prefix, cursor, 2)
					if err != nil {
						t.Fatalf("ListPage(%q, %q): %v", prefix, cursor, err)
					}
					pages++
					if pages > 10 {
						t.Fatalf("ListPage did not terminate")
					}
					for _, o := range objects {
						if o.UpdatedAt.Before(before) || o.UpdatedAt.After(after) {
							t.Errorf("object %q UpdatedAt = %v, outside [%v, %v]", o.Key, o.UpdatedAt, before, after)
						}
						got = append(got, o.Key)
					}
					if next == "" {
						break
					}
					if len(objects) != 2 {
						t.Errorf("non-final page has %d objects, want exactly the limit 2", len(objects))
					}
					cursor = next
				}
				if pages != 3 {
					t.Errorf("prefix %q: page count = %d, want 3 (2+2+1)", prefix, pages)
				}
				if fmt.Sprint(got) != fmt.Sprint(want) {
					t.Errorf("prefix %q: keys = %v, want stable ordered %v (no duplicates, no neighbors)", prefix, got, want)
				}
			}

			objects, next, err := b.ListPage(context.Background(), ns+"/p/zz", "", 2)
			if err != nil || len(objects) != 0 || next != "" {
				t.Errorf("ListPage(unmatched) = %v, %q, %v; want empty terminal page", objects, next, err)
			}
		})
	}
}

func TestConformance_ListPagePrefixAliasesRejected(t *testing.T) {
	t.Parallel()
	for _, bc := range backendCases() {
		t.Run(bc.name, func(t *testing.T) {
			t.Parallel()
			b := bc.new(t)
			canceled, cancel := context.WithCancel(context.Background())
			cancel()

			badPrefixes := []string{
				"/leading", "p//", "p///", "//", ".", "..", "./p", "../p",
				"p/./q", "p/../q", `p\q`, "p\x00", "p/..", "p/.",
			}
			for _, prefix := range badPrefixes {
				if _, _, err := b.ListPage(canceled, prefix, "", 10); !errors.Is(err, media.ErrInvalidKey) {
					t.Errorf("ListPage(prefix %q) err = %v, want ErrInvalidKey before I/O", prefix, err)
				}
			}
			for _, cursor := range []string{"/lead", "a//b", "a\x00", "trailing/"} {
				if _, _, err := b.ListPage(canceled, "p", cursor, 10); !errors.Is(err, media.ErrInvalidKey) {
					t.Errorf("ListPage(cursor %q) err = %v, want ErrInvalidKey before I/O", cursor, err)
				}
			}
			for _, limit := range []int{0, -1, 1001} {
				if _, _, err := b.ListPage(canceled, "p", "", limit); err == nil {
					t.Errorf("ListPage(limit %d) err = nil, want error", limit)
				}
			}
		})
	}
}

func TestConformance_FileSegmentShadowing(t *testing.T) {
	t.Parallel()
	for _, bc := range backendCases() {
		t.Run(bc.name, func(t *testing.T) {
			t.Parallel()
			b := bc.new(t)
			ns := namespace(t)
			mustPut(t, b, ns+"/leaf", "image/jpeg", []byte("x"))
			if _, _, err := b.Get(context.Background(), ns+"/leaf/child.jpg"); !errors.Is(err, media.ErrNotFound) {
				t.Errorf("Get(shadowed child) err = %v, want exactly ErrNotFound", err)
			}
			if err := b.Delete(context.Background(), ns+"/leaf/child.jpg"); !errors.Is(err, media.ErrNotFound) {
				t.Errorf("Delete(shadowed child) err = %v, want exactly ErrNotFound", err)
			}
		})
	}
}
