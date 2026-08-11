package auth

// start.go owns the /api/v1/auth/{provider}/start surface: the two ways a
// flow can begin, and the route policy every provider's start shares.
// P1.1 items 1 and 2 (docs/plans/phase-1-deferred.md) are both
// implemented here.
//
// ==== The two starts, and why they are different methods ====
//
// GET /auth/{provider}/start is the LOGIN start, unchanged: an
// unauthenticated top-level browser navigation that must stay reachable
// from anywhere -- a bookmarked or shared "sign in" link, an email,
// another site's "continue with aboutme" button -- and so carries no
// session, CSRF, or same-site requirement of any kind. It answers with a
// 302 to the provider.
//
// POST /auth/{provider}/start?purpose=link|reauth is the LINK/REAUTH
// start (P1.1 item 2), and it answers with the authorize URL in the
// ordinary {"data":...} envelope instead of redirecting. It rides
// RequireSession -> RequireCSRF, the same chain every other mutating
// endpoint uses.
//
// This replaces DD-C16, which gated the same two purposes on a GET by
// requiring same-site initiation (Sec-Fetch-Site: same-origin, else an
// Origin/Referer check). DD-C16 was verified fail-closed and did close
// the attack it was written for -- an attacker page top-level-navigates
// the victim to /start?purpose=reauth, which refreshes the recent-reauth
// window with no interaction against an already-consented provider, then
// to /start?purpose=link, which now passes the very gate the first step
// refreshed. What it could not do is survive its own dependencies: it was
// a second authorization primitive parallel to the CSRF machinery, and it
// rested on Sec-Fetch-Site or a same-origin Referer surviving the edge.
// P8-sec's job is security headers, and a global
// `Referrer-Policy: no-referrer` -- standard hardening nobody would think
// to question -- would have silently broken linking for every browser
// without Fetch Metadata, failing closed in the most confusing possible
// place.
//
// The POST needs none of that. It is a mutating method, so RequireCSRF
// enforces exact-Origin plus the synchronizer token on it by the same
// rules as every other write; it needs no DD-C17 companion ruling about
// which rejections may be redirects, because an API call's rejection is
// just a JSON error; and P11's bearer client can use it unchanged,
// whereas DD-C16 rejected native clients by construction (a Custom Tab
// sends Sec-Fetch-Site: none).
//
// DD-C16's PROTECTION is kept, and strengthened, rather than dropped: the
// GET now refuses purpose=link and purpose=reauth outright, for every
// caller, same-site or not -- including the same-origin request DD-C16
// itself admitted. The chain DD-C16 existed to break cannot be assembled
// out of GETs at all, which is a stronger statement than "cannot be
// assembled cross-site", and it holds without depending on a header.
//
// ==== Route policy (P1.1 item 1) ====
//
// Every start route is wrapped in this package's own rate limiter and a
// rejection log, and every start reaps a bounded batch of expired
// oauth_transactions before creating its own. The master plan makes each
// phase the owner of its own routes' policies; P1 owns the auth routes,
// and until this, /start was bounded only by the global 300/min per-IP
// default, wrote a row per request that nothing ever removed, and logged
// nothing at all when it rejected a request.

import (
	"context"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/dannyota/aboutme/apps/server/internal/api"
	"github.com/dannyota/aboutme/apps/server/internal/store"
)

// Rate-limit policy for the start routes (P1.1 item 1). These are route
// policy, owned by this phase, not one of docs/plans/budgets.md's
// cross-phase numeric budgets.
//
// 30 requests per minute per (account, IP) is an order of magnitude below
// the whole-server default (api.DefaultRateLimitRequests, 300/min) and an
// order of magnitude above any real visitor: beginning a login is one
// request per deliberate click, and even a frustrated visitor retrying a
// failing provider does not approach this. It leaves room for a modest
// shared NAT -- several colleagues signing in at once from one office
// address share a bucket -- while capping what a single address can
// extract from the two things a start actually costs: a database INSERT
// on an unauthenticated route, and one more attempt at the
// ?error=email_already_registered account-existence oracle downstream at
// /callback (which cannot be probed faster than starts can be created,
// since every callback needs a transaction a start issued).
const (
	startRateLimitRequests = 30
	startRateLimitWindow   = time.Minute
)

