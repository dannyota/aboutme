package oauthsrv_test

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/dannyota/aboutme/apps/server/internal/api"
	"github.com/dannyota/aboutme/apps/server/internal/mcpapi"
	"github.com/dannyota/aboutme/apps/server/internal/oauthsrv"
	"github.com/dannyota/aboutme/apps/server/internal/resumeapi"
)

type revokeRoutingExecutor struct{}

func (revokeRoutingExecutor) ExecuteAgent(context.Context, resumeapi.AgentPrincipal, resumeapi.AgentCall) resumeapi.AgentResponse {
	panic("revocation routing proof must not execute a resume tool")
}

func newRevokeRouter(t *testing.T, f oauthsrv.RevokeRoutingFixture) http.Handler {
	t.Helper()
	bearer, err := mcpapi.NewBearer(mcpapi.BearerDependencies{Queries: f.Queries, Clock: f.Clock, PublicOrigin: "https://aboutme.example"})
	if err != nil {
		t.Fatal(err)
	}
	rates, err := mcpapi.NewRatePolicies(mcpapi.RateConfig{
		TokenRequests: 60, TokenWindow: time.Minute, UserRequests: 120, UserWindow: time.Minute,
		ConcurrentPerUser: 2, MaxKeys: 10, Clock: f.Clock,
	})
	if err != nil {
		t.Fatal(err)
	}
	mcp, err := mcpapi.NewServer(mcpapi.ServerDependencies{
		Bearer: bearer, Resumes: revokeRoutingExecutor{}, Rates: rates, MaxRequestBodyBytes: 4 << 20,
	})
	if err != nil {
		t.Fatal(err)
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	return api.New(logger, f.Pool, api.Options{Clock: f.Clock}, nil, func(mux *http.ServeMux) {
		mux.HandleFunc("/oauth/revoke", f.Service.HandleRevoke)
		mux.Handle("/mcp", mcp)
	})
}

func routedRevoke(t *testing.T, handler http.Handler, raw, claim string) *httptest.ResponseRecorder {
	t.Helper()
	form := url.Values{"token": {raw}, "client_id": {claim}}
	r := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "https://aboutme.example/oauth/revoke", strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	return w
}

func routedMCP(t *testing.T, handler http.Handler, access string) *httptest.ResponseRecorder {
	t.Helper()
	body := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"synthetic-client","version":"1"}}}`
	r := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "https://aboutme.example/mcp", strings.NewReader(body))
	r.Header.Set("Authorization", "Bearer "+access)
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Accept", "application/json, text/event-stream")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	return w
}

func TestRevokeClaimedClientRejectsNextMCPRequest(t *testing.T) {
	for _, mode := range []string{"dcr", "cimd on", "cimd off"} {
		for _, kind := range []oauthsrv.TokenKind{oauthsrv.TokenKindAccess, oauthsrv.TokenKindRefresh} {
			for _, claim := range []string{"matching", "mismatched"} {
				t.Run(mode+"/"+string(kind)+"/"+claim, func(t *testing.T) {
					f := oauthsrv.NewRevokeRoutingFixture(t, mode)
					handler := newRevokeRouter(t, f)
					before := routedMCP(t, handler, f.AccessRaw)
					var initialized struct {
						Result struct {
							ServerInfo struct{ Name string }
						}
					}
					if before.Code != http.StatusOK || json.Unmarshal(before.Body.Bytes(), &initialized) != nil || initialized.Result.ServerInfo.Name != "aboutme" {
						t.Fatal("live access token did not reach the real MCP server through the router")
					}
					id := f.PublicID
					if claim == "mismatched" {
						id = "https://unknown.example/metadata"
					}
					baseline := routedRevoke(t, handler, "synthetic-malformed", id)
					raw := f.RefreshRaw
					if kind == oauthsrv.TokenKindAccess {
						raw = f.AccessRaw
					}
					response := routedRevoke(t, handler, raw, id)
					baselineHeaders, responseHeaders := baseline.Header().Clone(), response.Header().Clone()
					baselineHeaders.Del("X-Request-ID")
					responseHeaders.Del("X-Request-ID")
					if baseline.Code != http.StatusOK || response.Code != http.StatusOK || baseline.Body.Len() != 0 || response.Body.Len() != 0 ||
						!reflect.DeepEqual(baselineHeaders, responseHeaders) {
						t.Fatal("routed revocation did not return the same empty 200 and application headers")
					}
					after := routedMCP(t, handler, f.AccessRaw)
					if after.Code != http.StatusUnauthorized || after.Body.String() != `{"error":"unauthorized"}` ||
						after.Header().Get("WWW-Authenticate") != `Bearer resource_metadata="https://aboutme.example/.well-known/oauth-protected-resource"` {
						t.Fatal("next MCP request retained authority after claimed-client revocation")
					}
					f.AssertRevoked(t)
				})
			}
		}
	}
}
