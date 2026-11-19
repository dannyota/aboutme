package mcpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"danny.vn/aboutme/apps/server/internal/oauthsrv"
	"danny.vn/aboutme/apps/server/internal/store"
)

const answerBound = 5 * time.Second

const modernMeta = `"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28",` +
	`"io.modelcontextprotocol/clientInfo":{"name":"listen-test","version":"1"},` +
	`"io.modelcontextprotocol/clientCapabilities":{}}`

type staticOAuthQueries struct {
	store.OAuthQueries
	authority store.GetOAuthTokenAuthorityByDigestRow
}

func (q *staticOAuthQueries) GetOAuthTokenAuthorityByDigest(context.Context, []byte) (store.GetOAuthTokenAuthorityByDigestRow, error) {
	return q.authority, nil
}

func (q *staticOAuthQueries) TouchOAuthTokenLastUsed(context.Context, store.TouchOAuthTokenLastUsedParams) (int64, error) {
	return 0, nil
}

type mcpExchange struct {
	name     string
	version  string
	method   string
	toolName string
	body     string
}

func newStaticMCPHandler(t *testing.T) (http.Handler, string) {
	t.Helper()
	now := time.Date(2026, 11, 19, 4, 0, 0, 0, time.UTC)
	userID := uuid.MustParse("018f5b6a-9a3e-7c21-8b1e-0000000000a0")
	grantID := uuid.MustParse("018f5b6a-9a3e-7c21-8b1e-0000000000a1")
	clientID := uuid.MustParse("018f5b6a-9a3e-7c21-8b1e-0000000000a2")
	queries := &staticOAuthQueries{authority: store.GetOAuthTokenAuthorityByDigestRow{
		OAuthToken: store.OAuthToken{
			ID: uuid.MustParse("018f5b6a-9a3e-7c21-8b1e-0000000000a3"), Kind: string(oauthsrv.TokenKindAccess),
			ClientID: clientID, UserID: userID, GrantID: grantID, ExpiresAt: now.Add(time.Hour), FamilyExpiresAt: now.Add(time.Hour),
		},
		OAuthGrant: store.OAuthGrant{ID: grantID, UserID: userID, ClientID: clientID, Scopes: "resumes:read resumes:write"},
		User:       store.User{ID: userID},
	}}
	bearer, err := NewBearer(BearerDependencies{Queries: queries, Clock: func() time.Time { return now }, PublicOrigin: bearerPublicOrigin})
	if err != nil {
		t.Fatalf("NewBearer: %v", err)
	}
	raw, _, err := oauthsrv.NewToken(oauthsrv.TokenKindAccess, bytes.NewReader(bytes.Repeat([]byte{'l'}, 32)))
	if err != nil {
		t.Fatalf("NewToken: %v", err)
	}
	handler, err := NewServer(ServerDependencies{
		Bearer: bearer, Resumes: &recordingAgentExecutor{}, Projects: &recordingProjectExecutor{}, Rates: mustTestMCPRates(t),
		MaxRequestBodyBytes: maxMCPRequestBytes,
	})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	return handler, raw
}

func newMCPExchangeRequest(ctx context.Context, raw string, exchange mcpExchange) *http.Request {
	request := httptest.NewRequestWithContext(ctx, http.MethodPost, "/mcp", strings.NewReader(exchange.body))
	request.Header.Set("Authorization", "Bearer "+raw)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json, text/event-stream")
	if exchange.version != "" {
		request.Header.Set("Mcp-Protocol-Version", exchange.version)
	}
	if exchange.method != "" {
		request.Header.Set("Mcp-Method", exchange.method)
	}
	if exchange.toolName != "" {
		request.Header.Set("Mcp-Name", exchange.toolName)
	}
	return request
}

type servedExchange struct {
	recorder *httptest.ResponseRecorder
	cancel   context.CancelFunc
	done     chan struct{}
}

