package oauthsrv

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/dannyota/aboutme/apps/server/internal/store"
)

const (
	registerBodyLimit = 4096
	maxRedirectURIs   = 5
)

var (
	errRegisterRequestTooLarge = errors.New("oauth registration request too large")
	errRegisterRequestInvalid  = errors.New("oauth registration request invalid")
)

// registerAdmission is the narrow rate-admission boundary for registration.
// T09 implements it with the canonical Caddy client address; this service
// deliberately neither reads cookies nor derives an address itself.
type registerAdmission interface {
	AdmitRegister(time.Time, *http.Request) (allowed bool, retryAfterSeconds int)
}

type tokenAdmission interface {
	AdmitToken(time.Time, *http.Request) (allowed bool, retryAfterSeconds int)
	AdmitGrant(uuid.UUID, time.Time) (grantAttempt, bool, int)
	FinishGrant(grantAttempt, grantAttemptResult)
}

type grantAttempt struct {
	clientID uuid.UUID
	leaseID  uint64
	overflow bool
}

type grantAttemptResult uint8

const (
	grantAttemptRelease grantAttemptResult = iota
	grantAttemptFailure
	grantAttemptSuccess
)

// ServiceDependencies are the shared OAuth service dependencies. T05 and T06
// extend Service on this stable surface without re-plumbing their handlers.
type ServiceDependencies struct {
	Pool              *store.Pool
	Queries           store.OAuthQueries
	Clock             func() time.Time
	Entropy           io.Reader
	PublicOrigin      string
	RegisterAdmission registerAdmission
	TokenAdmission    tokenAdmission
	LiveGrantLimit    int
}

// Service is the OAuth authorization-server service shared by Phase PM tasks.
type Service struct {
	pool              *store.Pool
	queries           store.OAuthQueries
	clock             func() time.Time
	entropy           io.Reader
	publicOrigin      string
	registerAdmission registerAdmission
	tokenAdmission    tokenAdmission
	liveGrantLimit    int
}

