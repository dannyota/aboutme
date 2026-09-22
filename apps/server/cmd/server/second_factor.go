package main

// Passkey second-factor composition: the WebAuthn relying party, the
// secondfactor.Service, and the HTTP handlers that register every v0.4.2
// pending, state, passkey, and recovery route. See
// docs/design/passkey-second-factor-contract.md and
// docs/design/second-factor-authentication.md.

import (
	"crypto/rand"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/dannyota/aboutme/apps/server/internal/api"
	"github.com/dannyota/aboutme/apps/server/internal/auth"
	"github.com/dannyota/aboutme/apps/server/internal/authmail"
	"github.com/dannyota/aboutme/apps/server/internal/config"
	"github.com/dannyota/aboutme/apps/server/internal/secondfactor"
	"github.com/dannyota/aboutme/apps/server/internal/store"
)

// newSecondFactorRoutes builds the relying party, the factor service, and the
// handlers, and returns a registrar that attaches every second-factor route
// unconditionally: registration options and completion recheck
// cfg.PasskeyEnrollment themselves (auth.SecondFactorHandlers.RegisterRoutes),
// while assertion, recovery, removal, regeneration, and state routes ignore
// it, per the contract. Building the relying party from cfg.PublicOrigin
// always runs, so an origin that cannot host WebAuthn ceremonies fails
// startup closed the same way any other required dependency does; see
// docs/design/second-factor-authentication.md, "Passkeys", and
// internal/config/passkey.go for the narrower flag-gated check that gives an
// operator who never enables enrollment an earlier, clearer error.
func newSecondFactorRoutes(logger *slog.Logger, cfg config.Config, pool *store.Pool) (func(*http.ServeMux), error) {
	rp, err := secondfactor.NewRelyingParty(cfg.PublicOrigin)
	if err != nil {
		return nil, fmt.Errorf("create passkey relying party: %w", err)
	}
	ring, err := newMailKeyRing(cfg)
	if err != nil {
		return nil, err
	}
	outbox, err := authmail.NewOutbox(ring, time.Now)
	if err != nil {
		return nil, fmt.Errorf("create second factor mail outbox: %w", err)
	}
	pending := auth.NewPendingAuthenticationManager(pool, logger)
	sessions := auth.NewSessionManagerWithPool(pool)

	svc, err := secondfactor.New(secondfactor.Options{
		Pool:              pool,
		Pending:           pending,
		Sessions:          sessions,
		Outbox:            outbox,
		RelyingParty:      rp,
		EnrollmentEnabled: cfg.PasskeyEnrollment,
		Clock:             time.Now,
		Entropy:           rand.Reader,
		Logger:            logger,
	})
	if err != nil {
		return nil, fmt.Errorf("create second factor service: %w", err)
	}

	h, err := auth.NewSecondFactorHandlers(auth.SecondFactorHandlerOptions{
		Service:        svc,
		Pending:        pending,
		Sessions:       sessions,
		Limits:         auth.NewSecondFactorRatePolicies(),
		PublicOrigin:   cfg.PublicOrigin,
		TrustedProxies: api.TrustedProxies(cfg.TrustedProxyCIDRs),
		Clock:          time.Now,
		Logger:         logger,
	})
	if err != nil {
		return nil, fmt.Errorf("create second factor handlers: %w", err)
	}
	return h.RegisterRoutes, nil
}
