package media_test

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/dannyota/aboutme/apps/server/internal/media"
)

// D11: resumes/{canonical lowercase resume UUID}/photo-{32 lowercase hex
// from crypto/rand}.{jpg|png}. One constructor and one parser own this
// grammar; these tests pin it.

var testResumeID = uuid.MustParse("01890f47-7e8a-7b2a-8d70-9a1f2c3d4e5f")

func TestNewPhotoKey_Canonical(t *testing.T) {
	t.Parallel()
	// Deterministic randomness: 16 bytes 0x00..0x0f.
	randSource := bytes.NewReader([]byte{0x00, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f})
	key, err := media.NewPhotoKey(randSource, testResumeID, "jpg")
	if err != nil {
		t.Fatalf("NewPhotoKey: %v", err)
	}
	want := "resumes/01890f47-7e8a-7b2a-8d70-9a1f2c3d4e5f/photo-000102030405060708090a0b0c0d0e0f.jpg"
	if key != want {
		t.Errorf("key = %q, want %q", key, want)
	}
	// The constructor's output must satisfy the parser (grammar closure).
	ext, err := media.ParsePhotoKey(testResumeID, key)
	if err != nil {
		t.Errorf("ParsePhotoKey(constructed key): %v", err)
	}
	if ext != "jpg" {
		t.Errorf("ext = %q, want %q", ext, "jpg")
	}
}

func TestNewPhotoKey_PNG(t *testing.T) {
	t.Parallel()
	key, err := media.NewPhotoKey(bytes.NewReader(bytes.Repeat([]byte{0xab}, 16)), testResumeID, "png")
	if err != nil {
		t.Fatalf("NewPhotoKey: %v", err)
	}
	want := "resumes/01890f47-7e8a-7b2a-8d70-9a1f2c3d4e5f/photo-abababababababababababababababab.png"
	if key != want {
		t.Errorf("key = %q, want %q", key, want)
	}
}

func TestNewPhotoKey_Rejections(t *testing.T) {
	t.Parallel()
	sixteen := bytes.Repeat([]byte{0x01}, 16)
	// A WebP input is never stored as WebP (D11); only normalized jpg/png.
	for _, ext := range []string{"", "webp", "gif", "JPG", "jpeg", "PNG", "jpg ", ".jpg"} {
		if _, err := media.NewPhotoKey(bytes.NewReader(sixteen), testResumeID, ext); err == nil {
			t.Errorf("NewPhotoKey(ext %q) err = nil, want error", ext)
		}
	}
	if _, err := media.NewPhotoKey(bytes.NewReader(sixteen), uuid.Nil, "jpg"); err == nil {
		t.Errorf("NewPhotoKey(uuid.Nil) err = nil, want error")
	}
	// Exhausted randomness must fail, never truncate the suffix.
	if _, err := media.NewPhotoKey(bytes.NewReader([]byte{0x01}), testResumeID, "jpg"); err == nil {
		t.Errorf("NewPhotoKey(short randomness) err = nil, want error")
	}
}

func TestParsePhotoKey_AcceptsOnlyCanonicalOwnedKeys(t *testing.T) {
	t.Parallel()
	valid := "resumes/01890f47-7e8a-7b2a-8d70-9a1f2c3d4e5f/photo-0123456789abcdef0123456789abcdef.png"
	ext, err := media.ParsePhotoKey(testResumeID, valid)
	if err != nil {
		t.Fatalf("ParsePhotoKey(valid): %v", err)
	}
	if ext != "png" {
		t.Errorf("ext = %q, want png", ext)
	}

	otherResume := uuid.MustParse("01890f47-7e8a-7b2a-8d70-9a1f2c3d4e60")
	hexSuffix := "0123456789abcdef0123456789abcdef"
	rejected := []struct {
		name string
		key  string
	}{
		{"cross-resume", "resumes/" + otherResume.String() + "/photo-" + hexSuffix + ".png"},
		{"uppercase uuid", "resumes/01890F47-7E8A-7B2A-8D70-9A1F2C3D4E5F/photo-" + hexSuffix + ".png"},
		{"braced uuid alias", "resumes/{01890f47-7e8a-7b2a-8d70-9a1f2c3d4e5f}/photo-" + hexSuffix + ".png"},
		{"unhyphenated uuid alias", "resumes/01890f477e8a7b2a8d709a1f2c3d4e5f/photo-" + hexSuffix + ".png"},
		{"wrong root", "resume/" + testResumeID.String() + "/photo-" + hexSuffix + ".png"},
		{"missing photo- prefix", "resumes/" + testResumeID.String() + "/" + hexSuffix + ".png"},
		{"uppercase hex", "resumes/" + testResumeID.String() + "/photo-" + strings.ToUpper(hexSuffix) + ".png"},
		{"short hex", "resumes/" + testResumeID.String() + "/photo-" + hexSuffix[:31] + ".png"},
		{"long hex", "resumes/" + testResumeID.String() + "/photo-" + hexSuffix + "0.png"},
		{"non-hex suffix", "resumes/" + testResumeID.String() + "/photo-" + strings.Repeat("g", 32) + ".png"},
		{"webp extension", "resumes/" + testResumeID.String() + "/photo-" + hexSuffix + ".webp"},
		{"no extension", "resumes/" + testResumeID.String() + "/photo-" + hexSuffix},
		{"double extension", "resumes/" + testResumeID.String() + "/photo-" + hexSuffix + ".png.jpg"},
		{"extra segment", "resumes/" + testResumeID.String() + "/x/photo-" + hexSuffix + ".png"},
		{"missing segment", "resumes/photo-" + hexSuffix + ".png"},
		{"traversal", "resumes/../" + testResumeID.String() + "/photo-" + hexSuffix + ".png"},
		{"trailing slash", "resumes/" + testResumeID.String() + "/photo-" + hexSuffix + ".png/"},
		{"leading slash", "/resumes/" + testResumeID.String() + "/photo-" + hexSuffix + ".png"},
		{"empty", ""},
		{"nul byte", "resumes/" + testResumeID.String() + "/photo-" + hexSuffix + ".png\x00"},
	}
	for _, tc := range rejected {
		if _, err := media.ParsePhotoKey(testResumeID, tc.key); err == nil {
			t.Errorf("%s: ParsePhotoKey(%q) err = nil, want error", tc.name, tc.key)
		}
	}
	// The expected-resume binding also rejects the valid key under a
	// different owner (cross-resume keys never reach a backend).
	if _, err := media.ParsePhotoKey(otherResume, valid); err == nil {
		t.Errorf("ParsePhotoKey(other owner, valid key) err = nil, want error")
	}
}

func TestParsePhotoKey_ErrorsAreErrInvalidKey(t *testing.T) {
	t.Parallel()
	if _, err := media.ParsePhotoKey(testResumeID, "garbage"); !errors.Is(err, media.ErrInvalidKey) {
		t.Errorf("ParsePhotoKey err = %v, want ErrInvalidKey", err)
	}
}

// TestPutOutcomeValues pins the contract's wire-adjacent numeric order:
// the zero value is the safe "created nothing" state.
func TestPutOutcomeValues(t *testing.T) {
	t.Parallel()
	if media.PutNotCreated != 0 || media.PutCreated != 1 || media.PutUnknown != 2 {
		t.Errorf("PutOutcome values = %d/%d/%d, want 0/1/2", media.PutNotCreated, media.PutCreated, media.PutUnknown)
	}
}
