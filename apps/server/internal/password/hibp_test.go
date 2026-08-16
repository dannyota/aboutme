package password

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// hibpParts returns the 5-char uppercase prefix and 35-char uppercase suffix of
// a password's SHA-1 hex digest.
func hibpParts(password string) (prefix, suffix string) {
	d := sha1.Sum([]byte(password))
	full := strings.ToUpper(hex.EncodeToString(d[:]))
	return full[:5], full[5:]
}

func TestHIBPBreachedSendsOnlyFiveCharPrefix(t *testing.T) {
	t.Parallel()
	const password = "correct horse battery staple"
	prefix, suffix := hibpParts(password)

	var gotPath, gotQuery, gotHeader string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotQuery = r.URL.RawQuery
		gotHeader = r.Header.Get("Add-Padding")
		w.Header().Set("Content-Type", "text/plain")
		fmt.Fprintf(w, "%s:42\n", suffix) //nolint:errcheck // response-write errors are irrelevant in a test handler
	}))
	defer server.Close()

	h := NewHIBP(server.Client(), WithHIBPBaseURL(server.URL))
	breached, err := h.Breached(context.Background(), password)
	if err != nil {
		t.Fatalf("Breached error = %v", err)
	}
	if !breached {
		t.Error("Breached = false, want true")
	}
	if gotPath != "/range/"+prefix {
		t.Errorf("request path = %q, want %q", gotPath, "/range/"+prefix)
	}
	if gotQuery != "" {
		t.Errorf("request query = %q, want empty", gotQuery)
	}
	if gotHeader != "true" {
		t.Errorf("Add-Padding header = %q, want %q", gotHeader, "true")
	}
	if strings.Contains(gotPath, suffix) {
		t.Error("request path contains the full suffix; only the prefix may leave the process")
	}
}

func TestHIBPNotBreached(t *testing.T) {
	t.Parallel()
	const password = "a unique password value"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		fmt.Fprintf(w, "%s:7\n", strings.Repeat("0", 35)) //nolint:errcheck // response-write errors are irrelevant in a test handler
	}))
	defer server.Close()

	h := NewHIBP(server.Client(), WithHIBPBaseURL(server.URL))
	breached, err := h.Breached(context.Background(), password)
	if err != nil {
		t.Fatalf("Breached error = %v", err)
	}
	if breached {
		t.Error("Breached = true, want false")
	}
}

func TestHIBPNotFoundIsNotBreached(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer server.Close()

	h := NewHIBP(server.Client(), WithHIBPBaseURL(server.URL))
	breached, err := h.Breached(context.Background(), "correct horse battery staple")
	if err != nil {
		t.Fatalf("Breached error = %v", err)
	}
	if breached {
		t.Error("Breached = true, want false for a 404")
	}
}

func TestHIBPRejectsRedirect(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "https://evil.example/range/"+r.URL.Path, http.StatusFound)
	}))
	defer server.Close()

	h := NewHIBP(server.Client(), WithHIBPBaseURL(server.URL))
	if _, err := h.Breached(context.Background(), "correct horse battery staple"); err == nil {
		t.Fatal("Breached error = nil, want error for a redirect")
	}
}

func TestHIBPRejectsBadContentType(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintln(w, "{}") //nolint:errcheck // response-write errors are irrelevant in a test handler
	}))
	defer server.Close()

	h := NewHIBP(server.Client(), WithHIBPBaseURL(server.URL))
	if _, err := h.Breached(context.Background(), "correct horse battery staple"); err == nil {
		t.Fatal("Breached error = nil, want error for bad content type")
	}
}

func TestHIBPRejectsMalformedLines(t *testing.T) {
	t.Parallel()
	const password = "correct horse battery staple"

	cases := []struct {
		name string
		body string
	}{
		{"lowercase suffix", strings.Repeat("a", 35) + ":1\n"},
		{"short suffix", "ABCDE:1\n"},
		{"missing colon", strings.Repeat("0", 35) + "X1\n"},
		{"bad count", strings.Repeat("0", 35) + ":12a\n"},
		{"empty line", "\n"},
		{"duplicate suffix", strings.Repeat("0", 35) + ":1\n" + strings.Repeat("0", 35) + ":2\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/plain")
				fmt.Fprint(w, tc.body) //nolint:errcheck // response-write errors are irrelevant in a test handler
			}))
			defer server.Close()

			h := NewHIBP(server.Client(), WithHIBPBaseURL(server.URL))
			if _, err := h.Breached(context.Background(), password); err == nil {
				t.Errorf("Breached error = nil, want error for %s", tc.name)
			}
		})
	}
}

func TestHIBPRejectsOversizedResponse(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		// One line larger than the 128 KiB cap.
		fmt.Fprintf(w, "%s:1\n", strings.Repeat("0", hibpMaxResponse+100)) //nolint:errcheck // response-write errors are irrelevant in a test handler
	}))
	defer server.Close()

	h := NewHIBP(server.Client(), WithHIBPBaseURL(server.URL))
	if _, err := h.Breached(context.Background(), "correct horse battery staple"); err == nil {
		t.Fatal("Breached error = nil, want error for an oversized response")
	}
}

