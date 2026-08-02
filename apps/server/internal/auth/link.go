package auth

// link.go implements Task 10's link algorithm (task-10-brief.md, design
// decision 5, DD-C15): the ONE place a provider identity is ever attached
// to an ALREADY-authenticated user, and the ONE place a purpose=reauth
// round trip refreshes a session's reauthenticated_at. It replaces
// linkedin.go's interim DD-C12 safety net (resolveLinkedInUser's
// link/reauth arms), applying the same algorithm uniformly to every
// provider -- google (via handlers.go's handleGoogleCallback),
// github.go's handleGitHubCallback, and linkedin.go's
// handleLinkedInCallback all call resolveLinkOrReauth below, exactly the
// way all three call handlers.go's shared resolveLoginIdentity for a
// PurposeLogin transaction.
//
// Design spec §3: "Cross-provider linking is explicit (§3) and requires
// recent reauthentication ... Linking happens only from an authenticated
// session, never from the callback." The "authenticated session" that
// authorizes a link is captured ONCE, server-side, at Begin time
// (startPurposeAndLinkingUser below, called from every provider's own
// /start handler) -- tx.LinkingUserID is the resulting, trusted user id.
// A /callback can never be reached with a forged or substituted
// LinkingUserID: it isn't a request parameter at all, only a column on
// the server-side oauth_transactions row Consume looks up by the opaque,
// single-use __Host-oauth-tx handle (the same RFC 9700 mix-up defense
// transaction.go's own Consume already documents for cross-provider
// confusion, applied here to cross-USER confusion instead).

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/dannyota/aboutme/apps/server/internal/api"
	"github.com/dannyota/aboutme/apps/server/internal/store"
)

// ==== Why GET /start is never behind RequireCSRF -- and what DOES gate
// ==== purpose=link/reauth instead (DD-C16, Task 10 fix round 1, C1) ====
//
// Every /start route (Google/GitHub/LinkedIn, all three, all purposes) is
// registered as a bare GET via handlers.go's route() helper, never
// wrapped in sessionChain/RequireCSRF the way Task 9's JSON API is
// (RequireCSRF itself is a no-op on GET regardless -- csrf.go's
// isMutatingMethod). purpose=login must stay reachable from anywhere --
// a bookmarked or shared "sign in" link, an email, another site's
// "continue with aboutme" button -- so it carries no same-site
// requirement at all, and none of what follows applies to it.
//
// purpose=link/reauth is different, and the FIRST version of this
// comment (superseded here) got the risk assessment wrong: it argued
// RequireRecentReauth's 15-minute window "narrows the exploit window."
// That reasoning is circular -- Opus review's fix-round-1 finding C1
// traced the exact chain it missed: an attacker page top-level-navigates
// the victim (SameSite=Lax permits this for a GET -- design spec §3,
// "Lax because visitors arrive via external links") to
// /start?purpose=reauth FIRST. That request has no reauth gate of its
// own (reauth's whole point is to establish recency, not require it --
// see startPurposeAndLinkingUser's own doc comment), and against an
// already-consented provider it can complete with zero visible
// interaction, REFRESHING reauthenticated_at. The attacker page then
// navigates the victim to /start?purpose=link, which NOW passes
// RequireRecentReauth -- because the previous step just refreshed the
// very window that gate checks. A recent-reauth window can never gate an
// attack that itself refreshes the window; no amount of narrowing that
// window helps, because the attacker's own forced request is what resets
// the clock. The chain's remaining step (getting the victim's browser to
// complete a real provider consent grant for an identity the attacker
// controls) is a separate, real precondition, but the reauth-priming step
// removes the ONE thing this package could point to as already-mitigating
// it -- so this is not a bounded, accepted residual risk. It is a real
// gap, closed below.
//
// The fix (DD-C16) requires purpose=link/reauth's own /start request to
// be SAME-SITE INITIATED -- sameSiteInitiated (csrf.go), checked first,
// before any session read: Sec-Fetch-Site: same-origin when the browser
// sends it (any other value rejects outright, no fallback), else
// RequireCSRF's own Origin/Referer check (originAllowed, reused, not
// reimplemented). A cross-site top-level navigation -- exactly the
// attacker page's forced /start?purpose=reauth/link above -- now fails
// this check before Begin ever runs, regardless of the visitor's own
// session/reauth state, closing the chain at its first step rather than
// trying to harden a later one. Rejected with 403 csrf_rejected
// (rejectCSRF, csrf.go) -- this genuinely IS a cross-site-request
// rejection, not a new failure class needing its own vocabulary entry.
// purpose=login is deliberately untouched by this check (see above).

