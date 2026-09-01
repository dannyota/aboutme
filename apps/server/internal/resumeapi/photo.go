package resumeapi

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	schema "github.com/dannyota/aboutme/packages/schema/gen/go"

	"github.com/dannyota/aboutme/apps/server/internal/api"
	"github.com/dannyota/aboutme/apps/server/internal/media"
	"github.com/dannyota/aboutme/apps/server/internal/resume"
	"github.com/dannyota/aboutme/apps/server/internal/store"
)

const (
	photoRequestBytes      = int64(2_162_688)
	photoFileBytes         = 2_097_152
	photoFilenameBytes     = 255
	photoReadTimeout       = 60 * time.Second
	photoPutTimeout        = 5 * time.Second
	photoCandidateLifetime = 5 * time.Minute
	photoPutAttempts       = 3
)

var taskPhotoAdmission = media.NewPhotoAdmission()

func photoRoutes() []routeSpec {
	return []routeSpec{
		{Method: http.MethodPost, Pattern: apiResumePath + "/{id}/photo", Operation: "uploadResumePhoto", Mutation: true, Upload: true, OperationKind: operationPhotoCandidate, AcceptsWireVersion: true, EmitsWireVersion: true, Handler: (*Service).handleUploadResumePhoto},
		{Method: http.MethodGet, Pattern: apiResumePath + "/{id}/photo", Operation: "getResumePhoto", Handler: (*Service).handleGetResumePhoto},
		{Method: http.MethodPatch, Pattern: apiResumePath + "/{id}/photo", Operation: "updateResumePhotoCrop", Mutation: true, OperationKind: operationAggregate, AcceptsWireVersion: true, EmitsWireVersion: true, Handler: (*Service).handleUpdateResumePhotoCrop},
		{Method: http.MethodDelete, Pattern: apiResumePath + "/{id}/photo", Operation: "deleteResumePhoto", Mutation: true, OperationKind: operationAggregate, AcceptsWireVersion: true, EmitsWireVersion: true, Handler: (*Service).handleDeleteResumePhoto},
	}
}

type photoUploadInput struct {
	ResumeID uuid.UUID
	File     []byte
}

type photoCropInput struct {
	ResumeID uuid.UUID
	Crop     *schema.PhotoCrop
}

type photoDeleteInput struct {
	ResumeID uuid.UUID
}

func (s *Service) handleUploadResumePhoto(w http.ResponseWriter, r *http.Request) {
	// These preflight checks intentionally precede admission and body reads.
	if _, err := parseMutationHeaders(r, true, s.acceptedVersions); err != nil {
		writeResumeError(w, err)
		return
	}
	id, pathErr := parseResumePathID(r)
	if pathErr != nil {
		writeResumeError(w, pathErr)
		return
	}
	if err := requirePhotoMultipartContentType(r.Header); err != nil {
		writeResumeError(w, err)
		return
	}
	if r.ContentLength > photoRequestBytes {
		writeResumeError(w, photoTooLargeError())
		return
	}
	release, err := taskPhotoAdmission.Acquire(r.Context())
	if err != nil {
		if errors.Is(err, media.ErrMediaBusy) {
			writeResumeError(w, &clientError{Status: http.StatusServiceUnavailable, Code: "media_busy", Message: "photo intake is busy", Headers: map[string]string{"Retry-After": "1"}})
			return
		}
		writeResumeError(w, internalClientError())
		return
	}
	defer release()

	if err := http.NewResponseController(w).SetReadDeadline(time.Now().Add(photoReadTimeout)); err != nil {
		writeResumeError(w, internalClientError())
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, photoRequestBytes)
	var candidate photoCandidate
	spec := mutationSpec{
		RegisteredOperation: "uploadResumePhoto", RequireMatch: true,
		Decode: func(r *http.Request) (boundedInput, error) {
			file, decodeErr := decodePhotoMultipart(r)
			if decodeErr != nil {
				return boundedInput{}, decodeErr
			}
			return boundedInput{Payload: file, Value: photoUploadInput{ResumeID: id, File: file}}, nil
		},
		CanonicalTargets: func(input boundedInput) ([]string, error) {
			decoded, ok := input.Value.(photoUploadInput)
			if !ok {
				return nil, internalClientError()
			}
			return []string{"resume_id", decoded.ResumeID.String()}, nil
		},
		Prepare: func(ctx context.Context, input boundedInput, _ idempotencyInspection) (preparedInput, error) {
			decoded, ok := input.Value.(photoUploadInput)
			if !ok || s.blobs == nil {
				return preparedInput{}, internalClientError()
			}
			if _, ok := s.resumes.(resumeMediaDeletionQueue); !ok {
				return preparedInput{}, internalClientError()
			}
			normalizeStarted := time.Now()
			normalized, normalizeErr := media.NormalizePhoto(decoded.File)
			s.recordPhotoNormalizationDuration(time.Since(normalizeStarted))
			if normalizeErr != nil {
				release()
				return preparedInput{}, mapPhotoNormalizeError(normalizeErr)
			}
			created, createErr := createPhotoCandidate(ctx, s.blobs, s.photoRandom, decoded.ResumeID, normalized, s.clock)
			release()
			if createErr != nil {
				return preparedInput{}, internalClientError()
			}
			candidate = created.photoCandidate
			return s.preparePhotoReplacement(ctx, input, decoded.ResumeID, created.Key, created.ExecuteBefore)
		},
		Run:        photoCandidateOperation{aggregateOperation{service: s}},
		Transition: s.nonDrainingTransition,
		Finalize: func(ctx context.Context, prepared preparedInput, result resume.ExecuteResult, executeErr error) {
			s.finalizePhotoCandidate(candidate)(ctx, prepared, result, executeErr)
		},
	}
	s.executeMutation(w, r, spec)
}

