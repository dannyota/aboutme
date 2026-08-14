package resumeapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"golang.org/x/text/language"

	schema "github.com/dannyota/aboutme/packages/schema/gen/go"

	"github.com/dannyota/aboutme/apps/server/internal/auth"
	"github.com/dannyota/aboutme/apps/server/internal/media"
	"github.com/dannyota/aboutme/apps/server/internal/publicstate"
	"github.com/dannyota/aboutme/apps/server/internal/resume"
	"github.com/dannyota/aboutme/apps/server/internal/resume/docmigrate"
	"github.com/dannyota/aboutme/apps/server/internal/store"
)

func resumeRoutes() []routeSpec {
	return []routeSpec{
		{Method: http.MethodGet, Pattern: apiResumePath, Operation: "listResumes", AcceptsWireVersion: true, EmitsWireVersion: true, Handler: (*Service).handleListResumes},
		{Method: http.MethodPost, Pattern: apiResumePath, Operation: "createResume", Mutation: true, OperationKind: operationCreate, AcceptsWireVersion: true, EmitsWireVersion: true, Handler: (*Service).handleCreateResume},
		{Method: http.MethodGet, Pattern: apiResumePath + "/{id}", Operation: "getResume", AcceptsWireVersion: true, EmitsWireVersion: true, Handler: (*Service).handleGetResume},
		{Method: http.MethodPatch, Pattern: apiResumePath + "/{id}", Operation: "updateResumeMetadata", Mutation: true, OperationKind: operationMetadata, AcceptsWireVersion: true, EmitsWireVersion: true, Handler: (*Service).handleUpdateResumeMetadata},
		{Method: http.MethodDelete, Pattern: apiResumePath + "/{id}", Operation: "deleteResume", Mutation: true, OperationKind: operationDelete, AcceptsWireVersion: true, Handler: (*Service).handleDeleteResume},
		{Method: http.MethodPost, Pattern: apiResumePath + "/{id}/publish", Operation: "publishResume", Mutation: true, OperationKind: operationPublish, AcceptsWireVersion: true, EmitsWireVersion: true, Handler: (*Service).handlePublishResume},
	}
}

type publishMutationPrepared struct {
	ResumeID   uuid.UUID
	Input      publishInput
	ReleasedAt time.Time
}

type publishOperation struct{ service *Service }

func (op publishOperation) Run(ctx context.Context, qtx *store.Queries, mutation mutationContext, prepared preparedInput) (mutationRunResult, error) {
	input, ok := prepared.Value.(publishMutationPrepared)
	if !ok || mutation.ExpectedRevision == nil {
		return mutationRunResult{}, errors.New("resumeapi: publish operation received the wrong prepared input")
	}
	current, err := op.service.currentMutationResume(ctx, qtx, mutation, input.ResumeID)
	if err != nil {
		return mutationRunResult{}, err
	}
	state := currentPublish{Slug: current.Slug, Live: current.Live, DownloadEnabled: current.DownloadEnabled, SEOGeoEnabled: current.SEOGeoEnabled, Revision: current.Revision}
	validated := validatePublish(current.Doc, state, input.Input)
	if len(validated.Issues) != 0 {
		return mutationRunResult{}, publishInvalidError(validated.Issues)
	}
	if publishRequiresRecentReauth(state, validated) {
		if err := auth.RequireRecentReauth(mutation.Session, op.service.clock()); err != nil {
			return mutationRunResult{}, err
		}
	}
	if validated.ChangedSlug && validated.Effective.Slug != nil {
		if _, tombstoneErr := qtx.GetSlugTombstoneForUpdate(ctx, *validated.Effective.Slug); tombstoneErr == nil {
			op.service.recordTransactionOrder("tombstone")
			if _, consumeErr := qtx.ConsumeExpiredSlugTombstone(ctx, store.ConsumeExpiredSlugTombstoneParams{Slug: *validated.Effective.Slug, ReusableAt: op.service.clock()}); consumeErr != nil {
				if errors.Is(consumeErr, pgx.ErrNoRows) {
					return mutationRunResult{}, slugTakenError()
				}
				return mutationRunResult{}, consumeErr
			}
		} else if errors.Is(tombstoneErr, pgx.ErrNoRows) {
			op.service.recordTransactionOrder("tombstone")
		} else {
			return mutationRunResult{}, tombstoneErr
		}
		claimed, claimErr := qtx.GetSlugClaim(ctx, *validated.Effective.Slug)
		op.service.recordTransactionOrder("claim")
		if claimErr == nil && claimed != current.ID {
			return mutationRunResult{}, slugTakenError()
		}
		if claimErr != nil && !errors.Is(claimErr, pgx.ErrNoRows) {
			return mutationRunResult{}, claimErr
		}
	}
	if state.Slug != nil && validated.ChangedSlug {
		if _, err := qtx.InsertSlugTombstone(ctx, store.InsertSlugTombstoneParams{Slug: *state.Slug, ReleasedByUserID: &mutation.UserID, ReleasedAt: input.ReleasedAt}); err != nil {
			return mutationRunResult{}, err
		}
	}
	updated, err := qtx.PublishResumeCAS(ctx, store.PublishResumeCASParams{ID: current.ID, UserID: mutation.UserID, ExpectedRevision: *mutation.ExpectedRevision, Slug: validated.Effective.Slug, Live: validated.Effective.Live, DownloadEnabled: validated.Effective.DownloadEnabled, SEOGeoEnabled: validated.Effective.SEOGeoEnabled, UpdatedAt: op.service.clock()})
	if err != nil {
		return mutationRunResult{}, err
	}
	if publishChangesDiscovery(state, validated.Effective) {
		if _, err := qtx.AdvanceDiscoveryGeneration(ctx); err != nil {
			return mutationRunResult{}, err
		}
	}
	row, err := op.service.resumes.GetTx(ctx, qtx, mutation.UserID, updated.ID)
	if err != nil {
		return mutationRunResult{}, err
	}
	response, err := op.service.resumeResponseBuilder(http.StatusOK, false)(row, row.Doc, mutation.WireVersion)
	return mutationRunResult{Response: response}, err
}

