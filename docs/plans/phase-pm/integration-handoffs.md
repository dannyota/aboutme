# Phase PM integration handoffs

Frozen producer/consumer interfaces. A change to a frozen name or type is an
owner-window change, not a worker edit.

## Store contract (T01 → T03–T09)

`apps/server/internal/store/oauth_contract.go` exports `OAuthQueries` with row
locking and transactional shapes for: client create/get/delete and GC
candidates; code insert/consume (locked, single-use)/replay lookup; grant
upsert/get-live/count-live/revoke; token insert/get-by-digest (joined with grant
and user)/supersede/revoke-family/touch-last-used. Digest columns are `BYTEA`
exactly 32 bytes. All list/cleanup queries are bounded.

## Primitives (T03 → T04–T08)

```go
package oauthsrv

func NewToken(kind TokenKind, entropy io.Reader) (raw string, digest [32]byte, err error)
func ParseToken(raw string) (kind TokenKind, digest [32]byte, err error)
func NewCode(entropy io.Reader) (raw string, digest [32]byte, err error)
func ParseCode(raw string) (digest [32]byte, err error)
func VerifyS256(challenge string, verifier string) bool
func ValidateRedirectURI(raw string) error
func ValidateClientName(raw string) (canonical string, err error)
func ParseScopes(raw string) (Scopes, error)
```

Closed errors: `ErrTokenInvalid`, `ErrCodeInvalid`, `ErrRedirectInvalid`,
`ErrClientNameInvalid`, `ErrScopeInvalid`. Errors carry no input text.

Digest convention (T03, frozen): the stored 32-byte digest is SHA-256 over
the canonical raw spelling — prefix included for tokens (`amat_…`/`amrt_…`),
bare 43 characters for codes — which domain-separates kinds. Consumers
obtain digests only through `NewToken`/`ParseToken`/`NewCode`/`ParseCode`,
never by hashing material themselves. A short entropy read surfaces the raw
`io` error, which endpoint tasks map to `server_error`.

## Service surfaces (T04/T05/T06 → T09)

```go
func (s *Service) HandleRegister(w http.ResponseWriter, r *http.Request)
func (s *Service) HandleAuthorize(w http.ResponseWriter, r *http.Request)
func (s *Service) ConsentContext(ctx context.Context, userID uuid.UUID, q ConsentQuery) (ConsentView, error)
func (s *Service) ConsentDecision(ctx context.Context, userID uuid.UUID, d ConsentDecision) (redirectTo string, err error)
func (s *Service) HandleToken(w http.ResponseWriter, r *http.Request)
func (s *Service) HandleRevoke(w http.ResponseWriter, r *http.Request)
func (s *Service) HandleMetadata(w http.ResponseWriter, r *http.Request)
func (s *Service) HandleProtectedResourceMetadata(w http.ResponseWriter, r *http.Request)
```

## Bearer boundary (T07 → T08/T09)

```go
package mcpapi

type Principal struct {
  UserID  uuid.UUID
  GrantID uuid.UUID
  TokenID uuid.UUID
  Scopes  oauthsrv.Scopes
}

func (b *Bearer) Authenticate(r *http.Request) (Principal, error)
func RequireScope(p Principal, s oauthsrv.Scope) error
```

Failure maps to the M7 closed 401/403 responses; `Authenticate` never reads
cookies.

## Tool registry (T08 → T12)

`mcpapi.NewServer(deps)` returns an `http.Handler` for `/mcp` serving the
fifteen M6 tools in stateless JSON mode with the M6 error vocabulary. Tool
input/output schemas are generated from Go structs; T12's spec drives the
endpoint with plain JSON-RPC fetches from Node.

## OpenAPI operations (T02 → T10/T11)

- `getOAuthConsent` — GET `/api/v1/oauth/consent` (query = M8 fields) →
  `{ clientName, scopes }`.
- `postOAuthConsentDecision` — POST `/api/v1/oauth/consent` (body = M8 fields +
  `decision`) → `{ redirectTo }`.
- `listAgentGrants` — GET `/api/v1/me/agents` →
  `{ grants: [{ id, clientName, scopes, createdAt, lastUsedAt }] }`.
- `revokeAgentGrant` — DELETE `/api/v1/me/agents/{grantId}` → 204.

All four use the existing session, CSRF (mutations), and exact-Origin chain and
the strict JSON envelope.

## Owner windows

1. T00: Approved v4 + ADR 0026 + budgets + traceability + public-roots v6.
2. T01: migration 00009 + sqlc regeneration.
3. T02: OpenAPI + generated client.
4. W4 entry: `go get github.com/modelcontextprotocol/go-sdk@v1.7.0`; inspect
   `go.mod`, `go.sum`, `go.work.sum`; then run `go mod tidy` (the hosted CI
   tidy-is-a-no-op gate requires canonical files) and inspect that diff too.
5. T09: config, `.env.example` names, `main.go` composition.
6. T12: Makefile, harness scripts, contract tests, AGENTS.md check row.
7. W8: records, review, exit gates.

No worker edits a path inside an open owner window. Full gates run once per
phase, never concurrently.
