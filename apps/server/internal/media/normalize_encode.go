package media

import (
	"bytes"
	"image"
	stddraw "image/draw"
	"image/jpeg"
	"image/png"
	"io"

	xdraw "golang.org/x/image/draw"
)

func hasTransparency(source image.Image) bool {
	switch source := source.(type) {
	case *image.YCbCr, *image.Gray, *image.Gray16:
		return false
	case *image.NRGBA:
		for i := 3; i < len(source.Pix); i += 4 {
			if source.Pix[i] != 0xff {
				return true
			}
		}
		return false
	case *image.RGBA:
		for i := 3; i < len(source.Pix); i += 4 {
			if source.Pix[i] != 0xff {
				return true
			}
		}
		return false
	}
	bounds := source.Bounds()
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			_, _, _, alpha := source.At(x, y).RGBA()
			if alpha != 0xffff {
				return true
			}
		}
	}
	return false
}

func normalizeJPEG(source image.Image, orientation int) (NormalizedPhoto, error) {
	for _, edge := range []int{2048, 1792, 1536, 1280, 1024, 768, 512} {
		resized := resizePhoto(source, edge)
		if resized == nil {
			continue
		}
		oriented := orientPhoto(resized, orientation)
		var output bytes.Buffer
		if err := jpeg.Encode(&output, oriented, &jpeg.Options{Quality: jpegQuality}); err != nil {
			return NormalizedPhoto{}, invalidPhoto(ReasonNormalizationFailed)
		}
		if output.Len() <= MaxObjectBytes {
			return NormalizedPhoto{
				Bytes:       output.Bytes(),
				ContentType: "image/jpeg",
				Extension:   "jpg",
				Width:       oriented.Bounds().Dx(),
				Height:      oriented.Bounds().Dy(),
			}, nil
		}
	}
	return NormalizedPhoto{}, invalidPhoto(ReasonNormalizationFailed)
}

func normalizePNG(source image.Image, orientation int) (NormalizedPhoto, error) {
	return normalizePNGWithEncoder(source, orientation, encodeCanonicalPNG)
}

type pngEncoder func(io.Writer, image.Image) error

func encodeCanonicalPNG(output io.Writer, source image.Image) error {
	encoder := png.Encoder{CompressionLevel: png.BestSpeed}
	return encoder.Encode(output, source)
}

func normalizePNGWithEncoder(source image.Image, orientation int, encoder pngEncoder) (NormalizedPhoto, error) {
	for _, edge := range []int{1024, 896, 768, 640, 512} {
		resized := resizePhoto(source, edge)
		if resized == nil {
			continue
		}
		oriented := orientPhoto(resized, orientation)
		var output bytes.Buffer
		if err := encoder(&output, oriented); err != nil {
			return NormalizedPhoto{}, invalidPhoto(ReasonNormalizationFailed)
		}
		if output.Len() <= MaxObjectBytes {
			return NormalizedPhoto{
				Bytes:       output.Bytes(),
				ContentType: "image/png",
				Extension:   "png",
				Width:       oriented.Bounds().Dx(),
				Height:      oriented.Bounds().Dy(),
			}, nil
		}
	}
	return NormalizedPhoto{}, invalidPhoto(ReasonNormalizationFailed)
}

func resizePhoto(source image.Image, maxEdge int) *image.NRGBA {
	width, height := source.Bounds().Dx(), source.Bounds().Dy()
	longEdge := max(width, height)
	if longEdge <= maxEdge {
		if maxEdge == 2048 || maxEdge == 1024 {
			destination := image.NewNRGBA(image.Rect(0, 0, width, height))
			stddraw.Draw(destination, destination.Bounds(), source, source.Bounds().Min, stddraw.Src)
			return destination
		}
		return nil
	}
	var newWidth, newHeight int
	if width >= height {
		newWidth = maxEdge
		newHeight = max(1, (height*maxEdge+width/2)/width)
	} else {
		newHeight = maxEdge
		newWidth = max(1, (width*maxEdge+height/2)/height)
	}
	return tiledCatmullRomScale(source, newWidth, newHeight)
}

// tiledCatmullRomScale separates the two axis passes into narrow tiles. The
// x/image Scale implementation otherwise allocates a float64 temporary of
// destination-width times source-height, which exceeds the intake RSS budget
// at the accepted 4096-square boundary.
func tiledCatmullRomScale(source image.Image, newWidth, newHeight int) *image.NRGBA {
	const tile = 32
	sourceBounds := source.Bounds()
	height := sourceBounds.Dy()

	horizontal := image.NewRGBA(image.Rect(0, 0, newWidth, height))
	horizontalScaler := xdraw.CatmullRom.NewScaler(newWidth, tile, sourceBounds.Dx(), tile)
	for y := 0; y < height; y += tile {
		end := min(y+tile, height)
		sourceRect := image.Rect(sourceBounds.Min.X, sourceBounds.Min.Y+y, sourceBounds.Max.X, sourceBounds.Min.Y+end)
		destinationRect := image.Rect(0, y, newWidth, end)
		if end-y == tile {
			horizontalScaler.Scale(horizontal, destinationRect, source, sourceRect, stddraw.Src, nil)
		} else {
			xdraw.CatmullRom.Scale(horizontal, destinationRect, source, sourceRect, stddraw.Src, nil)
		}
	}

	vertical := image.NewNRGBA(image.Rect(0, 0, newWidth, newHeight))
	verticalScaler := xdraw.CatmullRom.NewScaler(tile, newHeight, tile, height)
	for x := 0; x < newWidth; x += tile {
		end := min(x+tile, newWidth)
		sourceRect := image.Rect(x, 0, end, height)
		destinationRect := image.Rect(x, 0, end, newHeight)
		if end-x == tile {
			verticalScaler.Scale(vertical, destinationRect, horizontal, sourceRect, stddraw.Src, nil)
		} else {
			xdraw.CatmullRom.Scale(vertical, destinationRect, horizontal, sourceRect, stddraw.Src, nil)
		}
	}

	return vertical
}