func startMCPExchange(t *testing.T, handler http.Handler, raw string, exchange mcpExchange) *servedExchange {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	served := &servedExchange{recorder: httptest.NewRecorder(), cancel: cancel, done: make(chan struct{})}
	request := newMCPExchangeRequest(ctx, raw, exchange)
	go func() {
		defer close(served.done)
		handler.ServeHTTP(served.recorder, request)
	}()
	t.Cleanup(func() { served.abandon(t) })
	return served
}

func (s *servedExchange) answeredBy(deadline time.Time) bool {
	timer := time.NewTimer(time.Until(deadline))
	defer timer.Stop()
	select {
	case <-s.done:
		return true
	case <-timer.C:
		return false
	}
}

func (s *servedExchange) abandon(t *testing.T) {
	t.Helper()
	s.cancel()
	select {
	case <-s.done:
	case <-time.After(answerBound):
		t.Fatal("request did not return after its client went away")
	}
}

func modernExchange(name string, id int, method, toolName, params string) mcpExchange {
	if params != "" {
		params += ","
	}
	body := `{"jsonrpc":"2.0","id":` + strconv.Itoa(id) + `,"method":"` + method + `","params":{` + params + modernMeta + `}}`
	return mcpExchange{name: name, version: "2026-07-28", method: method, toolName: toolName, body: body}
}

func everySubscription(id int) mcpExchange {
	return modernExchange("subscriptions/listen every notification", id, "subscriptions/listen", "",
		`"notifications":{"toolsListChanged":true,"promptsListChanged":true,"resourcesListChanged":true,"resourceSubscriptions":["aboutme://x"]}`)
}

func acknowledgedNotifications(t *testing.T, body string) string {
	t.Helper()
	for _, line := range strings.Split(body, "\n") {
		data, ok := strings.CutPrefix(line, "data: ")
		if !ok {
			continue
		}
		var message struct {
			Method string `json:"method"`
			Params struct {
				Notifications json.RawMessage `json:"notifications"`
			} `json:"params"`
		}
		if err := json.Unmarshal([]byte(data), &message); err != nil {
			t.Fatalf("decode event %q: %v", data, err)
		}
		if message.Method == "notifications/subscriptions/acknowledged" {
			return string(message.Params.Notifications)
		}
	}
	t.Fatalf("no notifications/subscriptions/acknowledged event in %q", body)
	return ""
}

func TestServer_ClientMessagesAreNeverHeldOpen(t *testing.T) {
	handler, raw := newStaticMCPHandler(t)
	legacy := func(name, body string) mcpExchange {
		return mcpExchange{name: name, version: "2025-06-18", body: body}
	}
	exchanges := []mcpExchange{
		{name: "legacy initialize", body: `{"jsonrpc":"2.0","id":0,"method":"initialize","params":{"protocolVersion":"2025-06-18",` +
			`"capabilities":{"roots":{"listChanged":true},"elicitation":{}},"clientInfo":{"name":"listen-test","version":"1"}}}`},
		legacy("legacy notifications/initialized", `{"jsonrpc":"2.0","method":"notifications/initialized"}`),
		legacy("legacy tools/list", `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`),
		legacy("legacy ping", `{"jsonrpc":"2.0","id":2,"method":"ping"}`),
		legacy("legacy logging/setLevel", `{"jsonrpc":"2.0","id":3,"method":"logging/setLevel","params":{"level":"info"}}`),
		legacy("legacy resources/list", `{"jsonrpc":"2.0","id":4,"method":"resources/list"}`),
		legacy("legacy prompts/list", `{"jsonrpc":"2.0","id":5,"method":"prompts/list"}`),
		legacy("legacy tools/call", `{"jsonrpc":"2.0","id":6,"method":"tools/call","params":{"name":"list_resumes","arguments":{}}}`),
		legacy("legacy notifications/cancelled", `{"jsonrpc":"2.0","method":"notifications/cancelled","params":{"requestId":6}}`), //nolint:misspell // MCP method name.
		legacy("legacy notifications/roots/list_changed", `{"jsonrpc":"2.0","method":"notifications/roots/list_changed"}`),
		legacy("legacy client response", `{"jsonrpc":"2.0","id":"server-1","result":{"roots":[]}}`),
		modernExchange("server/discover", 10, "server/discover", "", ""),
		modernExchange("tools/list", 11, "tools/list", "", ""),
		everySubscription(12),
		modernExchange("subscriptions/listen empty", 13, "subscriptions/listen", "", `"notifications":{}`),
		modernExchange("tools/call", 14, "tools/call", "list_resumes", `"name":"list_resumes","arguments":{}`),
		{name: "notifications/cancelled", version: "2026-07-28", method: "notifications/cancelled", body: `{"jsonrpc":"2.0","method":"notifications/cancelled","params":{"requestId":12}}`}, //nolint:misspell // MCP method name.
	}
	for _, exchange := range exchanges {
		t.Run(exchange.name, func(t *testing.T) {
			served := startMCPExchange(t, handler, raw, exchange)
			if !served.answeredBy(time.Now().Add(answerBound)) {
				served.abandon(t)
				t.Fatalf("still open after %v; on client cancel: %d %q", answerBound, served.recorder.Code, served.recorder.Body.String())
			}
			if code := served.recorder.Code; code != http.StatusOK && code != http.StatusAccepted {
				t.Fatalf("status = %d body %q", code, served.recorder.Body.String())
			}
			if exchange.method == "subscriptions/listen" {
				if granted := acknowledgedNotifications(t, served.recorder.Body.String()); granted != "{}" {
					t.Fatalf("acknowledged notifications = %s, want {}", granted)
				}
			}
		})
	}
}

