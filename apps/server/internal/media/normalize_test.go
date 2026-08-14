package media

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash/crc32"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestNormalizeAcceptsFrozenCorpusDeterministically(t *testing.T) {
	tests := []struct {
		path        string
		extension   string
		contentType string
		width       int
		height      int
	}{
		{"opaque-max-pixels.jpg", "jpg", "image/jpeg", 2048, 2048},
		{"alpha-max-pixels.png", "png", "image/png", 1024, 1024},
		{"alpha-max-pixels-oriented.png", "png", "image/png", 1024, 1024},
		{"rgba16-max-pixels-oriented.png", "jpg", "image/jpeg", 2048, 2048},
		{"gray16-boundary.png", "jpg", "image/jpeg", 2048, 2048},
		{"max-edge.jpg", "jpg", "image/jpeg", 2048, 1},
		{"blue-purple-pink-large.lossless.webp", "jpg", "image/jpeg", 600, 400},
		{"blue-purple-pink-large.no-filter.lossy.webp", "jpg", "image/jpeg", 600, 400},
	}

	for _, tc := range tests {
		t.Run(tc.path, func(t *testing.T) {
			source := readFixture(t, tc.path)
			first, err := NormalizePhoto(source)
			if err != nil {
				t.Fatalf("NormalizePhoto: %v", err)
			}
			second, err := NormalizePhoto(source)
			if err != nil {
				t.Fatalf("NormalizePhoto second call: %v", err)
			}
			if !bytes.Equal(first.Bytes, second.Bytes) {
				t.Fatal("repeated normalization produced different bytes")
			}
			if first.Extension != tc.extension || first.ContentType != tc.contentType {
				t.Fatalf("format = %q/%q, want %q/%q", first.Extension, first.ContentType, tc.extension, tc.contentType)
			}
			if first.Width != tc.width || first.Height != tc.height {
				t.Fatalf("dimensions = %dx%d, want %dx%d", first.Width, first.Height, tc.width, tc.height)
			}
			if len(first.Bytes) > MaxObjectBytes {
				t.Fatalf("normalized size = %d, exceeds %d", len(first.Bytes), MaxObjectBytes)
			}
			assertCanonicalOutput(t, first)
		})
	}
}

func TestNormalizeAppliesEveryExifOrientation(t *testing.T) {
	source := image.NewNRGBA(image.Rect(0, 0, 2, 3))
	colors := []color.NRGBA{
		{R: 10, A: 128}, {G: 20, A: 128},
		{B: 30, A: 128}, {R: 40, G: 50, A: 128},
		{R: 60, B: 70, A: 128}, {G: 80, B: 90, A: 128},
	}
	for i, c := range colors {
		source.SetNRGBA(i%2, i/2, c)
	}

	wants := map[uint16][]int{
		1: {0, 1, 2, 3, 4, 5},
		2: {1, 0, 3, 2, 5, 4},
		3: {5, 4, 3, 2, 1, 0},
		4: {4, 5, 2, 3, 0, 1},
		5: {0, 2, 4, 1, 3, 5},
		6: {4, 2, 0, 5, 3, 1},
		7: {5, 3, 1, 4, 2, 0},
		8: {1, 3, 5, 0, 2, 4},
	}
	for orientation := uint16(1); orientation <= 8; orientation++ {
		t.Run(string(rune('0'+orientation)), func(t *testing.T) {
			encoded := encodePNG(t, source)
			encoded = insertPNGChunk(t, encoded, "eXIf", tiffOrientation(orientation), "IDAT")
			got, err := NormalizePhoto(encoded)
			if err != nil {
				t.Fatalf("NormalizePhoto: %v", err)
			}
			decoded, err := png.Decode(bytes.NewReader(got.Bytes))
			if err != nil {
				t.Fatalf("decode normalized PNG: %v", err)
			}
			wantWidth, wantHeight := 2, 3
			if orientation >= 5 {
				wantWidth, wantHeight = 3, 2
			}
			if got.Width != wantWidth || got.Height != wantHeight {
				t.Fatalf("dimensions = %dx%d, want %dx%d", got.Width, got.Height, wantWidth, wantHeight)
			}
			for i, sourceIndex := range wants[orientation] {
				want := colors[sourceIndex]
				actualColor := color.NRGBAModel.Convert(decoded.At(i%wantWidth, i/wantWidth))
				actual, ok := actualColor.(color.NRGBA)
				if !ok {
					t.Fatalf("converted pixel type = %T, want color.NRGBA", actualColor)
				}
				if actual != want {
					t.Fatalf("pixel %d = %#v, want source pixel %d %#v", i, actual, sourceIndex, want)
				}
			}
		})
	}
}

