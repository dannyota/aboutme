package auth_test

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"go/ast"
	"go/parser"
	"go/token"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"github.com/google/uuid"

	"danny.vn/aboutme/apps/server/internal/auth"
	"danny.vn/aboutme/apps/server/internal/store"
)

func assertNoGitHubIdentity(t *testing.T, q *store.Queries, providerUserID string) {
	t.Helper()

	_, err := q.GetIdentityByProviderSubject(context.Background(), store.GetIdentityByProviderSubjectParams{
		Provider:       string(auth.ProviderGitHub),
		ProviderUserID: providerUserID,
	})
	if err == nil {
		t.Errorf("GetIdentityByProviderSubject(github, %q) unexpectedly found a row, want none (no identity row may be created on a rejected callback)", providerUserID)
	}
}

func assertNoLeakedParsingDetails(t *testing.T, body string) {
	t.Helper()

	forbidden := []string{"json", "unmarshal", "invalid character", "unexpected end", "malformed", "parse error", "syntax error", "eof"}
	lower := strings.ToLower(body)
	for _, word := range forbidden {
		if strings.Contains(lower, word) {
			t.Errorf("response body leaks a parsing-failure detail (contains %q): %q", word, body)
		}
	}
}

func randomIDAtLeast(t *testing.T, min int64) int64 {
	t.Helper()

	var buf [8]byte
	if _, err := rand.Read(buf[:]); err != nil {
		t.Fatalf("rand.Read: %v", err)
	}
	offset := int64(binary.BigEndian.Uint64(buf[:]) & 0x1FFFFFFF)
	return min + offset
}

func TestGitHubCallback_NoVerifiedPrimaryEmail(t *testing.T) {
	t.Parallel()

	githubID := uniqueGitHubUserID(t)
	unverifiedPrimaryEmail := uniqueEmail(t)
	unverifiedOtherEmail := uniqueEmail(t)
	gh := newGitHubStub(t,
		withTokenResponse("code-no-verified-primary", "access-token-no-verified-primary"),
		withUser(githubID, "octocat-no-verified-primary"),
		withEmails([]ghEmail{
			{Email: unverifiedPrimaryEmail, Primary: true, Verified: false},
			{Email: unverifiedOtherEmail, Primary: false, Verified: false},
		}),
	)
	handler, q := newTestService(t, withGitHubEndpoint(gh.URL))

	txCookie, state := beginGitHubFlow(t, handler)
	resp := doGitHubCallback(t, handler, "code-no-verified-primary", state, txCookie) //nolint:bodyclose // doGitHubCallback -> doGet closes the body itself before returning.

	errorCode := assertRejected(t, resp)
	if errorCode != "email_not_verified" {
		t.Errorf("error code = %q, want %q (same code as the OIDC providers)", errorCode, "email_not_verified")
	}

	assertNoGitHubIdentity(t, q, strconv.FormatInt(githubID, 10))
	assertNoUser(t, q, unverifiedPrimaryEmail)
	assertNoUser(t, q, unverifiedOtherEmail)
}

func TestGitHubCallback_MixUp_RejectsTransactionFromAnotherProvider(t *testing.T) {
	t.Parallel()

	gh := newGitHubStub(t)
	handler, q := newTestService(t, withGitHubEndpoint(gh.URL))

	ts := auth.NewTransactionStore(q)
	handle, tx, err := ts.Begin(context.Background(), auth.ProviderGoogle, auth.PurposeLogin, uuid.Nil,
		testPublicOrigin+"/api/v1/auth/google/callback")
	if err != nil {
		t.Fatalf("Begin(ProviderGoogle) error = %v", err)
	}

	googleTxCookie := requestCookie(auth.OAuthTxCookieName, handle)

	resp := doGitHubCallback(t, handler, "irrelevant-code-never-exchanged", tx.State, googleTxCookie) //nolint:bodyclose // doGitHubCallback -> doGet closes the body itself before returning.

	errorCode := assertRejected(t, resp)
	if errorCode != "auth_failed" {
		t.Errorf("error code = %q, want %q (ErrTransactionInvalid -- including a cross-provider mismatch -- funnels to the generic code, no oracle distinguishing it from any other rejected transaction)", errorCode, "auth_failed")
	}
}

type productionAuthFunc struct {
	file    string
	decl    *ast.FuncDecl
	imports map[string]string
}

func productionAuthFuncs(t *testing.T, dir string) map[string][]productionAuthFunc {
	t.Helper()

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read auth package directory %s: %v", dir, err)
	}

	funcs := make(map[string][]productionAuthFunc)
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go") {
			continue
		}

		path := filepath.Join(dir, entry.Name())
		parsed, parseErr := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if parseErr != nil {
			t.Fatalf("parse %s: %v", path, parseErr)
		}

		imports := make(map[string]string)
		for _, imp := range parsed.Imports {
			importPath, unquoteErr := strconv.Unquote(imp.Path.Value)
			if unquoteErr != nil {
				t.Fatalf("unquote import path %q in %s: %v", imp.Path.Value, path, unquoteErr)
			}
			name := filepath.Base(importPath)
			if imp.Name != nil {
				name = imp.Name.Name
			}
			imports[name] = importPath
		}

		for _, decl := range parsed.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			funcs[fn.Name.Name] = append(funcs[fn.Name.Name], productionAuthFunc{
				file:    path,
				decl:    fn,
				imports: imports,
			})
		}
	}
	return funcs
}

