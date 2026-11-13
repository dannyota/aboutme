package oauthsrv

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"time"

	"github.com/google/uuid"

	"danny.vn/aboutme/apps/server/internal/api"
	"danny.vn/aboutme/apps/server/internal/store"
)

type registerAdmission interface {
	AdmitRegister(time.Time, *http.Request) (allowed bool, retryAfterSeconds int)
}

type tokenAdmission interface {
	AdmitToken(time.Time, *http.Request) (allowed bool, retryAfterSeconds int)
	AdmitGrant(uuid.UUID, time.Time) (grantAttempt, bool, int)
	FinishGrant(grantAttempt, grantAttemptResult)
}

type GrantAttempt struct {
	clientID uuid.UUID
	leaseID  uint64
	overflow bool
}

type grantAttempt = GrantAttempt

type grantAttemptResult uint8

const (
	grantAttemptRelease grantAttemptResult = iota
	grantAttemptFailure
	grantAttemptSuccess
)

type ServiceDependencies struct {
	CIMD              *CIMDResolver
	CIMDEnabled       bool
	TrustedProxies    api.TrustedProxies
	Pool              *store.Pool
	Queries           store.OAuthQueries
	Clock             func() time.Time
	Entropy           io.Reader
	PublicOrigin      string
	RegisterAdmission registerAdmission
	TokenAdmission    tokenAdmission
	LiveGrantLimit    int
}

type Service struct {
	cimd              *CIMDResolver
	cimdEnabled       bool
	trustedProxies    api.TrustedProxies
	pool              *store.Pool
	queries           store.OAuthQueries
	clock             func() time.Time
	entropy           *lockedEntropy
	publicOrigin      string
	registerAdmission registerAdmission
	tokenAdmission    tokenAdmission
	liveGrantLimit    int
}

func NewService(ctx context.Context, dependencies ServiceDependencies) (*Service, error) {
	switch {
	case dependencies.Pool == nil:
		return nil, errors.New("oauth service: nil pool")
	case isNilDependency(dependencies.Queries):
		return nil, errors.New("oauth service: nil queries")
	case dependencies.Clock == nil:
		return nil, errors.New("oauth service: nil clock")
	case isNilDependency(dependencies.Entropy):
		return nil, errors.New("oauth service: nil entropy")
	case !IsCanonicalOrigin(dependencies.PublicOrigin):
		return nil, errors.New("oauth service: noncanonical public origin")
	case isNilDependency(dependencies.RegisterAdmission):
		return nil, errors.New("oauth service: nil register admission")
	case isNilDependency(dependencies.TokenAdmission):
		return nil, errors.New("oauth service: nil token admission")
	case dependencies.LiveGrantLimit <= 0:
		return nil, errors.New("oauth service: invalid live grant limit")
	case dependencies.CIMDEnabled && dependencies.CIMD == nil:
		return nil, errors.New("oauth service: missing CIMD resolver")
	}

	s := &Service{
		cimd: dependencies.CIMD, cimdEnabled: dependencies.CIMDEnabled, trustedProxies: dependencies.TrustedProxies,
		pool:              dependencies.Pool,
		queries:           dependencies.Queries,
		clock:             dependencies.Clock,
		entropy:           &lockedEntropy{reader: dependencies.Entropy},
		publicOrigin:      dependencies.PublicOrigin,
		registerAdmission: dependencies.RegisterAdmission,
		tokenAdmission:    dependencies.TokenAdmission,
		liveGrantLimit:    dependencies.LiveGrantLimit,
	}
	if err := s.CollectIdleClients(ctx); err != nil {
		return nil, fmt.Errorf("oauth service: startup idle-client GC: %w", err)
	}
	return s, nil
}

func isNilDependency(value any) bool {
	if value == nil {
		return true
	}
	v := reflect.ValueOf(value)
	switch v.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return v.IsNil()
	default:
		return false
	}
}