func TestNormalizeRejectsUnsafeRecognizedContainers(t *testing.T) {
	validPNG := encodePNG(t, image.NewNRGBA(image.Rect(0, 0, 2, 2)))
	validJPEG := encodeJPEG(t, image.NewNRGBA(image.Rect(0, 0, 2, 2)), 85)
	validWebP := readFixture(t, "blue-purple-pink-large.lossless.webp")

	tests := []struct {
		name   string
		input  []byte
		reason PhotoInvalidReason
	}{
		{"truncated jpeg", validJPEG[:len(validJPEG)-2], ReasonMalformed},
		{"truncated png", validPNG[:len(validPNG)-5], ReasonMalformed},
		{"truncated webp", validWebP[:len(validWebP)-1], ReasonMalformed},
		{"jpeg trailing bytes", appendCopy(validJPEG, []byte("<html>")), ReasonTrailingData},
		{"png trailing bytes", appendCopy(validPNG, []byte("<html>")), ReasonTrailingData},
		{"webp trailing bytes", appendCopy(validWebP, []byte("<html>")), ReasonTrailingData},
		{"apng", insertPNGChunk(t, validPNG, "acTL", []byte{0, 0, 0, 1, 0, 0, 0, 0}, "IDAT"), ReasonAnimated},
		{"animated webp", animatedWebPHeader(), ReasonAnimated},
		{"zero width", dimensionsOnlyPNG(t, 0, 1), ReasonDimensions},
		{"edge over limit", dimensionsOnlyPNG(t, 8193, 1), ReasonDimensions},
		{"pixel product over limit", dimensionsOnlyPNG(t, 4097, 4096), ReasonDimensions},
		{"overflow shaped dimensions", dimensionsOnlyPNG(t, 0xffffffff, 0xffffffff), ReasonDimensions},
		{"malformed png chunk length", corruptPNGChunkLength(validPNG), ReasonMalformed},
		{"malformed jpeg segment length", []byte{0xff, 0xd8, 0xff, 0xe0, 0, 1, 0xff, 0xd9}, ReasonMalformed},
		{"webp dimension disagreement", webPWithVP8XDimensions(t, validWebP, 1, 1), ReasonDimensions},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := NormalizePhoto(tc.input)
			assertInvalidReason(t, err, tc.reason)
		})
	}
}

func TestNormalizeRejectsBadOrientationMetadata(t *testing.T) {
	base := encodePNG(t, image.NewNRGBA(image.Rect(0, 0, 2, 2)))
	tests := []struct {
		name  string
		input []byte
	}{
		{"out of range", insertPNGChunk(t, base, "eXIf", tiffOrientation(9), "IDAT")},
		{"malformed TIFF", insertPNGChunk(t, base, "eXIf", []byte("not-tiff"), "IDAT")},
		{"duplicate", insertPNGChunk(t, insertPNGChunk(t, base, "eXIf", tiffOrientation(1), "IDAT"), "eXIf", tiffOrientation(1), "IDAT")},
		{"conflicting", insertPNGChunk(t, insertPNGChunk(t, base, "eXIf", tiffOrientation(1), "IDAT"), "eXIf", tiffOrientation(2), "IDAT")},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := NormalizePhoto(tc.input)
			assertInvalidReason(t, err, ReasonOrientation)
		})
	}
}

func TestNormalizeRejectsUnsupportedBytes(t *testing.T) {
	_, err := NormalizePhoto([]byte("GIF89a"))
	if !errors.Is(err, ErrUnsupportedMediaType) {
		t.Fatalf("error = %v, want ErrUnsupportedMediaType", err)
	}
}

