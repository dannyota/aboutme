package resumeapi

// Bounded, strict JSON request-body decoding: size and depth limits, no
// unknown fields, no duplicate object keys, and no trailing data.

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strings"
)

const (
	maxJSONBodyBytes = 256 * 1024
	maxJSONDepth     = 100
)

func decodeJSONBody(r *http.Request, target any) (boundedInput, error) {
	if err := requireJSONContentType(r.Header); err != nil {
		return boundedInput{}, err
	}
	raw, err := readBoundedBody(r.Body, maxJSONBodyBytes)
	if err != nil {
		return boundedInput{}, err
	}
	if err := validateJSONTokens(raw, maxJSONDepth); err != nil {
		return boundedInput{}, &clientError{Status: http.StatusBadRequest, Code: "request_invalid", Message: "request body is not valid JSON"}
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(target); err != nil {
		return boundedInput{}, &clientError{Status: http.StatusBadRequest, Code: "request_invalid", Message: "request body does not match the operation"}
	}
	if err := requireDecoderEOF(dec); err != nil {
		return boundedInput{}, &clientError{Status: http.StatusBadRequest, Code: "request_invalid", Message: "request body has trailing data"}
	}
	return boundedInput{Payload: raw, Value: target}, nil
}

func decodeDeleteBody(r *http.Request) (boundedInput, error) {
	if values := r.Header.Values("Content-Type"); len(values) > 0 {
		if len(values) != 1 || !isJSONContentType(values[0]) {
			return boundedInput{}, &clientError{Status: http.StatusBadRequest, Code: "request_invalid", Message: "DELETE Content-Type must be one application/json value"}
		}
	}
	if r.ContentLength > 0 {
		return boundedInput{}, &clientError{Status: http.StatusBadRequest, Code: "request_invalid", Message: "DELETE requests have no body"}
	}
	var one [1]byte
	n, err := r.Body.Read(one[:])
	if n != 0 || !errors.Is(err, io.EOF) {
		return boundedInput{}, &clientError{Status: http.StatusBadRequest, Code: "request_invalid", Message: "DELETE requests have no body"}
	}
	return boundedInput{Payload: []byte{}}, nil
}

func requireJSONContentType(header http.Header) error {
	values := header.Values("Content-Type")
	if len(values) != 1 || !isJSONContentType(values[0]) {
		return &clientError{Status: http.StatusBadRequest, Code: "request_invalid", Message: "Content-Type must be one application/json value"}
	}
	return nil
}

func isJSONContentType(value string) bool {
	if strings.Contains(value, ",") {
		return false
	}
	mediaType, params, err := mime.ParseMediaType(value)
	if err != nil || mediaType != "application/json" {
		return false
	}
	delete(params, "charset")
	return len(params) == 0
}

func readBoundedBody(body io.Reader, limit int64) ([]byte, error) {
	raw, err := io.ReadAll(io.LimitReader(body, limit+1))
	if err != nil {
		return nil, &clientError{Status: http.StatusBadRequest, Code: "request_invalid", Message: "request body could not be read"}
	}
	if int64(len(raw)) > limit {
		return nil, &clientError{Status: http.StatusRequestEntityTooLarge, Code: "body_too_large", Message: "request body exceeds the 262144 byte limit"}
	}
	return raw, nil
}

func validateJSONTokens(raw []byte, maxDepth int) error {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	if err := consumeJSONValue(dec, 0, maxDepth); err != nil {
		return err
	}
	return requireDecoderEOF(dec)
}

func consumeJSONValue(dec *json.Decoder, depth, maxDepth int) error {
	if depth > maxDepth {
		return errors.New("JSON nesting is too deep")
	}
	token, err := dec.Token()
	if err != nil {
		return err
	}
	delim, ok := token.(json.Delim)
	if !ok {
		return nil
	}
	switch delim {
	case '{':
		seen := make(map[string]struct{})
		for dec.More() {
			keyToken, keyErr := dec.Token()
			if keyErr != nil {
				return keyErr
			}
			key, ok := keyToken.(string)
			if !ok {
				return errors.New("object key is not a string")
			}
			if _, duplicate := seen[key]; duplicate {
				return fmt.Errorf("duplicate object key %q", key)
			}
			seen[key] = struct{}{}
			if consumeErr := consumeJSONValue(dec, depth+1, maxDepth); consumeErr != nil {
				return consumeErr
			}
		}
		_, err = dec.Token()
		return err
	case '[':
		for dec.More() {
			if consumeErr := consumeJSONValue(dec, depth+1, maxDepth); consumeErr != nil {
				return consumeErr
			}
		}
		_, err = dec.Token()
		return err
	default:
		return errors.New("unexpected closing JSON delimiter")
	}
}

func requireDecoderEOF(dec *json.Decoder) error {
	var trailing any
	err := dec.Decode(&trailing)
	if errors.Is(err, io.EOF) {
		return nil
	}
	if err == nil {
		return errors.New("multiple JSON values")
	}
	return err
}
