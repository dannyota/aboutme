// Package viewpass seals, opens, and merges the sign-in-to-view pass cookie
// (docs/design/viewer-analytics/sign-in-to-view.md "Pass cookie"; ADR 0022;
// AC-VIEW-007). A pass authorizes gated public reads of exactly one resume
// at exactly one pass epoch; it names no person, and this package keeps no
// copy anywhere. The caller injects the clock so every expiry and pruning
// decision is deterministic in tests.
package viewpass

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
)

const (
	// CookieName is the sign-in-to-view pass cookie's fixed name.
	// __Host- requires Secure, Path=/, and no Domain attribute, which this
	// package's Add always sets.
	CookieName = "__Host-view-pass"

	// MaxPasses bounds the cookie to at most 10 passes, one per resume.
	// Adding an 11th drops whichever kept pass expires first.
	MaxPasses = 10

	// Lifetime is how long a fresh pass authorizes reads: 7 days after
	// sign-in, never extended.
	Lifetime = 7 * 24 * time.Hour

	// domainLabel separates this HMAC's input space from every other
	// keyed use in the server, so a tag cannot be replayed as some other
	// domain's authenticator even if a key were ever reused.
	domainLabel = "aboutme.view-pass.v1"

	resumeIDBytes = 16
	epochBytes    = 4
	expiresBytes  = 8
	tagBytes      = sha256.Size
	fieldsBytes   = resumeIDBytes + epochBytes + expiresBytes
	passBytes     = fieldsBytes + tagBytes
)

// Seal produces one sealed pass for resumeID at passEpoch, expiring at
// expiresAt: base64url (unpadded) of resumeID (16 bytes), passEpoch (4,
// big-endian), expiresAt as Unix seconds (8, big-endian), and an
// HMAC-SHA-256 tag (32) under key over the fixed domain label and those
// three fields (design "Pass cookie").
func Seal(key []byte, resumeID uuid.UUID, passEpoch int32, expiresAt time.Time) string {
	buf := make([]byte, passBytes)
	copy(buf[:resumeIDBytes], resumeID[:])
	binary.BigEndian.PutUint32(buf[resumeIDBytes:resumeIDBytes+epochBytes], uint32(passEpoch))      //nolint:gosec // view_pass_epoch is >= 0 by database constraint.
	binary.BigEndian.PutUint64(buf[resumeIDBytes+epochBytes:fieldsBytes], uint64(expiresAt.Unix())) //nolint:gosec // Unix times after 1970 are positive.
	copy(buf[fieldsBytes:], tagFor(key, buf[:fieldsBytes]))
	return base64.RawURLEncoding.EncodeToString(buf)
}

func tagFor(key, fields []byte) []byte {
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(domainLabel))
	mac.Write(fields)
	return mac.Sum(nil)
}

// pass is one decoded and authenticated token, plus its original encoding
// so a valid pass can be re-emitted byte-for-byte without resealing.
type pass struct {
	resumeID  uuid.UUID
	epoch     int32
	expiresAt time.Time
	raw       string
}

// open decodes and authenticates one sealed pass token: wrong length,
// undecodable base64url, or a tag that does not match under key (compared
// in constant time) all report ok=false. It does not check expiry; callers
// decide that against their own clock.
func open(key []byte, token string) (p pass, ok bool) {
	decoded, err := base64.RawURLEncoding.Strict().DecodeString(token)
	if err != nil || len(decoded) != passBytes {
		return pass{}, false
	}
	fields, tag := decoded[:fieldsBytes], decoded[fieldsBytes:]
	if !hmac.Equal(tag, tagFor(key, fields)) {
		return pass{}, false
	}
	var resumeID uuid.UUID
	copy(resumeID[:], decoded[:resumeIDBytes])
	epoch := int32(binary.BigEndian.Uint32(decoded[resumeIDBytes : resumeIDBytes+epochBytes]))                     //nolint:gosec // sealed by this package's own Seal, which only writes a non-negative epoch.
	expiresAt := time.Unix(int64(binary.BigEndian.Uint64(decoded[resumeIDBytes+epochBytes:fieldsBytes])), 0).UTC() //nolint:gosec // sealed by this package's own Seal from a valid Unix time.
	return pass{resumeID: resumeID, epoch: epoch, expiresAt: expiresAt, raw: token}, true
}

// Valid reports whether cookieValue -- the full __Host-view-pass cookie
// value, one or more "."-separated sealed passes -- holds a pass for
// resumeID at exactly passEpoch that has not expired as of now. An older
// epoch, another resume's pass, an expired pass, and an unreadable token
// all report false; a cookie need only hold one matching pass among many.
func Valid(cookieValue string, key []byte, resumeID uuid.UUID, passEpoch int32, now time.Time) bool {
	for _, token := range splitPasses(cookieValue) {
		p, ok := open(key, token)
		if !ok || !now.Before(p.expiresAt) {
			continue
		}
		if p.resumeID == resumeID && p.epoch == passEpoch {
			return true
		}
	}
	return false
}

// Add merges newPass into existing (the current __Host-view-pass cookie
// value, "" if the viewer holds none yet) and returns the cookie to set.
// It drops every expired or unreadable pass, replaces any kept pass for
// the same resume as newPass (a fresh sign-in supersedes an old pass for
// that resume rather than accumulating a second one), and keeps at most
// MaxPasses, dropping whichever pass expires first when an 11th would
// otherwise be kept. Max-Age is the latest remaining expiry among the
// kept passes, in whole seconds; an unreadable or already-expired newPass
// is dropped like any other bad token, so Add is safe to call with
// attacker-controlled input in either argument.
func Add(key []byte, existing, newPass string, now time.Time) http.Cookie {
	newP, newOK := open(key, newPass)
	newOK = newOK && now.Before(newP.expiresAt)

	kept := make([]pass, 0, MaxPasses+1)
	for _, token := range splitPasses(existing) {
		p, ok := open(key, token)
		if !ok || !now.Before(p.expiresAt) {
			continue
		}
		if newOK && p.resumeID == newP.resumeID {
			continue
		}
		kept = append(kept, p)
	}
	if newOK {
		kept = append(kept, newP)
	}

	sort.Slice(kept, func(i, j int) bool { return kept[i].expiresAt.Before(kept[j].expiresAt) })
	if len(kept) > MaxPasses {
		kept = kept[len(kept)-MaxPasses:]
	}

	tokens := make([]string, len(kept))
	var maxAge time.Duration
	for i, p := range kept {
		tokens[i] = p.raw
		if remaining := p.expiresAt.Sub(now); remaining > maxAge {
			maxAge = remaining
		}
	}
	return http.Cookie{
		Name:     CookieName,
		Value:    strings.Join(tokens, "."),
		Path:     "/",
		Secure:   true,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(maxAge.Seconds()),
	}
}

func splitPasses(cookieValue string) []string {
	if cookieValue == "" {
		return nil
	}
	return strings.Split(cookieValue, ".")
}