func (s *Service) handlePublishResume(w http.ResponseWriter, r *http.Request) {
	s.executeMutation(w, r, mutationSpec{
		RegisteredOperation: "publishResume", RequireMatch: true,
		Decode: func(r *http.Request) (boundedInput, error) {
			id, err := parseResumePathID(r)
			if err != nil {
				return boundedInput{}, err
			}
			if err := requireJSONContentType(r.Header); err != nil {
				return boundedInput{}, err
			}
			var raw bytes.Buffer
			input, decodeErr := decodePublish(io.TeeReader(r.Body, &raw))
			if decodeErr != nil {
				return boundedInput{}, decodeErr
			}
			return boundedInput{Payload: raw.Bytes(), Value: publishMutationPrepared{ResumeID: id, Input: input}}, nil
		},
		CanonicalTargets: func(input boundedInput) ([]string, error) {
			value, ok := input.Value.(publishMutationPrepared)
			if !ok {
				return nil, internalClientError()
			}
			return []string{"resume_id", value.ResumeID.String()}, nil
		},
		Prepare: func(_ context.Context, input boundedInput, _ idempotencyInspection) (preparedInput, error) {
			return preparedInput{Input: input, Value: input.Value}, nil
		},
		Run:        publishOperation{service: s},
		Transition: s.publishTransition,
	})
}

