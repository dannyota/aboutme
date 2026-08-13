package media

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	"image/png"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestCanonicalImageOutput(t *testing.T) {
	tests := []struct {
		name            string
		width           int
		height          int
		alpha           bool
		wantWidth       int
		wantHeight      int
		wantExtension   string
		wantContentType string
	}{
		{
			name:            "opaque aspect ratio rounds to nearest pixel",
			width:           3000,
			height:          1000,
			wantWidth:       2048,
			wantHeight:      683,
			wantExtension:   "jpg",
			wantContentType: "image/jpeg",
		},
		{
			name:            "alpha aspect ratio rounds to nearest pixel",
			width:           1500,
			height:          1000,
			alpha:           true,
			wantWidth:       1024,
			wantHeight:      683,
			wantExtension:   "png",
			wantContentType: "image/png",
		},
		{
			name:            "small opaque image is not upscaled",
			width:           321,
			height:          123,
			wantWidth:       321,
			wantHeight:      123,
			wantExtension:   "jpg",
			wantContentType: "image/jpeg",
		},
		{
			name:            "small alpha image is not upscaled",
			width:           123,
			height:          321,
			alpha:           true,
			wantWidth:       123,
			wantHeight:      321,
			wantExtension:   "png",
			wantContentType: "image/png",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			source := phase2BPattern(tc.width, tc.height, tc.alpha)
			got, err := NormalizePhoto(phase2BEncodePNG(t, source))
			if err != nil {
				t.Fatalf("NormalizePhoto: %v", err)
			}
			if got.Width != tc.wantWidth || got.Height != tc.wantHeight {
				t.Fatalf("dimensions = %dx%d, want %dx%d", got.Width, got.Height, tc.wantWidth, tc.wantHeight)
			}
			if got.Extension != tc.wantExtension || got.ContentType != tc.wantContentType {
				t.Fatalf(
					"format = %q/%q, want %q/%q",
					got.Extension,
					got.ContentType,
					tc.wantExtension,
					tc.wantContentType,
				)
			}
			if len(got.Bytes) > 2_097_152 {
				t.Fatalf("normalized size = %d, want at most 2,097,152", len(got.Bytes))
			}
			phase2BAssertEncodedFormat(t, got)
		})
	}

	t.Run("opaque output uses JPEG quality 85", func(t *testing.T) {
		sourceBytes := phase2BEncodePNG(t, phase2BPattern(73, 41, false))
		got, err := NormalizePhoto(sourceBytes)
		if err != nil {
			t.Fatalf("NormalizePhoto: %v", err)
		}

		decoded, err := png.Decode(bytes.NewReader(sourceBytes))
		if err != nil {
			t.Fatalf("decode source PNG: %v", err)
		}
		canonicalPixels := image.NewNRGBA(image.Rect(0, 0, 73, 41))
		draw.Draw(canonicalPixels, canonicalPixels.Bounds(), decoded, decoded.Bounds().Min, draw.Src)

		var want bytes.Buffer
		if err := jpeg.Encode(&want, canonicalPixels, &jpeg.Options{Quality: 85}); err != nil {
			t.Fatalf("encode independent JPEG85 expectation: %v", err)
		}
		if !bytes.Equal(got.Bytes, want.Bytes()) {
			t.Fatal("normalized opaque output is not the independently encoded JPEG85 result")
		}

		for _, wrongQuality := range []int{84, 86} {
			var wrong bytes.Buffer
			if err := jpeg.Encode(&wrong, canonicalPixels, &jpeg.Options{Quality: wrongQuality}); err != nil {
				t.Fatalf("encode JPEG%d control: %v", wrongQuality, err)
			}
			if bytes.Equal(got.Bytes, wrong.Bytes()) {
				t.Fatalf("test fixture does not distinguish JPEG quality 85 from %d", wrongQuality)
			}
		}
	})
}

