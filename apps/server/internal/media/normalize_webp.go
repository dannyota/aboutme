package media

import (
	"bytes"
	"encoding/binary"
)

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
