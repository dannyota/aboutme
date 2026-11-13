package auth

type rejectReason int

const (
	reasonUnspecified rejectReason = iota

	reasonTxCookieMissing
	reasonTxInvalid
	reasonStateMismatch
	reasonConsentDenied
	reasonAuthorizationCodeMissing
	reasonTokenExchangeFailed
	reasonIDTokenMissing
	reasonIDTokenVerificationFailed
	reasonNonceMismatch
	reasonIDTokenClaimsDecodeFailed
	reasonEmailNotVerified
	reasonEmailAlreadyRegistered
	reasonLinkIdentityAlreadyClaimed
	reasonLinkOrReauthRejected

	reasonLinkedInRegistrationEmailUnverified
	reasonLinkedInProviderError

	reasonGitHubUserAPIFailed
	reasonGitHubUserIDMissing
	reasonGitHubUserEmailsAPIFailed
	reasonGitHubNoVerifiedPrimaryEmail

	reasonStartSessionRequired
	reasonStartReauthRequired
	reasonStartCSRFRejected
	reasonStartPurposeUnsupported
	reasonStartMethodNotAllowed
	reasonStartRateLimited
	reasonStartClientIPUnresolvable

	reasonStartSlugInvalid
	reasonStartViewProviderUnsupported
	reasonStartPageInvalid

	numRejectReasons
)

const unspecifiedReasonToken = "unspecified"

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
	reasonLinkedInProviderError:               "linkedin_provider_error",
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
	reasonStartSlugInvalid:                    "start_slug_invalid",
	reasonStartViewProviderUnsupported:        "start_view_provider_unsupported",
	reasonStartPageInvalid:                    "start_page_invalid",
}

func (r rejectReason) String() string {
	if token, ok := rejectReasonTokens[r]; ok {
		return token
	}
	return unspecifiedReasonToken
}
