package oauthsrv

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/netip"
	"net/url"
	"strings"

	"github.com/dannyota/aboutme/apps/server/internal/api"
	"github.com/dannyota/aboutme/apps/server/internal/store"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type oauthClientReader interface {
	GetDCRClient(context.Context, uuid.UUID) (store.OAuthClient, error)
	GetOAuthClientByMetadataURL(context.Context, string) (store.OAuthClient, error)
}

func (s *Service) storedPublicClient(ctx context.Context, q oauthClientReader, raw string) (store.OAuthClient, error) {
	var client store.OAuthClient
	var err error
	if strings.HasPrefix(raw, "https://") {
		origin, originErr := url.Parse(s.publicOrigin)
		if originErr != nil || origin.Hostname() == "" {
			return client, errOAuthInvalidClient
		}
		if _, err := parseCIMDURL(raw, origin.Hostname()); err != nil {
			return client, errOAuthInvalidClient
		}
		client, err = q.GetOAuthClientByMetadataURL(ctx, raw)
	} else {
		id, parseErr := uuid.Parse(raw)
		if parseErr != nil {
			return client, errOAuthInvalidClient
		}
		client, err = q.GetDCRClient(ctx, id)
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return store.OAuthClient{}, errOAuthUnknownClient
	}
	return client, err
}

func (s *Service) requestAddress(r *http.Request) netip.Addr {
	raw, ok := api.ClientIP(r, s.trustedProxies)
	if !ok {
		return netip.Addr{}
	}
	addr, err := netip.ParseAddr(raw)
	if err != nil {
		return netip.Addr{}
	}
	return addr
}

func (q ConsentQuery) publicID() string {
	if q.PublicClientID != "" {
		if !strings.HasPrefix(q.PublicClientID, "https://") {
			if id, err := uuid.Parse(q.PublicClientID); err == nil {
				return id.String()
			}
		}
		return q.PublicClientID
	}
	return q.ClientID.String()
}

func (s *Service) resolveConsent(ctx context.Context, q ConsentQuery) (ConsentQuery, store.OAuthClient, error) {
	if strings.HasPrefix(q.publicID(), "https://") {
		if !s.cimdEnabled || s.cimd == nil {
			return q, store.OAuthClient{}, ErrConsentInvalid
		}
		metadata, err := s.cimd.Resolve(ctx, q.publicID(), q.address)
		if err != nil {
			var limited *CIMDRateError
			if errors.As(err, &limited) {
				return q, store.OAuthClient{}, err
			}
			return q, store.OAuthClient{}, ErrConsentInvalid
		}
		if !metadata.matchesRedirect(q.RedirectURI) {
			return q, store.OAuthClient{}, ErrConsentInvalid
		}
		q.metadata = &metadata
		client, err := s.queries.GetOAuthClientByMetadataURL(ctx, q.publicID())
		if errors.Is(err, pgx.ErrNoRows) {
			return q, store.OAuthClient{ClientName: metadata.name}, nil
		}
		if err != nil {
			return q, store.OAuthClient{}, err
		}
		q.ClientID = client.ID
		client.ClientName = metadata.name
		return q, client, nil
	}
	client, err := s.storedPublicClient(ctx, s.queries, q.publicID())
	if errors.Is(err, errOAuthUnknownClient) {
		return q, client, ErrConsentNotFound
	}
	if errors.Is(err, errOAuthInvalidClient) {
		return q, client, ErrConsentInvalid
	}
	if err != nil {
		return q, client, err
	}
	q.ClientID = client.ID
	if !registeredRedirect(client, q.RedirectURI) {
		return q, client, ErrConsentNotFound
	}
	return q, client, nil
}

func consentRedirectMatches(client store.OAuthClient, q ConsentQuery) bool {
	if q.metadata != nil {
		return q.metadata.id == q.publicID() && q.metadata.matchesRedirect(q.RedirectURI)
	}
	return client.MetadataUrl == nil && registeredRedirect(client, q.RedirectURI)
}

func (s *Service) lockApprovalClient(ctx context.Context, qtx *store.Queries, request *ConsentQuery) (store.OAuthClient, error) {
	if request.metadata == nil {
		return qtx.GetOAuthClientForUpdate(ctx, request.ClientID)
	}
	redirects, err := json.Marshal(request.metadata.redirects)
	if err != nil {
		return store.OAuthClient{}, err
	}
	client, err := qtx.UpsertOAuthMetadataClient(ctx, store.UpsertOAuthMetadataClientParams{
		MetadataUrl: request.publicID(), ClientName: request.metadata.name, RedirectURIs: redirects, CreatedAt: s.clock(),
	})
	if err == nil {
		request.ClientID = client.ID
	}
	return client, err
}

func clientDisplay(metadataURL *string) (string, *string) {
	if metadataURL == nil {
		return "dcr", nil
	}
	u, err := url.Parse(*metadataURL)
	if err != nil || u.Hostname() == "" {
		return "cimd", nil
	}
	host := u.Hostname()
	return "cimd", &host
}
