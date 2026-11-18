package githubevidence

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"

	"github.com/jackc/pgx/v5"

	"danny.vn/aboutme/apps/server/internal/project"
	"danny.vn/aboutme/apps/server/internal/store"
)

const candidateIDBytes = 16

type evidenceSession struct {
	reader       *githubReader
	githubUserID int64
	login        string
	generation   int64
	epoch        int64
}

func (a account) connected() bool {
	row := a.connection
	return a.found && row.GithubUserID != nil && row.Login != nil && row.ConnectedAt != nil
}

func (s *Service) currentSession(a account) (*liveSession, bool) {
	live, isLive := s.registry.current(a.user.ID)
	if !isLive {
		return nil, false
	}
	if a.connected() && live.generation == a.connection.Generation && live.epoch == a.user.AuthEpoch && live.expiresAt.After(a.now) {
		return live, true
	}
	s.registry.endSession(a.user.ID, live)
	return nil, false
}

func (s *Service) openReader(ctx context.Context, sess store.Session) (evidenceSession, error) {
	if !s.enabled {
		return evidenceSession{}, project.ErrEvidenceUnavailable
	}
	var opened evidenceSession
	err := s.inAccount(ctx, sess, func(_ context.Context, a account) error {
		if live, isLive := s.currentSession(a); isLive {
			opened = evidenceSession{
				reader:       &githubReader{client: s.client, registry: s.registry, account: a.user.ID, session: live},
				githubUserID: *a.connection.GithubUserID, login: *a.connection.Login,
				generation: a.connection.Generation, epoch: a.user.AuthEpoch,
			}
		}
		return nil
	})
	if err != nil {
		return evidenceSession{}, err
	}
	if opened.reader == nil {
		return evidenceSession{}, project.ErrEvidenceSessionRequired
	}
	return opened, nil
}

func (s *Service) confirm(opened evidenceSession) func(context.Context, *store.Queries) (int64, error) {
	return func(ctx context.Context, q *store.Queries) (int64, error) {
		user, err := q.GetUserForUpdate(ctx, opened.reader.account)
		if err != nil {
			return 0, fmt.Errorf("githubevidence: lock account: %w", err)
		}
		connection, err := q.GetGitHubConnectionForUpdate(ctx, user.ID)
		if errors.Is(err, pgx.ErrNoRows) {
			return 0, project.ErrEvidenceSessionRequired
		}
		if err != nil {
			return 0, fmt.Errorf("githubevidence: lock connection: %w", err)
		}
		live, isLive := s.registry.current(user.ID)
		if !isLive || live != opened.reader.session || !live.expiresAt.After(s.now()) || user.AuthEpoch != opened.epoch ||
			connection.Generation != opened.generation || connection.GithubUserID == nil || *connection.GithubUserID != opened.githubUserID {
			return 0, project.ErrEvidenceSessionRequired
		}
		return connection.Binding, nil
	}
}

func (s *Service) candidateIDs(n int) ([]string, error) {
	ids := make([]string, n)
	raw := make([]byte, candidateIDBytes)
	for i := range ids {
		if _, err := io.ReadFull(s.entropy, raw); err != nil {
			return nil, fmt.Errorf("githubevidence: candidate id: %w", err)
		}
		ids[i] = base64.RawURLEncoding.EncodeToString(raw)
	}
	return ids, nil
}

func readError(err error) error {
	if errors.Is(err, errPageRange) {
		return project.ErrEvidenceRequest
	}
	failure := evidenceFailure(err)
	switch failure.code {
	case rateLimitedCode:
		return &project.EvidenceRateLimitError{Retry: failure.retry}
	case sessionRequiredCode:
		return project.ErrEvidenceSessionRequired
	case notConfirmedCode:
		return project.ErrEvidenceNotConfirmed
	case unsupportedLinkCode:
		return project.ErrEvidenceUnsupportedLink
	default:
		return project.ErrEvidenceUpstream
	}
}
