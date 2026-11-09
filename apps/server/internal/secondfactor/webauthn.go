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

type credentialDescriptor struct {
	Type       string   `json:"type"`
	ID         string   `json:"id"`
	Transports []string `json:"transports,omitempty"`
}

type registrationPublicKey struct {
	Challenge string `json:"challenge"`
	RP        struct {
		Name string `json:"name"`
		ID   string `json:"id"`
	} `json:"rp"`
	User struct {
		ID          string `json:"id"`
		Name        string `json:"name"`
		DisplayName string `json:"displayName"`
	} `json:"user"`
	PubKeyCredParams []struct {
		Type string `json:"type"`
		Alg  int    `json:"alg"`
	} `json:"pubKeyCredParams"`
	Timeout                int                    `json:"timeout"`
	ExcludeCredentials     []credentialDescriptor `json:"excludeCredentials"`
	AuthenticatorSelection struct {
		ResidentKey        string `json:"residentKey"`
		RequireResidentKey bool   `json:"requireResidentKey"`
		UserVerification   string `json:"userVerification"`
	} `json:"authenticatorSelection"`
	Attestation string `json:"attestation"`
}

type assertionPublicKey struct {
	Challenge        string                 `json:"challenge"`
	Timeout          int                    `json:"timeout"`
	RPID             string                 `json:"rpId"`
	AllowCredentials []credentialDescriptor `json:"allowCredentials"`
	UserVerification string                 `json:"userVerification"`
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
