package secondfactor

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/dannyota/aboutme/apps/server/internal/store"
)

// totpUnavailableMessage is the fixed JSON message logged for every TOTP
// key failure (docs/design/totp-key-management.md "Key failures").
const totpUnavailableMessage = "totp_unavailable"

// totpUnavailableInterval bounds totpUnavailableMessage to at most once per
// minute per reason per process (docs/design/budgets.md "totp_unavailable
// log line"; AC-SEC-008).
const totpUnavailableInterval = time.Minute

// The closed set of totp_unavailable reasons
// (docs/design/totp-key-management.md "Key failures").
const (
	TOTPUnavailableReasonUnknownKeyID  = "unknown_key_id"
	TOTPUnavailableReasonKeyIDOverflow = "key_id_overflow"
	TOTPUnavailableReasonDecryptFailed = "decrypt_failed"
)

// TOTPUnavailableSignal rate-limits the fixed totp_unavailable log line to
// at most once per minute per reason per process. The key-health checker
// and the lifecycle worker's per-row decrypt path share one instance, so
// the limit holds across both call sites
// (docs/design/totp-key-management.md "Key failures"; AC-SEC-008).
type TOTPUnavailableSignal struct {
	now    func() time.Time
	logger *slog.Logger

	mu   sync.Mutex
	last map[string]time.Time
}

// NewTOTPUnavailableSignal returns a signal that logs through logger, using
// now for the once-per-minute-per-reason window. logger may be nil, which
// makes Emit and EmitDecryptFailure silent no-ops.
func NewTOTPUnavailableSignal(now func() time.Time, logger *slog.Logger) *TOTPUnavailableSignal {
	return &TOTPUnavailableSignal{now: now, logger: logger, last: make(map[string]time.Time)}
}

// Emit logs totp_unavailable with reason, unless that reason already logged
// within the last minute in this process (docs/design/totp-key-management.md
// "Key failures").
func (s *TOTPUnavailableSignal) Emit(reason string) {
	s.emit(reason, nil)
}

// EmitDecryptFailure logs totp_unavailable with reason "decrypt_failed" and
// the failing row's record kind and internal ID, under the same
// once-per-minute-per-reason limit as Emit. Only decrypt_failed carries a
// record kind and row ID, never an account ID
// (docs/design/totp-key-management.md "Key failures").
func (s *TOTPUnavailableSignal) EmitDecryptFailure(kind TOTPRecordKind, id uuid.UUID) {
	s.emit(TOTPUnavailableReasonDecryptFailed, []any{"kind", string(kind), "id", id.String()})
}

// emit is the shared rate-limited log path for Emit and EmitDecryptFailure.
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

// TOTPHealthConfig supplies the key-health checker's fixed dependencies.
type TOTPHealthConfig struct {
	Pool   *store.Pool
	Ring   *TOTPKeyRing
	Now    func() time.Time
	Signal *TOTPUnavailableSignal
}

// totpKeyIDLister is the one store method Check needs: the bounded,
// unscoped three-ID query from docs/design/totp-key-management.md "Key
// failures". The narrow interface lets tests exercise Check's branching
// against a fake, since the real query scans every account's rows and so
// cannot be scoped to one test's fixtures against a shared database.
type totpKeyIDLister interface {
	ListTOTPActiveKeyIDs(ctx context.Context, now time.Time) ([]string, error)
}

// TOTPHealthChecker runs the bounded three-ID key-ring health check
// (docs/design/totp-key-management.md "Key failures"; AC-SEC-008). It never
// decrypts a row and never touches /readyz: a key-ring problem fails closed
// per TOTP row only and keeps readiness green.
type TOTPHealthChecker struct {
	q      totpKeyIDLister
	ring   *TOTPKeyRing
	now    func() time.Time
	signal *TOTPUnavailableSignal
}

// NewTOTPHealthChecker validates config and returns the checker. The
// caller runs Check at startup and every five minutes; this type starts no
// goroutine or timer of its own.
func NewTOTPHealthChecker(config TOTPHealthConfig) (*TOTPHealthChecker, error) {
	if config.Pool == nil || config.Ring == nil || config.Now == nil || config.Signal == nil {
		return nil, errors.New("secondfactor: totp health checker missing dependency")
	}
	return &TOTPHealthChecker{q: store.New(config.Pool), ring: config.Ring, now: config.Now, signal: config.Signal}, nil
}

// Check reads at most three distinct key IDs across credentials and
// unexpired enrollments without decrypting. A healthy ring holds at most
// two distinct IDs, so a third proves an ID outside the ring exists and
// emits key_id_overflow; every returned ID the ring does not hold emits
// unknown_key_id (docs/design/totp-key-management.md "Key failures"). A
// query failure is returned to the caller and emits nothing, since it is
// not one of the closed totp_unavailable reasons.
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