func TestMediaKeyCannotBeInfluenced(t *testing.T) {
	resumeID := uuid.MustParse("01890f47-7e8a-7b2a-8d70-9a1f2c3d4e5f")
	randomness := []byte{0x00, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f}
	wantOpaqueKey := "resumes/01890f47-7e8a-7b2a-8d70-9a1f2c3d4e5f/photo-000102030405060708090a0b0c0d0e0f.jpg"

	opaque := phase2BPattern(64, 32, false)
	opaquePNG := phase2BEncodePNG(t, opaque)
	opaquePNGWithMetadata := insertPNGChunk(t, opaquePNG, "tEXt", []byte("Description\x00<script>ignored</script>"), "IDAT")
	opaqueJPEG := phase2BEncodeJPEG(t, opaque, 70)
	opaqueWebP := readFixture(t, "blue-purple-pink-large.lossless.webp")

	for _, tc := range []struct {
		name  string
		input []byte
	}{
		{name: "PNG container", input: opaquePNG},
		{name: "PNG metadata", input: opaquePNGWithMetadata},
		{name: "JPEG container", input: opaqueJPEG},
		{name: "WebP container", input: opaqueWebP},
	} {
		t.Run(tc.name, func(t *testing.T) {
			normalized, err := NormalizePhoto(tc.input)
			if err != nil {
				t.Fatalf("NormalizePhoto: %v", err)
			}
			if normalized.Extension != "jpg" {
				t.Fatalf("normalized extension = %q, want jpg", normalized.Extension)
			}
			key, err := NewPhotoKey(bytes.NewReader(randomness), resumeID, normalized.Extension)
			if err != nil {
				t.Fatalf("NewPhotoKey: %v", err)
			}
			if key != wantOpaqueKey {
				t.Fatalf("key = %q, want %q", key, wantOpaqueKey)
			}
			if ext, err := ParsePhotoKey(resumeID, key); err != nil || ext != "jpg" {
				t.Fatalf("ParsePhotoKey = %q, %v; want jpg, nil", ext, err)
			}
		})
	}

	alpha, err := NormalizePhoto(phase2BEncodePNG(t, phase2BPattern(32, 64, true)))
	if err != nil {
		t.Fatalf("NormalizePhoto(alpha): %v", err)
	}
	alphaKey, err := NewPhotoKey(bytes.NewReader(randomness), resumeID, alpha.Extension)
	if err != nil {
		t.Fatalf("NewPhotoKey(alpha): %v", err)
	}
	wantAlphaKey := "resumes/01890f47-7e8a-7b2a-8d70-9a1f2c3d4e5f/photo-000102030405060708090a0b0c0d0e0f.png"
	if alphaKey != wantAlphaKey {
		t.Fatalf("alpha key = %q, want %q", alphaKey, wantAlphaKey)
	}

	first, err := NewPhotoKey(bytes.NewReader(bytes.Repeat([]byte{0x11}, 16)), resumeID, "jpg")
	if err != nil {
		t.Fatalf("NewPhotoKey(first): %v", err)
	}
	second, err := NewPhotoKey(bytes.NewReader(bytes.Repeat([]byte{0x22}, 16)), resumeID, "jpg")
	if err != nil {
		t.Fatalf("NewPhotoKey(second): %v", err)
	}
	if first == second {
		t.Fatalf("two random suffixes produced the same key %q", first)
	}
	for _, key := range []string{first, second} {
		if ext, err := ParsePhotoKey(resumeID, key); err != nil || ext != "jpg" {
			t.Fatalf("ParsePhotoKey(%q) = %q, %v; want jpg, nil", key, ext, err)
		}
	}
}

func TestMediaAdmissionAndCleanup(t *testing.T) {
	admission := newPhotoAdmission(15 * time.Millisecond)
	release, err := admission.Acquire(context.Background())
	if err != nil {
		t.Fatalf("initial Acquire: %v", err)
	}

	canceledContext, cancel := context.WithCancel(context.Background())
	canceled := make(chan error, 1)
	go func() {
		waitingRelease, acquireErr := admission.Acquire(canceledContext)
		if waitingRelease != nil {
			waitingRelease()
		}
		canceled <- acquireErr
	}()
	cancel()
	if err := <-canceled; !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled waiter error = %v, want context.Canceled", err)
	}

	if waitingRelease, err := admission.Acquire(context.Background()); !errors.Is(err, ErrMediaBusy) {
		if waitingRelease != nil {
			waitingRelease()
		}
		t.Fatalf("busy waiter error = %v, want ErrMediaBusy", err)
	}

	release()
	nextRelease, err := admission.Acquire(context.Background())
	if err != nil {
		t.Fatalf("Acquire after canceled and busy waiters: %v", err)
	}
	nextRelease()
}

func phase2BPattern(width, height int, alpha bool) *image.NRGBA {
	result := image.NewNRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			a := uint8(255)
			if alpha && x == width/2 && y == height/2 {
				a = 127
			}
			result.SetNRGBA(x, y, color.NRGBA{
				R: uint8((x*17 + y*3) % 251),
				G: uint8((x*5 + y*19) % 253),
				B: uint8((x*11 + y*7) % 255),
				A: a,
			})
		}
	}
	return result
}

func phase2BEncodePNG(t *testing.T, source image.Image) []byte {
	t.Helper()
	var output bytes.Buffer
	if err := png.Encode(&output, source); err != nil {
		t.Fatalf("encode PNG fixture: %v", err)
	}
	return output.Bytes()
}

func phase2BEncodeJPEG(t *testing.T, source image.Image, quality int) []byte {
	t.Helper()
	var output bytes.Buffer
	if err := jpeg.Encode(&output, source, &jpeg.Options{Quality: quality}); err != nil {
		t.Fatalf("encode JPEG fixture: %v", err)
	}
	return output.Bytes()
}

func phase2BAssertEncodedFormat(t *testing.T, got NormalizedPhoto) {
	t.Helper()
	reader := bytes.NewReader(got.Bytes)
	switch got.Extension {
	case "jpg":
		if _, err := jpeg.DecodeConfig(reader); err != nil {
			t.Fatalf("decode canonical JPEG: %v", err)
		}
	case "png":
		if _, err := png.DecodeConfig(reader); err != nil {
			t.Fatalf("decode canonical PNG: %v", err)
		}
	default:
		t.Fatalf("unexpected normalized extension %q", got.Extension)
	}
}
