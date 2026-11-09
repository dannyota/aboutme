package printrender

import (
	"bytes"
	"time"
)

// epochPDFDate is written when a render has no revision time.
const epochPDFDate = "D:19700101000000+00'00'"

const pdfDateLayout = "D:20060102150405+00'00'"

// pdfDate formats a revision time as a UTC PDF date the same length as
// Chromium's own, so replacing it moves no byte offset.
func pdfDate(revisionTime time.Time) (string, bool) {
	if revisionTime.IsZero() {
		return epochPDFDate, true
	}
	utc := revisionTime.UTC()
	if utc.Year() < 1970 || utc.Year() > 9999 {
		return "", false
	}
	return utc.Format(pdfDateLayout), true
}

// setPDFMetadataDates replaces the Info dictionary's CreationDate and ModDate
// with the revision time. The same revision therefore renders the same bytes.
func setPDFMetadataDates(data []byte, revisionTime time.Time) error {
	date, ok := pdfDate(revisionTime)
	if !ok {
		return ErrRenderFailed
	}
	if !bytes.HasPrefix(data, []byte("%PDF-1.4\n")) || bytes.Count(data, []byte("\n%%EOF")) != 1 {
		return ErrRenderFailed
	}
	marker := []byte("\nstartxref\n")
	if bytes.Count(data, marker) != 1 {
		return ErrRenderFailed
	}
	markerOffset := bytes.Index(data, marker)
	xrefStart := markerOffset + len(marker)
	xrefEnd := bytes.IndexByte(data[xrefStart:], '\n')
	if xrefEnd < 1 {
		return ErrRenderFailed
	}
	xrefEnd += xrefStart
	xrefOffset, ok := parsePDFDecimal(data[xrefStart:xrefEnd])
	if !ok || xrefOffset <= 0 || xrefOffset >= markerOffset || !bytes.Equal(data[xrefEnd:], []byte("\n%%EOF\n")) {
		return ErrRenderFailed
	}

	entries, trailerStart, err := parseClassicXRef(data, xrefOffset, markerOffset)
	if err != nil {
		return ErrRenderFailed
	}
	trailerTokens, trailerEnd, err := topLevelDictionaryTokens(data, trailerStart, markerOffset)
	if err != nil || trailerEnd != markerOffset {
		return ErrRenderFailed
	}
	if hasPDFName(trailerTokens, data, "/Encrypt") || hasPDFName(trailerTokens, data, "/Prev") || hasPDFName(trailerTokens, data, "/XRefStm") {
		return ErrRenderFailed
	}
	info, ok := onePDFReference(trailerTokens, data, "/Info")
	if !ok {
		return ErrRenderFailed
	}
	infoOffset, ok := entries[info]
	if !ok || infoOffset <= 0 || infoOffset >= xrefOffset {
		return ErrRenderFailed
	}
	infoLimit := xrefOffset
	for _, objectOffset := range entries {
		if objectOffset > infoOffset && objectOffset < infoLimit {
			infoLimit = objectOffset
		}
	}
	infoTokens, err := parseInfoObject(data, infoOffset, infoLimit, info)
	if err != nil {
		return ErrRenderFailed
	}
	creation, ok := onePDFDate(infoTokens, data, "/CreationDate")
	if !ok {
		return ErrRenderFailed
	}
	modified, ok := onePDFDate(infoTokens, data, "/ModDate")
	if !ok {
		return ErrRenderFailed
	}
	copy(data[creation.start:creation.end], date)
	copy(data[modified.start:modified.end], date)
	return nil
}

