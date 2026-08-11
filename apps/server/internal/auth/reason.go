package auth

// reason.go defines the CLOSED, compile-checked vocabulary of
// operator-facing reasons this package's auth funnel logs (P1.1 item 3,
// docs/plans/phase-1-deferred.md). It is the log-side counterpart to
// handlers.go's closed ?error= vocabulary, and the two are deliberately
// asymmetric: the ?error= codes are what the BROWSER is allowed to learn
// (DD-C3 collapses almost everything into one generic auth_failed
// precisely so a caller gets no oracle), while these reasons are what an
// OPERATOR needs to tell those collapsed cases apart afterwards.
//
// Why a distinct type rather than the free-text strings this replaces:
// P9A is this project's first contact with a real IdP. A systematic
// misconfiguration -- clock skew against the issuer, an issuer mismatch, a
// redirect_uri that does not byte-match what was registered -- presents to
// every single user as the same opaque ?error=auth_failed. The only signal
// an operator has is the Warn record's reason attribute, so it must be
// something a log pipeline can group, count, and alert on. A hand-written
// sentence at each call site is not: two call sites for one cause drift
// apart, a reworded string silently breaks a saved query, and nothing
// stops a future caller inventing a fourteenth phrasing.
//
// Why an integer enum rather than a named string type: a named string type
// does NOT close the vocabulary. Go converts an untyped string constant
// implicitly, so `logRejection(..., "whatever I feel like")` would still
// compile against a `type rejectReason string` parameter. An integer type
// admits no string literal at all, so every reason a call site can pass is
// necessarily one declared below -- which is what "compile-checked" has to
// mean to be worth anything.
//
// Adding a reason is a deliberate, reviewed act, exactly like adding an
// ?error= code: declare the constant in the block below, give it a token
// in rejectReasonTokens, and TestRejectReason_VocabularyIsClosedAndStable
// (reason_test.go) enforces that both halves happened.

// rejectReason identifies WHY the auth funnel rejected a request, for the
// server-side log only. It never reaches a client: the browser sees the
// ?error= code alone (handlers.go's closed vocabulary), which is coarser
// on purpose.
type rejectReason int

// The closed vocabulary. Values are deliberately unexported and carry no
// meaning outside this process -- rejectReasonTokens below maps each to
// the stable string an operator actually sees, and THAT is the compatible
// surface (renaming a constant is free; changing its token breaks a saved
// query, so treat tokens as the contract).
//
// reasonUnspecified is the zero value and is never a legitimate reason:
// it exists so a rejectReason that was never assigned renders as a
// visible placeholder rather than an empty attribute an operator would
// read as a missing field.
const (
	reasonUnspecified rejectReason = iota

	// ---- Shared /callback funnel, every provider.
	reasonTxCookieMissing            // no __Host-oauth-tx cookie, or it is malformed
	reasonTxInvalid                  // Consume rejected the handle: unknown, expired, replayed, or wrong provider
	reasonStateMismatch              // the OAuth state parameter is absent or differs from the transaction's
	reasonConsentDenied              // the provider itself reported ?error=access_denied (RFC 6749 §4.1.2.1)
	reasonAuthorizationCodeMissing   // neither a code nor a recognized error parameter came back
	reasonTokenExchangeFailed        // the authorization-code exchange failed at the provider
	reasonIDTokenMissing             // the token response carried no id_token
	reasonIDTokenVerificationFailed  // signature, issuer, audience, or expiry check failed
	reasonNonceMismatch              // the id_token's nonce is absent or differs from the transaction's
	reasonIDTokenClaimsDecodeFailed  // the id_token verified but its claims would not decode
	reasonEmailNotVerified           // design spec §3's Google rule: email absent, or email_verified != true
	reasonEmailAlreadyRegistered     // the verified email already belongs to an account reached another way
	reasonLinkIdentityAlreadyClaimed // DD-C15: the identity belongs to a user other than the linking one
	reasonLinkOrReauthRejected       // reauth against an unclaimed identity, or the completing session no longer matches

	// ---- LinkedIn-specific.
	reasonLinkedInRegistrationEmailUnverified // AC-AUTH-002: registration (not linking) needs a verified email

	// ---- GitHub-specific.
	reasonGitHubUserAPIFailed          // GET /user: non-200, network error, or malformed body
	reasonGitHubUserIDMissing          // GET /user succeeded but carried no id
	reasonGitHubUserEmailsAPIFailed    // GET /user/emails: non-200, network error, or malformed body
	reasonGitHubNoVerifiedPrimaryEmail // design spec §3's GitHub rule: verified primary only

	// ---- /start's own rejections, P1.1 item 1.
	reasonStartSessionRequired      // link/reauth start with no valid __Host-session cookie
	reasonStartReauthRequired       // link start whose session's last full OAuth login is stale
	reasonStartCSRFRejected         // link/reauth start rejected by RequireCSRF (origin, token, or content type)
	reasonStartPurposeUnsupported   // a POST start asking for something other than link or reauth
	reasonStartMethodNotAllowed     // link/reauth attempted over GET (P1.1 item 2: those are POST-only), or an unsupported method
	reasonStartRateLimited          // the start routes' own per-(account, IP) budget is exhausted
	reasonStartClientIPUnresolvable // no trusted, unambiguous client IP, so the request could not even be keyed

	// numRejectReasons is not a reason: it bounds the declared range so
	// reason_test.go can walk every value and prove each has a token.
	numRejectReasons
)