func TestHIBPTimeout(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(500 * time.Millisecond)
		w.Header().Set("Content-Type", "text/plain")
		fmt.Fprintln(w, strings.Repeat("0", 35)+":1") //nolint:errcheck // response-write errors are irrelevant in a test handler
	}))
	defer server.Close()

	h := NewHIBP(server.Client(), WithHIBPBaseURL(server.URL), WithHIBPTimeout(20*time.Millisecond))
	start := time.Now()
	_, err := h.Breached(context.Background(), "correct horse battery staple")
	if err == nil {
		t.Fatal("Breached error = nil, want timeout error")
	}
	if elapsed := time.Since(start); elapsed > 500*time.Millisecond {
		t.Errorf("Breached took %v, want it to return before the server finished", elapsed)
	}
}

func TestHIBPCacheServesDuringOutage(t *testing.T) {
	t.Parallel()
	const password = "correct horse battery staple"
	_, suffix := hibpParts(password)

	var fail atomicBool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if fail.load() {
			http.Error(w, "down", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/plain")
		fmt.Fprintf(w, "%s:5\n", suffix) //nolint:errcheck // response-write errors are irrelevant in a test handler
	}))
	defer server.Close()

	h := NewHIBP(server.Client(), WithHIBPBaseURL(server.URL))
	if breached, err := h.Breached(context.Background(), password); err != nil || !breached {
		t.Fatalf("first Breached = %v, %v; want true, nil", breached, err)
	}

	// Upstream goes down; the cached response must still answer.
	fail.store(true)
	if breached, err := h.Breached(context.Background(), password); err != nil || !breached {
		t.Errorf("cached Breached = %v, %v; want true, nil during outage", breached, err)
	}
}

func TestHIBPMissPlusFailureIsFailClosed(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "down", http.StatusInternalServerError)
	}))
	defer server.Close()

	h := NewHIBP(server.Client(), WithHIBPBaseURL(server.URL))
	if _, err := h.Breached(context.Background(), "a fresh password value"); err == nil {
		t.Fatal("Breached error = nil, want ErrBreachUnavailable on miss plus failure")
	}
}

// atomicBool is a race-free flag for toggling the test server from the test
// goroutine while the server handler reads it.
type atomicBool struct{ v atomic.Bool }

func (b *atomicBool) load() bool   { return b.v.Load() }
func (b *atomicBool) store(v bool) { b.v.Store(v) }

// makeDigests returns n strictly-increasing distinct [20]byte digests.
func makeDigests(n int) [][20]byte {
	out := make([][20]byte, n)
	for i := range out {
		v := uint64(i)
		for b := 0; b < 8; b++ {
			out[i][12+b] = byte(v >> (56 - 8*b))
		}
	}
	return out
}

func TestHIBPCacheTTL(t *testing.T) {
	t.Parallel()
	c := newHIBPCache()
	now := time.Now()
	c.put("AAAAA", makeDigests(3), now)
	if _, ok := c.get("AAAAA", now.Add(23*time.Hour)); !ok {
		t.Error("cache miss 1h before expiry, want hit")
	}
	if _, ok := c.get("AAAAA", now.Add(24*time.Hour)); ok {
		t.Error("cache hit at expiry, want expired miss")
	}
}

func TestHIBPCachePrefixCap(t *testing.T) {
	t.Parallel()
	c := newHIBPCache()
	now := time.Now()
	for i := 0; i < hibpMaxPrefixes+1; i++ {
		prefix := fmt.Sprintf("%05X", i)
		c.put(prefix, makeDigests(1), now)
	}
	c.mu.Lock()
	got := len(c.entries)
	c.mu.Unlock()
	if got != hibpMaxPrefixes {
		t.Errorf("entries = %d, want %d", got, hibpMaxPrefixes)
	}
	if _, ok := c.get("00000", now); ok {
		t.Error("oldest prefix still cached, want it evicted")
	}
	if _, ok := c.get(fmt.Sprintf("%05X", hibpMaxPrefixes), now); !ok {
		t.Error("newest prefix missing, want it retained")
	}
}

func TestHIBPCacheByteCap(t *testing.T) {
	t.Parallel()
	c := newHIBPCache()
	now := time.Now()
	// 700000 digests = 14 MiB, then 200000 = 4 MiB; total exceeds 16 MiB.
	c.put("AAAAA", makeDigests(700000), now)
	c.put("BBBBB", makeDigests(200000), now)

	c.mu.Lock()
	gotBytes := c.totalBytes
	gotLen := len(c.entries)
	c.mu.Unlock()
	if gotBytes > hibpMaxCacheBytes {
		t.Errorf("totalBytes = %d, want <= %d", gotBytes, hibpMaxCacheBytes)
	}
	if gotLen != 1 {
		t.Errorf("entries = %d, want 1 after byte-cap eviction", gotLen)
	}
	if _, ok := c.get("AAAAA", now); ok {
		t.Error("first (LRU) prefix still cached, want it evicted for the byte cap")
	}
	if _, ok := c.get("BBBBB", now); !ok {
		t.Error("second prefix missing, want it retained")
	}
}