func isOIDCOrJWTImport(importPath string) bool {
	lower := strings.ToLower(importPath)
	return strings.Contains(lower, "oidc") || strings.Contains(lower, "jwt") || strings.Contains(lower, "go-jose")
}

func TestGitHubCallback_NoOIDCImportInPackage(t *testing.T) {
	t.Parallel()

	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller(0) failed to resolve this test file's own path")
	}
	funcs := productionAuthFuncs(t, filepath.Dir(thisFile))
	roots := funcs["handleGitHubCallback"]
	if len(roots) == 0 {
		t.Fatal("handleGitHubCallback not found; the static no-OIDC guard did not inspect its root")
	}
	for _, root := range roots {
		for _, importPath := range root.imports {
			if isOIDCOrJWTImport(importPath) {
				t.Errorf("%s imports %q, want the GitHub implementation file to have no OIDC/JWT dependency (AC-AUTH-003)", filepath.Base(root.file), importPath)
			}
		}
	}

	queue := []string{"handleGitHubCallback"}
	visited := make(map[*ast.FuncDecl]bool)
	visitedNames := make(map[string]bool)

	for len(queue) > 0 {
		name := queue[0]
		queue = queue[1:]
		for _, fn := range funcs[name] {
			if visited[fn.decl] {
				continue
			}
			visited[fn.decl] = true
			visitedNames[name] = true

			ast.Inspect(fn.decl.Body, func(node ast.Node) bool {
				switch value := node.(type) {
				case *ast.CallExpr:
					switch called := value.Fun.(type) {
					case *ast.Ident:
						if len(funcs[called.Name]) > 0 {
							queue = append(queue, called.Name)
						}
					case *ast.SelectorExpr:
						if len(funcs[called.Sel.Name]) > 0 {
							queue = append(queue, called.Sel.Name)
						}
						if called.Sel.Name == "Claims" || called.Sel.Name == "Verifier" {
							t.Errorf("%s: GitHub-reachable function %s calls %s, want no ID-token claim verification (AC-AUTH-003)", filepath.Base(fn.file), name, called.Sel.Name)
						}
					}
				case *ast.SelectorExpr:
					qualifier, ok := value.X.(*ast.Ident)
					if !ok {
						break
					}
					importPath := fn.imports[qualifier.Name]
					if isOIDCOrJWTImport(importPath) {
						t.Errorf("%s: GitHub-reachable function %s uses %q, want plain OAuth2 without OIDC/JWT dependencies (AC-AUTH-003)", filepath.Base(fn.file), name, fn.imports[qualifier.Name])
					}
				case *ast.Ident:
					lower := strings.ToLower(value.Name)
					if strings.Contains(lower, "idtoken") || strings.Contains(lower, "jwt") {
						t.Errorf("%s: GitHub-reachable function %s references %q, want no ID-token or JWT dependency (AC-AUTH-003)", filepath.Base(fn.file), name, value.Name)
					}
				case *ast.BasicLit:
					if value.Kind != token.STRING {
						break
					}
					literal, unquoteErr := strconv.Unquote(value.Value)
					if unquoteErr == nil && strings.EqualFold(literal, "id_token") {
						t.Errorf("%s: GitHub-reachable function %s reads %q, want no OIDC token dependency (AC-AUTH-003)", filepath.Base(fn.file), name, literal)
					}
				}
				return true
			})
		}
	}

	for _, name := range []string{"resolveLinkOrReauth", "resolveProviderLogin", "createProviderLogin", "redirectAuthFailed"} {
		if !visitedNames[name] {
			t.Errorf("GitHub callback guard did not reach shared helper %s; the static path check is incomplete", name)
		}
	}
}

func TestGitHubCallback_EmailSelection_PrimaryUnverified_DoesNotFallBackToVerifiedSecondary(t *testing.T) {
	t.Parallel()

	unverifiedPrimary := uniqueEmail(t)
	verifiedSecondary := uniqueEmail(t)
	githubID := uniqueGitHubUserID(t)
	gh := newGitHubStub(t,
		withTokenResponse("code-no-fallback", "access-token-no-fallback"),
		withUser(githubID, "octocat-no-fallback"),
		withEmails([]ghEmail{
			{Email: unverifiedPrimary, Primary: true, Verified: false},
			{Email: verifiedSecondary, Primary: false, Verified: true},
		}),
	)
	handler, q := newTestService(t, withGitHubEndpoint(gh.URL))

	txCookie, state := beginGitHubFlow(t, handler)
	resp := doGitHubCallback(t, handler, "code-no-fallback", state, txCookie) //nolint:bodyclose // doGitHubCallback -> doGet closes the body itself before returning.

	errorCode := assertRejected(t, resp)
	if errorCode != "email_not_verified" {
		t.Errorf("error code = %q, want %q", errorCode, "email_not_verified")
	}

	assertNoUser(t, q, verifiedSecondary)
	assertNoUser(t, q, unverifiedPrimary)
	assertNoGitHubIdentity(t, q, strconv.FormatInt(githubID, 10))
}

