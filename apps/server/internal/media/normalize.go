package media

import (
	"bytes"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"image"
	"image/color"
	stddraw "image/draw"
	"image/jpeg"
	"image/png"
	"io"
	"math"
	"runtime"
	"runtime/debug"
	"sync"

	xdraw "golang.org/x/image/draw"
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

func scanJPEG(source []byte) (containerInfo, error) {
	info := containerInfo{kind: containerJPEG, orientation: 1}
	position := 2
	inScan := false
	orientationSeen := false
	for position < len(source) {
		if inScan {
			markerPosition, ok := nextJPEGScanMarker(source, position)
			if !ok {
				return containerInfo{}, invalidPhoto(ReasonMalformed)
			}
			position = markerPosition
			inScan = false
		}
		if source[position] != 0xff {
			return containerInfo{}, invalidPhoto(ReasonMalformed)
		}
		for position < len(source) && source[position] == 0xff {
			position++
		}
		if position >= len(source) || source[position] == 0x00 {
			return containerInfo{}, invalidPhoto(ReasonMalformed)
		}
		marker := source[position]
		position++
		switch marker {
		case 0xd9:
			if position != len(source) {
				return containerInfo{}, invalidPhoto(ReasonTrailingData)
			}
			if info.width == 0 || info.height == 0 {
				return containerInfo{}, invalidPhoto(ReasonMalformed)
			}
			return info, nil
		case 0xd8:
			return containerInfo{}, invalidPhoto(ReasonMalformed)
		case 0x01, 0xd0, 0xd1, 0xd2, 0xd3, 0xd4, 0xd5, 0xd6, 0xd7:
			continue
		}
		if position+2 > len(source) {
			return containerInfo{}, invalidPhoto(ReasonMalformed)
		}
		segmentLength := int(binary.BigEndian.Uint16(source[position : position+2]))
		if segmentLength < 2 || segmentLength > len(source)-position {
			return containerInfo{}, invalidPhoto(ReasonMalformed)
		}
		payload := source[position+2 : position+segmentLength]
		position += segmentLength
		if isJPEGStartOfFrame(marker) {
			if len(payload) < 6 {
				return containerInfo{}, invalidPhoto(ReasonMalformed)
			}
			height := int(binary.BigEndian.Uint16(payload[1:3]))
			width := int(binary.BigEndian.Uint16(payload[3:5]))
			if info.width != 0 && (info.width != width || info.height != height) {
				return containerInfo{}, invalidPhoto(ReasonDimensions)
			}
			info.width, info.height = width, height
		}
		if marker == 0xe1 && bytes.HasPrefix(payload, []byte("Exif\x00\x00")) {
			orientation, found, parseErr := parseTIFFOrientation(payload[6:])
			if parseErr != nil || (found && orientationSeen) {
				return containerInfo{}, invalidPhoto(ReasonOrientation)
			}
			if found {
				orientationSeen = true
				info.orientation = orientation
			}
		}
		if marker == 0xda {
			if len(payload) < 1 {
				return containerInfo{}, invalidPhoto(ReasonMalformed)
			}
			inScan = true
		}
	}
	return containerInfo{}, invalidPhoto(ReasonMalformed)
}

func nextJPEGScanMarker(source []byte, position int) (int, bool) {
	for position < len(source) {
		if source[position] != 0xff {
			position++
			continue
		}
		start := position
		for position < len(source) && source[position] == 0xff {
			position++
		}
		if position >= len(source) {
			return 0, false
		}
		marker := source[position]
		if marker == 0x00 {
			position++
			continue
		}
		if marker >= 0xd0 && marker <= 0xd7 {
			position++
			continue
		}
		return start, true
	}
	return 0, false
}

func isJPEGStartOfFrame(marker byte) bool {
	return (marker >= 0xc0 && marker <= 0xc3) ||
		(marker >= 0xc5 && marker <= 0xc7) ||
		(marker >= 0xc9 && marker <= 0xcb) ||
		(marker >= 0xcd && marker <= 0xcf)
}

func scanPNG(source []byte) (containerInfo, error) {
	info := containerInfo{kind: containerPNG, orientation: 1}
	position := 8
	seenHeader := false
	orientationSeen := false
	for position < len(source) {
		if position+12 > len(source) {
			return containerInfo{}, invalidPhoto(ReasonMalformed)
		}
		length := uint64(binary.BigEndian.Uint32(source[position : position+4]))
		remaining := len(source) - position - 12
		if length > uint64(remaining) { //nolint:gosec // remaining is non-negative after the header-size guard.
			return containerInfo{}, invalidPhoto(ReasonMalformed)
		}
		dataEnd := position + 8 + int(length) //nolint:gosec // length is bounded by the remaining in-memory byte slice above.
		chunkEnd := dataEnd + 4
		kind := string(source[position+4 : position+8])
		data := source[position+8 : dataEnd]
		if binary.BigEndian.Uint32(source[dataEnd:chunkEnd]) != crc32.ChecksumIEEE(source[position+4:dataEnd]) {
			return containerInfo{}, invalidPhoto(ReasonMalformed)
		}
		if !seenHeader && kind != "IHDR" {
			return containerInfo{}, invalidPhoto(ReasonMalformed)
		}
		switch kind {
		case "IHDR":
			if seenHeader || len(data) != 13 {
				return containerInfo{}, invalidPhoto(ReasonMalformed)
			}
			seenHeader = true
			info.width = int(binary.BigEndian.Uint32(data[:4]))
			info.height = int(binary.BigEndian.Uint32(data[4:8]))
		case "acTL", "fcTL", "fdAT":
			return containerInfo{}, invalidPhoto(ReasonAnimated)
		case "eXIf":
			orientation, found, parseErr := parseExifOrientation(data)
			if parseErr != nil || (found && orientationSeen) {
				return containerInfo{}, invalidPhoto(ReasonOrientation)
			}
			if found {
				orientationSeen = true
				info.orientation = orientation
			}
		case "IEND":
			if len(data) != 0 {
				return containerInfo{}, invalidPhoto(ReasonMalformed)
			}
			if chunkEnd != len(source) {
				return containerInfo{}, invalidPhoto(ReasonTrailingData)
			}
			if !seenHeader {
				return containerInfo{}, invalidPhoto(ReasonMalformed)
			}
			return info, nil
		}
		position = chunkEnd
	}
	return containerInfo{}, invalidPhoto(ReasonMalformed)
}

func scanWebP(source []byte) (containerInfo, error) {
	declaredEnd := uint64(binary.LittleEndian.Uint32(source[4:8])) + 8
	if declaredEnd < uint64(len(source)) {
		return containerInfo{}, invalidPhoto(ReasonTrailingData)
	}
	if declaredEnd > uint64(len(source)) {
		return containerInfo{}, invalidPhoto(ReasonMalformed)
	}
	info := containerInfo{kind: containerWebP, orientation: 1}
	position := 12
	var extendedWidth, extendedHeight int
	orientationSeen := false
	imageSeen := false
	for position < len(source) {
		if position+8 > len(source) {
			return containerInfo{}, invalidPhoto(ReasonMalformed)
		}
		kind := string(source[position : position+4])
		length := uint64(binary.LittleEndian.Uint32(source[position+4 : position+8]))
		if length > uint64(len(source)-position-8) { //nolint:gosec // the header-size guard makes the difference non-negative.
			return containerInfo{}, invalidPhoto(ReasonMalformed)
		}
		dataEnd := position + 8 + int(length) //nolint:gosec // length is bounded by the remaining in-memory byte slice above.
		chunkEnd := dataEnd
		if length%2 == 1 {
			chunkEnd++
			if chunkEnd > len(source) || source[dataEnd] != 0 {
				return containerInfo{}, invalidPhoto(ReasonMalformed)
			}
		}
		data := source[position+8 : dataEnd]
		switch kind {
		case "VP8X":
			if len(data) != 10 || extendedWidth != 0 || data[0]&0xc1 != 0 || data[1] != 0 || data[2] != 0 || data[3] != 0 {
				return containerInfo{}, invalidPhoto(ReasonMalformed)
			}
			if data[0]&0x02 != 0 {
				return containerInfo{}, invalidPhoto(ReasonAnimated)
			}
			extendedWidth = int(readUint24LE(data[4:7])) + 1
			extendedHeight = int(readUint24LE(data[7:10])) + 1
		case "ANIM", "ANMF":
			return containerInfo{}, invalidPhoto(ReasonAnimated)
		case "VP8 ":
			if imageSeen || len(data) < 10 || data[0]&1 != 0 || !bytes.Equal(data[3:6], []byte{0x9d, 0x01, 0x2a}) {
				return containerInfo{}, invalidPhoto(ReasonMalformed)
			}
			imageSeen = true
			info.width = int(binary.LittleEndian.Uint16(data[6:8]) & 0x3fff)
			info.height = int(binary.LittleEndian.Uint16(data[8:10]) & 0x3fff)
		case "VP8L":
			if imageSeen || len(data) < 5 || data[0] != 0x2f {
				return containerInfo{}, invalidPhoto(ReasonMalformed)
			}
			bits := binary.LittleEndian.Uint32(data[1:5])
			if bits>>29 != 0 {
				return containerInfo{}, invalidPhoto(ReasonMalformed)
			}
			imageSeen = true
			info.width = int(bits&0x3fff) + 1
			info.height = int((bits>>14)&0x3fff) + 1
		case "EXIF":
			orientation, found, parseErr := parseExifOrientation(data)
			if parseErr != nil || (found && orientationSeen) {
				return containerInfo{}, invalidPhoto(ReasonOrientation)
			}
			if found {
				orientationSeen = true
				info.orientation = orientation
			}
		}
		position = chunkEnd
	}
	if position != len(source) || !imageSeen {
		return containerInfo{}, invalidPhoto(ReasonMalformed)
	}
	if extendedWidth != 0 {
		if info.width != extendedWidth || info.height != extendedHeight {
			return containerInfo{}, invalidPhoto(ReasonDimensions)
		}
	}
	return info, nil
}

func readUint24LE(data []byte) uint32 {
	return uint32(data[0]) | uint32(data[1])<<8 | uint32(data[2])<<16
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
