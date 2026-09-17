package media

import (
	"encoding/binary"
	"hash/crc32"
)

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