func TestGitHubCallback_EmailsAPIMalformedJSON_NoLeakedParsingDetails(t *testing.T) {
	t.Parallel()

	githubID := uniqueGitHubUserID(t)
	gh := newGitHubStub(t,
		withTokenResponse("code-emails-malformed-adversarial", "access-token-emails-malformed-adversarial"),
		withUser(githubID, "octocat-malformed-emails"),
		withEmailsAPIMalformedJSON(),
	)
	handler, q := newTestService(t, withGitHubEndpoint(gh.URL))

	txCookie, state := beginGitHubFlow(t, handler)
	path := auth.GitHubCallbackPath + "?code=" + url.QueryEscape("code-emails-malformed-adversarial") + "&state=" + url.QueryEscape(state)
	resp, body := doGetCaptureBody(t, handler, path, txCookie) //nolint:bodyclose // doGetCaptureBody (google_adversarial_test.go) closes the body itself before returning.

	errorCode := assertRejected(t, resp)
	if errorCode != "auth_failed" {
		t.Errorf("error code = %q, want %q (a provider-side GitHub REST failure is a rejection, not a 500)", errorCode, "auth_failed")
	}
	assertNoLeakedParsingDetails(t, body)
	assertNoGitHubIdentity(t, q, strconv.FormatInt(githubID, 10))
}

func TestGitHubCallback_NumericIDConvertsToStringProviderUserID(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		id   int64
	}{
		{
			name: "id beyond 2^32 (int32 truncation-shaped)",
			id:   randomIDAtLeast(t, 10_000_000_000),
		},
		{
			name: "id beyond float64's exact-integer precision (2^53)",
			id:   randomIDAtLeast(t, (1<<53)+1),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			email := uniqueEmail(t)
			gh := newGitHubStub(t,
				withTokenResponse("code-numeric-id", "access-token-numeric-id"),
				withUser(tt.id, "octocat-numeric-id"),
				withEmails([]ghEmail{{Email: email, Primary: true, Verified: true}}),
			)
			handler, q := newTestService(t, withGitHubEndpoint(gh.URL))

			txCookie, state := beginGitHubFlow(t, handler)
			resp := doGitHubCallback(t, handler, "code-numeric-id", state, txCookie) //nolint:bodyclose // doGitHubCallback -> doGet closes the body itself before returning.
			if resp.StatusCode != http.StatusFound {
				t.Fatalf("callback status = %d, want %d", resp.StatusCode, http.StatusFound)
			}
			if got := resp.Header.Get("Location"); got != testPublicOrigin+"/app/resumes" {
				t.Errorf("callback Location = %q, want %q (a successful login redirects to the resume list)", got, testPublicOrigin+"/app/resumes")
			}
			if extractCookie(resp, auth.SessionCookieName) == nil {
				t.Fatal("callback missing __Host-session cookie")
			}

			want := strconv.FormatInt(tt.id, 10)
			identity, err := q.GetIdentityByProviderSubject(context.Background(), store.GetIdentityByProviderSubjectParams{
				Provider:       string(auth.ProviderGitHub),
				ProviderUserID: want,
			})
			if err != nil {
				t.Fatalf("GetIdentityByProviderSubject(github, %s) error = %v, want an identity row with provider_user_id exactly %q "+
					"(pinned decimal string conversion -- no scientific notation, no trailing \".0\", no float64 precision loss)", want, err, want)
			}

			usr, err := q.GetUserByEmail(context.Background(), email)
			if err != nil {
				t.Fatalf("GetUserByEmail(%q) error = %v", email, err)
			}
			if identity.UserID != usr.ID {
				t.Errorf("identity.UserID = %v, want %v (the same created user)", identity.UserID, usr.ID)
			}
		})
	}
}

func TestGitHubCallback_ProviderAccessDenied_RedirectsCancelled(t *testing.T) {
	t.Parallel()

	gh := newGitHubStub(t)
	handler, _ := newTestService(t, withGitHubEndpoint(gh.URL))

	txCookie, state := beginGitHubFlow(t, handler)
	path := auth.GitHubCallbackPath + "?error=access_denied&state=" + url.QueryEscape(state)
	resp := doGet(t, handler, path, txCookie) //nolint:bodyclose // doGet (handlers_test.go) closes the body itself before returning.

	errorCode := assertRejected(t, resp)
	const wantErrorCode = "cancelled" //nolint:misspell // Exact wire value uses double-L "cancelled".
	if errorCode != wantErrorCode {
		t.Errorf("error code = %q, want %q (provider-declined consent maps to the same code for every provider)", errorCode, wantErrorCode)
	}
}