func (s *Service) publishTransition(ctx context.Context, current resume.Resume, prepared preparedInput) (mutationTransition, error) {
	input, ok := prepared.Value.(publishMutationPrepared)
	if !ok || input.ResumeID != current.ID {
		return mutationTransition{}, errors.New("resumeapi: publish mutation has no resume target")
	}
	before := currentPublish{Slug: current.Slug, Live: current.Live, DownloadEnabled: current.DownloadEnabled, SEOGeoEnabled: current.SEOGeoEnabled, Revision: current.Revision}
	next := validatePublish(current.Doc, before, input.Input)
	if len(next.Issues) != 0 {
		return mutationTransition{}, publishInvalidError(next.Issues)
	}
	if session, ok := auth.SessionFromContext(ctx); !ok {
		return mutationTransition{}, &clientError{Status: http.StatusUnauthorized, Code: "session_required", Message: "a valid session is required"}
	} else if publishRequiresRecentReauth(before, next) {
		if err := auth.RequireRecentReauth(session, s.clock()); err != nil {
			return mutationTransition{}, err
		}
	}
	if !admitChangedSlugAttempt(s.slugAttempts, current.UserID, s.clock(), next) {
		return mutationTransition{}, &clientError{Status: http.StatusTooManyRequests, Code: "rate_limited", Message: "too many slug attempts", Headers: map[string]string{"Retry-After": "1"}}
	}
	if err := s.preflightPublishSlugAvailability(ctx, current.ID, next); err != nil {
		return mutationTransition{}, err
	}
	discovery := publishChangesDiscovery(before, next.Effective)
	revoking := (next.ChangedSlug && before.Slug != nil) || (before.Live && !next.Effective.Live) || (before.SEOGeoEnabled && !next.Effective.SEOGeoEnabled) || (before.DownloadEnabled && !next.Effective.DownloadEnabled)
	class := publicstate.NonDraining
	if revoking {
		class = publicstate.Revoking
	}
	descriptor := mutationTransition{ResumeID: input.ResumeID, Class: class, Global: discovery, Slugs: sortedPublishSlugs(before.Slug, next.Effective.Slug), Publish: &publishRecoveryProof{ResumeID: input.ResumeID, Effective: currentPublish{Slug: next.Effective.Slug, Live: next.Effective.Live, DownloadEnabled: next.Effective.DownloadEnabled, SEOGeoEnabled: next.Effective.SEOGeoEnabled, Revision: current.Revision + 1}}}
	if next.ChangedSlug && before.Slug != nil {
		releasedAt := normalizePostgresTimestamp(s.clock())
		oldSlug := *before.Slug
		descriptor.Publish.OldSlug, descriptor.Publish.ReleasedAt = &oldSlug, releasedAt
	}
	return descriptor, nil
}

func (s *Service) preflightPublishSlugAvailability(ctx context.Context, resumeID uuid.UUID, prepared publishPrepared) error {
	if !prepared.ChangedSlug || prepared.Effective.Slug == nil {
		return nil
	}
	if s.recoveryPool == nil {
		return errors.New("resumeapi: publish slug preflight pool is unavailable")
	}
	q := store.New(s.recoveryPool)
	claimed, claimErr := q.GetSlugClaim(ctx, *prepared.Effective.Slug)
	s.recordPublishPreflightOrder("claim")
	if claimErr == nil && claimed != resumeID {
		return slugTakenError()
	}
	if claimErr != nil && !errors.Is(claimErr, pgx.ErrNoRows) {
		return fmt.Errorf("resumeapi: publish slug preflight claim: %w", claimErr)
	}
	tombstone, tombstoneErr := q.GetSlugTombstoneForUpdate(ctx, *prepared.Effective.Slug)
	s.recordPublishPreflightOrder("tombstone")
	if tombstoneErr == nil {
		reusableAt := tombstone.ReleasedAt.Add(180 * 24 * time.Hour)
		if s.clock().Before(reusableAt) {
			return slugTakenError()
		}
	} else if !errors.Is(tombstoneErr, pgx.ErrNoRows) {
		return fmt.Errorf("resumeapi: publish slug preflight tombstone: %w", tombstoneErr)
	}
	return nil
}

func publishInvalidError(issues []publishIssue) *clientError {
	data := make([]map[string]string, len(issues))
	for i, issue := range issues {
		data[i] = map[string]string{"path": issue.Path, "code": issue.Code, "message": issue.Message}
	}
	return &clientError{Status: http.StatusUnprocessableEntity, Code: "publish_invalid", Message: "resume cannot be published", Details: map[string]any{"issues": data}}
}

func slugTakenError() *clientError {
	return &clientError{Status: http.StatusConflict, Code: "slug_taken", Message: "slug is unavailable"}
}

func publishChangesDiscovery(before, after currentPublish) bool {
	return before.Slug == nil != (after.Slug == nil) || (before.Slug != nil && after.Slug != nil && *before.Slug != *after.Slug) || before.Live != after.Live || before.SEOGeoEnabled != after.SEOGeoEnabled
}

func sortedPublishSlugs(old, next *string) []string {
	values := make([]string, 0, 2)
	if old != nil {
		values = append(values, *old)
	}
	if next != nil && (old == nil || *old != *next) {
		values = append(values, *next)
	}
	sort.Strings(values)
	return values
}

type resumePoolReader interface {
	Get(context.Context, uuid.UUID, uuid.UUID) (resume.Resume, error)
	List(context.Context, uuid.UUID) ([]resume.Resume, error)
}

type resumeMediaDeletionQueue interface {
	EnqueueMediaDeletionTx(context.Context, *store.Queries, uuid.UUID, string) error
}

