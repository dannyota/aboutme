// Package uatmock provides the local-only Google OIDC provider used by the
// native HTTPS authentication proof.
package uatmock

import (
	"crypto/rsa"
	"fmt"
	"net/http"
	"sync"
)

const (
	discoveryPath      = "/google/.well-known/openid-configuration"
	jwksPath           = "/google/jwks.json"
	tokenPath          = "/google/token"
	authorizePath      = "/__uat/oauth/google/authorize"
	googleCallbackPath = "/api/v1/auth/google/callback"

	maxFieldBytes = 2048
	maxFormBytes  = 16 << 10
)

// Service owns an ephemeral signing key and single-use authorization codes.
type Service struct {
	cfg Config
	key *rsa.PrivateKey
	mux *http.ServeMux

	mu    sync.Mutex
	codes map[string]codeBinding
}

// New constructs a closed, Google-only mock provider.
func New(cfg Config) (*Service, error) {
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	key, err := rsa.GenerateKey(cfg.Random, 2048)
	if err != nil {
		return nil, fmt.Errorf("uatmock: generate signing key: %w", err)
	}
	svc := &Service{
		cfg:   cfg,
		key:   key,
		mux:   http.NewServeMux(),
		codes: make(map[string]codeBinding),
	}
	svc.mux.HandleFunc(discoveryPath, svc.serveDiscovery)
	svc.mux.HandleFunc(jwksPath, svc.serveJWKS)
	svc.mux.HandleFunc(tokenPath, svc.serveToken)
	svc.mux.HandleFunc(authorizePath, svc.serveAuthorize)
	return svc, nil
}

// Handler returns the complete mock provider HTTP handler.
func (s *Service) Handler() http.Handler {
	return s.mux
}