// errIdentityAlreadyLinked is resolveLinkOrReauth's sentinel for DD-C15's
// named rejection: a purpose=link or purpose=reauth transaction's
// provider identity (provider, providerUserID) already belongs to a user
// OTHER than tx.LinkingUserID. The HTTP layer maps this to
// identityAlreadyLinkedErrorCode (handlers.go), the one ?error= code
// Task 10 gives its own distinct wire value rather than collapsing into
// the generic auth_failed -- the actor here IS authenticated and the
// condition is one they can act on (unlink the identity from the other
// account first, or use a different provider identity), unlike DD-C3's
// no-oracle rejections. This is the case that prevents hijacking someone
// else's already-claimed provider identity by linking it onto your own
// account: no row is ever mutated on this path (the pre-existing
// identity's own row is only read, by its unique (provider,
// provider_user_id) index, never written), and no session is issued for
// EITHER user.
var errIdentityAlreadyLinked = errors.New("auth: identity already linked to a different user")

// errLinkOrReauthRejected is resolveLinkOrReauth's sentinel for every
// OTHER way a purpose=link/reauth callback fails to resolve, collapsed
// into one DD-C3-style no-oracle code (the generic authFailedErrorCode)
// exactly like every rejection this package doesn't give a plan-pinned
// distinct code:
//
//   - purpose=reauth against an UNCLAIMED identity: "no auto-link via
//     reauth" (design spec) -- reauth only ever refreshes an
//     ALREADY-linked provider's recency, never attaches a new one, so an
//     unclaimed identity here is simply rejected rather than falling
//     through to link's own CreateIdentity behavior.
//   - the request completing this /callback carries no valid
//     __Host-session cookie, or one that no longer authenticates as
//     tx.LinkingUserID (touchReauthenticatedForCurrentSession's own
//     defense-in-depth: the flow started under one session, up to
//     oauthTxTTL=10 minutes ago, and something about it has since
//     changed -- expired, been revoked, or a different user is now using
//     this browser). Unreachable for purpose=link's own identity
//     resolution (which never reads any session at all, resolving
//     purely off tx.LinkingUserID -- see resolveLinkOrReauth's own doc
//     comment), reachable only inside the purpose=reauth branch that
//     needs a concrete session row to call TouchReauthenticated on.
var errLinkOrReauthRejected = errors.New("auth: link/reauth transaction rejected")

