package resumeapi

// Resume publish: validating and committing a publish mutation, and the
// slug-tombstone and discovery-generation bookkeeping it requires.

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/dannyota/aboutme/apps/server/internal/auth"
	"github.com/dannyota/aboutme/apps/server/internal/publicstate"
	"github.com/dannyota/aboutme/apps/server/internal/resume"
	"github.com/dannyota/aboutme/apps/server/internal/store"
)

type publishMutationPrepared struct {
	ResumeID   uuid.UUID
	Input      publishInput
	ReleasedAt time.Time
}

type publishOperation struct{ service *Service }

// Run applies a validated publish mutation inside the idempotency transaction.
func (op publishOperation) Run(ctx context.Context, qtx *store.Queries, mutation mutationContext, prepared preparedInput) (mutationRunResult, error) {
	input, ok := prepared.Value.(publishMutationPrepared)
	if !ok || mutation.ExpectedRevision == nil {
		return mutationRunResult{}, errors.New("resumeapi: publish operation received the wrong prepared input")
	}
	current, err := op.service.currentMutationResume(ctx, qtx, mutation, input.ResumeID)
	if err != nil {
		return mutationRunResult{}, err
	}
	state := currentPublishOf(current)
	validated := validatePublish(current.Doc, state, input.Input)
	if len(validated.Issues) != 0 {
		return mutationRunResult{}, publishInvalidError(validated.Issues)
	}
	if publishRequiresRecentReauth(state, validated) {
		if reauthErr := auth.RequireRecentReauth(mutation.Session, op.service.clock()); reauthErr != nil {
			return mutationRunResult{}, reauthErr
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
		if _, tombstoneErr := qtx.InsertSlugTombstone(ctx, store.InsertSlugTombstoneParams{Slug: *state.Slug, ReleasedByUserID: &mutation.UserID, ReleasedAt: input.ReleasedAt}); tombstoneErr != nil {
			return mutationRunResult{}, tombstoneErr
		}
	}
	updated, err := qtx.PublishResumeCAS(ctx, store.PublishResumeCASParams{ID: current.ID, UserID: mutation.UserID, ExpectedRevision: *mutation.ExpectedRevision, Slug: validated.Effective.Slug, Live: validated.Effective.Live, DownloadEnabled: validated.Effective.DownloadEnabled, SEOGeoEnabled: validated.Effective.SEOGeoEnabled, PublicTitle: validated.Effective.PublicTitle, FaviconEmoji: validated.Effective.FaviconEmoji, UpdatedAt: op.service.clock()})
	if err != nil {
		return mutationRunResult{}, err
	}
	if publishChangesDiscovery(state, validated.Effective) {
		if _, generationErr := qtx.AdvanceDiscoveryGeneration(ctx); generationErr != nil {
			return mutationRunResult{}, generationErr
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
	before := currentPublishOf(current)
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
	descriptor := mutationTransition{ResumeID: input.ResumeID, Class: class, Global: discovery, Slugs: sortedPublishSlugs(before.Slug, next.Effective.Slug), Publish: &publishRecoveryProof{ResumeID: input.ResumeID, Effective: currentPublish{Slug: next.Effective.Slug, Live: next.Effective.Live, DownloadEnabled: next.Effective.DownloadEnabled, SEOGeoEnabled: next.Effective.SEOGeoEnabled, PublicTitle: next.Effective.PublicTitle, FaviconEmoji: next.Effective.FaviconEmoji, Revision: current.Revision + 1}}}
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

func currentPublishOf(current resume.Resume) currentPublish {
	return currentPublish{
		Slug: current.Slug, Live: current.Live, DownloadEnabled: current.DownloadEnabled,
		SEOGeoEnabled: current.SEOGeoEnabled, PublicTitle: current.PublicTitle,
		FaviconEmoji: current.FaviconEmoji, Revision: current.Revision,
	}
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
