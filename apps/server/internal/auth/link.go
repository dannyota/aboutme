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
// (start.go's handleLinkStart, from the session RequireSession already
// authenticated) -- tx.LinkingUserID is the resulting, trusted user id.
// A /callback can never be reached with a forged or substituted
// LinkingUserID: it isn't a request parameter at all, only a column on
// the server-side oauth_transactions row Consume looks up by the opaque,
// single-use __Host-oauth-tx handle (the same RFC 9700 mix-up defense
// transaction.go's own Consume already documents for cross-provider
// confusion, applied here to cross-USER confusion instead).
//
// Trusted-at-Begin is necessary but not sufficient, though: the account
// could have revoked every session (DELETE /api/v1/sessions, "log out
// everywhere") in the window between Begin and this /callback, up to
// oauthTxTTL=10 minutes later. resolveLinkOrReauth's own
// authenticateLinkOrReauthSession therefore ALSO re-authenticates the
// completing request's own __Host-session cookie and requires it still
// name tx.LinkingUserID, for both purpose=link and purpose=reauth alike --
// Begin-time trust establishes WHO asked; this /callback-time check
// establishes that the same authenticated session is still live now.

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/dannyota/aboutme/apps/server/internal/store"
)

// ==== What gates a link/reauth flow, and where that gate now lives ====
//
// Nothing in THIS file gates the start of a link/reauth flow any more.
// P1.1 item 2 (docs/plans/phase-1-deferred.md) moved that start to
// POST /auth/{provider}/start, behind RequireSession + RequireCSRF, and
// start.go owns it end to end -- including the recent-reauth gate on
// purpose=link, which still runs at /start so a stale caller never
// creates a transaction row. See start.go's own top-of-file comment for
// why a CSRF-protected POST replaced DD-C16's same-site check on a GET,
// and why the GET now refuses those two purposes outright rather than
// merely refusing them cross-site.
//
// What remains here is the /callback half, which never had a start-side
// gate to lose: resolveLinkOrReauth below re-authenticates the completing
// request's own session against tx.LinkingUserID, because Begin-time
// trust establishes WHO asked and only this check establishes that the
// same authenticated session is still live now.

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
//     tx.LinkingUserID (authenticateLinkOrReauthSession's own
//     defense-in-depth, shared by BOTH purposes: the flow started under
//     one session, up to oauthTxTTL=10 minutes ago, and something about
//     it has since changed -- expired, been revoked (including via
//     DELETE /api/v1/sessions, "log out everywhere" -- see that
//     function's own doc comment for the gap this closes), or a
//     different user is now using this browser).
var errLinkOrReauthRejected = errors.New("auth: link/reauth transaction rejected")

// resolveLinkOrReauth resolves a purpose=link or purpose=reauth /callback
// for provider/providerUserID against tx (already consumed by
// TransactionStore.Consume, so tx.Purpose is one of these two and
// tx.LinkingUserID is trusted -- see this file's own top-of-file doc
// comment for why). r and w are needed for authenticateLinkOrReauthSession
// below -- the ONE authorization check both purposes share (fix: an
// earlier version of this function gave purpose=link and purpose=reauth
// two DIFFERENT trust models -- reauth re-authenticated the completing
// request's own __Host-session cookie and required it to still name
// tx.LinkingUserID; link trusted tx.LinkingUserID alone, resolving
// entirely from the Begin-time value with no re-check at /callback. That
// asymmetry meant DELETE /api/v1/sessions ("log out everywhere") --
// which revokes every session row but never touches oauth_transactions --
// left a PENDING purpose=link transaction fully able to complete for up
// to oauthTxTTL=10 minutes AFTER a visitor used their account's own
// recovery control, permanently attaching a provider identity with no
// unlink endpoint in v1 to undo it). Both purposes now call the exact
// same check, unconditionally, before either branch below does anything
// else -- there is no second, weaker path left for that asymmetry to
// reappear at.
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
//  0. authenticateLinkOrReauthSession(tx.LinkingUserID): the completing
//     request's own __Host-session cookie must still authenticate, and
//     still name tx.LinkingUserID, or the whole attempt is rejected
//     (errLinkOrReauthRejected) before either step below runs.
//  1. GetIdentityByProviderSubject(provider, providerUserID):
//     - found, belongs to tx.LinkingUserID already: idempotent success
//     (no-op) for purpose=link; for purpose=reauth, refresh
//     reauthenticated_at (SessionManager.TouchReauthenticated) on the
//     session step 0 just authenticated.
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
	sess, err := s.authenticateLinkOrReauthSession(r, w, tx.LinkingUserID)
	if err != nil {
		return err
	}

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
			if touchErr := s.sessionMgr.TouchReauthenticated(ctx, sess.ID); touchErr != nil {
				return fmt.Errorf("auth: resolve link or reauth: touch reauthenticated: %w", touchErr)
			}
			return nil
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