func TestNormalizeStripsMetadataAndSourceChunks(t *testing.T) {
	const privateMarker = `<script data-private="true">GPS XMP IPTC COMMENT THUMBNAIL ICC</script>`
	t.Run("PNG text profile and Exif forms", func(t *testing.T) {
		base := encodePNG(t, image.NewNRGBA(image.Rect(0, 0, 8, 8)))
		withMetadata := insertPNGChunk(t, base, "iCCP", []byte("profile\x00\x00"+privateMarker), "IDAT")
		withMetadata = insertPNGChunk(t, withMetadata, "tEXt", []byte("Comment\x00"+privateMarker), "IDAT")
		withMetadata = insertPNGChunk(t, withMetadata, "zTXt", []byte("XMP\x00\x00"+privateMarker), "IDAT")
		withMetadata = insertPNGChunk(t, withMetadata, "iTXt", []byte("XML:com.adobe.xmp\x00\x00\x00\x00\x00"+privateMarker), "IDAT")
		withMetadata = insertPNGChunk(t, withMetadata, "eXIf", append(tiffOrientation(1), []byte(privateMarker)...), "IDAT")
		assertNormalizedMetadataFree(t, withMetadata, privateMarker, "iCCP", "tEXt", "zTXt", "iTXt", "eXIf")
	})

	t.Run("JPEG Exif GPS XMP IPTC comment thumbnail and ICC forms", func(t *testing.T) {
		base := encodeJPEG(t, image.NewNRGBA(image.Rect(0, 0, 2, 3)), 90)
		segments := []jpegTestSegment{
			{marker: 0xe1, payload: append(append([]byte("Exif\x00\x00"), tiffOrientation(6)...), []byte("GPS thumbnail "+privateMarker)...)},
			{marker: 0xe1, payload: []byte("http://ns.adobe.com/xap/1.0/\x00<xmp>" + privateMarker + "</xmp>")},
			{marker: 0xe2, payload: []byte("ICC_PROFILE\x00\x01\x01" + privateMarker)},
			{marker: 0xed, payload: []byte("Photoshop 3.0\x008BIM IPTC " + privateMarker)},
			{marker: 0xfe, payload: []byte("comment " + privateMarker)},
		}
		got := assertNormalizedMetadataFree(t, insertJPEGSegments(t, base, segments...), privateMarker,
			"Exif", "http://ns.adobe.com/xap", "ICC_PROFILE", "Photoshop 3.0", "comment")
		if got.Width != 3 || got.Height != 2 {
			t.Fatalf("JPEG Exif orientation dimensions = %dx%d, want 3x2", got.Width, got.Height)
		}
	})

	t.Run("WebP Exif XMP and ICC forms", func(t *testing.T) {
		base := readFixture(t, "blue-purple-pink-large.lossless.webp")
		withMetadata := webPWithMetadata(t, base, 600, 400, []webPTestChunk{
			{kind: "ICCP", data: []byte("ICC " + privateMarker)},
			{kind: "EXIF", data: append(tiffOrientation(6), []byte("GPS thumbnail "+privateMarker)...)},
			{kind: "XMP ", data: []byte("<xmp>" + privateMarker + "</xmp>")},
		})
		got := assertNormalizedMetadataFree(t, withMetadata, privateMarker, "ICCP", "EXIF", "XMP ")
		if got.Width != 400 || got.Height != 600 {
			t.Fatalf("WebP Exif orientation dimensions = %dx%d, want 400x600", got.Width, got.Height)
		}
	})
}

func assertNormalizedMetadataFree(t *testing.T, source []byte, marker string, forms ...string) NormalizedPhoto {
	t.Helper()
	got, err := NormalizePhoto(source)
	if err != nil {
		t.Fatalf("NormalizePhoto: %v", err)
	}
	if bytes.Contains(got.Bytes, []byte(marker)) {
		t.Fatalf("normalized output retained private metadata marker %q", marker)
	}
	for _, form := range forms {
		if bytes.Contains(got.Bytes, []byte(form)) {
			t.Fatalf("normalized output retained source metadata form %q", form)
		}
	}
	return got
}

func TestNormalizeUsesFixedOpaqueDownscaleLadder(t *testing.T) {
	if testing.Short() {
		t.Skip("large deterministic image")
	}
	imageData := image.NewNRGBA(image.Rect(0, 0, 2048, 2048))
	var state uint32 = 1
	for y := range 2048 {
		for x := range 2048 {
			state = state*1664525 + 1013904223
			i := imageData.PixOffset(x, y)
			imageData.Pix[i+0] = byte(state)
			imageData.Pix[i+1] = byte(state >> 8)
			imageData.Pix[i+2] = byte(state >> 16)
			imageData.Pix[i+3] = 255
		}
	}
	source := encodeJPEG(t, imageData, 45)
	if len(source) > MaxObjectBytes {
		t.Fatalf("test source = %d bytes, exceeds intake limit", len(source))
	}
	got, err := NormalizePhoto(source)
	if err != nil {
		t.Fatalf("NormalizePhoto: %v", err)
	}
	if len(got.Bytes) > MaxObjectBytes {
		t.Fatalf("normalized size = %d, exceeds limit", len(got.Bytes))
	}
	allowed := map[int]bool{2048: true, 1792: true, 1536: true, 1280: true, 1024: true, 768: true, 512: true}
	if !allowed[got.Width] || got.Width != got.Height {
		t.Fatalf("dimensions = %dx%d, not on fixed opaque ladder", got.Width, got.Height)
	}
}

