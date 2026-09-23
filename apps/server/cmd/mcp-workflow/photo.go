package main

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"math"
	"net/http"

	schema "github.com/dannyota/aboutme/packages/schema/gen/go"
)

var errPhoto = errors.New("photo_invalid")

const (
	minJPEGPSNR          = 35.0
	maxDecodedPhotoBytes = 2_097_152
	// Stored photos are normalized to these long-edge limits, so a larger
	// header is not a stored photo and is rejected before pixel decode.
	maxJPEGEdge = 2048
	maxPNGEdge  = 1024
)

// photoMeta is the server-owned photo reference in a document.
type photoMeta = schema.Photo

// photoBytes is one decoded photo held in memory only.
type photoBytes struct {
	ContentType string
	Data        []byte
}

type photoCrop struct {
	X      float64
	Y      float64
	Width  float64
	Height float64
}

// Decoders are variables so tests can prove header checks run first.
var (
	decodePNGConfig  = png.DecodeConfig
	decodeJPEGConfig = jpeg.DecodeConfig
	decodePNG        = png.Decode
	decodeJPEG       = jpeg.Decode
)

func cropFromSchema(crop *schema.PhotoCrop) *photoCrop {
	if crop == nil {
		return nil
	}
	return &photoCrop{X: crop.X, Y: crop.Y, Width: crop.Width, Height: crop.Height}
}

func (c *photoCrop) equal(other *photoCrop) bool {
	if c == nil || other == nil {
		return c == nil && other == nil
	}
	return *c == *other
}

func (c *photoCrop) valid() bool {
	if c == nil {
		return false
	}
	for _, value := range []float64{c.X, c.Y, c.Width, c.Height} {
		if math.IsNaN(value) || math.IsInf(value, 0) || value < 0 || value > 1 {
			return false
		}
	}
	return c.Width > 0 && c.Height > 0
}

func (c *photoCrop) arguments() map[string]any {
	return map[string]any{"x": c.X, "y": c.Y, "width": c.Width, "height": c.Height}
}

// validatePhotoBytes checks size, declared and sniffed media type, and header
// dimensions without decoding pixels.
func validatePhotoBytes(photo photoBytes) error {
	if len(photo.Data) == 0 || len(photo.Data) > maxDecodedPhotoBytes || http.DetectContentType(photo.Data) != photo.ContentType {
		return errPhoto
	}
	_, err := photoConfig(photo)
	return err
}

func photoConfig(photo photoBytes) (image.Config, error) {
	var (
		config  image.Config
		err     error
		maxEdge int
	)
	switch photo.ContentType {
	case "image/png":
		config, err = decodePNGConfig(bytes.NewReader(photo.Data))
		maxEdge = maxPNGEdge
	case "image/jpeg":
		config, err = decodeJPEGConfig(bytes.NewReader(photo.Data))
		maxEdge = maxJPEGEdge
	default:
		return image.Config{}, errPhoto
	}
	if err != nil || config.Width < 1 || config.Height < 1 || config.Width > maxEdge || config.Height > maxEdge {
		return image.Config{}, errPhoto
	}
	return config, nil
}

// decodeBoundedPhoto decodes pixels only after the header passed the size and
// dimension bounds, and requires the decoded bounds to match the header.
func decodeBoundedPhoto(photo photoBytes) (image.Image, error) {
	if validateErr := validatePhotoBytes(photo); validateErr != nil {
		return nil, validateErr
	}
	config, err := photoConfig(photo)
	if err != nil {
		return nil, err
	}
	var decoded image.Image
	if photo.ContentType == "image/png" {
		decoded, err = decodePNG(bytes.NewReader(photo.Data))
	} else {
		decoded, err = decodeJPEG(bytes.NewReader(photo.Data))
	}
	if err != nil || decoded.Bounds().Dx() != config.Width || decoded.Bounds().Dy() != config.Height {
		return nil, errPhoto
	}
	return decoded, nil
}

// validatePhotoParity applies the design's visual parity rule: same media
// type, dimensions, and alpha behavior; exact PNG pixels; JPEG PSNR at least
// 35 dB; and equal crops.
func validatePhotoParity(source photoBytes, sourceCrop *photoCrop, target photoBytes, targetCrop *photoCrop) error {
	if source.ContentType != target.ContentType || !sourceCrop.equal(targetCrop) || (sourceCrop != nil && !sourceCrop.valid()) {
		return errPhoto
	}
	sourceImage, err := decodeBoundedPhoto(source)
	if err != nil {
		return errPhoto
	}
	targetImage, err := decodeBoundedPhoto(target)
	if err != nil {
		return errPhoto
	}
	if sourceImage.Bounds().Dx() != targetImage.Bounds().Dx() || sourceImage.Bounds().Dy() != targetImage.Bounds().Dy() ||
		hasAlpha(sourceImage) != hasAlpha(targetImage) {
		return errPhoto
	}
	if source.ContentType == "image/png" {
		if !equalPixels(sourceImage, targetImage) {
			return errPhoto
		}
		return nil
	}
	if jpegPSNR(sourceImage, targetImage) < minJPEGPSNR {
		return errPhoto
	}
	return nil
}

func hasAlpha(value image.Image) bool {
	bounds := value.Bounds()
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			if _, _, _, alpha := value.At(x, y).RGBA(); alpha != 0xffff {
				return true
			}
		}
	}
	return false
}

func nrgbaAt(value image.Image, x, y int) color.NRGBA {
	converted, ok := color.NRGBAModel.Convert(value.At(x, y)).(color.NRGBA)
	if !ok {
		return color.NRGBA{}
	}
	return converted
}

func equalPixels(left, right image.Image) bool {
	leftBounds, rightBounds := left.Bounds(), right.Bounds()
	if leftBounds.Dx() != rightBounds.Dx() || leftBounds.Dy() != rightBounds.Dy() {
		return false
	}
	for y := 0; y < leftBounds.Dy(); y++ {
		for x := 0; x < leftBounds.Dx(); x++ {
			if nrgbaAt(left, leftBounds.Min.X+x, leftBounds.Min.Y+y) != nrgbaAt(right, rightBounds.Min.X+x, rightBounds.Min.Y+y) {
				return false
			}
		}
	}
	return true
}

func jpegPSNR(left, right image.Image) float64 {
	leftBounds, rightBounds := left.Bounds(), right.Bounds()
	if leftBounds.Dx() != rightBounds.Dx() || leftBounds.Dy() != rightBounds.Dy() || leftBounds.Empty() {
		return 0
	}
	var squared float64
	for y := 0; y < leftBounds.Dy(); y++ {
		for x := 0; x < leftBounds.Dx(); x++ {
			a := nrgbaAt(left, leftBounds.Min.X+x, leftBounds.Min.Y+y)
			b := nrgbaAt(right, rightBounds.Min.X+x, rightBounds.Min.Y+y)
			for _, difference := range [3]float64{float64(a.R) - float64(b.R), float64(a.G) - float64(b.G), float64(a.B) - float64(b.B)} {
				squared += difference * difference
			}
		}
	}
	mse := squared / float64(leftBounds.Dx()*leftBounds.Dy()*3)
	if mse == 0 {
		return math.Inf(1)
	}
	return 10 * math.Log10((255*255)/mse)
}
