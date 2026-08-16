package password

import (
	"bytes"
	"context"
	"crypto/sha1" //nolint:gosec // HIBP range protocol keys its lookups on SHA-1 digests; the full digest/password never leaves the process
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ErrBreachUnavailable is the closed error returned when a breach lookup
// cannot be completed and no valid cached answer exists (fail closed).
var ErrBreachUnavailable = errors.New("breach service unavailable")

// HIBP budgets and protocol constants (D2).
const (
	hibpMaxPrefixes    = 256
	hibpMaxCacheBytes  = 16 * 1024 * 1024 // 16 MiB of parsed digest bytes
	hibpCacheTTL       = 24 * time.Hour
	hibpMaxResponse    = 128 * 1024
	hibpPrefixLen      = 5
	hibpSuffixHexLen   = 35 // 40 hex chars total, minus the 5-char prefix
	hibpDigestLen      = sha1.Size
	hibpDefaultTimeout = 5 * time.Second
)

// HIBP is the HaveIBeenPwned range client. It sends only the first five
// uppercase hex characters of the SHA-1 digest, never the full digest or
// password.
type HIBP struct {
	client  *http.Client
	baseURL string
	timeout time.Duration
	cache   *hibpCache
	now     func() time.Time
}

// HIBPOption customizes a HIBP client. Tests inject the base URL, clock, and a
// short timeout; production uses the defaults.
type HIBPOption func(*HIBP)

// WithHIBPBaseURL overrides the API origin (test servers use this).
func WithHIBPBaseURL(u string) HIBPOption { return func(h *HIBP) { h.baseURL = u } }

// WithHIBPClock injects the time source used for cache expiry.
func WithHIBPClock(now func() time.Time) HIBPOption { return func(h *HIBP) { h.now = now } }

// WithHIBPTimeout overrides the per-request deadline (tests use this to make
// timeout behavior deterministic; production keeps the 5s default).
func WithHIBPTimeout(d time.Duration) HIBPOption { return func(h *HIBP) { h.timeout = d } }

// NewHIBP returns a HIBP client. The supplied client is copied and forced to
// never follow redirects.
func NewHIBP(client *http.Client, opts ...HIBPOption) *HIBP {
	if client == nil {
		client = &http.Client{}
	}
	c := *client
	c.CheckRedirect = func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}
	h := &HIBP{
		client:  &c,
		baseURL: "https://api.pwnedpasswords.com",
		timeout: hibpDefaultTimeout,
		cache:   newHIBPCache(),
		now:     time.Now,
	}
	for _, o := range opts {
		o(h)
	}
	return h
}

// Breached reports whether the normalized password is in the HIBP corpus. A
// cache miss plus an upstream failure returns ErrBreachUnavailable.
func (h *HIBP) Breached(ctx context.Context, password string) (bool, error) {
	digest := sha1.Sum([]byte(password)) //nolint:gosec // HIBP requires SHA-1; the full digest never leaves the process
	prefix := strings.ToUpper(hex.EncodeToString(digest[:3]))[:hibpPrefixLen]

	if digests, ok := h.cache.get(prefix, h.now()); ok {
		return containsSHA1(digests, digest), nil
	}

	fetched, err := h.fetch(ctx, prefix)
	if err != nil {
		return false, ErrBreachUnavailable
	}
	h.cache.put(prefix, fetched, h.now())
	return containsSHA1(fetched, digest), nil
}

// fetch performs one range request and parses the suffix list. It returns a
// nil slice (not breached) for a 404 or an empty 200 body.
func (h *HIBP) fetch(ctx context.Context, prefix string) ([][20]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, h.timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, h.baseURL+"/range/"+prefix, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Add-Padding", "true")

	resp, err := h.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close() //nolint:errcheck // read-only GET response; nothing meaningful to do with a close error here

	switch resp.StatusCode {
	case http.StatusOK:
		return h.parseRangeBody(prefix, resp)
	case http.StatusNotFound:
		return nil, nil
	default:
		return nil, fmt.Errorf("password: HIBP unexpected status %d", resp.StatusCode)
	}
}

// parseRangeBody enforces the content type and the 128 KiB response cap before
// parsing any line.
func (h *HIBP) parseRangeBody(prefix string, resp *http.Response) ([][20]byte, error) {
	mt, _, err := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	if err != nil || mt != "text/plain" {
		return nil, errors.New("password: HIBP bad content type")
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, hibpMaxResponse+1))
	if err != nil {
		return nil, err
	}
	if len(body) > hibpMaxResponse {
		return nil, errors.New("password: HIBP response exceeds cap")
	}
	if len(body) == 0 {
		return nil, nil
	}
	return h.parseRangeLines(prefix, body)
}