type resumeSummaryJSON struct {
	ID              uuid.UUID `json:"id"`
	Title           string    `json:"title"`
	Lng             string    `json:"lng"`
	Revision        string    `json:"revision"`
	Live            bool      `json:"live"`
	Slug            *string   `json:"slug"`
	DownloadEnabled bool      `json:"downloadEnabled"`
	SEOGeoEnabled   bool      `json:"seoGeoEnabled"`
	SchemaVersion   int32     `json:"schemaVersion"`
	CreatedAt       time.Time `json:"createdAt"`
	UpdatedAt       time.Time `json:"updatedAt"`
}

type resumeJSON struct {
	resumeSummaryJSON
	Document json.RawMessage `json:"document"`
}

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

func (s *Service) handleListResumes(w http.ResponseWriter, r *http.Request) {
	version, versionErr := resolveWireVersion(r.Header, s.acceptedVersions)
	if versionErr != nil {
		writeResumeError(w, versionErr)
		return
	}
	session, ok := auth.SessionFromContext(r.Context())
	if !ok {
		writeResumeError(w, &clientError{Status: http.StatusUnauthorized, Code: "session_required", Message: "a valid session is required"})
		return
	}
	reader, ok := s.resumes.(resumePoolReader)
	if !ok {
		writeResumeError(w, internalClientError())
		return
	}
	rows, err := reader.List(r.Context(), session.UserID)
	if err != nil {
		writeResumeError(w, mapMutationError(err))
		return
	}
	data := make([]resumeSummaryJSON, len(rows))
	for i := range rows {
		data[i] = makeResumeSummary(rows[i], version)
	}
	body, err := json.Marshal(struct {
		Data []resumeSummaryJSON `json:"data"`
	}{Data: data})
	if err != nil {
		writeResumeError(w, internalClientError())
		return
	}
	writeStoredResponse(w, resume.StoredResponse{
		Status: http.StatusOK, Body: body,
		Headers: map[string]string{wireVersionHeader: wireVersionString(version)},
	})
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

func (s *Service) handleGetResume(w http.ResponseWriter, r *http.Request) {
	version, versionErr := resolveWireVersion(r.Header, s.acceptedVersions)
	if versionErr != nil {
		writeResumeError(w, versionErr)
		return
	}
	id, err := parseResumePathID(r)
	if err != nil {
		writeResumeError(w, err)
		return
	}
	session, ok := auth.SessionFromContext(r.Context())
	if !ok {
		writeResumeError(w, &clientError{Status: http.StatusUnauthorized, Code: "session_required", Message: "a valid session is required"})
		return
	}
	reader, ok := s.resumes.(resumePoolReader)
	if !ok {
		writeResumeError(w, internalClientError())
		return
	}
	row, getErr := reader.Get(r.Context(), session.UserID, id)
	if getErr != nil {
		writeResumeError(w, mapMutationError(getErr))
		return
	}
	response, responseErr := s.makeResumeResponse(row, row.Doc, version, http.StatusOK, false)
	if responseErr != nil {
		writeResumeError(w, internalClientError())
		return
	}
	writeStoredResponse(w, response)
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

func parseResumePathID(r *http.Request) (uuid.UUID, *clientError) {
	raw := r.PathValue("id")
	id, err := uuid.Parse(raw)
	if err != nil || id == uuid.Nil || id.String() != raw {
		return uuid.Nil, &clientError{Status: http.StatusBadRequest, Code: "request_invalid", Message: "resume id must be one canonical UUID"}
	}
	return id, nil
}

func decodeResumeTitle(raw json.RawMessage, required bool) (string, error) {
	if len(raw) == 0 {
		if required {
			return "", documentInvalid("title", "title is required")
		}
		return "", nil
	}
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return "", documentInvalid("title", "title must be a string")
	}
	var title string
	if err := json.Unmarshal(raw, &title); err != nil {
		return "", documentInvalid("title", "title must be a string")
	}
	if utf8.RuneCountInString(title) > resume.MaxTitleCharacters {
		return "", documentInvalid("title", "title exceeds 160 code points")
	}
	return title, nil
}

func decodeResumeLanguage(raw json.RawMessage) (*string, error) {
	if len(raw) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return nil, nil
	}
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return nil, documentInvalid("lng", "language must be a string or null")
	}
	if value == "" {
		return nil, nil
	}
	tag, err := language.Parse(value)
	if err != nil {
		return nil, documentInvalid("lng", "language must be a valid BCP 47 tag")
	}
	canonical := tag.String()
	if utf8.RuneCountInString(canonical) > resume.MaxLngCharacters {
		return nil, documentInvalid("lng", "canonical language tag exceeds 35 code points")
	}
	return &canonical, nil
}

