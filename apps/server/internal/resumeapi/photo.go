package resumeapi

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/google/uuid"

	schema "github.com/dannyota/aboutme/packages/schema/gen/go"

	"github.com/dannyota/aboutme/apps/server/internal/api"
	"github.com/dannyota/aboutme/apps/server/internal/media"
	"github.com/dannyota/aboutme/apps/server/internal/resume"
	"github.com/dannyota/aboutme/apps/server/internal/store"
)

func photoRoutes() []routeSpec {
	return []routeSpec{
		{Method: http.MethodPost, Pattern: apiResumePath + "/{id}/photo", Operation: "uploadResumePhoto", Mutation: true, Upload: true, OperationKind: operationPhotoCandidate, AcceptsWireVersion: true, EmitsWireVersion: true, Handler: (*Service).handleUploadResumePhoto},
		{Method: http.MethodGet, Pattern: apiResumePath + "/{id}/photo", Operation: "getResumePhoto", Handler: (*Service).handleGetResumePhoto},
		{Method: http.MethodPatch, Pattern: apiResumePath + "/{id}/photo", Operation: "updateResumePhotoCrop", Mutation: true, OperationKind: operationAggregate, AcceptsWireVersion: true, EmitsWireVersion: true, Handler: (*Service).handleUpdateResumePhotoCrop},
		{Method: http.MethodDelete, Pattern: apiResumePath + "/{id}/photo", Operation: "deleteResumePhoto", Mutation: true, OperationKind: operationAggregate, AcceptsWireVersion: true, EmitsWireVersion: true, Handler: (*Service).handleDeleteResumePhoto},
	}
}

type photoCropInput struct {
	// ResumeID is the resume named by the request path.
	ResumeID uuid.UUID
	// Crop is the requested crop; nil clears the stored crop.
	Crop *schema.PhotoCrop
}

type photoDeleteInput struct {
	// ResumeID is the resume named by the request path.
	ResumeID uuid.UUID
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
	operation := "deleteResumePhoto"
	if _, agentCall := agentPrincipalFromContext(r.Context()); agentCall {
		operation = "mcp.deleteResumePhoto"
	}
	spec := mutationSpec{
		RegisteredOperation: operation, RequireMatch: true,
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
				Response: func(row resume.Resume, doc schema.Resume, version int32) (resume.StoredResponse, error) {
					if _, agentCall := agentPrincipalFromContext(ctx); agentCall {
						return s.resumeResponseBuilder(http.StatusOK, false)(row, doc, version)
					}
					return deletedChildResponse(row, doc, version)
				},
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

func photoNotFoundError() *clientError {
	return &clientError{Status: http.StatusNotFound, Code: "media_not_found", Message: "resume photo not found"}
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