type preparedPhotoCandidate struct {
	photoCandidate
	ExecuteBefore time.Time
}

func createPhotoCandidate(ctx context.Context, backend media.Backend, random io.Reader, resumeID uuid.UUID,
	normalized media.NormalizedPhoto, clock func() time.Time,
) (preparedPhotoCandidate, error) {
	for range photoPutAttempts {
		key, err := media.NewPhotoKey(random, resumeID, normalized.Extension)
		if err != nil {
			return preparedPhotoCandidate{}, err
		}
		putCtx, cancel := context.WithTimeout(ctx, photoPutTimeout)
		outcome, putErr := backend.Put(putCtx, key, normalized.ContentType, bytes.NewReader(normalized.Bytes), int64(len(normalized.Bytes)))
		cancel()
		switch {
		case outcome == media.PutCreated && putErr == nil:
			now := time.Now()
			if clock != nil {
				now = clock()
			}
			return preparedPhotoCandidate{
				photoCandidate: photoCandidate{Key: key, Created: true},
				ExecuteBefore:  now.Add(photoCandidateLifetime),
			}, nil
		case outcome == media.PutNotCreated && errors.Is(putErr, media.ErrAlreadyExists):
			continue
		default:
			if putErr == nil {
				putErr = errors.New("media: invalid backend put outcome")
			}
			return preparedPhotoCandidate{}, putErr
		}
	}
	return preparedPhotoCandidate{}, media.ErrAlreadyExists
}

func (s *Service) preparePhotoReplacement(invariantCtx context.Context, input boundedInput, resumeID uuid.UUID, newKey string, executeBefore time.Time) (preparedInput, error) {
	queue, ok := s.resumes.(resumeMediaDeletionQueue)
	if !ok {
		return preparedInput{}, internalClientError()
	}
	var oldKey string
	apply := func(document json.RawMessage) (json.RawMessage, error) {
		changed, transactionOldKey, err := applyPhotoReplacement(document, resumeID, newKey)
		if err != nil {
			if errors.Is(err, media.ErrInvalidKey) {
				s.recordPhotoKeyInvariant(invariantCtx)
			}
			return nil, err
		}
		oldKey = transactionOldKey
		return changed, nil
	}
	return preparedInput{Input: input, ExecuteBefore: executeBefore, Value: aggregatePreparedInput{
		ResumeID: resumeID,
		Apply:    apply,
		BeforeSave: func(ctx context.Context, qtx *store.Queries, _ resume.Resume, _ schema.Resume) error {
			if oldKey == "" {
				return nil
			}
			return queue.EnqueueMediaDeletionTx(ctx, qtx, resumeID, oldKey)
		},
		Response: s.resumeResponseBuilder(http.StatusOK, false),
	}}, nil
}