// startReapBatch bounds how many expired oauth_transactions rows one
// start may delete. See DeleteExpiredOAuthTransactions (sql/queries.sql)
// for why the bound exists at all; 200 is comfortably more than one
// request's own share of arrivals at any plausible rate, so the table
// drains rather than merely holding steady, while still costing a
// constant.
const startReapBatch = 200

// authorizeURLBuilder is the provider-specific half of a start: it does
// whatever that provider needs (OIDC discovery for Google/LinkedIn, none
// for GitHub), creates the transaction row, and returns the raw
// __Host-oauth-tx handle plus the authorize URL to send the visitor to.
//
// op names the failing step for writeInternalError's operator log and is
// empty on success -- provider-specific, so it cannot be derived by the
// shared driver.
type authorizeURLBuilder func(ctx context.Context, purpose Purpose, linkingUserID uuid.UUID) (handle, authURL, op string, err error)

// startAuthorizeResponse is POST /start's success body inside the
// standard {"data":...} envelope. The client navigates the browser to
// authorizeUrl itself -- the server deliberately does not 302, because a
// 302 answering a fetch() would be followed by the fetch, not the
// browser, and the provider's consent screen has to be a real top-level
// navigation.
type startAuthorizeResponse struct {
	AuthorizeURL string `json:"authorizeUrl"`
}

// startRoute builds the complete handler registered at one provider's
// start path: method dispatch, this package's rate limit, the session and
// CSRF chain on the POST branch, and the rejection log around all of it.
//
// limit is passed in rather than built here so all three providers share
// ONE limiter instance (one bounded key store for the whole auth-start
// surface, not three that each get their own MaxKeys allowance).
//
// Middleware order on each branch, and why:
//
//	GET:  logStartRejections -> limit -> handleLoginStart
//	POST: logStartRejections -> RequireSession -> limit -> RequireCSRF -> handleLinkStart
//
// The limiter sits OUTSIDE everything on the GET branch: an anonymous
// caller has no account to key on, so the composite key degrades to its
// IP dimension, and rejecting before any work is exactly right.
//
// On the POST branch it sits INSIDE RequireSession, which is what makes
// the composite key's account dimension real (RequireSession publishes
// the authenticated account via api.WithAccountID). The cost is that an
// unauthenticated POST flood reaches RequireSession's session lookup
// bounded only by the global 300/min limiter -- the same exposure every
// other session-authenticated route in this server already has, not a new
// one. The gain is that a compromised or abusive ACCOUNT is bounded
// per-account, which a purely address-keyed limit cannot express at all
// once that account moves between addresses.
func (s *Service) startRoute(provider Provider, build authorizeURLBuilder, limit api.Middleware) http.Handler {
	login := limit(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.handleLoginStart(w, r, provider, build)
	}))
	link := RequireSession(s.sessionMgr)(limit(RequireCSRF(s.publicOrigin)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.handleLinkStart(w, r, provider, build)
	}))))

	return s.logStartRejections(provider)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			login.ServeHTTP(w, r)
		case http.MethodPost:
			link.ServeHTTP(w, r)
		default:
			// Exact-match dispatch, HEAD included (DD-C8): a start has a
			// real server-side side effect on every request that reaches
			// its handler, so a link-preview or prefetch crawler's HEAD
			// must never be treated as a bodyless GET.
			markStartRejection(r.Context(), reasonStartMethodNotAllowed)
			w.Header().Set("Allow", "GET, POST")
			api.WriteError(w, http.StatusMethodNotAllowed, "method_not_allowed",
				"method not allowed on "+r.URL.Path+"; use GET (login) or POST (link/reauth)")
		}
	}))
}

