package resumeapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/google/uuid"

	schema "github.com/dannyota/aboutme/packages/schema/gen/go"

	"github.com/dannyota/aboutme/apps/server/internal/api"
	"github.com/dannyota/aboutme/apps/server/internal/media"
	"github.com/dannyota/aboutme/apps/server/internal/resume"
	"github.com/dannyota/aboutme/apps/server/internal/store"
)

// prepareDocumentForPersistence sanitizes a new canonical document exactly
// once, then applies the complete-document store validation.
func prepareDocumentForPersistence(doc schema.Resume) (schema.Resume, error) {
	return (&Service{}).prepareDocumentForPersistence(doc)
}

func (s *Service) prepareDocumentForPersistence(doc schema.Resume) (schema.Resume, error) {
	doc = s.sanitizeForPersistence(doc)
	return validateDocumentForPersistence(doc)
}

func (s *Service) sanitizeForPersistence(doc schema.Resume) schema.Resume {
	if s.sanitizeDocument == nil {
		return sanitizeDocument(doc)
	}
	return s.sanitizeDocument(doc)
}

// validateDocumentForPersistence applies the complete-document store checks
// without sanitizing content that already crossed the sanitizer boundary.
func validateDocumentForPersistence(doc schema.Resume) (schema.Resume, error) {
	if err := resume.ValidateForStore(doc); err != nil {
		return schema.Resume{}, err
	}
	return doc, nil
}

type mutationResponseBuilder func(resume.Resume, schema.Resume, int32) (resume.StoredResponse, error)

type operationKind uint8

const (
	operationNone operationKind = iota
	operationCreate
	operationMetadata
	operationDelete
	operationAggregate
	operationPhotoCandidate
)

func (kind operationKind) build(service *Service) mutationOperation {
	switch kind {
	case operationCreate:
		return createOperation{service: service}
	case operationMetadata:
		return metadataOperation{service: service}
	case operationDelete:
		return deleteOperation{service: service}
	case operationAggregate:
		return aggregateOperation{service: service}
	case operationPhotoCandidate:
		return photoCandidateOperation{aggregateOperation{service: service}}
	case operationNone:
		return nil
	default:
		return nil
	}
}

type createPreparedInput struct {
	Document schema.Resume
	Title    string
	Language *string
	Response mutationResponseBuilder
}

type aggregatePreparedInput struct {
	ResumeID   uuid.UUID
	Apply      func(json.RawMessage) (json.RawMessage, error)
	Response   mutationResponseBuilder
	BeforeSave func(context.Context, *store.Queries, resume.Resume, schema.Resume) error
}

type metadataPreparedInput struct {
	aggregatePreparedInput
	Title    string
	Language *string
}

type deletePreparedInput struct {
	ResumeID     uuid.UUID
	BeforeDelete func(context.Context, *store.Queries, resume.Resume) error
	Response     mutationResponseBuilder
}

type createOperation struct{ service *Service }

// Run creates one resume through the transaction-bound store seam.
func (op createOperation) Run(ctx context.Context, qtx *store.Queries, mutation mutationContext,
	prepared preparedInput,
) (mutationRunResult, error) {
	input, ok := prepared.Value.(createPreparedInput)
	if !ok || input.Response == nil {
		return mutationRunResult{}, fmt.Errorf("resumeapi: create operation received the wrong prepared input")
	}
	doc, err := op.service.prepareDocumentForPersistence(input.Document)
	if err != nil {
		return mutationRunResult{}, err
	}
	created, err := op.service.resumes.CreateTx(ctx, qtx, mutation.UserID, input.Title, input.Language, doc)
	if err != nil {
		return mutationRunResult{}, err
	}
	response, err := input.Response(created, created.Doc, mutation.WireVersion)
	return mutationRunResult{Response: response}, err
}

type aggregateOperation struct{ service *Service }