// resolveLinkOrReauth resolves a purpose=link or purpose=reauth /callback
// for provider/providerUserID against tx (already consumed by
// TransactionStore.Consume, so tx.Purpose is one of these two and
// tx.LinkingUserID is trusted -- see this file's own top-of-file doc
// comment for why). r and w are needed ONLY for purpose=reauth's
// TouchReauthenticated call, which needs a concrete session ROW, not just
// tx.LinkingUserID's bare user id -- see touchReauthenticatedForCurrentSession's
// own doc comment for why that can only be read from the CURRENT request's
// own __Host-session cookie, never recovered from tx itself.
// purpose=link's own resolution below never reads r/w at all: it resolves
// purely off tx.LinkingUserID, exactly as design spec §3 intends ("Linking
// happens only from an authenticated session" -- the ONE captured at
// Begin, not whatever session happens to be current at /callback time).
//
// Deliberately takes NO email at all: a link/reauth attaches or
// reauthenticates purely by (provider, providerUserID) --
// users.email is never read, compared, or written by this function. Per
// spec, LinkedIn linking is allowed without a verified email, and nothing
// in the spec restricts linking a provider identity whose email differs
// from the account's registered email; every provider's own /callback
// handler skips its own email-verified requirement entirely for
// PurposeLink/PurposeReauth (see e.g. handleGoogleCallback's purpose
// branch, checked BEFORE the claims.EmailVerified gate) specifically so
// this function is never even handed one to (mis)use.
//
// Algorithm:
//
//  1. GetIdentityByProviderSubject(provider, providerUserID):
//     - found, belongs to tx.LinkingUserID already: idempotent success
//     (no-op) for purpose=link; for purpose=reauth, refresh
//     reauthenticated_at via touchReauthenticatedForCurrentSession.
//     - found, belongs to a DIFFERENT user: errIdentityAlreadyLinked
//     (DD-C15) for either purpose -- no row mutated.
//     - not found (unclaimed): purpose=link creates identities with
//     user_id = tx.LinkingUserID, NEVER a new user row.
//     purpose=reauth rejects (errLinkOrReauthRejected) -- "no auto-link
//     via reauth."
//
// A link flow never resolves to, or issues a session for, any user other
// than tx.LinkingUserID -- and on success this function itself never
// issues a session at all (the caller, handleGoogleCallback/
// handleGitHubCallback/handleLinkedInCallback, already has one; see each
// callback's own purpose branch, which skips SessionManager.Issue
// entirely for PurposeLink/PurposeReauth).
func (s *Service) resolveLinkOrReauth(ctx context.Context, r *http.Request, w http.ResponseWriter, tx Transaction, provider Provider, providerUserID string) error {
	identity, err := s.q.GetIdentityByProviderSubject(ctx, store.GetIdentityByProviderSubjectParams{
		Provider:       string(provider),
		ProviderUserID: providerUserID,
	})
	switch {
	case err == nil:
		if identity.UserID != tx.LinkingUserID {
			return errIdentityAlreadyLinked
		}
		if tx.Purpose == PurposeReauth {
			return s.touchReauthenticatedForCurrentSession(ctx, r, w, tx.LinkingUserID)
		}
		// purpose == PurposeLink, already linked to the SAME user:
		// idempotent no-op success -- a naive re-INSERT here would hit
		// identities_provider_subject_key's UNIQUE constraint; returning
		// straight to success instead means a repeated link attempt (a
		// double click, a retried request) never surfaces that as an
		// error.
		return nil
	case errors.Is(err, pgx.ErrNoRows):
		if tx.Purpose == PurposeReauth {
			// No auto-link via reauth (design spec): reauth only ever
			// refreshes an ALREADY-linked provider's recency, never
			// attaches a new one.
			return errLinkOrReauthRejected
		}
		if _, createErr := s.q.CreateIdentity(ctx, store.CreateIdentityParams{
			UserID:         tx.LinkingUserID,
			Provider:       string(provider),
			ProviderUserID: providerUserID,
		}); createErr != nil {
			if isUniqueViolation(createErr) {
				// Lost a race against a concurrent link attempt for this
				// exact (provider, providerUserID) between the
				// GetIdentityByProviderSubject check above and this INSERT
				// (fix round 1, I3) -- an entirely ORDINARY outcome from the
				// caller's perspective, not a server defect: re-read and
				// re-decide exactly like the already-found branch above,
				// rather than surfacing a raw 500 on what DD-C4 requires stay
				// a redirect.
				reread, getErr := s.q.GetIdentityByProviderSubject(ctx, store.GetIdentityByProviderSubjectParams{
					Provider:       string(provider),
					ProviderUserID: providerUserID,
				})
				if getErr != nil {
					return fmt.Errorf("auth: resolve link or reauth: get identity after race: %w", getErr)
				}
				if reread.UserID != tx.LinkingUserID {
					return errIdentityAlreadyLinked
				}
				return nil // idempotent success, same as the pre-race already-linked-to-self branch above.
			}
			return fmt.Errorf("auth: resolve link or reauth: create identity: %w", createErr)
		}
		return nil
	default:
		return fmt.Errorf("auth: resolve link or reauth: get identity: %w", err)
	}
}

