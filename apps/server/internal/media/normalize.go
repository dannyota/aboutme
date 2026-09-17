package media

import (
	"bytes"
	"errors"
	"image"
	"image/jpeg"
	"image/png"
	"math"
	"runtime"
	"runtime/debug"
	"sync"

	"golang.org/x/image/webp"
)

const (
	maxSourceEdge   = 8_192
	maxSourcePixels = 16_777_216
	jpegQuality     = 85
)

// ErrUnsupportedMediaType reports bytes that are not JPEG, PNG, or WebP.
var ErrUnsupportedMediaType = errors.New("media: unsupported photo type")

var normalizationRuntimeMu sync.Mutex

// PhotoInvalidReason is the closed client-safe normalization failure reason.
type PhotoInvalidReason string

// Closed client-safe media_invalid reasons.
const (
	ReasonMalformed           PhotoInvalidReason = "malformed"
	ReasonAnimated            PhotoInvalidReason = "animated"
	ReasonDimensions          PhotoInvalidReason = "dimensions"
	ReasonOrientation         PhotoInvalidReason = "orientation"
	ReasonTrailingData        PhotoInvalidReason = "trailing_data"
	ReasonNormalizationFailed PhotoInvalidReason = "normalization_failed"
)

// PhotoInvalidError exposes only D19's closed reason vocabulary.
type PhotoInvalidError struct {
	Reason PhotoInvalidReason
}

func (e *PhotoInvalidError) Error() string {
	return "media: invalid photo (" + string(e.Reason) + ")"
}

// NormalizedPhoto is a canonical bounded JPEG or PNG and its metadata.
type NormalizedPhoto struct {
	Bytes       []byte
	ContentType string
	Extension   string
	Width       int
	Height      int
}

type photoContainer uint8

const (
	containerJPEG photoContainer = iota + 1
	containerPNG
	containerWebP
)

type containerInfo struct {
	kind        photoContainer
	width       int
	height      int
	orientation int
}

func invalidPhoto(reason PhotoInvalidReason) error {
	return &PhotoInvalidError{Reason: reason}
}

// NormalizePhoto validates and fully decodes one JPEG, PNG, or WebP before
// emitting metadata-free canonical JPEG or PNG bytes.
func NormalizePhoto(source []byte) (NormalizedPhoto, error) {
	normalizationRuntimeMu.Lock()
	defer normalizationRuntimeMu.Unlock()
	var memory runtime.MemStats
	runtime.ReadMemStats(&memory)
	memoryLimit := int64(180 * 1024 * 1024)
	if memory.HeapAlloc > math.MaxInt64-uint64(memoryLimit) {
		memoryLimit = math.MaxInt64
	} else {
		memoryLimit += int64(memory.HeapAlloc) //nolint:gosec // guarded above by the MaxInt64 comparison.
	}
	previousMemoryLimit := debug.SetMemoryLimit(memoryLimit)
	defer debug.SetMemoryLimit(previousMemoryLimit)

	info, err := scanPhotoContainer(source)
	if err != nil {
		return NormalizedPhoto{}, err
	}
	if dimensionsErr := checkPhotoDimensions(info.width, info.height); dimensionsErr != nil {
		return NormalizedPhoto{}, dimensionsErr
	}

	configured, err := decodePhotoConfig(info.kind, source)
	if err != nil {
		return NormalizedPhoto{}, invalidPhoto(ReasonMalformed)
	}
	if configured.Width != info.width || configured.Height != info.height {
		return NormalizedPhoto{}, invalidPhoto(ReasonDimensions)
	}
	if dimensionsErr := checkPhotoDimensions(configured.Width, configured.Height); dimensionsErr != nil {
		return NormalizedPhoto{}, dimensionsErr
	}

	decoded, err := decodePhoto(info.kind, source)
	if err != nil {
		return NormalizedPhoto{}, invalidPhoto(ReasonMalformed)
	}
	bounds := decoded.Bounds()
	if bounds.Dx() != info.width || bounds.Dy() != info.height {
		return NormalizedPhoto{}, invalidPhoto(ReasonDimensions)
	}
	if err := checkPhotoDimensions(bounds.Dx(), bounds.Dy()); err != nil {
		return NormalizedPhoto{}, err
	}
	decoded = compact16BitPhoto(decoded)

	if hasTransparency(decoded) {
		return normalizePNG(decoded, info.orientation)
	}
	return normalizeJPEG(decoded, info.orientation)
}

func compact16BitPhoto(source image.Image) image.Image {
	switch source := source.(type) {
	case *image.NRGBA64:
		pixels := source.Pix
		pixelCount := source.Bounds().Dx() * source.Bounds().Dy()
		for index := 0; index < pixelCount; index++ {
			from, to := index*8, index*4
			pixels[to+0] = pixels[from+0]
			pixels[to+1] = pixels[from+2]
			pixels[to+2] = pixels[from+4]
			pixels[to+3] = pixels[from+6]
		}
		return &image.NRGBA{Pix: pixels[:pixelCount*4], Stride: source.Bounds().Dx() * 4, Rect: source.Bounds()}
	case *image.RGBA64:
		pixels := source.Pix
		pixelCount := source.Bounds().Dx() * source.Bounds().Dy()
		for index := 0; index < pixelCount; index++ {
			from, to := index*8, index*4
			pixels[to+0] = pixels[from+0]
			pixels[to+1] = pixels[from+2]
			pixels[to+2] = pixels[from+4]
			pixels[to+3] = pixels[from+6]
		}
		return &image.RGBA{Pix: pixels[:pixelCount*4], Stride: source.Bounds().Dx() * 4, Rect: source.Bounds()}
	default:
		return source
	}
}

func scanPhotoContainer(source []byte) (containerInfo, error) {
	switch {
	case len(source) >= 2 && source[0] == 0xff && source[1] == 0xd8:
		return scanJPEG(source)
	case len(source) >= 8 && bytes.Equal(source[:8], []byte{137, 80, 78, 71, 13, 10, 26, 10}):
		return scanPNG(source)
	case len(source) >= 12 && string(source[:4]) == "RIFF" && string(source[8:12]) == "WEBP":
		return scanWebP(source)
	default:
		return containerInfo{}, ErrUnsupportedMediaType
	}
}

func checkPhotoDimensions(width, height int) error {
	if width < 1 || height < 1 || width > maxSourceEdge || height > maxSourceEdge {
		return invalidPhoto(ReasonDimensions)
	}
	if uint64(width)*uint64(height) > maxSourcePixels {
		return invalidPhoto(ReasonDimensions)
	}
	return nil
}

func decodePhotoConfig(kind photoContainer, source []byte) (image.Config, error) {
	reader := bytes.NewReader(source)
	switch kind {
	case containerJPEG:
		return jpeg.DecodeConfig(reader)
	case containerPNG:
		return png.DecodeConfig(reader)
	case containerWebP:
		return webp.DecodeConfig(reader)
	default:
		return image.Config{}, errors.New("media: unknown photo container")
	}
}

func decodePhoto(kind photoContainer, source []byte) (image.Image, error) {
	reader := bytes.NewReader(source)
	switch kind {
	case containerJPEG:
		return jpeg.Decode(reader)
	case containerPNG:
		return png.Decode(reader)
	case containerWebP:
		return webp.Decode(reader)
	default:
		return nil, errors.New("media: unknown photo container")
	}
}
