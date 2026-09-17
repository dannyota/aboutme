package resumeapi

// Resume create, metadata update, and delete: their request/prepared
// input types and mutationOperation and HTTP handler implementations.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/google/uuid"

	schema "github.com/dannyota/aboutme/packages/schema/gen/go"

	"github.com/dannyota/aboutme/apps/server/internal/media"
	"github.com/dannyota/aboutme/apps/server/internal/resume"
	"github.com/dannyota/aboutme/apps/server/internal/resume/docmigrate"
	"github.com/dannyota/aboutme/apps/server/internal/store"
)

type resumeCreateRequest struct {
	Title    json.RawMessage `json:"title"`
	Lng      json.RawMessage `json:"lng"`
	Document json.RawMessage `json:"document"`
}

type resumeMetadataRequest struct {
	Title json.RawMessage `json:"title"`
	Lng   json.RawMessage `json:"lng"`
}

type resumeTargetInput struct {
	ResumeID uuid.UUID
	Request  resumeMetadataRequest
}

type resumeDeleteInput struct {
	ResumeID uuid.UUID
}

type resumeMetadataPrepared struct {
	ResumeID        uuid.UUID
	TitlePresent    bool
	Title           string
	LanguagePresent bool
	Language        *string
	Response        mutationResponseBuilder
}
type resumeMetadataMutation struct{ service *Service }

// Run implements mutationOperation for metadata replacement.
func (op resumeMetadataMutation) Run(ctx context.Context, qtx *store.Queries, mutation mutationContext,
	prepared preparedInput,
) (mutationRunResult, error) {
	input, ok := prepared.Value.(resumeMetadataPrepared)
	if !ok || input.Response == nil || mutation.ExpectedRevision == nil {
		return mutationRunResult{}, fmt.Errorf("resumeapi: metadata mutation received the wrong prepared input")
	}
	current, err := op.service.currentMutationResume(ctx, qtx, mutation, input.ResumeID)
	if err != nil {
		return mutationRunResult{}, err
	}
	title := current.Title
	if input.TitlePresent {
		title = input.Title
	}
	lng := current.Lng
	if input.LanguagePresent {
		lng = input.Language
	}
	doc, err := op.service.prepareDocumentForPersistence(current.Doc)
	if err != nil {
		return mutationRunResult{}, err
	}
	if _, saveErr := op.service.resumes.SaveMetadataAndDocumentTx(
		ctx, qtx, mutation.UserID, input.ResumeID, title, lng, doc, *mutation.ExpectedRevision,
	); saveErr != nil {
		return mutationRunResult{}, saveErr
	}
	updated, err := op.service.resumes.GetTx(ctx, qtx, mutation.UserID, input.ResumeID)
	if err != nil {
		return mutationRunResult{}, err
	}
	response, err := input.Response(updated, updated.Doc, mutation.WireVersion)
	return mutationRunResult{Response: response}, err
}
func (s *Service) handleCreateResume(w http.ResponseWriter, r *http.Request) {
	spec := mutationSpec{
		RegisteredOperation: "createResume",
		Decode: func(r *http.Request) (boundedInput, error) {
			var request resumeCreateRequest
			return decodeJSONBody(r, &request)
		},
		CanonicalTargets: func(boundedInput) ([]string, error) { return nil, nil },
		Prepare: func(_ context.Context, input boundedInput, inspection idempotencyInspection) (preparedInput, error) {
			request, ok := input.Value.(*resumeCreateRequest)
			if !ok {
				return preparedInput{}, internalClientError()
			}
			title, err := decodeResumeTitle(request.Title, true)
			if err != nil {
				return preparedInput{}, err
			}
			lng, err := decodeResumeLanguage(request.Lng)
			if err != nil {
				return preparedInput{}, err
			}
			doc := defaultResumeDocument()
			if len(request.Document) != 0 {
				if bytes.Equal(bytes.TrimSpace(request.Document), []byte("null")) {
					return preparedInput{}, documentInvalid("document", "document must be an object")
				}
				if seedCarriesPhoto(request.Document) {
					return preparedInput{}, documentInvalid("personalDetails.photo", "photo is server-owned")
				}
				if s.projector == nil {
					return preparedInput{}, internalClientError()
				}
				accepted, _, acceptErr := s.projector.AcceptWire(request.Document, inspectionWireVersion(inspection, r, s.acceptedVersions))
				if acceptErr != nil {
					return preparedInput{}, documentInvalid("document", "document does not match the declared schema version")
				}
				doc, acceptErr = strictDecodeCurrentDocument(accepted)
				if acceptErr != nil {
					return preparedInput{}, documentInvalid("document", "document does not match the current schema")
				}
			}
			return preparedInput{Input: input, Value: createPreparedInput{
				Document: doc, Title: title, Language: lng,
				Response: s.resumeResponseBuilder(http.StatusCreated, true),
			}}, nil
		},
		Run: createOperation{service: s},
	}
	s.executeMutation(w, r, spec)
}

