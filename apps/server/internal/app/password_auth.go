package app

import (
	"context"
	"crypto/rand"
	"fmt"
	"log/slog"
	mathrand "math/rand/v2"
	"time"

	"github.com/google/uuid"

	"danny.vn/aboutme/apps/server/internal/api"
	"danny.vn/aboutme/apps/server/internal/auth"
	"danny.vn/aboutme/apps/server/internal/authmail"
	"danny.vn/aboutme/apps/server/internal/config"
	"danny.vn/aboutme/apps/server/internal/mailcapture"
	"danny.vn/aboutme/apps/server/internal/password"
	"danny.vn/aboutme/apps/server/internal/privacynotice"
	"danny.vn/aboutme/apps/server/internal/store"
)

type PasswordAuth struct {
	Service *auth.PasswordService
	Worker  *authmail.Worker
}

func NewPasswordAuth(ctx context.Context, logger *slog.Logger, cfg config.Config, pool *store.Pool, queries *store.Queries) (*PasswordAuth, error) {
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
		Pool:                 pool,
		Queries:              queries,
		Sessions:             auth.NewSessionManagerWithPool(pool),
		Policy:               policy,
		Hasher:               hasher,
		Outbox:               outbox,
		Limits:               limits,
		PublicOrigin:         cfg.PublicOrigin,
		Clock:                time.Now,
		Entropy:              rand.Reader,
		Logger:               logger,
		TrustedProxies:       api.TrustedProxies(cfg.TrustedProxyCIDRs),
		RegistrationDisabled: cfg.PasswordRegistrationDisabled,
	})
	if err != nil {
		return nil, fmt.Errorf("create password service: %w", err)
	}

	notices, err := privacynotice.New(pool, outbox, time.Now)
	if err != nil {
		return nil, fmt.Errorf("create privacy notice service: %w", err)
	}
	worker, err := authmail.NewWorker(authmail.WorkerOptions{
		Pool:            pool,
		Queries:         queries,
		KeyRing:         ring,
		Sender:          sender,
		FooterNote:      authmail.FooterNoteForMode(cfg.AuthEmail.Mode),
		Clock:           time.Now,
		Jitter:          fullJitter,
		Logger:          logger,
		WorkerID:        uuid.New(),
		NoticeAuthority: notices,
	})
	if err != nil {
		return nil, fmt.Errorf("create mail worker: %w", err)
	}

	return &PasswordAuth{Service: service, Worker: worker}, nil
}

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
			FromName:         cfg.AuthEmail.SESFromName,
			ConfigurationSet: cfg.AuthEmail.SESConfigSet,
			Client:           client,
			Logger:           logger,
		})
		if err != nil {
			return nil, fmt.Errorf("create SES sender: %w", err)
		}
		return sender, nil
	case authmailModeSMTPValue:
		sender, err := authmail.NewSMTPSender(authmail.SMTPOptions{
			Host:     cfg.AuthEmail.SMTPHost,
			Port:     cfg.AuthEmail.SMTPPort,
			TLSMode:  authmail.SMTPTLSMode(cfg.AuthEmail.SMTPTLS),
			Username: cfg.AuthEmail.SMTPUsername,
			Password: cfg.AuthEmail.SMTPPassword.Reveal(),
			From:     cfg.AuthEmail.SESFrom,
			FromName: cfg.AuthEmail.SESFromName,
			Logger:   logger,
		})
		if err != nil {
			return nil, fmt.Errorf("create SMTP sender: %w", err)
		}
		return sender, nil
	default:
		return nil, fmt.Errorf("unsupported auth email mode %q", cfg.AuthEmail.Mode)
	}
}

const (
	authmailModeCaptureValue = "capture"
	authmailModeSESValue     = "ses"
	authmailModeSMTPValue    = "smtp"
)

func fullJitter(d time.Duration) time.Duration {
	if d <= 0 {
		return 0
	}
	return time.Duration(mathrand.Int64N(int64(d) + 1)) //nolint:gosec // bounded retry backoff, not secret material
}