func TestNormalizeUsesFixedAlphaDownscaleLadder(t *testing.T) {
	source := image.NewNRGBA(image.Rect(0, 0, 2048, 2048))
	source.SetNRGBA(0, 0, color.NRGBA{A: 128})

	var attempted []int
	encoder := func(w io.Writer, img image.Image) error {
		attempted = append(attempted, max(img.Bounds().Dx(), img.Bounds().Dy()))
		switch attempted[len(attempted)-1] {
		case 1024:
			_, err := w.Write(bytes.Repeat([]byte{0}, MaxObjectBytes+1))
			return err
		case 896:
			canonical := png.Encoder{CompressionLevel: png.BestSpeed}
			return canonical.Encode(w, img)
		default:
			t.Fatalf("unexpected alpha ladder rung %d", attempted[len(attempted)-1])
			return nil
		}
	}

	got, err := normalizePNGWithEncoder(source, 1, encoder)
	if err != nil {
		t.Fatalf("normalizePNGWithEncoder: %v", err)
	}
	if len(attempted) != 2 || attempted[0] != 1024 || attempted[1] != 896 {
		t.Fatalf("attempted widths = %v, want [1024 896]", attempted)
	}
	if got.Extension != "png" || got.ContentType != "image/png" {
		t.Fatalf("format = %q/%q, want png/image/png", got.Extension, got.ContentType)
	}
	if got.Width != 896 || got.Height != 896 {
		t.Fatalf("dimensions = %dx%d, want 896x896", got.Width, got.Height)
	}
	if len(got.Bytes) > MaxObjectBytes {
		t.Fatalf("normalized size = %d, exceeds %d", len(got.Bytes), MaxObjectBytes)
	}
	assertCanonicalOutput(t, got)
}