// inspectionWireVersion returns the version already bound into the request
// fingerprint. Prepare does not otherwise receive mutation headers.
func inspectionWireVersion(_ idempotencyInspection, r *http.Request, accepted []int32) int32 {
	version, err := resolveWireVersion(r.Header, accepted)
	if err != nil {
		return docmigrate.CurrentVersion
	}
	return version
}
func (s *Service) handleUpdateResumeMetadata(w http.ResponseWriter, r *http.Request) {
	spec := mutationSpec{
		RegisteredOperation: "updateResumeMetadata", RequireMatch: true,
		Decode: func(r *http.Request) (boundedInput, error) {
			id, err := parseResumePathID(r)
			if err != nil {
				return boundedInput{}, err
			}
			var request resumeMetadataRequest
			decoded, decodeErr := decodeJSONBody(r, &request)
			if decodeErr != nil {
				return boundedInput{}, decodeErr
			}
			decoded.Value = resumeTargetInput{ResumeID: id, Request: request}
			return decoded, nil
		},
		CanonicalTargets: resumeTarget,
		Prepare: func(_ context.Context, input boundedInput, _ idempotencyInspection) (preparedInput, error) {
			decoded, ok := input.Value.(resumeTargetInput)
			if !ok {
				return preparedInput{}, internalClientError()
			}
			if len(decoded.Request.Title) == 0 && len(decoded.Request.Lng) == 0 {
				return preparedInput{}, documentInvalid("", "at least one metadata field is required")
			}
			prepared := resumeMetadataPrepared{
				ResumeID: decoded.ResumeID, TitlePresent: len(decoded.Request.Title) != 0,
				LanguagePresent: len(decoded.Request.Lng) != 0,
				Response:        s.resumeResponseBuilder(http.StatusOK, false),
			}
			var err error
			if prepared.TitlePresent {
				prepared.Title, err = decodeResumeTitle(decoded.Request.Title, true)
				if err != nil {
					return preparedInput{}, err
				}
			}
			if prepared.LanguagePresent {
				prepared.Language, err = decodeResumeLanguage(decoded.Request.Lng)
				if err != nil {
					return preparedInput{}, err
				}
			}
			return preparedInput{Input: input, Value: prepared}, nil
		},
		Run:        operationMetadata.build(s),
		Transition: s.nonDrainingTransition,
	}
	s.executeMutation(w, r, spec)
}
func (s *Service) handleDeleteResume(w http.ResponseWriter, r *http.Request) {
	spec := mutationSpec{
		RegisteredOperation: "deleteResume", RequireMatch: true,
		Decode: func(r *http.Request) (boundedInput, error) {
			id, err := parseResumePathID(r)
			if err != nil {
				return boundedInput{}, err
			}
			decoded, decodeErr := decodeDeleteBody(r)
			if decodeErr != nil {
				return boundedInput{}, decodeErr
			}
			decoded.Value = resumeDeleteInput{ResumeID: id}
			return decoded, nil
		},
		CanonicalTargets: func(input boundedInput) ([]string, error) {
			decoded, ok := input.Value.(resumeDeleteInput)
			if !ok {
				return nil, internalClientError()
			}
			return []string{"resume_id", decoded.ResumeID.String()}, nil
		},
		Prepare: func(_ context.Context, input boundedInput, _ idempotencyInspection) (preparedInput, error) {
			decoded, ok := input.Value.(resumeDeleteInput)
			if !ok {
				return preparedInput{}, internalClientError()
			}
			queue, ok := s.resumes.(resumeMediaDeletionQueue)
			if !ok {
				return preparedInput{}, internalClientError()
			}
			return preparedInput{Input: input, Value: deletePreparedInput{
				ResumeID: decoded.ResumeID,
				BeforeDelete: func(ctx context.Context, qtx *store.Queries, deleted resume.Resume) error {
					photo := deleted.Doc.PersonalDetails.Photo
					if photo == nil {
						return nil
					}
					if _, err := media.ParsePhotoKey(deleted.ID, photo.Key); err != nil {
						s.recordPhotoKeyInvariant(ctx)
						return fmt.Errorf("resumeapi: validate deleted photo key: %w", err)
					}
					return queue.EnqueueMediaDeletionTx(ctx, qtx, deleted.ID, photo.Key)
				},
				Response: func(resume.Resume, schema.Resume, int32) (resume.StoredResponse, error) {
					return resume.StoredResponse{Status: http.StatusNoContent}, nil
				},
			}}, nil
		},
		Run:        deleteOperation{service: s},
		Transition: s.deleteTransition,
	}
	s.executeMutation(w, r, spec)
}

func resumeTarget(input boundedInput) ([]string, error) {
	decoded, ok := input.Value.(resumeTargetInput)
	if !ok {
		return nil, internalClientError()
	}
	return []string{"resume_id", decoded.ResumeID.String()}, nil
}
