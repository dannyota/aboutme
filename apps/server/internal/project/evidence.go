package project

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"danny.vn/aboutme/apps/server/internal/store"
)

const (
	MaxEvidence       = 10
	EvidenceItemBytes = 1024

	EvidenceRepository  = "repository"
	EvidencePullRequest = "pullRequest"
	EvidenceRelease     = "release"

	FlagRuleFailed = "rule_failed"
	FlagGone       = "gone"

	MaxEvidenceURLCharacters = 300
	evidenceURLPrefix        = "https://github.com/"
)

var (
	ErrEvidenceUnavailable     = errors.New("project evidence is unavailable")
	ErrEvidenceSessionRequired = errors.New("project evidence needs a live GitHub session")
	ErrEvidenceNotConfirmed    = errors.New("GitHub does not confirm the evidence")
	ErrEvidenceDuplicate       = errors.New("project evidence is already attached")
	ErrEvidenceLimit           = errors.New("project evidence limit reached")
	ErrEvidenceUnsupportedLink = errors.New("project evidence link is unsupported")
	ErrEvidenceUpstream        = errors.New("GitHub did not respond as expected")
	ErrEvidenceNotFound        = errors.New("project evidence not found")
	ErrEvidenceRequest         = errors.New("project evidence request is invalid")
)

type EvidenceRateLimitError struct {
	Retry time.Duration
}

func (e *EvidenceRateLimitError) Error() string { return "project evidence reads are rate limited" }

type Evidence struct {
	ID             uuid.UUID `json:"id"`
	Kind           string    `json:"kind"`
	URL            string    `json:"url"`
	RepositoryName string    `json:"repositoryName"`
	Title          string    `json:"title"`
	ItemDate       time.Time `json:"itemDate"`
	CheckedAt      time.Time `json:"checkedAt"`
	Confirmed      bool      `json:"confirmed"`
	FlagReason     *string   `json:"flagReason"`
}

func ValidEvidenceKind(kind string) bool {
	return kind == EvidenceRepository || kind == EvidencePullRequest || kind == EvidenceRelease
}

var (
	evidenceRepositoryURL  = regexp.MustCompile(`^https://github\.com/[^/?#[:space:]]+/[^/?#[:space:]]+$`)
	evidencePullRequestURL = regexp.MustCompile(`^https://github\.com/[^/?#[:space:]]+/[^/?#[:space:]]+/pull/[1-9][0-9]*$`)
	evidenceReleaseURL     = regexp.MustCompile(`^https://github\.com/[^/?#[:space:]]+/[^/?#[:space:]]+/releases/tag/[^/?#[:space:]]+$`)
)

func ValidEvidenceURL(kind, url string) bool {
	var grammar *regexp.Regexp
	switch kind {
	case EvidenceRepository:
		grammar = evidenceRepositoryURL
	case EvidencePullRequest:
		grammar = evidencePullRequestURL
	case EvidenceRelease:
		grammar = evidenceReleaseURL
	default:
		return false
	}
	return utf8.ValidString(url) && utf8.RuneCountInString(url) <= MaxEvidenceURLCharacters &&
		strings.HasPrefix(url, evidenceURLPrefix) && grammar.MatchString(url)
}

func evidenceOf(row store.ListEvidenceForProjectsRow) Evidence {
	return Evidence{
		ID: row.ID, Kind: row.Kind, URL: row.Url, RepositoryName: row.RepositoryName, Title: row.Title,
		ItemDate: row.ItemDate.UTC(), CheckedAt: row.CheckedAt.UTC(), Confirmed: row.Confirmed, FlagReason: row.FlagReason,
	}
}

func (s *Store) EvidenceTx(ctx context.Context, q *store.Queries, owner, id uuid.UUID) ([]Evidence, error) {
	rows, err := q.ListProjectEvidence(ctx, store.ListProjectEvidenceParams{UserID: owner, ProjectID: id})
	if err != nil {
		return nil, fmt.Errorf("project: list evidence: %w", err)
	}
	out := make([]Evidence, len(rows))
	for i, row := range rows {
		out[i] = evidenceOf(store.ListEvidenceForProjectsRow(row))
	}
	return out, nil
}

func (s *Store) EvidenceByProjectTx(ctx context.Context, q *store.Queries, owner uuid.UUID, ids []uuid.UUID) (map[uuid.UUID][]Evidence, error) {
	rows, err := q.ListEvidenceForProjects(ctx, store.ListEvidenceForProjectsParams{UserID: owner, ProjectIds: ids})
	if err != nil {
		return nil, fmt.Errorf("project: list evidence: %w", err)
	}
	out := make(map[uuid.UUID][]Evidence, len(ids))
	for _, id := range ids {
		out[id] = []Evidence{}
	}
	for _, row := range rows {
		out[row.ProjectID] = append(out[row.ProjectID], evidenceOf(row))
	}
	return out, nil
}

func (s *Store) EvidenceRoomTx(ctx context.Context, q *store.Queries, owner, id uuid.UUID, items int) error {
	n, err := q.ProjectStoredDocumentBytes(ctx, store.ProjectStoredDocumentBytesParams{ID: id, UserID: owner})
	if err != nil {
		return fmt.Errorf("project: measure stored document: %w", err)
	}
	if int(n)+items*EvidenceItemBytes > MaxStoredBytes {
		return ErrInvalid
	}
	return nil
}

func evidenceBytes(ctx context.Context, q *store.Queries, id uuid.UUID) (int, error) {
	if id == uuid.Nil {
		return 0, nil
	}
	n, err := q.CountProjectEvidence(ctx, id)
	if err != nil {
		return 0, fmt.Errorf("project: count evidence: %w", err)
	}
	return int(n) * EvidenceItemBytes, nil
}