// touchReauthenticatedForCurrentSession refreshes reauthenticated_at
// (SessionManager.TouchReauthenticated, Task 7) on the session
// authenticating the request completing THIS /callback -- the reason
// resolveLinkOrReauth needs r/w at all. TouchReauthenticated takes a
// concrete session id, and oauth_transactions carries no session-id
// column (only linking_user_id, a bare user id -- a user can hold many
// concurrent sessions across devices, so "the session to refresh" cannot
// be recovered from tx alone). The __Host-session cookie on THIS request
// is the one and only place that session id can come from: /callback is a
// public route (reachable by an anonymous purpose=login visitor too, so
// it can never be wrapped in route-level RequireSession the way
// sessionChain's JSON API is), but the SAME browser that began this
// purpose=reauth flow at /start (RequireSession-wrapped there, via
// startPurposeAndLinkingUser) is, in the ordinary case, still carrying
// that exact session cookie when the provider redirects it back here, up
// to oauthTxTTL=10 minutes later.
//
// Re-authenticates independently (readAndAuthenticateSession, the same
// helper RequireSession itself calls) rather than trusting tx.LinkingUserID
// alone, and verifies the re-authenticated session's own UserID still
// equals linkingUserID before touching anything -- defense-in-depth
// against the narrow window where the two could have diverged (the
// session was revoked, rotated past its grace window, or -- a shared
// browser -- a different user is now signed in) between /start and this
// /callback. Any failure of either check is errLinkOrReauthRejected, the
// same generic, no-oracle code as reauth's other rejection case, since
// distinguishing "no cookie" from "wrong user" from "session expired"
// here would hand an observer more than DD-C3's contract allows anywhere
// else in this package.
func (s *Service) touchReauthenticatedForCurrentSession(ctx context.Context, r *http.Request, w http.ResponseWriter, linkingUserID uuid.UUID) error {
	sess, rotated, err := readAndAuthenticateSession(r, s.sessionMgr)
	if err != nil {
		if errors.Is(err, ErrSessionInvalid) {
			return errLinkOrReauthRejected
		}
		return fmt.Errorf("auth: touch reauthenticated: authenticate session: %w", err)
	}
	if sess.UserID != linkingUserID {
		return errLinkOrReauthRejected
	}
	if rotated != "" {
		SetSessionCookie(w, rotated)
	}
	if err := s.sessionMgr.TouchReauthenticated(ctx, sess.ID); err != nil {
		return fmt.Errorf("auth: touch reauthenticated: %w", err)
	}
	return nil
}

// redirectLinkOrReauthError maps resolveLinkOrReauth's error taxonomy to
// the appropriate /callback response: errIdentityAlreadyLinked to its own
// distinct, plan-pinned code; errLinkOrReauthRejected to the generic
// no-oracle code; anything else (a wrapped database error) to the
// standard opaque 500, exactly like every other /callback resolve-step
// failure in this package (handleGoogleCallback's own resolve_login_identity
// case, etc.).
func (s *Service) redirectLinkOrReauthError(w http.ResponseWriter, r *http.Request, provider Provider, purpose Purpose, err error) {
	switch {
	case errors.Is(err, errIdentityAlreadyLinked):
		s.redirectWithError(w, r, provider, purpose, identityAlreadyLinkedErrorCode,
			"link/reauth identity already claimed by a different user")
	case errors.Is(err, errLinkOrReauthRejected):
		s.redirectAuthFailed(w, r, provider, purpose,
			"link/reauth transaction rejected (unclaimed identity via reauth, or the completing request's session no longer matches the linking user)")
	default:
		s.writeInternalError(w, r, provider, "resolve_link_or_reauth", err)
	}
}

