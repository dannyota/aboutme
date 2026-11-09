// Package secondfactor implements passkey and recovery-code second factors:
// the WebAuthn relying party, strict wire decoding, recovery codes, and the
// transactional factor service behind the auth second-factor routes. The
// contract is docs/design/passkey-second-factor-contract.md and the numeric
// limits are in docs/design/budgets.md.
package secondfactor

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/protocol/webauthncose"
	"github.com/go-webauthn/webauthn/webauthn"

	"github.com/dannyota/aboutme/apps/server/internal/auth"
	"github.com/dannyota/aboutme/apps/server/internal/store"
)

// WebAuthn decoded-byte bounds from docs/design/budgets.md.
const (
	challengeBytes          = 32
	ceremonyTokenBytes      = 32
	userHandleBytes         = 32
	maxUserHandleBytes      = 64
	minCredentialIDBytes    = 16
	maxCredentialIDBytes    = 1023
	maxPublicKeyBytes       = 2048
	maxClientDataBytes      = 4096
	maxAttestationBytes     = 16384
	maxAuthenticatorBytes   = 4096
	maxSignatureBytes       = 1024
	maxTransportHints       = 8
	maxTransportHintBytes   = 32
	ceremonyTimeoutMillis   = 300000
	relyingPartyDisplayName = "aboutme.vn"
)

// canonicalTransports is the closed transport set in its fixed order.
var canonicalTransports = []string{"usb", "nfc", "ble", "smart-card", "hybrid", "internal"}

var credentialParameters = []protocol.CredentialParameter{
	{Type: protocol.PublicKeyCredentialType, Algorithm: webauthncose.AlgES256},
	{Type: protocol.PublicKeyCredentialType, Algorithm: webauthncose.AlgRS256},
}

// RelyingParty is the single WebAuthn relying-party policy: the canonical
// origin's host as exact RP ID, the exact origin, required user verification,
// no cross-origin or top-origin ceremonies, and no extensions.
type RelyingParty struct {
	id     string
	origin string
	lib    *webauthn.WebAuthn
}

// NewRelyingParty derives the relying party from the canonical public origin.
// It accepts an https origin whose host is a DNS name, or an http or https
// origin on localhost. An IP literal, path, query, fragment, or user info is
// rejected, so an enabled enrollment flag fails startup closed.
func NewRelyingParty(publicOrigin string) (*RelyingParty, error) {
	u, err := url.Parse(publicOrigin)
	if err != nil || u.Opaque != "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" ||
		(u.Path != "" && u.Path != "/") || u.Host == "" {
		return nil, errors.New("secondfactor: public origin is not a plain origin")
	}
	host := u.Hostname()
	if host != strings.ToLower(host) || net.ParseIP(host) != nil || !validRelyingPartyHost(host) {
		return nil, errors.New("secondfactor: public origin host is not a valid relying party ID")
	}
	if u.Scheme != "https" && (u.Scheme != "http" || host != "localhost") {
		return nil, errors.New("secondfactor: public origin must use https")
	}
	origin := u.Scheme + "://" + u.Host
	lib, err := webauthn.New(&webauthn.Config{
		RPID:          host,
		RPDisplayName: relyingPartyDisplayName,
		RPOrigins:     []string{origin},
	})
	if err != nil {
		return nil, fmt.Errorf("secondfactor: relying party: %w", err)
	}
	return &RelyingParty{id: host, origin: origin, lib: lib}, nil
}

// ID returns the relying party ID.
func (rp *RelyingParty) ID() string {
	return rp.id
}

func validRelyingPartyHost(host string) bool {
	if host == "localhost" {
		return true
	}
	labels := strings.Split(host, ".")
	if len(host) > 253 || len(labels) < 2 {
		return false
	}
	for _, label := range labels {
		if label == "" || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for i := 0; i < len(label); i++ {
			c := label[i]
			if (c < 'a' || c > 'z') && (c < '0' || c > '9') && c != '-' {
				return false
			}
		}
	}
	return true
}

// ---- options ----

// credentialDescriptor is one WebAuthn credential descriptor in the options
// JSON.
type credentialDescriptor struct {
	// Type is always "public-key".
	Type string `json:"type"`
	// ID is the stored credential ID, unpadded base64url.
	ID string `json:"id"`
	// Transports lists the stored transport hints in canonical order; it is
	// omitted when none is stored.
	Transports []string `json:"transports,omitempty"`
}