func TestNormalizationBudget(t *testing.T) {
	if os.Getenv("ABOUTME_RUN_NORMALIZATION_BENCHMARK") != "1" {
		t.Skip("set ABOUTME_RUN_NORMALIZATION_BENCHMARK=1 to run the controlled-cgroup normalization benchmark")
	}
	if helperMode := os.Getenv("ABOUTME_NORMALIZATION_BUDGET_HELPER"); helperMode != "" {
		if helperMode == "baseline" {
			return
		}
		if helperMode != "normalize" {
			t.Fatalf("unknown normalization helper mode %q", helperMode)
		}
		source := readFixture(t, os.Getenv("ABOUTME_NORMALIZATION_BUDGET_FIXTURE"))
		started := time.Now()
		if _, err := NormalizePhoto(source); err != nil {
			t.Fatalf("NormalizePhoto: %v", err)
		}
		fmt.Printf("ABOUTME_NORMALIZATION_DURATION_NS=%d\n", time.Since(started).Nanoseconds())
		return
	}

	manifestBytes, err := os.ReadFile(filepath.Join("testdata", "normalization-benchmark-manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	var manifest struct {
		Protocol struct {
			WarmupsPerFixture         int    `json:"warmupsPerFixture"`
			MeasuredSamplesPerFixture int    `json:"measuredSamplesPerFixture"`
			FreshHelperProcess        bool   `json:"freshHelperProcessPerSample"`
			MaxDurationMilliseconds   int    `json:"maxDurationMilliseconds"`
			MaxRSSDeltaBytes          int64  `json:"maxRssDeltaBytes"`
			RawEvidencePath           string `json:"rawEvidencePath"`
		} `json:"protocol"`
		Fixtures []struct {
			Path   string `json:"path"`
			SHA256 string `json:"sha256"`
			Bytes  int    `json:"bytes"`
		} `json:"fixtures"`
	}
	if decodeErr := json.Unmarshal(manifestBytes, &manifest); decodeErr != nil {
		t.Fatalf("decode manifest: %v", decodeErr)
	}
	if len(manifest.Fixtures) == 0 || manifest.Protocol.WarmupsPerFixture != 3 ||
		manifest.Protocol.MeasuredSamplesPerFixture != 20 || !manifest.Protocol.FreshHelperProcess ||
		manifest.Protocol.MaxDurationMilliseconds != 5000 || manifest.Protocol.MaxRSSDeltaBytes != 192*1024*1024 ||
		manifest.Protocol.RawEvidencePath == "" {
		t.Fatal("manifest is missing the frozen resource protocol")
	}

	executable, err := os.Executable()
	if err != nil {
		t.Fatalf("test executable: %v", err)
	}
	type sample struct {
		Fixture          string `json:"fixture"`
		Sample           int    `json:"sample"`
		DurationNS       int64  `json:"durationNanoseconds"`
		MaximumRSSBytes  int64  `json:"maximumRssBytes"`
		BaselineRSSBytes int64  `json:"baselineRssBytes"`
		RSSDeltaBytes    int64  `json:"rssDeltaBytes"`
	}
	evidence := struct {
		ManifestSHA256 string   `json:"manifestSha256"`
		GeneratedAt    string   `json:"generatedAt"`
		Samples        []sample `json:"samples"`
	}{
		ManifestSHA256: hex.EncodeToString(sha256Sum(manifestBytes)),
		GeneratedAt:    time.Now().UTC().Format(time.RFC3339Nano),
	}

	for _, fixture := range manifest.Fixtures {
		source := readFixture(t, fixture.Path)
		if len(source) != fixture.Bytes {
			t.Fatalf("%s size = %d, want %d", fixture.Path, len(source), fixture.Bytes)
		}
		digest := sha256.Sum256(source)
		if hex.EncodeToString(digest[:]) != fixture.SHA256 {
			t.Fatalf("%s hash drifted", fixture.Path)
		}
		for warmup := 0; warmup < manifest.Protocol.WarmupsPerFixture; warmup++ {
			runNormalizationWarmup(t, executable, fixture.Path)
		}
		baselineRSS := runTimedNormalizationHelper(t, executable, "baseline", "").maximumRSSBytes
		for index := 0; index < manifest.Protocol.MeasuredSamplesPerFixture; index++ {
			measurement := runTimedNormalizationHelper(t, executable, "normalize", fixture.Path)
			delta := measurement.maximumRSSBytes - baselineRSS
			if delta < 0 {
				delta = 0
			}
			evidence.Samples = append(evidence.Samples, sample{
				Fixture: fixture.Path, Sample: index + 1, DurationNS: measurement.durationNS,
				MaximumRSSBytes: measurement.maximumRSSBytes, BaselineRSSBytes: baselineRSS, RSSDeltaBytes: delta,
			})
			if measurement.durationNS > int64(time.Duration(manifest.Protocol.MaxDurationMilliseconds)*time.Millisecond) {
				t.Fatalf("NormalizePhoto(%s) sample %d took %v", fixture.Path, index+1, time.Duration(measurement.durationNS))
			}
			if delta > manifest.Protocol.MaxRSSDeltaBytes {
				t.Fatalf("NormalizePhoto(%s) sample %d RSS delta = %d, exceeds %d", fixture.Path, index+1, delta, manifest.Protocol.MaxRSSDeltaBytes)
			}
		}
	}
	evidenceBytes, err := json.MarshalIndent(evidence, "", "  ")
	if err != nil {
		t.Fatalf("encode resource evidence: %v", err)
	}
	repositoryRoot, err := filepath.Abs(filepath.Join("..", "..", "..", ".."))
	if err != nil {
		t.Fatalf("repository root: %v", err)
	}
	evidencePath := filepath.Join(repositoryRoot, filepath.FromSlash(manifest.Protocol.RawEvidencePath))
	if err := os.MkdirAll(filepath.Dir(evidencePath), 0o755); err != nil {
		t.Fatalf("create evidence directory: %v", err)
	}
	if err := os.WriteFile(evidencePath, append(evidenceBytes, '\n'), 0o644); err != nil {
		t.Fatalf("write resource evidence: %v", err)
	}
	t.Logf("wrote %d samples to %s", len(evidence.Samples), evidencePath)
}

func TestNormalizationBudgetRequiresExplicitOptIn(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatalf("test executable: %v", err)
	}
	before, existedBefore := normalizationBudgetEvidenceSnapshot(t)

	command := exec.CommandContext(t.Context(), executable, "-test.run=^TestNormalizationBudget$", "-test.count=1", "-test.v")
	command.Env = normalizationBudgetSubprocessEnv(false)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("ordinary normalization test invocation: %v\n%s", err, output)
	}
	if !bytes.Contains(output, []byte("--- SKIP: TestNormalizationBudget")) {
		t.Fatalf("ordinary normalization test invocation did not skip:\n%s", output)
	}
	after, existsAfter := normalizationBudgetEvidenceSnapshot(t)
	if existedBefore != existsAfter || !bytes.Equal(before, after) {
		t.Fatal("ordinary normalization test invocation changed the authoritative evidence artifact")
	}

	command = exec.CommandContext(t.Context(), executable, "-test.run=^TestNormalizationBudget$", "-test.count=1", "-test.v")
	command.Env = normalizationBudgetSubprocessEnv(true)
	output, err = command.CombinedOutput()
	if err == nil || !bytes.Contains(output, []byte(`unknown normalization helper mode "opt-in-probe"`)) {
		t.Fatalf("opted-in invocation did not reach normalization benchmark behavior: err=%v\n%s", err, output)
	}
}

