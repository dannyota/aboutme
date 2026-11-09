package resumeapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/google/uuid"

	schema "github.com/dannyota/aboutme/packages/schema/gen/go"

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

type photoUploadInput struct {
	// ResumeID is the resume named by the request path.
	ResumeID uuid.UUID
	// File is the raw uploaded file part, before normalization.
	File []byte
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
	// ExecuteBefore is when the candidate's create-to-commit lifetime ends.
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
