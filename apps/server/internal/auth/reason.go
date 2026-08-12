package auth

// Rejection reasons form a closed, stable vocabulary for operator logs. Client
// error codes are deliberately coarser to avoid exposing an authentication
// oracle. See docs/design/security.md.

// rejectReason identifies a server-side rejection cause. It never reaches the
// client. The integer type prevents callers from passing arbitrary strings.
type rejectReason int

// reasonUnspecified makes an unset reason visible in logs.
const (
	reasonUnspecified rejectReason = iota

	// Shared callback funnel.
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
	reasonEmailNotVerified           // Google registration email is absent or unverified
	reasonEmailAlreadyRegistered     // the verified email already belongs to an account reached another way
	reasonLinkIdentityAlreadyClaimed // the identity belongs to a user other than the linking one
	reasonLinkOrReauthRejected       // reauth against an unclaimed identity, or the completing session no longer matches

	// LinkedIn.
	reasonLinkedInRegistrationEmailUnverified // registration, not linking, needs a verified email

	// GitHub.
	reasonGitHubUserAPIFailed          // GET /user: non-200, network error, or malformed body
	reasonGitHubUserIDMissing          // GET /user succeeded but carried no id
	reasonGitHubUserEmailsAPIFailed    // GET /user/emails: non-200, network error, or malformed body
	reasonGitHubNoVerifiedPrimaryEmail // no verified primary email

	// Authorization start.
	reasonStartSessionRequired      // link/reauth start with no valid __Host-session cookie
	reasonStartReauthRequired       // link start whose session's last full OAuth login is stale
	reasonStartCSRFRejected         // link/reauth start rejected by RequireCSRF (origin, token, or content type)
	reasonStartPurposeUnsupported   // a POST start asking for something other than link or reauth
	reasonStartMethodNotAllowed     // link/reauth attempted over GET, or an unsupported method
	reasonStartRateLimited          // the start routes' own per-(account, IP) budget is exhausted
	reasonStartClientIPUnresolvable // no trusted, unambiguous client IP, so the request could not even be keyed

	// numRejectReasons bounds exhaustive token tests.
	numRejectReasons
)

// unspecifiedReasonToken marks a missing or invalid internal classification.
const unspecifiedReasonToken = "unspecified"

// rejectReasonTokens are stable because operator queries depend on them.
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

// String returns the stable log token. Callers must pass this string to slog;
// its JSON handler otherwise serializes the integer-backed type as a number.
func (r rejectReason) String() string {
	if token, ok := rejectReasonTokens[r]; ok {
		return token
	}
	return unspecifiedReasonToken
}
