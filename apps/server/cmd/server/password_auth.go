package main

// Password-authentication composition (Phase PA T09). This file builds the
// blocklist/policy/hasher, the mail key ring and outbox, the mode-selected
// sender (SES or loopback capture), the rate policies, the password service,
// and the leased mail worker — then run() wires the routes and joins the worker
// on shutdown. No AWS client is ever constructed in capture mode.

import (
	"context"
	"crypto/rand"
	"fmt"
	"log/slog"
	mathrand "math/rand/v2"
	"time"

	"github.com/google/uuid"

	"github.com/dannyota/aboutme/apps/server/internal/api"
	"github.com/dannyota/aboutme/apps/server/internal/auth"
	"github.com/dannyota/aboutme/apps/server/internal/authmail"
	"github.com/dannyota/aboutme/apps/server/internal/config"
	"github.com/dannyota/aboutme/apps/server/internal/mailcapture"
	"github.com/dannyota/aboutme/apps/server/internal/password"
	"github.com/dannyota/aboutme/apps/server/internal/store"
)

// passwordAuthRuntime is the fully-composed password service and its mail
// worker.
type passwordAuthRuntime struct {
	service *auth.PasswordService
	worker  *authmail.Worker
}

// newPasswordAuth builds every password-mail dependency and returns the service
// plus worker. It fails closed on any construction error so a missing or
// misconfigured dependency refuses to start rather than degrading silently.
func newPasswordAuth(ctx context.Context, logger *slog.Logger, cfg config.Config, pool *store.Pool, queries *store.Queries) (*passwordAuthRuntime, error) {
	blocklist, err := password.LoadBlocklist()
	if err != nil {
		return nil, fmt.Errorf("load password blocklist: %w", err)
	}
	policy := password.NewPolicy(blocklist, password.NewHIBP(nil))
	hasher, err := password.NewHasher(password.DefaultHashPolicy(), rand.Reader, password.NewAdmission())
	if err != nil {
		return nil, fmt.Errorf("create password hasher: %w", err)
	}

	ring, err := newMailKeyRing(cfg)
	if err != nil {
		return nil, err
	}
	// Startup readiness: every key ID referenced by a live (pending/leased)
	// job must be present in the ring, or the worker cannot decrypt it later.
	liveKeyIDs, err := queries.ListLiveAuthEmailJobKeyIDs(ctx, time.Now())
	if err != nil {
		return nil, fmt.Errorf("list live mail key ids: %w", err)
	}
	knownKeys := map[string]bool{cfg.AuthEmail.ActiveKeyID: true}
	if cfg.AuthEmail.HasPrevious {
		knownKeys[cfg.AuthEmail.PreviousKeyID] = true
	}
	for _, id := range liveKeyIDs {
		if !knownKeys[id] {
			return nil, fmt.Errorf("mail key ring is missing key %q referenced by a live job", id)
		}
	}
	outbox, err := authmail.NewOutbox(ring, time.Now)
	if err != nil {
		return nil, fmt.Errorf("create mail outbox: %w", err)
	}

	sender, err := newMailSender(ctx, cfg, logger)
	if err != nil {
		return nil, err
	}

	limits, err := auth.NewPasswordRatePolicies(cfg.AuthEmail.RateHMACKey)
	if err != nil {
		return nil, fmt.Errorf("create password rate policies: %w", err)
	}

	service, err := auth.NewPasswordService(auth.PasswordServiceOptions{
		Pool:           pool,
		Queries:        queries,
		Sessions:       auth.NewSessionManagerWithPool(pool),
		Policy:         policy,
		Hasher:         hasher,
		Outbox:         outbox,
		Limits:         limits,
		PublicOrigin:   cfg.PublicOrigin,
		Clock:          time.Now,
		Entropy:        rand.Reader,
		Logger:         logger,
		TrustedProxies: api.TrustedProxies(cfg.TrustedProxyCIDRs),
	})
	if err != nil {
		return nil, fmt.Errorf("create password service: %w", err)
	}

	worker, err := authmail.NewWorker(authmail.WorkerOptions{
		Pool:     pool,
		Queries:  queries,
		KeyRing:  ring,
		Sender:   sender,
		Clock:    time.Now,
		Jitter:   fullJitter,
		Logger:   logger,
		WorkerID: uuid.New(),
	})
	if err != nil {
		return nil, fmt.Errorf("create mail worker: %w", err)
	}

	return &passwordAuthRuntime{service: service, worker: worker}, nil
}

// newMailKeyRing builds the D3 key ring from config: exactly one active key plus
// at most one previous key.
func newMailKeyRing(cfg config.Config) (*authmail.KeyRing, error) {
	keys := map[string][32]byte{cfg.AuthEmail.ActiveKeyID: cfg.AuthEmail.ActiveKey}
	if cfg.AuthEmail.HasPrevious {
		keys[cfg.AuthEmail.PreviousKeyID] = cfg.AuthEmail.PreviousKey
	}
	ring, err := authmail.NewKeyRing(cfg.AuthEmail.ActiveKeyID, keys, rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("create mail key ring: %w", err)
	}
	return ring, nil
}

// newMailSender selects the capture or SES sender. Capture mode constructs no
// AWS client; SES mode uses the standard credential chain and never reads a
// credential env field.
func newMailSender(ctx context.Context, cfg config.Config, logger *slog.Logger) (authmail.Sender, error) {
	switch cfg.AuthEmail.Mode {
	case authmailModeCaptureValue:
		sender, err := mailcapture.NewClient(cfg.AuthEmail.CaptureURL, cfg.AuthEmail.CaptureBearer[:], logger)
		if err != nil {
			return nil, fmt.Errorf("create capture sender: %w", err)
		}
		return sender, nil
	case authmailModeSESValue:
		client, err := authmail.NewSESClient(ctx, cfg.AuthEmail.SESRegion)
		if err != nil {
			return nil, fmt.Errorf("create SES client: %w", err)
		}
		sender, err := authmail.NewSESSender(authmail.SESOptions{
			Region:           cfg.AuthEmail.SESRegion,
			From:             cfg.AuthEmail.SESFrom,
			ConfigurationSet: cfg.AuthEmail.SESConfigSet,
			Client:           client,
			Logger:           logger,
		})
		if err != nil {
			return nil, fmt.Errorf("create SES sender: %w", err)
		}
		return sender, nil
	default:
		return nil, fmt.Errorf("unsupported auth email mode %q", cfg.AuthEmail.Mode)
	}
}

// The config package owns the mode strings; these mirrors keep main.go from
// spelling them a second time while the mode stays a closed two-value set.
const (
	authmailModeCaptureValue = "capture"
	authmailModeSESValue     = "ses"
)

// fullJitter returns a uniform random duration in [0, d]. The worker owns retry
// timing, so the jitter source is injected here rather than inside the worker.
func fullJitter(d time.Duration) time.Duration {
	if d <= 0 {
		return 0
	}
	return time.Duration(mathrand.Int64N(int64(d) + 1)) //nolint:gosec // bounded retry backoff, not secret material
}
