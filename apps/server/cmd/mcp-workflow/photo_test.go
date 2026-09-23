package main

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"io"
	"testing"
)

func reencodeJPEG(t *testing.T, data []byte, quality int) []byte {
	t.Helper()
	decoded, err := jpeg.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	var encoded bytes.Buffer
	if encodeErr := jpeg.Encode(&encoded, decoded, &jpeg.Options{Quality: quality}); encodeErr != nil {
		t.Fatal(encodeErr)
	}
	return encoded.Bytes()
}

func TestPhotoParityAcceptsExactPNGAndNormalizedJPEG(t *testing.T) {
	crop := &photoCrop{X: 0.1, Y: 0.05, Width: 0.8, Height: 0.8}
	transparent := photoBytes{ContentType: "image/png", Data: testPNG(t, 16, 12, true)}
	if err := validatePhotoParity(transparent, crop, transparent, &photoCrop{X: 0.1, Y: 0.05, Width: 0.8, Height: 0.8}); err != nil {
		t.Fatalf("transparent PNG rejected: %v", err)
	}
	source := testJPEG(t, 16, 12)
	normalized := photoBytes{ContentType: "image/jpeg", Data: reencodeJPEG(t, source, 100)}
	if err := validatePhotoParity(photoBytes{ContentType: "image/jpeg", Data: source}, nil, normalized, nil); err != nil {
		t.Fatalf("re-encoded JPEG rejected: %v", err)
	}
}

func TestPhotoParityRejectsTypeDimensionsAlphaPixelsQualityAndCrop(t *testing.T) {
	jpegSource := photoBytes{ContentType: "image/jpeg", Data: testJPEG(t, 16, 12)}
	pngSource := photoBytes{ContentType: "image/png", Data: testPNG(t, 16, 12, true)}
	crop := &photoCrop{X: 0, Y: 0, Width: 1, Height: 1}
	changedPixel := testImage(16, 12, true)
	changedPixel.SetNRGBA(3, 4, color.NRGBA{R: 1, G: 2, B: 3, A: 96})
	degraded := image.NewNRGBA(image.Rect(0, 0, 16, 12))
	for y := 0; y < 12; y++ {
		for x := 0; x < 16; x++ {
			degraded.SetNRGBA(x, y, color.NRGBA{R: 255, G: 0, B: 255, A: 255})
		}
	}
	for name, test := range map[string]struct {
		source, target         photoBytes
		sourceCrop, targetCrop *photoCrop
	}{
		"media type":     {jpegSource, photoBytes{ContentType: "image/png", Data: testPNG(t, 16, 12, false)}, nil, nil},
		"dimensions":     {jpegSource, photoBytes{ContentType: "image/jpeg", Data: testJPEG(t, 16, 13)}, nil, nil},
		"alpha":          {pngSource, photoBytes{ContentType: "image/png", Data: testPNG(t, 16, 12, false)}, nil, nil},
		"pixel":          {pngSource, photoBytes{ContentType: "image/png", Data: encodePNG(t, changedPixel)}, nil, nil},
		"jpeg quality":   {jpegSource, photoBytes{ContentType: "image/jpeg", Data: encodeJPEG(t, degraded)}, nil, nil},
		"crop mismatch":  {pngSource, pngSource, crop, &photoCrop{X: 0.1, Y: 0, Width: 0.9, Height: 1}},
		"crop absent":    {pngSource, pngSource, crop, nil},
		"crop invalid":   {pngSource, pngSource, &photoCrop{Width: 2, Height: 1}, &photoCrop{Width: 2, Height: 1}},
		"declared lie":   {jpegSource, photoBytes{ContentType: "image/jpeg", Data: pngSource.Data}, nil, nil},
		"truncated jpeg": {jpegSource, photoBytes{ContentType: "image/jpeg", Data: jpegSource.Data[:len(jpegSource.Data)/2]}, nil, nil},
	} {
		t.Run(name, func(t *testing.T) {
			if err := validatePhotoParity(test.source, test.sourceCrop, test.target, test.targetCrop); !errors.Is(err, errPhoto) {
				t.Fatalf("accepted mismatch: %v", err)
			}
		})
	}
}

func encodePNG(t *testing.T, value image.Image) []byte {
	t.Helper()
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, value); err != nil {
		t.Fatal(err)
	}
	return encoded.Bytes()
}

func encodeJPEG(t *testing.T, value image.Image) []byte {
	t.Helper()
	var encoded bytes.Buffer
	if err := jpeg.Encode(&encoded, value, &jpeg.Options{Quality: 95}); err != nil {
		t.Fatal(err)
	}
	return encoded.Bytes()
}

// countDecodes replaces the pixel decoders for one test and reports how many
// full decodes ran.
func countDecodes(t *testing.T) *int {
	t.Helper()
	count := 0
	originalPNG, originalJPEG := decodePNG, decodeJPEG
	decodePNG = func(reader io.Reader) (image.Image, error) { count++; return originalPNG(reader) }
	decodeJPEG = func(reader io.Reader) (image.Image, error) { count++; return originalJPEG(reader) }
	t.Cleanup(func() { decodePNG, decodeJPEG = originalPNG, originalJPEG })
	return &count
}

func TestPhotoHeaderBoundsRejectBeforePixelDecode(t *testing.T) {
	count := countDecodes(t)
	oversizedPNG := photoBytes{ContentType: "image/png", Data: testPNG(t, maxPNGEdge+1, 1, false)}
	oversizedJPEG := photoBytes{ContentType: "image/jpeg", Data: testJPEG(t, 1, maxJPEGEdge+1)}
	oversizedBytes := photoBytes{ContentType: "image/png", Data: append(testPNG(t, 1, 1, false), make([]byte, maxDecodedPhotoBytes)...)}
	for name, photo := range map[string]photoBytes{"png edge": oversizedPNG, "jpeg edge": oversizedJPEG, "byte size": oversizedBytes} {
		if err := validatePhotoParity(photo, nil, photo, nil); !errors.Is(err, errPhoto) {
			t.Fatalf("%s accepted: %v", name, err)
		}
	}
	if *count != 0 {
		t.Fatalf("full decode ran %d times before the header bound", *count)
	}
	within := photoBytes{ContentType: "image/png", Data: testPNG(t, maxPNGEdge, 1, false)}
	if err := validatePhotoParity(within, nil, within, nil); err != nil || *count != 2 {
		t.Fatalf("in-bound photo err = %v decodes = %d", err, *count)
	}
}
