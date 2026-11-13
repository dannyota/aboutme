package secondfactor

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"

	"danny.vn/aboutme/apps/server/internal/store"
)

const totpUnavailableMessage = "totp_unavailable"

const totpUnavailableInterval = time.Minute

const (
	TOTPUnavailableReasonUnknownKeyID  = "unknown_key_id"
	TOTPUnavailableReasonKeyIDOverflow = "key_id_overflow"
	TOTPUnavailableReasonDecryptFailed = "decrypt_failed"
)

type TOTPUnavailableSignal struct {
	now    func() time.Time
	logger *slog.Logger

	mu   sync.Mutex
	last map[string]time.Time
}

func NewTOTPUnavailableSignal(now func() time.Time, logger *slog.Logger) *TOTPUnavailableSignal {
	return &TOTPUnavailableSignal{now: now, logger: logger, last: make(map[string]time.Time)}
}

func (s *TOTPUnavailableSignal) Emit(reason string) {
	s.emit(reason, nil)
}

func (s *TOTPUnavailableSignal) EmitDecryptFailure(kind TOTPRecordKind, id uuid.UUID) {
	s.emit(TOTPUnavailableReasonDecryptFailed, []any{"kind", string(kind), "id", id.String()})
}

func (s *TOTPUnavailableSignal) emit(reason string, attrs []any) {
	now := s.now()
	s.mu.Lock()
	last, seen := s.last[reason]
	if seen && now.Sub(last) < totpUnavailableInterval {
		s.mu.Unlock()
		return
	}
	s.last[reason] = now
	s.mu.Unlock()
	if s.logger == nil {
		return
	}
	args := append([]any{"reason", reason}, attrs...)
	s.logger.Warn(totpUnavailableMessage, args...)
}

type TOTPHealthConfig struct {
	Pool   *store.Pool
	Ring   *TOTPKeyRing
	Now    func() time.Time
	Signal *TOTPUnavailableSignal
}

type totpKeyIDLister interface {
	ListTOTPActiveKeyIDs(ctx context.Context, now time.Time) ([]string, error)
}

type TOTPHealthChecker struct {
	q      totpKeyIDLister
	ring   *TOTPKeyRing
	now    func() time.Time
	signal *TOTPUnavailableSignal
}

func NewTOTPHealthChecker(config TOTPHealthConfig) (*TOTPHealthChecker, error) {
	if config.Pool == nil || config.Ring == nil || config.Now == nil || config.Signal == nil {
		return nil, errors.New("secondfactor: totp health checker missing dependency")
	}
	return &TOTPHealthChecker{q: store.New(config.Pool), ring: config.Ring, now: config.Now, signal: config.Signal}, nil
}

func (c *TOTPHealthChecker) Check(ctx context.Context) error {
	ids, err := c.q.ListTOTPActiveKeyIDs(ctx, c.now())
	if err != nil {
		return fmt.Errorf("secondfactor: totp key health check: %w", err)
	}
	if len(ids) > 2 {
		c.signal.Emit(TOTPUnavailableReasonKeyIDOverflow)
	}
	for _, id := range ids {
		if !c.ring.KnownKeyID(id) {
			c.signal.Emit(TOTPUnavailableReasonUnknownKeyID)
		}
	}
	return nil
}