func parseClassicXRef(data []byte, offset, limit int) (map[pdfObjectRef]int, int, error) {
	if offset+5 > limit || !bytes.Equal(data[offset:offset+5], []byte("xref\n")) {
		return nil, 0, ErrRenderFailed
	}
	entries := make(map[pdfObjectRef]int)
	seen := make(map[int]bool)
	usedOffsets := make(map[int]bool)
	position := offset + 5
	for {
		line, next, ok := readPDFLine(data, position, limit)
		if !ok {
			return nil, 0, ErrRenderFailed
		}
		position = next
		if bytes.Equal(line, []byte("trailer")) {
			return entries, position, nil
		}
		first, count, ok := twoPDFDecimals(line)
		if !ok || count < 1 || first > len(data) || count > len(data)/20 || first > len(data)-count {
			return nil, 0, ErrRenderFailed
		}
		for index := 0; index < count; index++ {
			line, next, ok = readPDFLine(data, position, limit)
			if !ok || len(line) != 19 || line[10] != ' ' || line[16] != ' ' || line[18] != ' ' || (line[17] != 'n' && line[17] != 'f') {
				return nil, 0, ErrRenderFailed
			}
			position = next
			objectNumber := first + index
			if seen[objectNumber] {
				return nil, 0, ErrRenderFailed
			}
			seen[objectNumber] = true
			objectOffset, offsetOK := parsePDFDecimal(line[:10])
			generation, generationOK := parsePDFDecimal(line[11:16])
			if !offsetOK || !generationOK {
				return nil, 0, ErrRenderFailed
			}
			if line[17] == 'n' {
				ref := pdfObjectRef{number: objectNumber, generation: generation}
				if objectOffset <= 0 || objectOffset >= offset || usedOffsets[objectOffset] {
					return nil, 0, ErrRenderFailed
				}
				usedOffsets[objectOffset] = true
				entries[ref] = objectOffset
			}
		}
	}
}

func parseInfoObject(data []byte, offset, limit int, want pdfObjectRef) ([]pdfToken, error) {
	number, position, err := nextPDFToken(data, offset, limit)
	if err != nil || number.start != offset || !tokenDecimalEquals(data, number, want.number) {
		return nil, ErrRenderFailed
	}
	generation, position, err := nextPDFToken(data, position, limit)
	if err != nil || !tokenDecimalEquals(data, generation, want.generation) {
		return nil, ErrRenderFailed
	}
	object, position, err := nextPDFToken(data, position, limit)
	if err != nil || !tokenEquals(data, object, "obj") {
		return nil, ErrRenderFailed
	}
	tokens, dictionaryEnd, err := topLevelDictionaryTokens(data, position, limit)
	if err != nil {
		return nil, ErrRenderFailed
	}
	endObject, position, err := nextPDFToken(data, dictionaryEnd, limit)
	if err != nil || !tokenEquals(data, endObject, "endobj") {
		return nil, ErrRenderFailed
	}
	for position < limit && isPDFWhitespace(data[position]) {
		position++
	}
	if position != limit {
		return nil, ErrRenderFailed
	}
	return tokens, nil
}

func topLevelDictionaryTokens(data []byte, start, limit int) ([]pdfToken, int, error) {
	first, position, err := nextPDFToken(data, start, limit)
	if err != nil || !tokenEquals(data, first, "<<") {
		return nil, 0, ErrRenderFailed
	}
	dictionaryDepth := 1
	arrayDepth := 0
	var tokens []pdfToken
	for dictionaryDepth > 0 {
		token, next, err := nextPDFToken(data, position, limit)
		if err != nil {
			return nil, 0, ErrRenderFailed
		}
		position = next
		switch string(data[token.start:token.end]) {
		case "<<":
			dictionaryDepth++
		case ">>":
			dictionaryDepth--
			if dictionaryDepth < 0 {
				return nil, 0, ErrRenderFailed
			}
		case "[":
			arrayDepth++
		case "]":
			arrayDepth--
			if arrayDepth < 0 {
				return nil, 0, ErrRenderFailed
			}
		default:
			if dictionaryDepth == 1 && arrayDepth == 0 {
				tokens = append(tokens, token)
			}
		}
	}
	if arrayDepth != 0 {
		return nil, 0, ErrRenderFailed
	}
	for position < limit && isPDFWhitespace(data[position]) {
		position++
	}
	return tokens, position, nil
}
