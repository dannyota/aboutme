// Package media stores private resume media objects behind one Backend
// contract with two implementations: a rooted filesystem store (native
// development and unit tests) and an S3-compatible store (compose/UAT,
// staging, production). See docs/design/deployment.md ("Media") and
// docs/adr/0019-private-media-delivery.md.
//
// Object creation is conditional and fails on an existing key: no overwrite
// path exists anywhere in this package (ADR 0019). Put distinguishes a
// proved create, a proved non-create, and an unknown remote outcome so a
// request path can never delete a key that might belong to a collision
// winner. Deletion reports an already-absent object as ErrNotFound.
//
// Object keys are canonical, forward-slash-separated, nonempty segments
// produced only by this package's callers (D11). Both backends run the same
// key validation before any I/O; the S3 backend passes the validated bytes
// unchanged and the filesystem backend additionally re-roots the cleaned
// path as a second defense, so the two backends can never name the same
// object through different strings.
package media

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
)

// ErrNotFound is returned by Get and Delete for an absent key. Both
// backends return exactly this error, never a backend-specific one.
var ErrNotFound = errors.New("media: object not found")

// ErrAlreadyExists is returned by Put when the key already names an object.
// It always accompanies PutNotCreated and the existing bytes are unchanged.
var ErrAlreadyExists = errors.New("media: object already exists")

// ErrInvalidKey is returned when a key, prefix, or cursor fails the
// canonical key grammar. It is always returned before any I/O.
var ErrInvalidKey = errors.New("media: invalid object key")

// MaxObjectBytes is the largest object either backend accepts: the resume
// photo normalized-object budget from docs/design/budgets.md ("Resume photo
// file / normalized object", 2,097,152 bytes). Enforcing it here is defense
// in depth below the HTTP intake path, and it also bounds the memory Put
// buffers while proving the body's EOF sits exactly at size.
const MaxObjectBytes = 2_097_152

// maxKeyBytes matches the S3 hard key limit; anything longer can never be
// a canonical key on either backend.
const maxKeyBytes = 1024

// maxContentTypeBytes bounds the stored content type: it must round-trip
// as a single header-safe line on both backends.
const maxContentTypeBytes = 255

// maxListLimit is the largest single ListPage window either backend serves;
// it matches the S3 ListObjectsV2 MaxKeys ceiling so a larger request
// cannot be silently clipped into a lying page size.
const maxListLimit = 1000

// PutOutcome classifies what a Put call proved about object creation.
type PutOutcome uint8

const (
	// PutNotCreated means the backend knows this call created no object.
	PutNotCreated PutOutcome = iota
	// PutCreated means this call created the complete object.
	PutCreated
	// PutUnknown means the remote result is ambiguous. The named key may or
	// may not exist and must not be deleted by the request path.
	PutUnknown
)

// Backend is the whole storage contract. The only valid Put result pairs
// are (PutCreated, nil), (PutNotCreated, non-nil), and (PutUnknown,
// non-nil); ErrAlreadyExists is a PutNotCreated error.
type Backend interface {
	// Put is create-only. Nil error requires PutCreated. ErrAlreadyExists
	// requires PutNotCreated and leaves existing bytes unchanged. A remote
	// request whose commit cannot be proved returns PutUnknown with an
	// error. Put accepts only a body whose EOF is exactly at size and never
	// exposes a partial object when the body is shorter or longer.
	Put(ctx context.Context, key, contentType string, body io.Reader, size int64) (PutOutcome, error)
	// Get returns the object body and stored content type, or ErrNotFound.
	Get(ctx context.Context, key string) (io.ReadCloser, string, error)
	// Delete removes the object, or returns ErrNotFound when it is absent.
	Delete(ctx context.Context, key string) error
	// ListPage returns at most limit objects whose keys start with prefix,
	// in ascending lexicographic key order, strictly after cursor. A
	// non-empty nextCursor continues the listing; empty means exhausted.
	ListPage(ctx context.Context, prefix, cursor string, limit int) (objects []Object, nextCursor string, err error)
}