// parseRangeLines parses strict ASCII "<35-hex-uppercase>:<decimal>" lines,
// reconstructs each full 20-byte digest from the prefix, rejects malformed
// lines and duplicate suffixes, and returns the digests sorted.
func (h *HIBP) parseRangeLines(prefix string, body []byte) ([][20]byte, error) {
	text := string(body)
	lines := strings.Split(text, "\n")
	if strings.HasSuffix(text, "\n") {
		lines = lines[:len(lines)-1]
	}

	digests := make([][20]byte, 0, len(lines))
	seen := make(map[[20]byte]struct{}, len(lines))
	for _, line := range lines {
		line = strings.TrimSuffix(line, "\r")
		d, err := parseHIBPSuffix(prefix, line)
		if err != nil {
			return nil, err
		}
		if _, dup := seen[d]; dup {
			return nil, errors.New("password: HIBP duplicate suffix")
		}
		seen[d] = struct{}{}
		digests = append(digests, d)
	}
	sort.Slice(digests, func(i, j int) bool {
		return bytes.Compare(digests[i][:], digests[j][:]) < 0
	})
	return digests, nil
}

// parseHIBPSuffix parses one response line into a full [20]byte digest.
func parseHIBPSuffix(prefix, line string) ([20]byte, error) {
	var zero [20]byte
	if len(line) < hibpSuffixHexLen+2 { // suffix + ':' + at least one count digit
		return zero, errors.New("password: HIBP suffix line too short")
	}
	if line[hibpSuffixHexLen] != ':' {
		return zero, errors.New("password: HIBP suffix line missing colon")
	}
	suffix := line[:hibpSuffixHexLen]
	count := line[hibpSuffixHexLen+1:]
	if !isUpperHex(suffix) {
		return zero, errors.New("password: HIBP suffix is not uppercase hex")
	}
	if !isDecimal(count) {
		return zero, errors.New("password: HIBP count is not decimal")
	}
	if _, err := strconv.ParseUint(count, 10, 64); err != nil {
		return zero, errors.New("password: HIBP count overflow")
	}

	fullHex := prefix + suffix
	if len(fullHex) != 2*hibpDigestLen {
		return zero, errors.New("password: HIBP digest length invalid")
	}
	raw, err := hex.DecodeString(fullHex)
	if err != nil {
		return zero, errors.New("password: HIBP digest encoding invalid")
	}
	var d [20]byte
	copy(d[:], raw)
	return d, nil
}

func containsSHA1(digests [][20]byte, target [20]byte) bool {
	i := sort.Search(len(digests), func(i int) bool {
		return bytes.Compare(digests[i][:], target[:]) >= 0
	})
	return i < len(digests) && digests[i] == target
}

func isUpperHex(s string) bool {
	if len(s) == 0 {
		return false
	}
	for i := 0; i < len(s); i++ {
		if !isUpperHexChar(s[i]) {
			return false
		}
	}
	return true
}

// isUpperHexChar reports whether c is an uppercase ASCII hex digit.
func isUpperHexChar(c byte) bool {
	return c >= '0' && c <= '9' || c >= 'A' && c <= 'F'
}

func isDecimal(s string) bool {
	if len(s) == 0 {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

// hibpCache is a bounded, TTL'd LRU over parsed HIBP range responses. It holds
// at most 256 prefixes and 16 MiB of digest bytes for 24 hours, evicting
// expired entries first and then least-recently-used entries.
type hibpCache struct {
	mu         sync.Mutex
	entries    map[string]*hibpCacheEntry
	totalBytes int
	counter    uint64
}

type hibpCacheEntry struct {
	digests   [][20]byte
	expiresAt time.Time
	touch     uint64
}

func newHIBPCache() *hibpCache {
	return &hibpCache{entries: make(map[string]*hibpCacheEntry)}
}

func (c *hibpCache) get(prefix string, now time.Time) ([][20]byte, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.entries[prefix]
	if !ok {
		return nil, false
	}
	if !now.Before(e.expiresAt) {
		c.totalBytes -= len(e.digests) * hibpDigestLen
		delete(c.entries, prefix)
		return nil, false
	}
	c.counter++
	e.touch = c.counter
	return e.digests, true
}

func (c *hibpCache) put(prefix string, digests [][20]byte, now time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if old, ok := c.entries[prefix]; ok {
		c.totalBytes -= len(old.digests) * hibpDigestLen
		delete(c.entries, prefix)
	}
	for k, e := range c.entries {
		if !now.Before(e.expiresAt) {
			c.totalBytes -= len(e.digests) * hibpDigestLen
			delete(c.entries, k)
		}
	}

	size := len(digests) * hibpDigestLen
	if size > hibpMaxCacheBytes {
		return // refuse to cache a single entry larger than the whole budget
	}
	c.counter++
	c.entries[prefix] = &hibpCacheEntry{
		digests:   digests,
		expiresAt: now.Add(hibpCacheTTL),
		touch:     c.counter,
	}
	c.totalBytes += size

	for len(c.entries) > hibpMaxPrefixes || c.totalBytes > hibpMaxCacheBytes {
		var evictKey string
		evictTouch := ^uint64(0)
		for k, e := range c.entries {
			if e.touch < evictTouch {
				evictTouch = e.touch
				evictKey = k
			}
		}
		if evictKey == "" {
			return
		}
		c.totalBytes -= len(c.entries[evictKey].digests) * hibpDigestLen
		delete(c.entries, evictKey)
	}
}