func normalizationBudgetSubprocessEnv(optIn bool) []string {
	const (
		optInKey  = "ABOUTME_RUN_NORMALIZATION_BENCHMARK"
		helperKey = "ABOUTME_NORMALIZATION_BUDGET_HELPER"
	)
	environment := make([]string, 0, len(os.Environ())+2)
	for _, entry := range os.Environ() {
		if strings.HasPrefix(entry, optInKey+"=") || strings.HasPrefix(entry, helperKey+"=") {
			continue
		}
		environment = append(environment, entry)
	}
	if optIn {
		environment = append(environment, optInKey+"=1")
	}
	return append(environment, helperKey+"=opt-in-probe")
}

func normalizationBudgetEvidenceSnapshot(t *testing.T) ([]byte, bool) {
	t.Helper()
	manifestBytes, err := os.ReadFile(filepath.Join("testdata", "normalization-benchmark-manifest.json"))
	if err != nil {
		t.Fatalf("read normalization benchmark manifest: %v", err)
	}
	var manifest struct {
		Protocol struct {
			RawEvidencePath string `json:"rawEvidencePath"`
		} `json:"protocol"`
	}
	if decodeErr := json.Unmarshal(manifestBytes, &manifest); decodeErr != nil {
		t.Fatalf("decode normalization benchmark manifest: %v", decodeErr)
	}
	repositoryRoot, err := filepath.Abs(filepath.Join("..", "..", "..", ".."))
	if err != nil {
		t.Fatalf("repository root: %v", err)
	}
	evidence, err := os.ReadFile(filepath.Join(repositoryRoot, filepath.FromSlash(manifest.Protocol.RawEvidencePath)))
	if errors.Is(err, os.ErrNotExist) {
		return nil, false
	}
	if err != nil {
		t.Fatalf("read normalization benchmark evidence: %v", err)
	}
	return evidence, true
}

type normalizationMeasurement struct {
	durationNS      int64
	maximumRSSBytes int64
}

func runNormalizationWarmup(t *testing.T, executable, fixture string) {
	t.Helper()
	command := exec.CommandContext(t.Context(), executable, "-test.run=^TestNormalizationBudget$", "-test.count=1")
	command.Env = append(os.Environ(),
		"ABOUTME_NORMALIZATION_BUDGET_HELPER=normalize",
		"ABOUTME_NORMALIZATION_BUDGET_FIXTURE="+fixture,
	)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("normalization warm-up for %s: %v\n%s", fixture, err, output)
	}
}

func runTimedNormalizationHelper(t *testing.T, executable, mode, fixture string) normalizationMeasurement {
	t.Helper()
	command := exec.CommandContext(t.Context(), "/usr/bin/time", "-v", executable, "-test.run=^TestNormalizationBudget$", "-test.count=1")
	command.Env = append(os.Environ(),
		"LC_ALL=C",
		"ABOUTME_NORMALIZATION_BUDGET_HELPER="+mode,
		"ABOUTME_NORMALIZATION_BUDGET_FIXTURE="+fixture,
	)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("timed normalization helper (%s, %s): %v\n%s", mode, fixture, err, output)
	}
	measurement := normalizationMeasurement{}
	for _, line := range strings.Split(string(output), "\n") {
		line = strings.TrimSpace(line)
		if value, ok := strings.CutPrefix(line, "ABOUTME_NORMALIZATION_DURATION_NS="); ok {
			measurement.durationNS, err = strconv.ParseInt(value, 10, 64)
			if err != nil {
				t.Fatalf("parse helper duration %q: %v", value, err)
			}
		}
		if value, ok := strings.CutPrefix(line, "Maximum resident set size (kbytes):"); ok {
			kilobytes, parseErr := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
			if parseErr != nil {
				t.Fatalf("parse helper RSS %q: %v", value, parseErr)
			}
			measurement.maximumRSSBytes = kilobytes * 1024
		}
	}
	if measurement.maximumRSSBytes == 0 || (mode == "normalize" && measurement.durationNS == 0) {
		t.Fatalf("timed helper output is missing measurements:\n%s", output)
	}
	return measurement
}

func sha256Sum(data []byte) []byte {
	digest := sha256.Sum256(data)
	return digest[:]
}

