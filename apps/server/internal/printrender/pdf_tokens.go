package printrender

import (
	"bytes"
	"strconv"
)

type pdfObjectRef struct {
	number     int
	generation int
}

type pdfToken struct {
	start int
	end   int
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
		if value.end-value.start != len(epochPDFDate)+2 || data[value.start] != '(' || data[value.end-1] != ')' {
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
	if len(value) != len(epochPDFDate) || !bytes.Equal(value[:2], []byte("D:")) || !bytes.Equal(value[16:], []byte("+00'00'")) {
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