// NewService validates its fixed dependencies and performs the startup M1
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
	case !canonicalOrigin(dependencies.PublicOrigin):
		return nil, errors.New("oauth service: noncanonical public origin")
	case isNilDependency(dependencies.RegisterAdmission):
		return nil, errors.New("oauth service: nil register admission")
	case isNilDependency(dependencies.TokenAdmission):
		return nil, errors.New("oauth service: nil token admission")
	case dependencies.LiveGrantLimit <= 0:
		return nil, errors.New("oauth service: invalid live grant limit")
	}

	s := &Service{
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

func canonicalOrigin(raw string) bool {
	if raw == "" || len(raw) > 512 {
		return false
	}
	for i := range raw {
		if raw[i] < 0x21 || raw[i] > 0x7e {
			return false
		}
	}
	u, err := url.Parse(raw)
	if err != nil || u.User != nil || u.Host == "" || u.Path != "" || u.RawPath != "" || u.RawQuery != "" || u.Fragment != "" {
		return false
	}
	scheme := strings.ToLower(u.Scheme)
	if scheme != "http" && scheme != "https" {
		return false
	}
	host := strings.ToLower(u.Hostname())
	if host == "" {
		return false
	}
	if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	port := u.Port()
	if port != "" && !(scheme == "http" && port == "80") && !(scheme == "https" && port == "443") {
		host += ":" + port
	}
	return raw == scheme+"://"+host
}

// HandleRegister registers a public OAuth client. It intentionally never
// reads a session cookie and has no CSRF check because it is a raw protocol
// endpoint rather than a browser-authenticated route.
func (s *Service) HandleRegister(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeOAuthError(w, http.StatusMethodNotAllowed)
		return
	}
	if !exactJSONContentType(r.Header) {
		writeOAuthError(w, http.StatusUnsupportedMediaType)
		return
	}
	allowed, retryAfter := s.registerAdmission.AdmitRegister(s.clock(), r)
	if !allowed {
		if retryAfter < 1 {
			retryAfter = 1
		}
		w.Header().Set("Retry-After", strconv.Itoa(retryAfter))
		writeOAuthError(w, http.StatusTooManyRequests)
		return
	}

	registration, err := decodeRegistration(r.Body)
	if err != nil {
		if errors.Is(err, errRegisterRequestTooLarge) {
			writeOAuthError(w, http.StatusRequestEntityTooLarge)
			return
		}
		writeOAuthError(w, http.StatusBadRequest)
		return
	}
	clientName, err := ValidateClientName(registration.ClientName)
	if err != nil {
		writeOAuthError(w, http.StatusBadRequest)
		return
	}
	if len(registration.RedirectURIs) < 1 || len(registration.RedirectURIs) > maxRedirectURIs {
		writeOAuthError(w, http.StatusBadRequest)
		return
	}
	for _, redirectURI := range registration.RedirectURIs {
		if ValidateRedirectURI(redirectURI) != nil {
			writeOAuthError(w, http.StatusBadRequest)
			return
		}
	}
	if registration.TokenEndpointAuthMethod != "" && registration.TokenEndpointAuthMethod != "none" {
		writeOAuthError(w, http.StatusBadRequest)
		return
	}

	redirectURIs, err := json.Marshal(registration.RedirectURIs)
	if err != nil {
		writeOAuthServerError(w)
		return
	}
	if err := s.CollectIdleClients(r.Context()); err != nil {
		writeOAuthServerError(w)
		return
	}
	client, err := s.queries.CreateOAuthClient(r.Context(), store.CreateOAuthClientParams{
		ClientName:   clientName,
		RedirectURIs: redirectURIs,
		CreatedAt:    s.clock(),
	})
	if err != nil {
		writeOAuthServerError(w)
		return
	}

	response, err := json.Marshal(registerResponse{
		ClientID:                client.ID.String(),
		ClientName:              clientName,
		RedirectURIs:            registration.RedirectURIs,
		TokenEndpointAuthMethod: "none",
	})
	if err != nil {
		writeOAuthServerError(w)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_, _ = w.Write(response)
}

type registrationInput struct {
	ClientName              string
	RedirectURIs            []string
	TokenEndpointAuthMethod string
}

type registerResponse struct {
	ClientID                string   `json:"client_id"`
	ClientName              string   `json:"client_name"`
	RedirectURIs            []string `json:"redirect_uris"`
	TokenEndpointAuthMethod string   `json:"token_endpoint_auth_method"`
}

func exactJSONContentType(header http.Header) bool {
	values := header.Values("Content-Type")
	return len(values) == 1 && values[0] == "application/json"
}

func decodeRegistration(body io.Reader) (registrationInput, error) {
	raw, err := io.ReadAll(io.LimitReader(body, registerBodyLimit+1))
	if err != nil {
		return registrationInput{}, errRegisterRequestInvalid
	}
	if len(raw) > registerBodyLimit {
		return registrationInput{}, errRegisterRequestTooLarge
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	token, err := decoder.Token()
	if err != nil {
		return registrationInput{}, errRegisterRequestInvalid
	}
	if delimiter, ok := token.(json.Delim); !ok || delimiter != '{' {
		return registrationInput{}, errRegisterRequestInvalid
	}
	fields := make(map[string]json.RawMessage, 3)
	for decoder.More() {
		token, err := decoder.Token()
		if err != nil {
			return registrationInput{}, errRegisterRequestInvalid
		}
		name, ok := token.(string)
		if !ok {
			return registrationInput{}, errRegisterRequestInvalid
		}
		if _, exists := fields[name]; exists {
			return registrationInput{}, errRegisterRequestInvalid
		}
		switch name {
		case "client_name", "redirect_uris", "token_endpoint_auth_method":
		default:
			return registrationInput{}, errRegisterRequestInvalid
		}
		var value json.RawMessage
		if err := decoder.Decode(&value); err != nil {
			return registrationInput{}, errRegisterRequestInvalid
		}
		fields[name] = value
	}
	if token, err := decoder.Token(); err != nil || token != json.Delim('}') {
		return registrationInput{}, errRegisterRequestInvalid
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return registrationInput{}, errRegisterRequestInvalid
	}
	name, nameOK := fields["client_name"]
	redirects, redirectsOK := fields["redirect_uris"]
	if !nameOK || !redirectsOK {
		return registrationInput{}, errRegisterRequestInvalid
	}
	var registration registrationInput
	if err := json.Unmarshal(name, &registration.ClientName); err != nil {
		return registrationInput{}, errRegisterRequestInvalid
	}
	if err := json.Unmarshal(redirects, &registration.RedirectURIs); err != nil {
		return registrationInput{}, errRegisterRequestInvalid
	}
	if authMethod, ok := fields["token_endpoint_auth_method"]; ok {
		if err := json.Unmarshal(authMethod, &registration.TokenEndpointAuthMethod); err != nil || registration.TokenEndpointAuthMethod != "none" {
			return registrationInput{}, errRegisterRequestInvalid
		}
	}
	return registration, nil
}

func writeOAuthError(w http.ResponseWriter, status int) {
	writeOAuthErrorBody(w, status, "invalid_request", "The request is invalid.")
}

func writeOAuthServerError(w http.ResponseWriter) {
	writeOAuthErrorBody(w, http.StatusInternalServerError, "server_error", "The server encountered an error.")
}

func writeOAuthErrorBody(w http.ResponseWriter, status int, code, description string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = fmt.Fprintf(w, `{"error":%q,"error_description":%q}`, code, description)
}