// startRateLimit builds the one api.RateLimit middleware instance every
// start route shares (P1.1 item 1).
//
// The key is composite -- account and IP, api.CompositeKeyFunc's own
// documented design-spec §3 shape -- so each (account, address) pair
// holds its own budget. On the GET login branch there is no account, so
// the key is effectively the address alone, which is the only dimension
// an anonymous request has. On the POST link/reauth branch both dimensions
// are real (see startRoute for why the limiter sits inside
// RequireSession).
//
// Requests/Window come from Service fields rather than the constants
// directly so a test can shrink the budget to a handful of requests
// (SetStartRateLimitForTest) instead of driving 30 real starts.
func (s *Service) startRateLimit() api.Middleware {
	return api.RateLimit(api.RateLimiterConfig{
		Requests:       s.startRateLimitRequests,
		Window:         s.startRateLimitWindow,
		TrustedProxies: s.trustedProxies,
		Key:            api.CompositeKeyFunc(api.AccountKeyFunc, api.IPKeyFunc),
		Logger:         s.logger,
	})
}

// handleLoginStart serves GET /auth/{provider}/start: an ordinary,
// unauthenticated login start.
//
// The ?purpose= parameter is no longer a way to reach a link or reauth
// flow (P1.1 item 2 -- see this file's own top-of-file comment): those two
// literals are refused with 405 and an Allow: POST, before any session
// read, any database access, and any cookie is set. Every OTHER value --
// absent, "login", or an unrecognized string -- is an ordinary login
// start, the same documented "(login default)" reading this route has
// always had.
//
// 405 rather than a silent downgrade to a login start: a caller that
// asked to link and got a login authorize URL back would send its visitor
// through a flow that does something else entirely, with a redirect
// chain that looks like it worked.
func (s *Service) handleLoginStart(w http.ResponseWriter, r *http.Request, provider Provider, build authorizeURLBuilder) {
	switch Purpose(r.URL.Query().Get("purpose")) {
	case PurposeLink, PurposeReauth:
		markStartRejection(r.Context(), reasonStartMethodNotAllowed)
		w.Header().Set("Allow", http.MethodPost)
		api.WriteError(w, http.StatusMethodNotAllowed, "method_not_allowed",
			"purpose=link and purpose=reauth require POST on "+r.URL.Path)
		return
	}

	ctx := withProviderHTTPClient(r.Context())
	s.reapExpiredOAuthTransactions(ctx, r, provider)

	handle, authURL, op, err := build(ctx, PurposeLogin, uuid.Nil)
	if err != nil {
		s.writeInternalError(w, r, provider, op, err)
		return
	}

	SetOAuthTxCookie(w, handle)
	http.Redirect(w, r, authURL, http.StatusFound)
}

