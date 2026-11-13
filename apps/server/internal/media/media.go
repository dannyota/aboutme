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

var ErrNotFound = errors.New("media: object not found")

var ErrAlreadyExists = errors.New("media: object already exists")

var ErrInvalidKey = errors.New("media: invalid object key")

const MaxObjectBytes = 2_097_152

// S3 object keys are at most 1024 bytes.
const maxKeyBytes = 1024

const maxContentTypeBytes = 255

// S3 ListObjectsV2 returns at most 1000 keys per page (MaxKeys).
const maxListLimit = 1000

type PutOutcome uint8

const (
	PutNotCreated PutOutcome = iota
	PutCreated
	PutUnknown
)

type Backend interface {
	Put(ctx context.Context, key, contentType string, body io.Reader, size int64) (PutOutcome, error)
	Get(ctx context.Context, key string) (io.ReadCloser, string, error)
	Delete(ctx context.Context, key string) error
	ListPage(ctx context.Context, prefix, cursor string, limit int) (objects []Object, nextCursor string, err error)
}

type Object struct {
	Key       string
	UpdatedAt time.Time
}

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

func validatePrefix(prefix string) error {
	if prefix == "" {
		return nil
	}
	return validateKey(strings.TrimSuffix(prefix, "/"))
}

func validateCursor(cursor string) error {
	if cursor == "" {
		return nil
	}
	return validateKey(cursor)
}

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

func readExact(body io.Reader, size int64) ([]byte, error) {
	buf := make([]byte, size)
	if _, err := io.ReadFull(body, buf); err != nil {
		return nil, fmt.Errorf("media: body ended before the declared size %d: %w", size, err)
	}
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

const photoKeyRandomBytes = 16

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
	if len(rest) != hexLen+len(".jpg") {
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
