package media

import (
	"bytes"
	"encoding/binary"
)

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
