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

type Service struct {
	cfg Config
	key *rsa.PrivateKey
	mux *http.ServeMux

	mu    sync.Mutex
	codes map[string]codeBinding

	linkedin *linkedinMock
}

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
	if cfg.LinkedInClientID != "" {
		svc.linkedin = newLinkedInMock(svc, cfg)
		svc.linkedin.register(svc.mux)
	}
	return svc, nil
}

func (s *Service) Handler() http.Handler {
	return s.mux
}