// Run applies one complete-document aggregate mutation and revision CAS.
func (op aggregateOperation) Run(ctx context.Context, qtx *store.Queries, mutation mutationContext,
	prepared preparedInput,
) (mutationRunResult, error) {
	input, ok := prepared.Value.(aggregatePreparedInput)
	if !ok || input.Apply == nil || input.Response == nil || mutation.ExpectedRevision == nil {
		return mutationRunResult{}, fmt.Errorf("resumeapi: aggregate operation received the wrong prepared input")
	}
	current, err := op.service.resumes.GetTx(ctx, qtx, mutation.UserID, input.ResumeID)
	if err != nil {
		return mutationRunResult{}, err
	}
	doc, err := op.service.applyAtWireVersion(current.Doc, mutation.WireVersion, input.Apply)
	if err != nil {
		return mutationRunResult{}, err
	}
	if input.BeforeSave != nil {
		hookDocument, cloneErr := cloneDocumentForHook(doc)
		if cloneErr != nil {
			return mutationRunResult{}, cloneErr
		}
		if beforeSaveErr := input.BeforeSave(ctx, qtx, current, hookDocument); beforeSaveErr != nil {
			return mutationRunResult{}, beforeSaveErr
		}
	}
	revision, err := op.service.resumes.SaveDocumentTx(ctx, qtx, mutation.UserID, input.ResumeID, doc, *mutation.ExpectedRevision)
	if err != nil {
		return mutationRunResult{}, err
	}
	current.Revision = revision
	current.Doc = doc
	response, err := input.Response(current, doc, mutation.WireVersion)
	return mutationRunResult{Response: response}, err
}

type metadataOperation struct{ service *Service }

// Run persists metadata and the complete current document in one CAS.
func (op metadataOperation) Run(ctx context.Context, qtx *store.Queries, mutation mutationContext,
	prepared preparedInput,
) (mutationRunResult, error) {
	input, ok := prepared.Value.(metadataPreparedInput)
	if !ok || input.Apply == nil || input.Response == nil || mutation.ExpectedRevision == nil {
		return mutationRunResult{}, fmt.Errorf("resumeapi: metadata operation received the wrong prepared input")
	}
	current, err := op.service.resumes.GetTx(ctx, qtx, mutation.UserID, input.ResumeID)
	if err != nil {
		return mutationRunResult{}, err
	}
	doc, err := op.service.applyAtWireVersion(current.Doc, mutation.WireVersion, input.Apply)
	if err != nil {
		return mutationRunResult{}, err
	}
	if input.BeforeSave != nil {
		hookDocument, cloneErr := cloneDocumentForHook(doc)
		if cloneErr != nil {
			return mutationRunResult{}, cloneErr
		}
		if beforeSaveErr := input.BeforeSave(ctx, qtx, current, hookDocument); beforeSaveErr != nil {
			return mutationRunResult{}, beforeSaveErr
		}
	}
	revision, err := op.service.resumes.SaveMetadataAndDocumentTx(
		ctx, qtx, mutation.UserID, input.ResumeID, input.Title, input.Language, doc, *mutation.ExpectedRevision,
	)
	if err != nil {
		return mutationRunResult{}, err
	}
	current.Title = input.Title
	current.Lng = input.Language
	current.Revision = revision
	current.Doc = doc
	response, err := input.Response(current, doc, mutation.WireVersion)
	return mutationRunResult{Response: response}, err
}

type deleteOperation struct{ service *Service }

// Run performs the revision-aware whole-resume delete without a document write.
func (op deleteOperation) Run(ctx context.Context, qtx *store.Queries, mutation mutationContext,
	prepared preparedInput,
) (mutationRunResult, error) {
	input, ok := prepared.Value.(deletePreparedInput)
	if !ok || input.Response == nil || mutation.ExpectedRevision == nil {
		return mutationRunResult{}, fmt.Errorf("resumeapi: delete operation received the wrong prepared input")
	}
	deleted, err := op.service.resumes.DeleteTx(ctx, qtx, mutation.UserID, input.ResumeID, *mutation.ExpectedRevision)
	if err != nil {
		return mutationRunResult{}, err
	}
	if input.BeforeDelete != nil {
		if beforeDeleteErr := input.BeforeDelete(ctx, qtx, deleted); beforeDeleteErr != nil {
			return mutationRunResult{}, beforeDeleteErr
		}
	}
	response, err := input.Response(deleted, deleted.Doc, mutation.WireVersion)
	return mutationRunResult{Response: response}, err
}