// registrationPublicKey is the publicKey options member of a passkey
// registration ceremony.
type registrationPublicKey struct {
	// Challenge is the ceremony's 32-byte challenge, unpadded base64url.
	Challenge string `json:"challenge"`
	// RP names the relying party.
	RP struct {
		// Name is the relying-party display name.
		Name string `json:"name"`
		// ID is the relying-party ID, the canonical origin's host.
		ID string `json:"id"`
	} `json:"rp"`
	// User identifies the account that registers the passkey.
	User struct {
		// ID is the account's WebAuthn user handle, unpadded base64url.
		ID string `json:"id"`
		// Name is the account email.
		Name string `json:"name"`
		// DisplayName is the account display name.
		DisplayName string `json:"displayName"`
	} `json:"user"`
	// PubKeyCredParams lists the accepted credential algorithms in order of
	// preference: ES256, then RS256.
	PubKeyCredParams []struct {
		// Type is always "public-key".
		Type string `json:"type"`
		// Alg is the COSE algorithm identifier.
		Alg int `json:"alg"`
	} `json:"pubKeyCredParams"`
	// Timeout is the ceremony timeout in milliseconds.
	Timeout int `json:"timeout"`
	// ExcludeCredentials lists the account's active passkeys so an
	// authenticator that already holds one of them does not register again.
	ExcludeCredentials []credentialDescriptor `json:"excludeCredentials"`
	// AuthenticatorSelection requires a discoverable credential and user
	// verification.
	AuthenticatorSelection struct {
		// ResidentKey is always "required".
		ResidentKey string `json:"residentKey"`
		// RequireResidentKey is always true; it repeats ResidentKey for
		// WebAuthn Level 1 clients.
		RequireResidentKey bool `json:"requireResidentKey"`
		// UserVerification is always "required".
		UserVerification string `json:"userVerification"`
	} `json:"authenticatorSelection"`
	// Attestation is always "none".
	Attestation string `json:"attestation"`
}

// assertionPublicKey is the publicKey options member of a pending passkey
// assertion ceremony.
type assertionPublicKey struct {
	// Challenge is the ceremony's 32-byte challenge, unpadded base64url.
	Challenge string `json:"challenge"`
	// Timeout is the ceremony timeout in milliseconds.
	Timeout int `json:"timeout"`
	// RPID is the relying-party ID, the canonical origin's host.
	RPID string `json:"rpId"`
	// AllowCredentials lists the account's active passkeys; it is never
	// empty.
	AllowCredentials []credentialDescriptor `json:"allowCredentials"`
	// UserVerification is always "required".
	UserVerification string `json:"userVerification"`
}

func descriptors(credentials []store.WebauthnCredential) []credentialDescriptor {
	out := make([]credentialDescriptor, 0, len(credentials))
	for _, credential := range credentials {
		out = append(out, credentialDescriptor{
			Type:       string(protocol.PublicKeyCredentialType),
			ID:         base64.RawURLEncoding.EncodeToString(credential.CredentialID),
			Transports: canonicalTransportList(credential.Transports),
		})
	}
	return out
}

func (rp *RelyingParty) registrationOptions(challenge, handle []byte, email, displayName string, credentials []store.WebauthnCredential) (json.RawMessage, error) {
	var options registrationPublicKey
	options.Challenge = base64.RawURLEncoding.EncodeToString(challenge)
	options.RP.Name = relyingPartyDisplayName
	options.RP.ID = rp.id
	options.User.ID = base64.RawURLEncoding.EncodeToString(handle)
	options.User.Name = email
	options.User.DisplayName = displayName
	for _, param := range credentialParameters {
		options.PubKeyCredParams = append(options.PubKeyCredParams, struct {
			Type string `json:"type"`
			Alg  int    `json:"alg"`
		}{Type: string(param.Type), Alg: int(param.Algorithm)})
	}
	options.Timeout = ceremonyTimeoutMillis
	options.ExcludeCredentials = descriptors(credentials)
	options.AuthenticatorSelection.ResidentKey = string(protocol.ResidentKeyRequirementRequired)
	options.AuthenticatorSelection.RequireResidentKey = true
	options.AuthenticatorSelection.UserVerification = string(protocol.VerificationRequired)
	options.Attestation = string(protocol.PreferNoAttestation)
	return json.Marshal(options)
}

func (rp *RelyingParty) assertionOptions(challenge []byte, credentials []store.WebauthnCredential) (json.RawMessage, error) {
	if len(credentials) == 0 {
		return nil, auth.ErrSecondFactorNotFound
	}
	return json.Marshal(assertionPublicKey{
		Challenge:        base64.RawURLEncoding.EncodeToString(challenge),
		Timeout:          ceremonyTimeoutMillis,
		RPID:             rp.id,
		AllowCredentials: descriptors(credentials),
		UserVerification: string(protocol.VerificationRequired),
	})
}