func TestServer_DiscoveryAdvertisesNoToolListChanges(t *testing.T) {
	handler, raw := newStaticMCPHandler(t)
	for _, exchange := range []mcpExchange{
		{name: "initialize", body: `{"jsonrpc":"2.0","id":0,"method":"initialize","params":{"protocolVersion":"2025-06-18",` +
			`"capabilities":{},"clientInfo":{"name":"listen-test","version":"1"}}}`},
		modernExchange("server/discover", 1, "server/discover", "", ""),
	} {
		t.Run(exchange.name, func(t *testing.T) {
			served := startMCPExchange(t, handler, raw, exchange)
			if !served.answeredBy(time.Now().Add(answerBound)) {
				served.abandon(t)
				t.Fatalf("still open after %v", answerBound)
			}
			var response struct {
				Result struct {
					Capabilities struct {
						Tools map[string]any `json:"tools"`
					} `json:"capabilities"`
				} `json:"result"`
			}
			if err := json.Unmarshal(served.recorder.Body.Bytes(), &response); err != nil {
				t.Fatalf("decode %d %q: %v", served.recorder.Code, served.recorder.Body.String(), err)
			}
			tools := response.Result.Capabilities.Tools
			if tools == nil {
				t.Fatalf("capabilities omit tools: %s", served.recorder.Body.String())
			}
			if listChanged, ok := tools["listChanged"]; ok {
				t.Fatalf("tools capability advertises listChanged=%v", listChanged)
			}
		})
	}
}

func TestServer_ToolListSubscriptionsCannotExhaustRequestSlots(t *testing.T) {
	handler, raw := newStaticMCPHandler(t)
	subscriptions := make([]*servedExchange, 4)
	for i := range subscriptions {
		subscriptions[i] = startMCPExchange(t, handler, raw, everySubscription(20+i))
	}
	deadline := time.Now().Add(answerBound)
	open := 0
	for _, served := range subscriptions {
		if !served.answeredBy(deadline) {
			open++
		}
	}
	if open > 0 {
		t.Errorf("%d of 4 subscriptions/listen requests still open after %v", open, answerBound)
	}

	list := startMCPExchange(t, handler, raw, modernExchange("tools/list", 30, "tools/list", "", ""))
	if !list.answeredBy(time.Now().Add(answerBound)) {
		list.abandon(t)
		t.Fatalf("tools/list still open after %v", answerBound)
	}
	if list.recorder.Code != http.StatusOK || !strings.Contains(list.recorder.Body.String(), `"tools":[`) {
		t.Fatalf("tools/list after four subscriptions = %d %q", list.recorder.Code, list.recorder.Body.String())
	}
}
