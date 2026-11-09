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

	"github.com/dannyota/aboutme/apps/server/internal/api"
	"github.com/dannyota/aboutme/apps/server/internal/store"
)

// registerAdmission is the narrow rate-admission boundary for registration.
// It uses the canonical Caddy client address; this service neither reads
// cookies nor derives an address itself.
type registerAdmission interface {
	// AdmitRegister reports whether a registration request at the given time
	// is admitted and, when it is not, the whole seconds to wait before
	// retrying.
	AdmitRegister(time.Time, *http.Request) (allowed bool, retryAfterSeconds int)
}

// tokenAdmission is the narrow rate-admission boundary for the token endpoint:
// a per-address request budget and a per-grant failure budget.
type tokenAdmission interface {
	// AdmitToken reports whether a token request at the given time is
	// admitted and, when it is not, the whole seconds to wait before
	// retrying.
	AdmitToken(time.Time, *http.Request) (allowed bool, retryAfterSeconds int)
	// AdmitGrant reserves one failed-grant slot for the grant ID at the given
	// time before the request is processed. It returns the reservation,
	// whether the grant is admitted, and, when it is not, the whole seconds to
	// wait before retrying.
	AdmitGrant(uuid.UUID, time.Time) (grantAttempt, bool, int)
	// FinishGrant resolves one admitted reservation with the request's
	// outcome: a failure keeps one failure, a release frees only the
	// reservation, and a success clears a tracked grant's failure window.
	FinishGrant(grantAttempt, grantAttemptResult)
}

// GrantAttempt is an opaque failed-grant reservation.
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

// ServiceDependencies are the shared OAuth service dependencies.
type ServiceDependencies struct {
	// CIMD resolves client ID metadata document clients. It is required when
	// CIMDEnabled is true and may be nil otherwise.
	CIMD *CIMDResolver
	// CIMDEnabled admits HTTPS metadata-document client IDs and advertises
	// that support in the authorization-server metadata.
	CIMDEnabled bool
	// TrustedProxies are the proxy prefixes whose forwarded client address is
	// trusted when deriving the request address passed to CIMD resolution.
	TrustedProxies api.TrustedProxies
	// Pool begins the transactions for consent, code, token, revocation and
	// idle-client work. It must not be nil.
	Pool *store.Pool
	// Queries runs the OAuth client, code, grant and token queries outside a
	// transaction. It must not be nil.
	Queries store.OAuthQueries
	// Clock returns the current time for registration, expiry and admission
	// decisions. It must not be nil.
	Clock func() time.Time
	// Entropy supplies the random bytes for authorization codes and tokens.
	// It must not be nil.
	Entropy io.Reader
	// PublicOrigin is the canonical public origin, such as
	// https://aboutme.vn. It is the issuer, the endpoint base and the exact
	// Origin the consent routes require.
	PublicOrigin string
	// RegisterAdmission rate-limits client registration requests. It must not
	// be nil.
	RegisterAdmission registerAdmission
	// TokenAdmission rate-limits token requests and failed grant attempts. It
	// must not be nil.
	TokenAdmission tokenAdmission
	// LiveGrantLimit is the most live grants one account may hold. It must be
	// positive.
	LiveGrantLimit int
}

// Service is the OAuth authorization-server service.
type Service struct {
	cimd              *CIMDResolver
	cimdEnabled       bool
	trustedProxies    api.TrustedProxies
	pool              *store.Pool
	queries           store.OAuthQueries
	clock             func() time.Time
	entropy           io.Reader
	publicOrigin      string
	registerAdmission registerAdmission
	tokenAdmission    tokenAdmission
	liveGrantLimit    int
}

// NewService validates its fixed dependencies and performs the idle-client
// cleanup sweep before making the service available to routes.
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
		entropy:           dependencies.Entropy,
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