// Object is one stored object as seen by ListPage. UpdatedAt feeds the
// orphan sweep's minimum-age gate (budgets.md "Media orphan minimum age").
type Object struct {
	Key       string
	UpdatedAt time.Time
}

// validateKey enforces the canonical object-key grammar shared by both
// backends: nonempty forward-slash-separated segments with no empty, "." or
// ".." segment (which also excludes repeated, leading, and trailing
// separators), no backslash, no NUL or other control byte, valid UTF-8, and
// at most maxKeyBytes bytes. It never performs I/O.
func validateKey(key string) error {
	if key == "" {
		return fmt.Errorf("%w: empty key", ErrInvalidKey)
	}
	if len(key) > maxKeyBytes {
		return fmt.Errorf("%w: key longer than %d bytes", ErrInvalidKey, maxKeyBytes)
	}
	if !utf8.ValidString(key) {
		return fmt.Errorf("%w: key is not valid UTF-8", ErrInvalidKey)
	}
	for _, r := range key {
		if r == '\\' {
			return fmt.Errorf("%w: key contains a backslash", ErrInvalidKey)
		}
		if r < 0x20 || r == 0x7f {
			return fmt.Errorf("%w: key contains a control byte", ErrInvalidKey)
		}
	}
	for _, segment := range strings.Split(key, "/") {
		switch segment {
		case "":
			return fmt.Errorf("%w: empty, leading, trailing, or repeated separator", ErrInvalidKey)
		case ".", "..":
			return fmt.Errorf("%w: %q segment", ErrInvalidKey, segment)
		}
	}
	return nil
}

// validatePrefix accepts the canonical key grammar plus exactly one
// optional trailing separator, and the empty prefix (list everything).
// Every other alias is rejected, so FS and S3 cannot scope the same page
// through different strings.
func validatePrefix(prefix string) error {
	if prefix == "" {
		return nil
	}
	// TrimSuffix removes at most one trailing separator, so "p//" still
	// fails validateKey's trailing-separator check.
	return validateKey(strings.TrimSuffix(prefix, "/"))
}

// validateCursor accepts an empty cursor (start) or a canonical key.
func validateCursor(cursor string) error {
	if cursor == "" {
		return nil
	}
	return validateKey(cursor)
}

// validateContentType requires a nonempty single-line bounded value so the
// stored content type round-trips identically on both backends.
func validateContentType(contentType string) error {
	if contentType == "" {
		return errors.New("media: empty content type")
	}
	if len(contentType) > maxContentTypeBytes {
		return fmt.Errorf("media: content type longer than %d bytes", maxContentTypeBytes)
	}
	for i := 0; i < len(contentType); i++ {
		if c := contentType[i]; c < 0x20 || c == 0x7f {
			return errors.New("media: content type contains a control byte")
		}
	}
	return nil
}