func assertCanonicalOutput(t *testing.T, got NormalizedPhoto) {
	t.Helper()
	switch got.Extension {
	case "jpg":
		cfg, err := jpeg.DecodeConfig(bytes.NewReader(got.Bytes))
		if err != nil {
			t.Fatalf("decode JPEG config: %v", err)
		}
		if cfg.Width != got.Width || cfg.Height != got.Height {
			t.Fatalf("JPEG dimensions = %dx%d, result says %dx%d", cfg.Width, cfg.Height, got.Width, got.Height)
		}
		assertBaseline420JPEG(t, got.Bytes)
	case "png":
		cfg, err := png.DecodeConfig(bytes.NewReader(got.Bytes))
		if err != nil {
			t.Fatalf("decode PNG config: %v", err)
		}
		if cfg.Width != got.Width || cfg.Height != got.Height {
			t.Fatalf("PNG dimensions = %dx%d, result says %dx%d", cfg.Width, cfg.Height, got.Width, got.Height)
		}
		if got.Bytes[28] != 0 {
			t.Fatalf("PNG interlace method = %d, want 0", got.Bytes[28])
		}
	default:
		t.Fatalf("unexpected extension %q", got.Extension)
	}
}

func assertBaseline420JPEG(t *testing.T, data []byte) {
	t.Helper()
	for pos := 2; pos+4 <= len(data); {
		if data[pos] != 0xff {
			t.Fatalf("invalid JPEG marker at %d", pos)
		}
		marker := data[pos+1]
		pos += 2
		if marker == 0xc0 {
			length := int(binary.BigEndian.Uint16(data[pos : pos+2]))
			segment := data[pos+2 : pos+length]
			if len(segment) < 15 || segment[5] != 3 || segment[7] != 0x22 || segment[10] != 0x11 || segment[13] != 0x11 {
				t.Fatalf("JPEG SOF0 is not 3-component 4:2:0: %x", segment)
			}
			return
		}
		if marker == 0xd9 || marker == 0xda {
			break
		}
		length := int(binary.BigEndian.Uint16(data[pos : pos+2]))
		pos += length
	}
	t.Fatal("baseline SOF0 marker not found")
}

func assertInvalidReason(t *testing.T, err error, want PhotoInvalidReason) {
	t.Helper()
	var invalid *PhotoInvalidError
	if !errors.As(err, &invalid) {
		t.Fatalf("error = %v, want PhotoInvalidError(%q)", err, want)
	}
	if invalid.Reason != want {
		t.Fatalf("reason = %q, want %q", invalid.Reason, want)
	}
	if strings.Contains(invalid.Error(), "EOF") || strings.Contains(invalid.Error(), "chunk") {
		t.Fatalf("client-facing error leaked decoder detail: %q", invalid.Error())
	}
}

func readFixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func encodePNG(t *testing.T, source image.Image) []byte {
	t.Helper()
	var out bytes.Buffer
	encoder := png.Encoder{CompressionLevel: png.BestSpeed}
	if err := encoder.Encode(&out, source); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

func encodeJPEG(t *testing.T, source image.Image, quality int) []byte {
	t.Helper()
	var out bytes.Buffer
	if err := jpeg.Encode(&out, source, &jpeg.Options{Quality: quality}); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

func insertPNGChunk(t *testing.T, source []byte, kind string, data []byte, before string) []byte {
	t.Helper()
	for pos := 8; pos+12 <= len(source); {
		length := int(binary.BigEndian.Uint32(source[pos : pos+4]))
		end := pos + 12 + length
		if end > len(source) {
			t.Fatal("invalid PNG test fixture")
		}
		if string(source[pos+4:pos+8]) == before {
			chunk := make([]byte, 12+len(data))
			binary.BigEndian.PutUint32(chunk[:4], uint32(len(data)))
			copy(chunk[4:8], kind)
			copy(chunk[8:8+len(data)], data)
			binary.BigEndian.PutUint32(chunk[8+len(data):], crc32.ChecksumIEEE(chunk[4:8+len(data)]))
			out := append([]byte(nil), source[:pos]...)
			out = append(out, chunk...)
			return append(out, source[pos:]...)
		}
		pos = end
	}
	t.Fatalf("PNG chunk %q not found", before)
	return nil
}

func tiffOrientation(value uint16) []byte {
	out := make([]byte, 26)
	copy(out[:4], []byte{'I', 'I', 42, 0})
	binary.LittleEndian.PutUint32(out[4:8], 8)
	binary.LittleEndian.PutUint16(out[8:10], 1)
	binary.LittleEndian.PutUint16(out[10:12], 0x0112)
	binary.LittleEndian.PutUint16(out[12:14], 3)
	binary.LittleEndian.PutUint32(out[14:18], 1)
	binary.LittleEndian.PutUint16(out[18:20], value)
	return out
}

type jpegTestSegment struct {
	marker  byte
	payload []byte
}

func insertJPEGSegments(t *testing.T, source []byte, segments ...jpegTestSegment) []byte {
	t.Helper()
	if len(source) < 2 || !bytes.Equal(source[:2], []byte{0xff, 0xd8}) {
		t.Fatal("invalid JPEG test fixture")
	}
	out := append([]byte(nil), source[:2]...)
	for _, segment := range segments {
		if len(segment.payload)+2 > 0xffff {
			t.Fatal("JPEG test segment exceeds its length field")
		}
		header := []byte{0xff, segment.marker, 0, 0}
		binary.BigEndian.PutUint16(header[2:], uint16(len(segment.payload)+2))
		out = append(out, header...)
		out = append(out, segment.payload...)
	}
	return append(out, source[2:]...)
}

type webPTestChunk struct {
	kind string
	data []byte
}

func webPWithMetadata(t *testing.T, source []byte, width, height uint32, chunks []webPTestChunk) []byte {
	t.Helper()
	if len(source) < 12 || string(source[:4]) != "RIFF" || string(source[8:12]) != "WEBP" {
		t.Fatal("invalid WebP test fixture")
	}
	vp8x := make([]byte, 10)
	vp8x[0] = 0x3c // ICC, alpha, Exif, and XMP flags.
	putUint24LE(vp8x[4:7], width-1)
	putUint24LE(vp8x[7:10], height-1)
	out := append([]byte(nil), source[:12]...)
	out = appendWebPTestChunk(t, out, "VP8X", vp8x)
	for _, chunk := range chunks {
		out = appendWebPTestChunk(t, out, chunk.kind, chunk.data)
	}
	out = append(out, source[12:]...)
	binary.LittleEndian.PutUint32(out[4:8], uint32(len(out)-8))
	return out
}

func appendWebPTestChunk(t *testing.T, target []byte, kind string, data []byte) []byte {
	t.Helper()
	if len(kind) != 4 {
		t.Fatalf("WebP test chunk kind %q does not have four bytes", kind)
	}
	target = append(target, kind...)
	length := make([]byte, 4)
	binary.LittleEndian.PutUint32(length, uint32(len(data)))
	target = append(target, length...)
	target = append(target, data...)
	if len(data)%2 == 1 {
		target = append(target, 0)
	}
	return target
}

func dimensionsOnlyPNG(t *testing.T, width, height uint32) []byte {
	t.Helper()
	data := make([]byte, 13)
	binary.BigEndian.PutUint32(data[0:4], width)
	binary.BigEndian.PutUint32(data[4:8], height)
	data[8], data[9], data[10], data[11], data[12] = 8, 6, 0, 0, 0
	base := []byte{137, 80, 78, 71, 13, 10, 26, 10}
	base = appendPNGChunk(base, "IHDR", data)
	return appendPNGChunk(base, "IEND", nil)
}

func appendPNGChunk(target []byte, kind string, data []byte) []byte {
	chunk := make([]byte, 12+len(data))
	binary.BigEndian.PutUint32(chunk[:4], uint32(len(data)))
	copy(chunk[4:8], kind)
	copy(chunk[8:], data)
	binary.BigEndian.PutUint32(chunk[8+len(data):], crc32.ChecksumIEEE(chunk[4:8+len(data)]))
	return append(target, chunk...)
}

func corruptPNGChunkLength(source []byte) []byte {
	out := append([]byte(nil), source...)
	binary.BigEndian.PutUint32(out[8:12], ^uint32(0))
	return out
}

func animatedWebPHeader() []byte {
	chunk := []byte{'V', 'P', '8', 'X', 10, 0, 0, 0, 0x02, 0, 0, 0, 0, 0, 0, 0, 0, 0}
	out := append([]byte("RIFF"), make([]byte, 4)...)
	out = append(out, []byte("WEBP")...)
	out = append(out, chunk...)
	binary.LittleEndian.PutUint32(out[4:8], uint32(len(out)-8))
	return out
}

func webPWithVP8XDimensions(t *testing.T, source []byte, width, height uint32) []byte {
	t.Helper()
	if len(source) < 12 || string(source[:4]) != "RIFF" {
		t.Fatal("invalid WebP fixture")
	}
	payload := make([]byte, 10)
	putUint24LE(payload[4:7], width-1)
	putUint24LE(payload[7:10], height-1)
	chunk := append([]byte{'V', 'P', '8', 'X', 10, 0, 0, 0}, payload...)
	out := append([]byte(nil), source[:12]...)
	out = append(out, chunk...)
	out = append(out, source[12:]...)
	binary.LittleEndian.PutUint32(out[4:8], uint32(len(out)-8))
	return out
}

func putUint24LE(target []byte, value uint32) {
	target[0], target[1], target[2] = byte(value), byte(value>>8), byte(value>>16)
}

func appendCopy(left, right []byte) []byte {
	out := append([]byte(nil), left...)
	return append(out, right...)
}
