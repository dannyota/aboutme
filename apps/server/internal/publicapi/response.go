package publicapi

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"strconv"
	"strings"
)

var ErrInvalidSelectedResponse = errors.New("invalid public response")

const maxSelectedBodyBytes = 2_097_152

type SelectedResponse struct {
	Status int
	Header http.Header
	Body   []byte
}

func NewSelectedResponse(status int, contentType, cacheControl string, body []byte, extra http.Header) (SelectedResponse, error) {
	if status != http.StatusOK || contentType == "" || cacheControl != "no-cache, must-revalidate" || len(body) == 0 || len(body) > maxSelectedBodyBytes || containsCookie(extra) {
		return SelectedResponse{}, ErrInvalidSelectedResponse
	}
	for i := range contentType {
		if contentType[i] < 0x20 || contentType[i] == 0x7f {
			return SelectedResponse{}, ErrInvalidSelectedResponse
		}
	}
	header := make(http.Header)
	if extra != nil {
		header = extra.Clone()
	}
	header.Set("Content-Type", contentType)
	header.Set("Cache-Control", cacheControl)
	header.Set("Content-Length", strconv.Itoa(len(body)))
	digest := sha256.Sum256(body)
	header.Set("ETag", `"`+hex.EncodeToString(digest[:])+`"`)
	return SelectedResponse{Status: status, Header: header, Body: append([]byte{}, body...)}, nil
}

func containsCookie(header http.Header) bool {
	for key := range header {
		if strings.EqualFold(key, "set-cookie") {
			return true
		}
	}
	return false
}

func (s SelectedResponse) ServeHTTP(w http.ResponseWriter, request *http.Request) {
	tag, present, valid := parseIfNoneMatch(request.Header)
	if !valid {
		serveConditionalError(w, request)
		return
	}
	header := s.Header.Clone()
	if present && tag == header.Get("ETag") {
		header.Del("Content-Length")
		writeHeader(w, header)
		w.WriteHeader(http.StatusNotModified)
		return
	}
	writeHeader(w, header)
	w.WriteHeader(s.Status)
	if request.Method != http.MethodHead {
		_, _ = w.Write(s.Body)
	}
}

func serveConditionalError(w http.ResponseWriter, request *http.Request) {
	body := []byte("{\"error\":{\"code\":\"request_invalid\",\"message\":\"request is invalid\"}}\n")
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache, must-revalidate")
	w.Header().Set("Content-Length", strconv.Itoa(len(body)))
	w.WriteHeader(http.StatusBadRequest)
	if request.Method != http.MethodHead {
		_, _ = w.Write(body)
	}
}

func writeHeader(w http.ResponseWriter, header http.Header) {
	for key, values := range header {
		for _, value := range values {
			w.Header().Add(key, value)
		}
	}
}