func (s *Service) handleGetResumePhoto(w http.ResponseWriter, r *http.Request) {
	id, err := parseResumePathID(r)
	if err != nil {
		writeResumeError(w, err)
		return
	}
	match, matchErr := photoIfNoneMatch(r.Header.Values("If-None-Match"), "")
	_ = match
	if matchErr != nil {
		writeResumeError(w, matchErr)
		return
	}
	userID, userErr := requestUserID(r)
	if userErr != nil {
		writeResumeError(w, userErr)
		return
	}
	reader, ok := s.resumes.(resumePoolReader)
	if !ok || s.blobs == nil {
		writeResumeError(w, internalClientError())
		return
	}
	row, readErr := reader.Get(r.Context(), userID, id)
	if readErr != nil {
		writeResumeError(w, mapMutationError(readErr))
		return
	}
	photo := row.Doc.PersonalDetails.Photo
	if photo == nil {
		writeResumeError(w, photoNotFoundError())
		return
	}
	ext, parseErr := media.ParsePhotoKey(id, photo.Key)
	if parseErr != nil {
		s.recordPhotoKeyInvariant(r.Context())
		writeResumeError(w, internalClientError())
		return
	}
	etag := photoObjectETag(photo.Key)
	matched, matchErr := photoIfNoneMatch(r.Header.Values("If-None-Match"), etag)
	if matchErr != nil {
		writeResumeError(w, matchErr)
		return
	}
	if matched {
		w.Header().Set("ETag", etag)
		w.Header().Set("Cache-Control", api.CacheControlNoStore)
		w.WriteHeader(http.StatusNotModified)
		return
	}
	body, contentType, getErr := s.blobs.Get(r.Context(), photo.Key)
	if getErr != nil {
		if errors.Is(getErr, media.ErrNotFound) {
			writeResumeError(w, photoNotFoundError())
			return
		}
		writeResumeError(w, internalClientError())
		return
	}
	requestContext := r.Context()
	defer func() {
		if closeErr := body.Close(); closeErr != nil {
			s.logger.ErrorContext(requestContext, "close photo response body", "error", closeErr)
		}
	}()
	wantContentType := "image/jpeg"
	if ext == "png" {
		wantContentType = "image/png"
	}
	if contentType != wantContentType {
		writeResumeError(w, internalClientError())
		return
	}
	w.Header().Set("ETag", etag)
	w.Header().Set("Cache-Control", api.CacheControlNoStore)
	w.Header().Set("Content-Type", contentType)
	w.WriteHeader(http.StatusOK)
	if _, copyErr := io.Copy(w, body); copyErr != nil {
		s.logger.ErrorContext(requestContext, "write photo response", "error", copyErr)
	}
}

func (s *Service) handleUpdateResumePhotoCrop(w http.ResponseWriter, r *http.Request) {
	spec := mutationSpec{
		RegisteredOperation: "updateResumePhotoCrop", RequireMatch: true,
		Decode: func(r *http.Request) (boundedInput, error) {
			id, err := parseResumePathID(r)
			if err != nil {
				return boundedInput{}, err
			}
			var request map[string]json.RawMessage
			decoded, decodeErr := decodeJSONBody(r, &request)
			if decodeErr != nil {
				return boundedInput{}, decodeErr
			}
			crop, cropErr := decodePhotoCrop(decoded.Payload)
			if cropErr != nil {
				return boundedInput{}, cropErr
			}
			decoded.Value = photoCropInput{ResumeID: id, Crop: crop}
			return decoded, nil
		},
		CanonicalTargets: photoMutationTarget,
		Prepare: func(ctx context.Context, input boundedInput, _ idempotencyInspection) (preparedInput, error) {
			decoded, ok := input.Value.(photoCropInput)
			if !ok {
				return preparedInput{}, internalClientError()
			}
			return preparedInput{Input: input, Value: aggregatePreparedInput{
				ResumeID: decoded.ResumeID,
				Apply: func(document json.RawMessage) (json.RawMessage, error) {
					changed, _, err := applyPhotoChange(document, decoded.ResumeID, decoded.Crop, false)
					if errors.Is(err, media.ErrInvalidKey) {
						s.recordPhotoKeyInvariant(ctx)
					}
					return changed, err
				},
				Response: s.resumeResponseBuilder(http.StatusOK, false),
			}}, nil
		},
		Run:        aggregateOperation{service: s},
		Transition: s.nonDrainingTransition,
	}
	s.executeMutation(w, r, spec)
}

