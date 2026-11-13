package app

import (
	"crypto/rand"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"time"

	"danny.vn/aboutme/apps/server/internal/api"
	"danny.vn/aboutme/apps/server/internal/auth"
	"danny.vn/aboutme/apps/server/internal/authmail"
	"danny.vn/aboutme/apps/server/internal/config"
	"danny.vn/aboutme/apps/server/internal/secondfactor"
	"danny.vn/aboutme/apps/server/internal/store"
)

type SecondFactor struct {
	RegisterRoutes func(*http.ServeMux)
	TOTPHealth     *secondfactor.TOTPHealthChecker
}

func NewSecondFactor(logger *slog.Logger, cfg config.Config, pool *store.Pool) (SecondFactor, error) {
	rp, err := secondfactor.NewRelyingParty(cfg.PublicOrigin)
	if err != nil {
		return SecondFactor{}, fmt.Errorf("create passkey relying party: %w", err)
	}
	ring, err := newMailKeyRing(cfg)
	if err != nil {
		return SecondFactor{}, err
	}
	outbox, err := authmail.NewOutbox(ring, time.Now)
	if err != nil {
		return SecondFactor{}, fmt.Errorf("create second factor mail outbox: %w", err)
	}
	pending := auth.NewPendingAuthenticationManager(pool, logger)
	sessions := auth.NewSessionManagerWithPool(pool)

	totpRing, err := secondfactor.NewTOTPKeyRing(cfg.TOTPActiveKey, cfg.TOTPPreviousKey, rand.Reader)
	if err != nil {
		return SecondFactor{}, fmt.Errorf("create totp key ring: %w", err)
	}
	totpIssuer, err := totpIssuerFromPublicOrigin(cfg.PublicOrigin)
	if err != nil {
		return SecondFactor{}, fmt.Errorf("derive totp issuer: %w", err)
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
		return SecondFactor{}, fmt.Errorf("create second factor service: %w", err)
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
		return SecondFactor{}, fmt.Errorf("create second factor handlers: %w", err)
	}

	health, err := secondfactor.NewTOTPHealthChecker(secondfactor.TOTPHealthConfig{
		Pool: pool, Ring: totpRing, Now: time.Now, Signal: totpSignal,
	})
	if err != nil {
		return SecondFactor{}, fmt.Errorf("create totp health checker: %w", err)
	}
	return SecondFactor{RegisterRoutes: h.RegisterRoutes, TOTPHealth: health}, nil
}

func totpIssuerFromPublicOrigin(publicOrigin string) (string, error) {
	u, err := url.Parse(publicOrigin)
	if err != nil || u.Host == "" {
		return "", errors.New("parse public origin for totp issuer")
	}
	return u.Hostname(), nil
}
