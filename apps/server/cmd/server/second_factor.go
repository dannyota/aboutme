package main

// Second-factor composition: the WebAuthn relying party, the TOTP key ring
// and health checker, the secondfactor.Service, and the HTTP handlers that
// register every pending, state, passkey, recovery, and TOTP route. See
// docs/design/passkey-second-factor-contract.md,
// docs/design/totp-second-factor-contract.md,
// docs/design/totp-key-management.md, and
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

// secondFactorRuntime is newSecondFactorRoutes's result: the route registrar
// every caller already used, plus the TOTP key-health checker the caller
// must run at startup and on a five-minute ticker
// (docs/design/totp-key-management.md "Key failures").
type secondFactorRuntime struct {
	RegisterRoutes func(*http.ServeMux)
	TOTPHealth     *secondfactor.TOTPHealthChecker
}

// newSecondFactorRoutes builds the relying party, the TOTP key ring and
// unavailable signal, the factor service, the TOTP health checker, and the
// handlers, and returns a registrar that attaches every second-factor route
// unconditionally: registration options and completion recheck
// cfg.PasskeyEnrollment, and TOTP enrollment start and completion recheck
// cfg.TOTPEnrollment, themselves
// (auth.SecondFactorHandlers.RegisterRoutes), while assertion, recovery,
// removal, regeneration, state, and TOTP verification and removal routes
// ignore their respective flags, per the contract. Building the relying
// party from cfg.PublicOrigin and the TOTP key ring from cfg.TOTPActiveKey
// always run, so an origin that cannot host WebAuthn ceremonies or a
// malformed key ring fails startup closed the same way any other required
// dependency does; see docs/design/second-factor-authentication.md,
// "Passkeys", docs/design/totp-key-management.md, "Key ring", and
// internal/config/passkey.go and internal/config/totp.go for the narrower
// checks that give an operator a clearer, earlier error.
func newSecondFactorRoutes(logger *slog.Logger, cfg config.Config, pool *store.Pool) (secondFactorRuntime, error) {
	rp, err := secondfactor.NewRelyingParty(cfg.PublicOrigin)
	if err != nil {
		return secondFactorRuntime{}, fmt.Errorf("create passkey relying party: %w", err)
	}
	ring, err := newMailKeyRing(cfg)
	if err != nil {
		return secondFactorRuntime{}, err
	}
	outbox, err := authmail.NewOutbox(ring, time.Now)
	if err != nil {
		return secondFactorRuntime{}, fmt.Errorf("create second factor mail outbox: %w", err)
	}
	pending := auth.NewPendingAuthenticationManager(pool, logger)
	sessions := auth.NewSessionManagerWithPool(pool)

	totpRing, err := secondfactor.NewTOTPKeyRing(cfg.TOTPActiveKey, cfg.TOTPPreviousKey, rand.Reader)
	if err != nil {
		return secondFactorRuntime{}, fmt.Errorf("create totp key ring: %w", err)
	}
	totpIssuer, err := totpIssuerFromPublicOrigin(cfg.PublicOrigin)
	if err != nil {
		return secondFactorRuntime{}, fmt.Errorf("derive totp issuer: %w", err)
	}
	totpSignal := secondfactor.NewTOTPUnavailableSignal(time.Now, logger)

	svc, err := secondfactor.New(secondfactor.Options{
		Pool:                  pool,
		Pending:               pending,
		Sessions:              sessions,
		Outbox:                outbox,
		RelyingParty:          rp,
		EnrollmentEnabled:     cfg.PasskeyEnrollment,
		TOTPKeyRing:           totpRing,
		TOTPEnrollmentEnabled: cfg.TOTPEnrollment,
		TOTPIssuer:            totpIssuer,
		TOTPSignal:            totpSignal,
		Clock:                 time.Now,
		Entropy:               rand.Reader,
		Logger:                logger,
	})
	if err != nil {
		return secondFactorRuntime{}, fmt.Errorf("create second factor service: %w", err)
	}

	h, err := auth.NewSecondFactorHandlers(auth.SecondFactorHandlerOptions{
		Service:        svc,
		TOTP:           svc,
		Pending:        pending,
		Sessions:       sessions,
		Limits:         auth.NewSecondFactorRatePolicies(),
		PublicOrigin:   cfg.PublicOrigin,
		TrustedProxies: api.TrustedProxies(cfg.TrustedProxyCIDRs),
		Clock:          time.Now,
		Logger:         logger,
	})
	if err != nil {
		return secondFactorRuntime{}, fmt.Errorf("create second factor handlers: %w", err)
	}

	health, err := secondfactor.NewTOTPHealthChecker(secondfactor.TOTPHealthConfig{
		Pool: pool, Ring: totpRing, Now: time.Now, Signal: totpSignal,
	})
	if err != nil {
		return secondFactorRuntime{}, fmt.Errorf("create totp health checker: %w", err)
	}
	return secondFactorRuntime{RegisterRoutes: h.RegisterRoutes, TOTPHealth: health}, nil
}