// startPurposeAndLinkingUser resolves a /start request's ?purpose= query
// parameter (task-10-brief.md: "Start routes gain ?purpose=link|reauth
// handling (login default)") and, for purpose=link/reauth only,
// authenticates the caller's current session -- the one and only place
// tx.LinkingUserID is ever decided (see this file's own top-of-file doc
// comment on why that makes it trustworthy at /callback time).
//
// Any query value other than the literal "link" or "reauth" -- absent,
// "login", or an unrecognized string -- resolves to an ordinary,
// unauthenticated PurposeLogin start: the brief's own "(login default)"
// parenthetical, chosen deliberately over inventing a new, unreviewed
// "invalid purpose" error code for a bad value (this package's own
// documented convention, handlers.go's closed ?error= vocabulary comment:
// "A new distinct code is a deliberate, reviewed decision -- never
// something to add ad hoc at a new call site"). The safest, least
// surprising interpretation of a value nobody asked for is the one
// requiring no privilege and creating the least consequential transaction
// row.
//
// For purpose=link/reauth, in order: (1) sameSiteInitiated (csrf.go,
// DD-C16, fix round 1 C1) -- checked FIRST, before any session read or
// database access, rejecting with 403 csrf_rejected (rejectCSRF) on a
// cross-site request. This is the control that actually closes the
// reauth-then-link chain Opus review traced (see this file's top-of-file
// comment) -- neither of the checks below can substitute for it, since
// both operate on state an attacker's own forced request can manipulate
// same as a legitimate one. (2) reads and authenticates the __Host-session
// cookie via readAndAuthenticateSession (the same logic RequireSession
// itself uses -- see that function's own doc comment for why this cannot
// simply BE RequireSession, a route-level http.Handler middleware that
// cannot vary by a runtime query parameter the way this per-request
// dispatch does), responding exactly like RequireSession's own 401
// rejectSession on no/invalid session. (3) purpose=link additionally
// requires RequireRecentReauth (Task 7) BEFORE any transaction row is
// created, responding 403 reauthRequiredCode on a stale one --
// task-10-brief.md's binding requirement that this check happens at
// /start, never deferred to /callback, so a stale-reauth caller's attempt
// never even creates a database row. purpose=reauth deliberately has NO
// such gate: its entire point is to let a caller with a stale (or
// never-yet-established) recent reauthentication refresh it, so requiring
// a fresh one first would be circular -- DD-C16's same-site check above
// is what makes that safe now, closing the exact gap a reauth round trip
// with no reauth gate of its own used to leave open.
//
// ok reports whether the caller should proceed; on false the appropriate
// response has already been written and the caller must return
// immediately without ever calling TransactionStore.Begin.
func (s *Service) startPurposeAndLinkingUser(w http.ResponseWriter, r *http.Request) (purpose Purpose, linkingUserID uuid.UUID, ok bool) {
	switch Purpose(r.URL.Query().Get("purpose")) {
	case PurposeLink:
		purpose = PurposeLink
	case PurposeReauth:
		purpose = PurposeReauth
	default:
		return PurposeLogin, uuid.Nil, true
	}

	if !sameSiteInitiated(r, s.publicOrigin) {
		rejectCSRF(w)
		return "", uuid.Nil, false
	}

	sess, rotated, err := readAndAuthenticateSession(r, s.sessionMgr)
	if err != nil {
		if errors.Is(err, ErrSessionInvalid) {
			rejectSession(w)
			return "", uuid.Nil, false
		}
		api.WriteError(w, http.StatusInternalServerError, "internal_error", "an internal error occurred")
		return "", uuid.Nil, false
	}
	if rotated != "" {
		SetSessionCookie(w, rotated)
	}

	if purpose == PurposeLink {
		if err := RequireRecentReauth(sess, s.sessionMgr.now()); err != nil {
			api.WriteError(w, http.StatusForbidden, reauthRequiredCode, "recent reauthentication is required")
			return "", uuid.Nil, false
		}
	}

	return purpose, sess.UserID, true
}