// handleLinkStart serves POST /auth/{provider}/start?purpose=link|reauth
// (P1.1 item 2). RequireSession and RequireCSRF have already run, so the
// caller is authenticated and the request is proven same-origin and
// token-bearing; what is left is this route's own two rules.
//
//  1. The purpose must be exactly "link" or "reauth". This route exists
//     for those two and nothing else -- a login start is the GET's job --
//     so an absent or unrecognized value is a 400, never a quietly
//     substituted default. (This is the opposite ruling to the GET's
//     "(login default)", deliberately: the GET's default is the LEAST
//     privileged interpretation of an unknown value, while defaulting
//     here would hand back an authorize URL for a flow the caller did not
//     ask for.)
//  2. purpose=link requires a recent reauthentication (design spec §3's
//     compensating control for the long session timeouts), checked here
//     at /start and never deferred to /callback, so a stale caller's
//     attempt never creates a database row. purpose=reauth deliberately
//     has NO such gate -- its entire point is to re-establish recency, so
//     requiring recency first would be circular. That was the property
//     DD-C16 had to protect on a GET; on a CSRF-protected POST it needs
//     no special protection, because a cross-site page cannot make the
//     request at all.
//
// Both rejections are ordinary JSON errors rather than DD-C17's
// redirects: DD-C17 exists because a GET /start is a top-level browser
// navigation, and a raw JSON body would render as a document to a human.
// A POST from the settings page is a fetch() whose caller parses the
// envelope, so 403 reauth_required -- the exact code and shape Task 9's
// session-authenticated endpoints already return for the same condition
// (DD-C11) -- is both correct and one fewer response shape for a client
// to handle.
func (s *Service) handleLinkStart(w http.ResponseWriter, r *http.Request, provider Provider, build authorizeURLBuilder) {
	sess, ok := SessionFromContext(r.Context())
	if !ok {
		// Unreachable through RegisterRoutes' own wiring (RequireSession
		// populates this or rejects), but fail closed rather than panic if
		// a future refactor ever reorders the chain.
		markStartRejection(r.Context(), reasonStartSessionRequired)
		rejectSession(w)
		return
	}

	purpose := Purpose(r.URL.Query().Get("purpose"))
	if purpose != PurposeLink && purpose != PurposeReauth {
		markStartRejection(r.Context(), reasonStartPurposeUnsupported)
		api.WriteError(w, http.StatusBadRequest, "bad_request",
			"purpose must be link or reauth on POST "+r.URL.Path)
		return
	}

	if purpose == PurposeLink {
		if err := RequireRecentReauth(sess, s.sessionMgr.now()); err != nil {
			markStartRejection(r.Context(), reasonStartReauthRequired)
			api.WriteError(w, http.StatusForbidden, reauthRequiredCode, "recent reauthentication is required")
			return
		}
	}

	ctx := withProviderHTTPClient(r.Context())
	s.reapExpiredOAuthTransactions(ctx, r, provider)

	handle, authURL, op, err := build(ctx, purpose, sess.UserID)
	if err != nil {
		s.writeInternalError(w, r, provider, op, err)
		return
	}

	SetOAuthTxCookie(w, handle)
	api.WriteData(w, http.StatusOK, startAuthorizeResponse{AuthorizeURL: authURL})
}

// reapExpiredOAuthTransactions deletes a bounded batch of expired
// oauth_transactions rows before this start creates its own (P1.1 item
// 1). Best effort by design: a failure here is logged and the start
// proceeds, because the visitor's login must not fail over housekeeping
// for rows that are already dead. Dead-row cleanup at large -- the
// scheduled, global retention sweep -- remains P8-priv's; this exists so
// unauthenticated traffic cannot grow the table unchecked in the
// meantime.
func (s *Service) reapExpiredOAuthTransactions(ctx context.Context, r *http.Request, provider Provider) {
	if _, err := s.q.DeleteExpiredOAuthTransactions(ctx, store.DeleteExpiredOAuthTransactionsParams{
		Cutoff:  s.tx.now(),
		MaxRows: startReapBatch,
	}); err != nil {
		s.logInternalError(r, provider, "reap_oauth_transactions", err)
	}
}

// ==== /start rejection logging (P1.1 item 1) ==============================
//
// internal/api's Logging middleware already records every request's
// status and request id, so a rejected start was never invisible -- but a
// status code alone cannot distinguish a stale reauth window from a
// failed CSRF check from an exhausted rate limit, and it does not say
// which provider was being started. Those are exactly the distinctions an
// operator needs when link attempts start failing, and they are the
// counterpart to logRejection's existing coverage of the /callback half
// of the same funnel.

// startRejectionContextKey is an unexported context key type
// (google.github.io/styleguide/go/decisions#contexts), so the recorder
// below can never collide with a value stored elsewhere.
type startRejectionContextKey struct{}

// startRejectionRecord carries the precise reason an inner layer rejected
// a start, from that layer out to logStartRejections. It exists because
// the two most interesting rejections are written by SHARED middleware
// (RequireSession's 401, RequireCSRF's 403) that knows nothing about
// /start, while the rest are written by this file's own handlers -- and
// inferring all of them from the status code alone would conflate the
// handler's own 403 (stale reauth) with RequireCSRF's (cross-site).
//
// Written only by markStartRejection, on the request's own goroutine,
// before the response is written; read only after the whole chain has
// returned. No lock is needed and none would help: there is no
// concurrency here, only ordering.
type startRejectionRecord struct {
	reason rejectReason
	set    bool
}