// validatePut runs every pre-I/O Put check shared by both backends. A nil
// error means the key, content type, and size are acceptable and the
// context was still live at the check.
func validatePut(ctx context.Context, key, contentType string, size int64) error {
	if err := validateKey(key); err != nil {
		return err
	}
	if err := validateContentType(contentType); err != nil {
		return err
	}
	if size < 0 {
		return fmt.Errorf("media: negative object size %d", size)
	}
	if size > MaxObjectBytes {
		return fmt.Errorf("media: object size %d exceeds the %d-byte budget", size, MaxObjectBytes)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return nil
}

// validateListPage runs every pre-I/O ListPage check shared by both
// backends.
func validateListPage(ctx context.Context, prefix, cursor string, limit int) error {
	if err := validatePrefix(prefix); err != nil {
		return err
	}
	if err := validateCursor(cursor); err != nil {
		return err
	}
	if limit < 1 || limit > maxListLimit {
		return fmt.Errorf("media: list limit %d outside 1..%d", limit, maxListLimit)
	}
	return ctx.Err()
}

// readExact reads exactly size bytes from body and proves EOF sits exactly
// there, so neither backend can ever commit a partial or oversized object.
// size is already bounded by MaxObjectBytes when this is called.
func readExact(body io.Reader, size int64) ([]byte, error) {
	buf := make([]byte, size)
	if _, err := io.ReadFull(body, buf); err != nil {
		return nil, fmt.Errorf("media: body ended before the declared size %d: %w", size, err)
	}
	// One probe read past size must observe EOF and no bytes.
	var probe [1]byte
	for {
		n, err := body.Read(probe[:])
		if n > 0 {
			return nil, fmt.Errorf("media: body continues past the declared size %d", size)
		}
		if err == io.EOF {
			return buf, nil
		}
		if err != nil {
			return nil, fmt.Errorf("media: reading past the declared size %d: %w", size, err)
		}
	}
}

// photoKeyRandomBytes is the crypto/rand suffix width D11 fixes: 16 bytes,
// rendered as 32 lowercase hex characters.
const photoKeyRandomBytes = 16

// NewPhotoKey constructs the one D11 photo key:
// resumes/{canonical lowercase resume UUID}/photo-{32 lowercase hex}.{jpg|png}.
// randSource supplies the 16 random suffix bytes; production passes
// crypto/rand.Reader and tests inject a deterministic reader. ext is
// exactly "jpg" or "png" from normalized output — never a client-supplied
// value, and an input WebP is never stored as WebP.
func NewPhotoKey(randSource io.Reader, resumeID uuid.UUID, ext string) (string, error) {
	if ext != "jpg" && ext != "png" {
		return "", fmt.Errorf("media: photo extension %q is not jpg or png", ext)
	}
	if resumeID == uuid.Nil {
		return "", errors.New("media: photo key requires a non-nil resume ID")
	}
	var raw [photoKeyRandomBytes]byte
	if _, err := io.ReadFull(randSource, raw[:]); err != nil {
		return "", fmt.Errorf("media: reading photo key randomness: %w", err)
	}
	return "resumes/" + resumeID.String() + "/photo-" + hex.EncodeToString(raw[:]) + "." + ext, nil
}

// ParsePhotoKey proves that a stored key is canonical D11 grammar and that
// it belongs to resumeID, returning the key's extension. Callers must use
// it before any Get or Delete so a malformed or cross-resume key never
// reaches a backend.
func ParsePhotoKey(resumeID uuid.UUID, key string) (ext string, err error) {
	if resumeID == uuid.Nil {
		return "", fmt.Errorf("%w: expected resume ID is nil", ErrInvalidKey)
	}
	if err := validateKey(key); err != nil {
		return "", err
	}
	segments := strings.Split(key, "/")
	if len(segments) != 3 || segments[0] != "resumes" {
		return "", fmt.Errorf("%w: not a resumes/{id}/photo key", ErrInvalidKey)
	}
	parsed, parseErr := uuid.Parse(segments[1])
	// uuid.Parse accepts uppercase, braced, urn, and unhyphenated aliases;
	// requiring the canonical serialization to match byte-for-byte rejects
	// every alias spelling.
	if parseErr != nil || parsed.String() != segments[1] {
		return "", fmt.Errorf("%w: resume segment is not a canonical lowercase UUID", ErrInvalidKey)
	}
	if parsed != resumeID {
		return "", fmt.Errorf("%w: key belongs to a different resume", ErrInvalidKey)
	}
	leaf := segments[2]
	const prefix, hexLen = "photo-", 32
	if !strings.HasPrefix(leaf, prefix) {
		return "", fmt.Errorf("%w: leaf segment is not photo-*", ErrInvalidKey)
	}
	rest := leaf[len(prefix):]
	if len(rest) != hexLen+len(".jpg") { // ".jpg" and ".png" share a length
		return "", fmt.Errorf("%w: leaf segment has the wrong length", ErrInvalidKey)
	}
	suffix := rest[:hexLen]
	for i := 0; i < len(suffix); i++ {
		c := suffix[i]
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return "", fmt.Errorf("%w: suffix is not 32 lowercase hex characters", ErrInvalidKey)
		}
	}
	switch rest[hexLen:] {
	case ".jpg":
		return "jpg", nil
	case ".png":
		return "png", nil
	default:
		return "", fmt.Errorf("%w: extension is not .jpg or .png", ErrInvalidKey)
	}
}
