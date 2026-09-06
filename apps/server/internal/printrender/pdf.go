package printrender

import (
	"bytes"
	"strconv"
)

const canonicalPDFDate = "D:19700101000000+00'00'"

type pdfObjectRef struct {
	number     int
	generation int
}

type pdfToken struct {
	start int
	end   int
}

func canonicalizePDFMetadata(data []byte) error {
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
	copy(data[creation.start:creation.end], canonicalPDFDate)
	copy(data[modified.start:modified.end], canonicalPDFDate)
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

func onePDFReference(tokens []pdfToken, data []byte, name string) (pdfObjectRef, bool) {
	var result pdfObjectRef
	found := false
	for index, token := range tokens {
		if !tokenEquals(data, token, name) {
			continue
		}
		if found || index+3 >= len(tokens) || !tokenEquals(data, tokens[index+3], "R") {
			return pdfObjectRef{}, false
		}
		number, numberOK := parsePDFDecimal(data[tokens[index+1].start:tokens[index+1].end])
		generation, generationOK := parsePDFDecimal(data[tokens[index+2].start:tokens[index+2].end])
		if !numberOK || !generationOK {
			return pdfObjectRef{}, false
		}
		result = pdfObjectRef{number: number, generation: generation}
		found = true
	}
	return result, found
}

func onePDFDate(tokens []pdfToken, data []byte, name string) (pdfToken, bool) {
	var result pdfToken
	found := false
	for index, token := range tokens {
		if !tokenEquals(data, token, name) {
			continue
		}
		if found || index+1 >= len(tokens) {
			return pdfToken{}, false
		}
		value := tokens[index+1]
		if value.end-value.start != len(canonicalPDFDate)+2 || data[value.start] != '(' || data[value.end-1] != ')' {
			return pdfToken{}, false
		}
		content := data[value.start+1 : value.end-1]
		if !validUTCChromePDFDate(content) {
			return pdfToken{}, false
		}
		result = pdfToken{start: value.start + 1, end: value.end - 1}
		found = true
	}
	return result, found
}

func validUTCChromePDFDate(value []byte) bool {
	if len(value) != len(canonicalPDFDate) || !bytes.Equal(value[:2], []byte("D:")) || !bytes.Equal(value[16:], []byte("+00'00'")) {
		return false
	}
	for _, index := range []int{2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15} {
		if value[index] < '0' || value[index] > '9' {
			return false
		}
	}
	return true
}

func hasPDFName(tokens []pdfToken, data []byte, name string) bool {
	for _, token := range tokens {
		if tokenEquals(data, token, name) {
			return true
		}
	}
	return false
}

func tokenDecimalEquals(data []byte, token pdfToken, want int) bool {
	value, ok := parsePDFDecimal(data[token.start:token.end])
	return ok && value == want
}

func tokenEquals(data []byte, token pdfToken, want string) bool {
	return bytes.Equal(data[token.start:token.end], []byte(want))
}

func nextPDFToken(data []byte, start, limit int) (pdfToken, int, error) {
	position := start
	for position < limit {
		if isPDFWhitespace(data[position]) {
			position++
			continue
		}
		if data[position] == '%' {
			for position < limit && data[position] != '\n' && data[position] != '\r' {
				position++
			}
			continue
		}
		break
	}
	if position >= limit {
		return pdfToken{}, position, ErrRenderFailed
	}
	tokenStart := position
	switch data[position] {
	case '<':
		position++
		if position < limit && data[position] == '<' {
			return pdfToken{start: tokenStart, end: position + 1}, position + 1, nil
		}
		for position < limit && data[position] != '>' {
			position++
		}
		if position >= limit {
			return pdfToken{}, position, ErrRenderFailed
		}
		return pdfToken{start: tokenStart, end: position + 1}, position + 1, nil
	case '>':
		if position+1 >= limit || data[position+1] != '>' {
			return pdfToken{}, position, ErrRenderFailed
		}
		return pdfToken{start: tokenStart, end: position + 2}, position + 2, nil
	case '[', ']':
		return pdfToken{start: tokenStart, end: position + 1}, position + 1, nil
	case '(':
		depth := 1
		position++
		for position < limit && depth > 0 {
			switch data[position] {
			case '\\':
				position++
				if position < limit {
					position++
				}
			case '(':
				depth++
				position++
			case ')':
				depth--
				position++
			default:
				position++
			}
		}
		if depth != 0 {
			return pdfToken{}, position, ErrRenderFailed
		}
		return pdfToken{start: tokenStart, end: position}, position, nil
	case '/':
		position++
		for position < limit && !isPDFWhitespace(data[position]) && !isPDFDelimiter(data[position]) {
			position++
		}
		if position == tokenStart+1 {
			return pdfToken{}, position, ErrRenderFailed
		}
		return pdfToken{start: tokenStart, end: position}, position, nil
	default:
		for position < limit && !isPDFWhitespace(data[position]) && !isPDFDelimiter(data[position]) {
			position++
		}
		if position == tokenStart {
			return pdfToken{}, position, ErrRenderFailed
		}
		return pdfToken{start: tokenStart, end: position}, position, nil
	}
}

func readPDFLine(data []byte, start, limit int) ([]byte, int, bool) {
	if start >= limit {
		return nil, start, false
	}
	end := bytes.IndexByte(data[start:limit], '\n')
	if end < 0 {
		return nil, start, false
	}
	end += start
	return data[start:end], end + 1, true
}

func twoPDFDecimals(line []byte) (int, int, bool) {
	space := bytes.IndexByte(line, ' ')
	if space < 1 || bytes.IndexByte(line[space+1:], ' ') >= 0 {
		return 0, 0, false
	}
	first, firstOK := parsePDFDecimal(line[:space])
	second, secondOK := parsePDFDecimal(line[space+1:])
	return first, second, firstOK && secondOK
}

func parsePDFDecimal(value []byte) (int, bool) {
	if len(value) == 0 {
		return 0, false
	}
	for _, character := range value {
		if character < '0' || character > '9' {
			return 0, false
		}
	}
	parsed, err := strconv.Atoi(string(value))
	return parsed, err == nil
}

func isPDFWhitespace(value byte) bool {
	return value == 0 || value == '\t' || value == '\n' || value == '\f' || value == '\r' || value == ' '
}

func isPDFDelimiter(value byte) bool {
	return value == '(' || value == ')' || value == '<' || value == '>' || value == '[' || value == ']' || value == '{' || value == '}' || value == '/' || value == '%'
}