func projectResumeLanguage(value *string) string {
	if value == nil || *value == "" {
		return language.Und.String()
	}
	tag, err := language.Parse(*value)
	if err != nil {
		return language.Und.String()
	}
	canonical := tag.String()
	if utf8.RuneCountInString(canonical) > resume.MaxLngCharacters {
		return language.Und.String()
	}
	return canonical
}

func documentInvalid(path, message string) *clientError {
	return &clientError{
		Status: http.StatusUnprocessableEntity, Code: "document_invalid", Message: "resume document is invalid",
		Details: map[string]any{"issues": []map[string]string{{"path": path, "code": "invalid", "message": message}}},
	}
}

func seedCarriesPhoto(raw json.RawMessage) bool {
	var root map[string]json.RawMessage
	if json.Unmarshal(raw, &root) != nil {
		return false
	}
	var personal map[string]json.RawMessage
	if json.Unmarshal(root["personalDetails"], &personal) != nil {
		return false
	}
	_, present := personal["photo"]
	return present
}

func defaultResumeDocument() schema.Resume {
	return schema.Resume{
		SchemaVersion:   int64(docmigrate.CurrentVersion),
		PersonalDetails: schema.PersonalDetails{Details: []schema.PersonalDetail{}},
		Content:         map[string]schema.Section{},
		Customization: schema.Customization{
			Font:    schema.Font{Family: schema.Inter, BaseSizePx: 14},
			Colors:  schema.Colors{Primary: "#1a1a1a", Text: "#1a1a1a", Background: "#ffffff"},
			Spacing: schema.Spacing{SectionGap: 16, EntryGap: 8, LineHeight: 1.4},
			Heading: schema.Heading{Style: schema.Normal, ShowRule: false},
			Layout:  schema.Layout{Columns: 1, Sections: schema.Sections{Main: []string{}, Sidebar: []string{}}},
			SectionDisplay: schema.SectionDisplay{
				Skill: schema.SkillClass{Style: schema.Text}, Language: schema.LanguageClass{Style: schema.Text},
			},
			PageFormat: schema.A4, DateFormat: schema.MmYyyy,
		},
	}
}

func makeResumeSummary(row resume.Resume, version int32) resumeSummaryJSON {
	return resumeSummaryJSON{
		ID: row.ID, Title: row.Title, Lng: projectResumeLanguage(row.Lng),
		Revision: strconv.FormatInt(row.Revision, 10), Live: row.Live, Slug: row.Slug,
		DownloadEnabled: row.DownloadEnabled, SEOGeoEnabled: row.SEOGeoEnabled,
		SchemaVersion: version, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
	}
}

func (s *Service) resumeResponseBuilder(status int, location bool) mutationResponseBuilder {
	return func(row resume.Resume, doc schema.Resume, version int32) (resume.StoredResponse, error) {
		return s.makeResumeResponse(row, doc, version, status, location)
	}
}

func (s *Service) makeResumeResponse(row resume.Resume, doc schema.Resume, version int32, status int,
	location bool,
) (resume.StoredResponse, error) {
	if s.projector == nil {
		return resume.StoredResponse{}, fmt.Errorf("resumeapi: no document projector")
	}
	canonical, err := resume.AssembleCanonical(doc)
	if err != nil {
		return resume.StoredResponse{}, err
	}
	wire, err := s.projector.EmitWire(canonical, version)
	if err != nil {
		return resume.StoredResponse{}, err
	}
	body, err := json.Marshal(struct {
		Data resumeJSON `json:"data"`
	}{Data: resumeJSON{resumeSummaryJSON: makeResumeSummary(row, version), Document: wire}})
	if err != nil {
		return resume.StoredResponse{}, err
	}
	headers := map[string]string{
		"ETag": fmt.Sprintf(`"r%d"`, row.Revision), wireVersionHeader: wireVersionString(version),
	}
	if location {
		headers["Location"] = apiResumePath + "/" + row.ID.String()
	}
	return resume.StoredResponse{Status: status, Body: body, Headers: headers}, nil
}
