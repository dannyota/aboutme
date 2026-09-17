package media

import (
	"bytes"
	"encoding/binary"
	"errors"
	"image"
	"image/color"
	"io"
)

func orientPhoto(source image.Image, orientation int) image.Image {
	if orientation == 1 {
		return source
	}
	return orientedImage{source: source, orientation: orientation}
}

type orientedImage struct {
	source      image.Image
	orientation int
}

// ColorModel implements image.Image.
func (oriented orientedImage) ColorModel() color.Model { return oriented.source.ColorModel() }

// Bounds implements image.Image with orientation-aware dimensions.
func (oriented orientedImage) Bounds() image.Rectangle {
	bounds := oriented.source.Bounds()
	if oriented.orientation >= 5 {
		return image.Rect(0, 0, bounds.Dy(), bounds.Dx())
	}
	return image.Rect(0, 0, bounds.Dx(), bounds.Dy())
}

// At implements image.Image by mapping into the unrotated source.
func (oriented orientedImage) At(x, y int) color.Color {
	bounds := oriented.source.Bounds()
	width, height := bounds.Dx(), bounds.Dy()
	var sourceX, sourceY int
	switch oriented.orientation {
	case 2:
		sourceX, sourceY = width-1-x, y
	case 3:
		sourceX, sourceY = width-1-x, height-1-y
	case 4:
		sourceX, sourceY = x, height-1-y
	case 5:
		sourceX, sourceY = y, x
	case 6:
		sourceX, sourceY = y, height-1-x
	case 7:
		sourceX, sourceY = width-1-y, height-1-x
	case 8:
		sourceX, sourceY = width-1-y, x
	default:
		sourceX, sourceY = x, y
	}
	return oriented.source.At(bounds.Min.X+sourceX, bounds.Min.Y+sourceY)
}

func parseExifOrientation(data []byte) (int, bool, error) {
	if bytes.HasPrefix(data, []byte("Exif\x00\x00")) {
		data = data[6:]
	}
	return parseTIFFOrientation(data)
}

func parseTIFFOrientation(data []byte) (int, bool, error) {
	if len(data) < 8 {
		return 0, false, errors.New("short TIFF")
	}
	var order binary.ByteOrder
	switch string(data[:2]) {
	case "II":
		order = binary.LittleEndian
	case "MM":
		order = binary.BigEndian
	default:
		return 0, false, errors.New("invalid TIFF byte order")
	}
	if order.Uint16(data[2:4]) != 42 {
		return 0, false, errors.New("invalid TIFF magic")
	}
	offset := uint64(order.Uint32(data[4:8]))
	if offset > uint64(len(data)-2) { //nolint:gosec // len(data) is non-negative and at least eight here.
		return 0, false, io.ErrUnexpectedEOF
	}
	count := uint64(order.Uint16(data[offset : offset+2]))
	entriesStart := offset + 2
	if count > (uint64(len(data))-entriesStart)/12 {
		return 0, false, io.ErrUnexpectedEOF
	}
	entriesEnd := entriesStart + count*12
	if entriesEnd+4 > uint64(len(data)) {
		return 0, false, io.ErrUnexpectedEOF
	}
	found := false
	orientation := 1
	for index := uint64(0); index < count; index++ {
		entry := data[entriesStart+index*12 : entriesStart+(index+1)*12]
		if order.Uint16(entry[:2]) != 0x0112 {
			continue
		}
		if found || order.Uint16(entry[2:4]) != 3 || order.Uint32(entry[4:8]) != 1 {
			return 0, false, errors.New("invalid orientation tag")
		}
		orientation = int(order.Uint16(entry[8:10]))
		if orientation < 1 || orientation > 8 {
			return 0, false, errors.New("orientation out of range")
		}
		found = true
	}
	return orientation, found, nil
}