func (s *Service) handleDeleteResumePhoto(w http.ResponseWriter, r *http.Request) {
	spec := mutationSpec{
		RegisteredOperation: "deleteResumePhoto", RequireMatch: true,
		Decode: func(r *http.Request) (boundedInput, error) {
			id, err := parseResumePathID(r)
			if err != nil {
				return boundedInput{}, err
			}
			decoded, decodeErr := decodeDeleteBody(r)
			if decodeErr != nil {
				return boundedInput{}, decodeErr
			}
			decoded.Value = photoDeleteInput{ResumeID: id}
			return decoded, nil
		},
		CanonicalTargets: photoMutationTarget,
		Prepare: func(ctx context.Context, input boundedInput, _ idempotencyInspection) (preparedInput, error) {
			decoded, ok := input.Value.(photoDeleteInput)
			queue, queueOK := s.resumes.(resumeMediaDeletionQueue)
			if !ok || !queueOK {
				return preparedInput{}, internalClientError()
			}
			var oldKey string
			return preparedInput{Input: input, Value: aggregatePreparedInput{
				ResumeID: decoded.ResumeID,
				Apply: func(document json.RawMessage) (json.RawMessage, error) {
					changed, transactionKey, err := applyPhotoChange(document, decoded.ResumeID, nil, true)
					if errors.Is(err, media.ErrInvalidKey) {
						s.recordPhotoKeyInvariant(ctx)
					}
					oldKey = transactionKey
					return changed, err
				},
				BeforeSave: func(ctx context.Context, qtx *store.Queries, _ resume.Resume, _ schema.Resume) error {
					return queue.EnqueueMediaDeletionTx(ctx, qtx, decoded.ResumeID, oldKey)
				},
				Response: deletedChildResponse,
			}}, nil
		},
		Run:        aggregateOperation{service: s},
		Transition: s.nonDrainingTransition,
	}
	s.executeMutation(w, r, spec)
}

func photoMutationTarget(input boundedInput) ([]string, error) {
	var id uuid.UUID
	switch decoded := input.Value.(type) {
	case photoCropInput:
		id = decoded.ResumeID
	case photoDeleteInput:
		id = decoded.ResumeID
	default:
		return nil, internalClientError()
	}
	return []string{"resume_id", id.String()}, nil
}

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

func photoNotFoundError() *clientError {
	return &clientError{Status: http.StatusNotFound, Code: "media_not_found", Message: "resume photo not found"}
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

func applyPhotoReplacement(document json.RawMessage, resumeID uuid.UUID, newKey string) (json.RawMessage, string, error) {
	if _, err := media.ParsePhotoKey(resumeID, newKey); err != nil {
		return nil, "", err
	}
	var doc schema.Resume
	if err := json.Unmarshal(document, &doc); err != nil {
		return nil, "", err
	}
	oldKey := ""
	if doc.PersonalDetails.Photo != nil {
		oldKey = doc.PersonalDetails.Photo.Key
		if _, err := media.ParsePhotoKey(resumeID, oldKey); err != nil {
			return nil, "", err
		}
	}
	doc.PersonalDetails.Photo = &schema.Photo{Key: newKey}
	changed, err := json.Marshal(doc)
	return changed, oldKey, err
}

func applyPhotoChange(document json.RawMessage, resumeID uuid.UUID, crop *schema.PhotoCrop, deletePhoto bool) (json.RawMessage, string, error) {
	var doc schema.Resume
	if err := json.Unmarshal(document, &doc); err != nil {
		return nil, "", err
	}
	if doc.PersonalDetails.Photo == nil {
		return nil, "", photoNotFoundError()
	}
	key := doc.PersonalDetails.Photo.Key
	if _, err := media.ParsePhotoKey(resumeID, key); err != nil {
		return nil, "", err
	}
	if deletePhoto {
		doc.PersonalDetails.Photo = nil
	} else {
		doc.PersonalDetails.Photo.Crop = crop
	}
	changed, err := json.Marshal(doc)
	return changed, key, err
}

func photoObjectETag(key string) string {
	digest := sha256.Sum256([]byte(key))
	return `"` + hex.EncodeToString(digest[:]) + `"`
}

func photoIfNoneMatch(values []string, current string) (bool, *clientError) {
	if len(values) == 0 {
		return false, nil
	}
	value := values[0]
	if len(values) != 1 || strings.Contains(value, ",") || strings.HasPrefix(value, "W/") || len(value) < 2 || value[0] != '"' || value[len(value)-1] != '"' {
		return false, &clientError{Status: http.StatusBadRequest, Code: "request_invalid", Message: "If-None-Match must be one well-formed strong tag"}
	}
	for _, c := range value[1 : len(value)-1] {
		if c < 0x21 || c == 0x7f || c == '"' {
			return false, &clientError{Status: http.StatusBadRequest, Code: "request_invalid", Message: "If-None-Match must be one well-formed strong tag"}
		}
	}
	return current != "" && value == current, nil
}