// unspecifiedReasonToken is what a rejectReason outside the declared range
// renders as. Deliberately not "" and deliberately not a plausible cause:
// it means the code is at fault, not the request.
const unspecifiedReasonToken = "unspecified"

// rejectReasonTokens maps each declared reason to the stable, snake_case
// token an operator groups and alerts on. Keep these values stable:
// renaming a Go constant costs nothing, but changing a token silently
// breaks every saved query and alert built on it.
var rejectReasonTokens = map[rejectReason]string{
	reasonTxCookieMissing:                     "tx_cookie_missing",
	reasonTxInvalid:                           "tx_invalid",
	reasonStateMismatch:                       "state_mismatch",
	reasonConsentDenied:                       "consent_denied",
	reasonAuthorizationCodeMissing:            "authorization_code_missing",
	reasonTokenExchangeFailed:                 "token_exchange_failed",
	reasonIDTokenMissing:                      "id_token_missing",
	reasonIDTokenVerificationFailed:           "id_token_verification_failed",
	reasonNonceMismatch:                       "nonce_mismatch",
	reasonIDTokenClaimsDecodeFailed:           "id_token_claims_decode_failed",
	reasonEmailNotVerified:                    "email_not_verified",
	reasonEmailAlreadyRegistered:              "email_already_registered",
	reasonLinkIdentityAlreadyClaimed:          "link_identity_already_claimed",
	reasonLinkOrReauthRejected:                "link_or_reauth_rejected",
	reasonLinkedInRegistrationEmailUnverified: "linkedin_registration_email_unverified",
	reasonGitHubUserAPIFailed:                 "github_user_api_failed",
	reasonGitHubUserIDMissing:                 "github_user_id_missing",
	reasonGitHubUserEmailsAPIFailed:           "github_user_emails_api_failed",
	reasonGitHubNoVerifiedPrimaryEmail:        "github_no_verified_primary_email",
	reasonStartSessionRequired:                "start_session_required",
	reasonStartReauthRequired:                 "start_reauth_required",
	reasonStartCSRFRejected:                   "start_csrf_rejected",
	reasonStartPurposeUnsupported:             "start_purpose_unsupported",
	reasonStartMethodNotAllowed:               "start_method_not_allowed",
	reasonStartRateLimited:                    "start_rate_limited",
	reasonStartClientIPUnresolvable:           "start_client_ip_unresolvable",
}

// String returns r's stable operator-facing token, or
// unspecifiedReasonToken for the zero value and anything else outside the
// declared range.
//
// Every log call site must pass String()'s result, never a rejectReason
// directly: slog's JSON handler marshals an unrecognized value through
// encoding/json, which renders this integer-backed type as a bare number
// and loses the token. Being a Stringer is not enough there (the text
// handler would honor it; the JSON one this server uses would not).
func (r rejectReason) String() string {
	if token, ok := rejectReasonTokens[r]; ok {
		return token
	}
	return unspecifiedReasonToken
}
