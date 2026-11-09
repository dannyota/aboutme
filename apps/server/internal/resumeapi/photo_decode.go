package resumeapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"strings"
	"unicode/utf8"

	schema "github.com/dannyota/aboutme/packages/schema/gen/go"

	"github.com/dannyota/aboutme/apps/server/internal/media"
)

func requirePhotoMultipartContentType(header http.Header) *clientError {
	values := header.Values("Content-Type")
	if len(values) != 1 || strings.Contains(values[0], ",") {
		return &clientError{Status: http.StatusBadRequest, Code: "request_invalid", Message: "Content-Type must be one multipart/form-data value with a boundary"}
	}
	mediaType, params, err := mime.ParseMediaType(values[0])
	if err != nil || mediaType != "multipart/form-data" || len(params) != 1 || params["boundary"] == "" {
		return &clientError{Status: http.StatusBadRequest, Code: "request_invalid", Message: "Content-Type must be one multipart/form-data value with a boundary"}
	}
	return nil
}

func decodePhotoMultipart(r *http.Request) ([]byte, error) {
	_, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || params["boundary"] == "" {
		return nil, requestInvalidPhoto()
	}
	closing := newMultipartClosingObserver(r.Body, params["boundary"])
	reader := multipart.NewReader(closing, params["boundary"])
	part, err := reader.NextRawPart()
	if err != nil {
		return nil, mapPhotoMultipartError(err)
	}
	dispositions := part.Header.Values("Content-Disposition")
	if len(dispositions) != 1 {
		return nil, requestInvalidPhoto()
	}
	disposition, dispositionParams, err := mime.ParseMediaType(dispositions[0])
	filename := dispositionParams["filename"]
	if err != nil || disposition != "form-data" || len(dispositionParams) != 2 ||
		dispositionParams["name"] != "file" || filename == "" || !utf8.ValidString(filename) || len(filename) > photoFilenameBytes {
		return nil, requestInvalidPhoto()
	}
	if len(part.Header.Values("Content-Transfer-Encoding")) != 0 {
		return nil, requestInvalidPhoto()
	}
	file, err := io.ReadAll(io.LimitReader(part, photoFileBytes+1))
	if err != nil {
		return nil, mapPhotoMultipartError(err)
	}
	if len(file) > photoFileBytes {
		return nil, photoTooLargeError()
	}
	if closeErr := part.Close(); closeErr != nil {
		return nil, mapPhotoMultipartError(closeErr)
	}
	if _, nextErr := reader.NextRawPart(); !errors.Is(nextErr, io.EOF) {
		if nextErr == nil {
			return nil, requestInvalidPhoto()
		}
		return nil, mapPhotoMultipartError(nextErr)
	}
	epilogue, err := io.ReadAll(closing)
	if err != nil {
		return nil, mapPhotoMultipartError(err)
	}
	if len(epilogue) != 0 || !closing.cleanClose() {
		return nil, requestInvalidPhoto()
	}
	return file, nil
}

type multipartClosingObserver struct {
	reader io.Reader
	marker []byte
	window []byte
	found  bool
	after  []byte
	count  int
}

func newMultipartClosingObserver(reader io.Reader, boundary string) *multipartClosingObserver {
	return &multipartClosingObserver{reader: reader, marker: []byte("\r\n--" + boundary + "--")}
}

// Read reads from the wrapped body and records whether the closing boundary
// and the bytes after it have been seen.
func (o *multipartClosingObserver) Read(p []byte) (int, error) {
	n, err := o.reader.Read(p)
	for _, b := range p[:n] {
		if o.found {
			o.count++
			if len(o.after) < 3 {
				o.after = append(o.after, b)
			}
			continue
		}
		o.window = append(o.window, b)
		if len(o.window) > len(o.marker) {
			o.window = o.window[1:]
		}
		if len(o.window) == len(o.marker) && bytes.Equal(o.window, o.marker) {
			o.found = true
		}
	}
	return n, err
}

func (o *multipartClosingObserver) cleanClose() bool {
	return o.found && (o.count == 0 || o.count == 2 && bytes.Equal(o.after, []byte("\r\n")))
}

func mapPhotoMultipartError(err error) *clientError {
	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) {
		return photoTooLargeError()
	}
	return requestInvalidPhoto()
}

func mapPhotoNormalizeError(err error) *clientError {
	if errors.Is(err, media.ErrUnsupportedMediaType) {
		return &clientError{Status: http.StatusUnsupportedMediaType, Code: "media_type_unsupported", Message: "image type is not supported"}
	}
	var invalid *media.PhotoInvalidError
	if errors.As(err, &invalid) {
		return mediaInvalidError(string(invalid.Reason))
	}
	return internalClientError()
}

func requestInvalidPhoto() *clientError {
	return &clientError{Status: http.StatusBadRequest, Code: "request_invalid", Message: "multipart body must contain exactly one raw file part"}
}

func photoTooLargeError() *clientError {
	return &clientError{Status: http.StatusRequestEntityTooLarge, Code: "media_too_large", Message: "photo exceeds the upload limit"}
}

func decodePhotoCrop(raw []byte) (*schema.PhotoCrop, error) {
	var root map[string]json.RawMessage
	if err := json.Unmarshal(raw, &root); err != nil || len(root) != 1 {
		return nil, requestInvalid("request body must contain only crop")
	}
	cropRaw, ok := root["crop"]
	if !ok {
		return nil, requestInvalid("request body must contain crop")
	}
	if bytes.Equal(bytes.TrimSpace(cropRaw), []byte("null")) {
		return nil, nil
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(cropRaw, &fields); err != nil || len(fields) != 4 {
		return nil, documentInvalid("crop", "crop must contain x, y, width, and height")
	}
	for name := range fields {
		switch name {
		case "x", "y", "width", "height":
		default:
			return nil, documentInvalid("crop."+name, "crop contains an unknown field")
		}
	}
	for _, name := range []string{"x", "y", "width", "height"} {
		value, present := fields[name]
		if !present || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return nil, documentInvalid("crop."+name, "crop coordinates must be numbers")
		}
	}
	var crop schema.PhotoCrop
	if err := json.Unmarshal(cropRaw, &crop); err != nil {
		return nil, documentInvalid("crop", "crop coordinates must be numbers")
	}
	if crop.X < 0 || crop.X > 1 || crop.Y < 0 || crop.Y > 1 || crop.Width <= 0 || crop.Width > 1 || crop.Height <= 0 || crop.Height > 1 {
		return nil, documentInvalid("crop", "crop coordinates are outside their bounds")
	}
	return &crop, nil
}