// photoCandidateOperation commits a proved-created candidate only through
// the ordinary aggregate operation. Candidate creation and compensation stay
// outside the database callback.
type photoCandidateOperation struct{ aggregateOperation }

type photoCandidate struct {
	Key     string
	Created bool
}

const photoCandidateCleanupTimeout = 5 * time.Second

func (s *Service) finalizePhotoCandidate(candidate photoCandidate) func(context.Context, preparedInput, resume.ExecuteResult, error) {
	return func(ctx context.Context, _ preparedInput, result resume.ExecuteResult, _ error) {
		if !candidate.Created {
			return
		}
		if result.Replayed && result.Outcome != resume.CommitCommitted {
			s.logger.ErrorContext(ctx, "resume photo candidate invariant failed",
				"request_id", api.RequestIDFromContext(ctx))
			return
		}
		deleteCandidate := result.Replayed || result.Outcome == resume.CommitNotAttempted ||
			result.Outcome == resume.CommitDefinitelyRolledBack
		if !deleteCandidate || s.blobs == nil {
			return
		}
		cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), photoCandidateCleanupTimeout)
		defer cancel()
		if err := s.blobs.Delete(cleanupCtx, candidate.Key); err != nil &&
			!errors.Is(err, media.ErrNotFound) {
			s.logger.ErrorContext(ctx, "resume photo candidate cleanup failed",
				"request_id", api.RequestIDFromContext(ctx))
		}
	}
}

func strictDecodeCurrentDocument(raw []byte) (schema.Resume, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	var doc schema.Resume
	if err := dec.Decode(&doc); err != nil {
		return schema.Resume{}, fmt.Errorf("resumeapi: decode current document: %w", err)
	}
	var trailing any
	if err := dec.Decode(&trailing); !errors.Is(err, io.EOF) {
		return schema.Resume{}, fmt.Errorf("resumeapi: decode current document: trailing data")
	}
	return doc, nil
}

func cloneDocumentForHook(doc schema.Resume) (schema.Resume, error) {
	raw, err := json.Marshal(doc)
	if err != nil {
		return schema.Resume{}, fmt.Errorf("resumeapi: clone document for pre-save hook: %w", err)
	}
	cloned, err := strictDecodeCurrentDocument(raw)
	if err != nil {
		return schema.Resume{}, fmt.Errorf("resumeapi: clone document for pre-save hook: %w", err)
	}
	return cloned, nil
}

// applyAtWireVersion preserves the fixed down-emit, apply, up-accept order.
// apply receives and returns a complete document at the caller's version.
func (s *Service) applyAtWireVersion(current schema.Resume, version int32,
	apply func(json.RawMessage) (json.RawMessage, error),
) (schema.Resume, error) {
	canonical, err := resume.AssembleCanonical(current)
	if err != nil {
		return schema.Resume{}, err
	}
	wire, err := s.projector.EmitWire(canonical, version)
	if err != nil {
		return schema.Resume{}, fmt.Errorf("resumeapi: emit wire document: %w", err)
	}
	changed, err := apply(wire)
	if err != nil {
		return schema.Resume{}, err
	}
	wireDocument, err := strictDecodeCurrentDocument(changed)
	if err != nil {
		return schema.Resume{}, err
	}
	sanitizedWire, err := json.Marshal(s.sanitizeForPersistence(wireDocument))
	if err != nil {
		return schema.Resume{}, fmt.Errorf("resumeapi: encode sanitized wire document: %w", err)
	}
	accepted, _, err := s.projector.AcceptWire(sanitizedWire, version)
	if err != nil {
		return schema.Resume{}, fmt.Errorf("resumeapi: accept wire document: %w", err)
	}
	doc, err := strictDecodeCurrentDocument(accepted)
	if err != nil {
		return schema.Resume{}, err
	}
	return validateDocumentForPersistence(doc)
}
