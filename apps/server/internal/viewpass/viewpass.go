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
	CookieName = "__Host-view-pass"

	MaxPasses = 10

	Lifetime = 7 * 24 * time.Hour

	domainLabel = "aboutme.view-pass.v1"

	resumeIDBytes = 16
	epochBytes    = 4
	expiresBytes  = 8
	tagBytes      = sha256.Size
	fieldsBytes   = resumeIDBytes + epochBytes + expiresBytes
	passBytes     = fieldsBytes + tagBytes
)

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

type pass struct {
	resumeID  uuid.UUID
	epoch     int32
	expiresAt time.Time
	raw       string
}

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