// authenticateLinkOrReauthSession re-authenticates the request completing
// THIS /callback against linkingUserID (tx.LinkingUserID) -- the ONE
// authorization check resolveLinkOrReauth's purpose=link and
// purpose=reauth branches now share (see that function's own doc comment
// for the gap this closes). TouchReauthenticated takes a concrete session
// id, and oauth_transactions carries no session-id column (only
// linking_user_id, a bare user id -- a user can hold many concurrent
// sessions across devices, so "the session to touch" cannot be recovered
// from tx alone); the __Host-session cookie on THIS request is the one and
// only place that session id can come from -- /callback is a public route
// (reachable by an anonymous purpose=login visitor too, so it can never be
// wrapped in route-level RequireSession the way sessionChain's JSON API
// is), but the SAME browser that began this purpose=link/reauth flow at
// POST /start (RequireSession-wrapped there -- start.go) is, in the
// ordinary case, still carrying that exact session cookie when
// the provider redirects it back here, up to oauthTxTTL=10 minutes later.
//
// Re-authenticates independently (readAndAuthenticateSession, the same
// helper RequireSession itself calls) rather than trusting linkingUserID
// alone, and verifies the re-authenticated session's own UserID still
// equals linkingUserID before returning it -- defense-in-depth against the
// narrow window where the two could have diverged (the session was
// revoked -- including by DELETE /api/v1/sessions in between /start and
// this /callback -- rotated past its grace window, or -- a shared browser
// -- a different user is now signed in). Any failure of either check is
// errLinkOrReauthRejected, the same generic, no-oracle code as this
// package's other link/reauth rejections, since distinguishing "no
// cookie" from "wrong user" from "session expired" here would hand an
// observer more than DD-C3's contract allows anywhere else in this
// package.
//
// A rotated successor cookie is set as soon as Authenticate reports one --
// BEFORE the sess.UserID comparison below, not after (fix: the previous
// ordering here discarded an already-minted, already-persisted successor
// token on a mismatch, silently stranding the browser on a dead
// predecessor once its rotation grace window lapsed, even though the
// rotation write itself had already durably happened). Whether to honor
// THIS request's authorization is independent of whether to tell the
// browser about a credential the database already committed to.
func (s *Service) authenticateLinkOrReauthSession(r *http.Request, w http.ResponseWriter, linkingUserID uuid.UUID) (store.Session, error) {
	sess, rotated, err := readAndAuthenticateSession(r, s.sessionMgr)
	if rotated != "" {
		SetSessionCookie(w, rotated)
	}
	if err != nil {
		if errors.Is(err, ErrSessionInvalid) {
			return store.Session{}, errLinkOrReauthRejected
		}
		return store.Session{}, fmt.Errorf("auth: authenticate link or reauth: %w", err)
	}
	if sess.UserID != linkingUserID {
		return store.Session{}, errLinkOrReauthRejected
	}
	return sess, nil
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
			reasonLinkIdentityAlreadyClaimed)
	case errors.Is(err, errLinkOrReauthRejected):
		s.redirectAuthFailed(w, r, provider, purpose,
			reasonLinkOrReauthRejected)
	default:
		s.writeInternalError(w, r, provider, "resolve_link_or_reauth", err)
	}
}