// markStartRejection records why THIS start is being rejected, for
// logStartRejections to emit once the chain unwinds. A no-op if the
// request did not come through logStartRejections (a handler called
// directly from a test, say), so it is always safe to call.
func markStartRejection(ctx context.Context, reason rejectReason) {
	if rec, ok := ctx.Value(startRejectionContextKey{}).(*startRejectionRecord); ok {
		rec.reason, rec.set = reason, true
	}
}

// startStatusRecorder captures the status code the chain wrote, so
// logStartRejections can tell a rejection from a successful start without
// every layer having to report back.
//
// Unwrap is what keeps this wrapper honest: http.ResponseController
// reaches Flush/Hijack/SetWriteDeadline through wrapper layers only via
// Unwrap, and a naked `w.(http.Flusher)` assertion against a wrapper
// silently reports "not supported" rather than erroring. No start route
// streams today, so nothing here needs it yet -- which is exactly why it
// has to be written now, while the omission would be invisible.
type startStatusRecorder struct {
	http.ResponseWriter
	status int
}

// Unwrap returns the wrapped writer for http.ResponseController.
func (w *startStatusRecorder) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}

// WriteHeader records the first status written and forwards it.
func (w *startStatusRecorder) WriteHeader(status int) {
	if w.status == 0 {
		w.status = status
	}
	w.ResponseWriter.WriteHeader(status)
}

// Write records net/http's own implicit WriteHeader(200) for a handler
// that writes a body without setting a status, so an unrecorded status
// can never be mistaken for a rejection.
func (w *startStatusRecorder) Write(b []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK // net/http's own implicit WriteHeader(200).
	}
	return w.ResponseWriter.Write(b)
}

// logStartRejections wraps a whole start route and emits exactly one Warn
// record per rejected request, naming the provider and a typed reason
// (reason.go's closed vocabulary). Successful starts log nothing here --
// the access log already records them.
func (s *Service) logStartRejections(provider Provider) api.Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			rec := &startRejectionRecord{}
			recorder := &startStatusRecorder{ResponseWriter: w}

			next.ServeHTTP(recorder, r.WithContext(context.WithValue(r.Context(), startRejectionContextKey{}, rec)))

			if recorder.status < http.StatusBadRequest {
				return
			}
			reason := rec.reason
			if !rec.set {
				reason = startReasonForStatus(recorder.status)
			}
			if s.logger == nil {
				return
			}
			s.logger.WarnContext(r.Context(), "auth: start rejected",
				"request_id", api.RequestIDFromContext(r.Context()),
				"provider", provider,
				"status", recorder.status,
				"reason", reason.String(),
			)
		})
	}
}

// startReasonForStatus maps a rejection written by SHARED middleware --
// which cannot call markStartRejection, since it knows nothing about
// /start -- to this package's own vocabulary. Every status listed here is
// produced by exactly one layer of the start chain, so the mapping is
// unambiguous:
//
//   - 401: RequireSession, the only layer that writes one.
//   - 403: RequireCSRF. The handler's own 403 (stale reauth) never
//     reaches this function -- handleLinkStart marks it explicitly first.
//   - 429: api.RateLimit's budget rejection.
//   - 400: api.RateLimit's invalid_client_ip (the handler's own 400 for
//     an unsupported purpose is marked explicitly, like the 403 above).
//
// Anything else falls through to reasonUnspecified rather than being
// guessed at: a wrong reason in a log is worse than an obviously absent
// one.
func startReasonForStatus(status int) rejectReason {
	switch status {
	case http.StatusUnauthorized:
		return reasonStartSessionRequired
	case http.StatusForbidden:
		return reasonStartCSRFRejected
	case http.StatusTooManyRequests:
		return reasonStartRateLimited
	case http.StatusBadRequest:
		return reasonStartClientIPUnresolvable
	default:
		return reasonUnspecified
	}
}
