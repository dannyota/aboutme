package app

import (
	"log/slog"
	"time"

	"danny.vn/aboutme/apps/server/internal/auth"
	"danny.vn/aboutme/apps/server/internal/config"
	"danny.vn/aboutme/apps/server/internal/githubevidence"
	"danny.vn/aboutme/apps/server/internal/store"
)

func NewGitHubEvidence(cfg config.Config, logger *slog.Logger, pool *store.Pool, sessions *auth.SessionManager) (*githubevidence.Service, error) {
	return githubevidence.New(githubevidence.Options{
		Config:       cfg.GitHubEvidence,
		PublicOrigin: cfg.PublicOrigin,
		Pool:         pool,
		Sessions:     sessions,
		Logger:       logger,
		Clock:        time.Now,
	})
}
