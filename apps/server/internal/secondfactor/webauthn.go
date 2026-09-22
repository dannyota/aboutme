// Package secondfactor implements passkey and recovery-code second factors:
// the WebAuthn relying party, strict wire decoding, recovery codes, and the
// transactional factor service behind the auth second-factor routes. The
// contract is docs/design/passkey-second-factor-contract.md and the numeric
// limits are in docs/design/budgets.md.
package secondfactor

import (
	"bytes"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"net/url"
	"strings"
	"unicode/utf8"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/protocol/webauthncose"
	"github.com/go-webauthn/webauthn/webauthn"
	"github.com/google/uuid"

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
	relyingPartyDisplayName = "aboutme"
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
	if u.Scheme != "https" && !(u.Scheme == "http" && host == "localhost") {
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

// ---- strict wire decoding ----

// passkeyRegistration is a decoded registration completion.
type passkeyRegistration struct {
	ceremonyDigest []byte
	response       protocol.CredentialCreationResponse
	transports     []string
}

// SecondFactorMethod names the passkey method.
func (*passkeyRegistration) SecondFactorMethod() string { return auth.SecondFactorMethodPasskey }

// passkeyAssertion is a decoded assertion completion.
type passkeyAssertion struct {
	ceremonyDigest []byte
	rawID          []byte
	response       protocol.CredentialAssertionResponse
}

// SecondFactorMethod names the passkey method.
func (*passkeyAssertion) SecondFactorMethod() string { return auth.SecondFactorMethodPasskey }

var errRequestInvalid = auth.ErrSecondFactorRequestInvalid

// decodeRegistration strictly decodes a registration completion body. It
// rejects invalid UTF-8, duplicate or unknown members, missing members,
// noncanonical base64url, oversized fields, and trailing data before any CBOR
// or signature work.
func decodeRegistration(body []byte) (*passkeyRegistration, error) {
	top, err := strictObject(body, "ceremonyId", "credential")
	if err != nil {
		return nil, err
	}
	digest, err := decodeCeremonyID(top["ceremonyId"])
	if err != nil {
		return nil, err
	}
	id, rawID, response, err := decodeCredentialEnvelope(top["credential"])
	if err != nil {
		return nil, err
	}
	fields, err := strictObject(response, "clientDataJSON", "attestationObject", "transports")
	if err != nil {
		return nil, err
	}
	clientData, err := base64Member(fields["clientDataJSON"], 1, maxClientDataBytes)
	if err != nil {
		return nil, err
	}
	attestation, err := base64Member(fields["attestationObject"], 1, maxAttestationBytes)
	if err != nil {
		return nil, err
	}
	transports, err := transportMember(fields["transports"])
	if err != nil {
		return nil, err
	}
	reg := &passkeyRegistration{ceremonyDigest: digest, transports: transports}
	reg.response.ID = id
	reg.response.Type = string(protocol.PublicKeyCredentialType)
	reg.response.RawID = rawID
	reg.response.AttestationResponse.ClientDataJSON = clientData
	reg.response.AttestationResponse.AttestationObject = attestation
	reg.response.AttestationResponse.Transports = transports
	return reg, nil
}

// decodeAssertion strictly decodes an assertion completion body under the same
// rules as decodeRegistration. userHandle is required and may be null.
func decodeAssertion(body []byte) (*passkeyAssertion, error) {
	top, err := strictObject(body, "ceremonyId", "credential")
	if err != nil {
		return nil, err
	}
	digest, err := decodeCeremonyID(top["ceremonyId"])
	if err != nil {
		return nil, err
	}
	id, rawID, response, err := decodeCredentialEnvelope(top["credential"])
	if err != nil {
		return nil, err
	}
	fields, err := strictObject(response, "clientDataJSON", "authenticatorData", "signature", "userHandle")
	if err != nil {
		return nil, err
	}
	clientData, err := base64Member(fields["clientDataJSON"], 1, maxClientDataBytes)
	if err != nil {
		return nil, err
	}
	authData, err := base64Member(fields["authenticatorData"], 1, maxAuthenticatorBytes)
	if err != nil {
		return nil, err
	}
	signature, err := base64Member(fields["signature"], 1, maxSignatureBytes)
	if err != nil {
		return nil, err
	}
	var userHandle []byte
	if !bytes.Equal(bytes.TrimSpace(fields["userHandle"]), []byte("null")) {
		if userHandle, err = base64Member(fields["userHandle"], 1, maxUserHandleBytes); err != nil {
			return nil, err
		}
	}
	a := &passkeyAssertion{ceremonyDigest: digest, rawID: rawID}
	a.response.ID = id
	a.response.Type = string(protocol.PublicKeyCredentialType)
	a.response.RawID = rawID
	a.response.AssertionResponse.ClientDataJSON = clientData
	a.response.AssertionResponse.AuthenticatorData = authData
	a.response.AssertionResponse.Signature = signature
	a.response.AssertionResponse.UserHandle = userHandle
	return a, nil
}

// decodeCredentialEnvelope checks the shared credential members and returns
// the id text, raw ID bytes, and the raw response object.
func decodeCredentialEnvelope(raw json.RawMessage) (string, []byte, json.RawMessage, error) {
	credential, err := strictObject(raw, "id", "rawId", "type", "response", "clientExtensionResults")
	if err != nil {
		return "", nil, nil, err
	}
	id, err := stringMember(credential["id"])
	if err != nil {
		return "", nil, nil, err
	}
	idBytes, err := canonicalBase64(id, minCredentialIDBytes, maxCredentialIDBytes)
	if err != nil {
		return "", nil, nil, err
	}
	rawID, err := base64Member(credential["rawId"], minCredentialIDBytes, maxCredentialIDBytes)
	if err != nil || !bytes.Equal(idBytes, rawID) {
		return "", nil, nil, errRequestInvalid
	}
	if kind, kindErr := stringMember(credential["type"]); kindErr != nil || kind != string(protocol.PublicKeyCredentialType) {
		return "", nil, nil, errRequestInvalid
	}
	// No extension is requested, so the client extension output must be empty.
	if _, extErr := strictObject(credential["clientExtensionResults"]); extErr != nil {
		return "", nil, nil, extErr
	}
	return id, rawID, credential["response"], nil
}

// strictObject decodes raw as exactly one JSON object whose member names are
// exactly names, with no duplicates and no trailing data.
func strictObject(raw []byte, names ...string) (map[string]json.RawMessage, error) {
	if !utf8.Valid(raw) {
		return nil, errRequestInvalid
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	if tok, err := dec.Token(); err != nil || tok != json.Delim('{') {
		return nil, errRequestInvalid
	}
	members := make(map[string]json.RawMessage, len(names))
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			return nil, errRequestInvalid
		}
		key, ok := tok.(string)
		if !ok {
			return nil, errRequestInvalid
		}
		if _, dup := members[key]; dup {
			return nil, errRequestInvalid
		}
		var value json.RawMessage
		if err = dec.Decode(&value); err != nil {
			return nil, errRequestInvalid
		}
		members[key] = value
	}
	if tok, err := dec.Token(); err != nil || tok != json.Delim('}') {
		return nil, errRequestInvalid
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return nil, errRequestInvalid
	}
	if len(members) != len(names) {
		return nil, errRequestInvalid
	}
	for _, name := range names {
		if _, ok := members[name]; !ok {
			return nil, errRequestInvalid
		}
	}
	return members, nil
}

func stringMember(raw json.RawMessage) (string, error) {
	var value string
	if len(raw) == 0 || raw[0] != '"' || json.Unmarshal(raw, &value) != nil {
		return "", errRequestInvalid
	}
	return value, nil
}

func base64Member(raw json.RawMessage, minBytes, maxBytes int) ([]byte, error) {
	text, err := stringMember(raw)
	if err != nil {
		return nil, err
	}
	return canonicalBase64(text, minBytes, maxBytes)
}

// canonicalBase64 decodes unpadded base64url and requires the text to be the
// canonical encoding of the decoded bytes, which rejects padding, line breaks,
// and nonzero trailing bits.
func canonicalBase64(text string, minBytes, maxBytes int) ([]byte, error) {
	if len(text) > base64.RawURLEncoding.EncodedLen(maxBytes) {
		return nil, errRequestInvalid
	}
	decoded, err := base64.RawURLEncoding.Strict().DecodeString(text)
	if err != nil || len(decoded) < minBytes || len(decoded) > maxBytes ||
		base64.RawURLEncoding.EncodeToString(decoded) != text {
		return nil, errRequestInvalid
	}
	return decoded, nil
}

// decodeCeremonyID returns the SHA-256 digest of the 32-byte ceremony token.
func decodeCeremonyID(raw json.RawMessage) ([]byte, error) {
	token, err := base64Member(raw, ceremonyTokenBytes, ceremonyTokenBytes)
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(token)
	return sum[:], nil
}

// transportMember decodes the required transport hint array. Hints never
// grant authority; unknown hints are dropped and known ones are returned in
// the fixed canonical order without duplicates.
func transportMember(raw json.RawMessage) ([]string, error) {
	var items []json.RawMessage
	if len(raw) == 0 || raw[0] != '[' || json.Unmarshal(raw, &items) != nil || len(items) > maxTransportHints {
		return nil, errRequestInvalid
	}
	hints := make([]string, 0, len(items))
	for _, item := range items {
		hint, err := stringMember(item)
		if err != nil || hint == "" || len(hint) > maxTransportHintBytes {
			return nil, errRequestInvalid
		}
		hints = append(hints, hint)
	}
	return canonicalTransportList(hints), nil
}

func canonicalTransportList(hints []string) []string {
	var out []string
	for _, known := range canonicalTransports {
		for _, hint := range hints {
			if hint == known {
				out = append(out, known)
				break
			}
		}
	}
	return out
}

// ---- verification ----

// verifiedPasskey is the public material and display hints of a proven new
// credential. No library credential object is stored.
type verifiedPasskey struct {
	credentialID   []byte
	publicKey      []byte
	signCount      uint32
	backupEligible bool
	backupState    bool
	transports     []string
}

type passkeyUser struct {
	handle      []byte
	name        string
	displayName string
	credentials []webauthn.Credential
}

// WebAuthnID returns the account's stable user handle.
func (u passkeyUser) WebAuthnID() []byte { return u.handle }

// WebAuthnName returns the canonical email shown during registration.
func (u passkeyUser) WebAuthnName() string { return u.name }

// WebAuthnDisplayName returns the account name shown during registration.
func (u passkeyUser) WebAuthnDisplayName() string { return u.displayName }

// WebAuthnCredentials returns the account's active credentials.
func (u passkeyUser) WebAuthnCredentials() []webauthn.Credential { return u.credentials }

// checkClientData applies the exact ceremony type, exact origin, no
// cross-origin or top-origin, and the stored challenge digest before any
// signature work.
func (rp *RelyingParty) checkClientData(clientData protocol.CollectedClientData, ceremony protocol.CeremonyType, challengeDigest []byte) error {
	if clientData.Type != ceremony || clientData.Origin != rp.origin || clientData.CrossOrigin || clientData.TopOrigin != "" {
		return auth.ErrSecondFactorVerificationFailed
	}
	challenge, err := canonicalBase64(clientData.Challenge, challengeBytes, challengeBytes)
	if err != nil {
		return auth.ErrSecondFactorVerificationFailed
	}
	sum := sha256.Sum256(challenge)
	if subtle.ConstantTimeCompare(sum[:], challengeDigest) != 1 {
		return auth.ErrSecondFactorVerificationFailed
	}
	return nil
}

// verifyRegistration proves a new credential against the ceremony's challenge
// digest and user handle.
func (rp *RelyingParty) verifyRegistration(reg *passkeyRegistration, challengeDigest, handle []byte, name, displayName string) (verifiedPasskey, error) {
	parsed, err := reg.response.Parse()
	if err != nil {
		return verifiedPasskey{}, auth.ErrSecondFactorVerificationFailed
	}
	clientData := parsed.Response.CollectedClientData
	if err = rp.checkClientData(clientData, protocol.CreateCeremony, challengeDigest); err != nil {
		return verifiedPasskey{}, err
	}
	session := webauthn.SessionData{
		Challenge: clientData.Challenge, RelyingPartyID: rp.id, UserID: handle,
		UserVerification: protocol.VerificationRequired, CredParams: credentialParameters,
	}
	user := passkeyUser{handle: handle, name: name, displayName: displayName}
	credential, err := rp.lib.CreateCredential(user, session, parsed)
	if err != nil {
		return verifiedPasskey{}, auth.ErrSecondFactorVerificationFailed
	}
	if len(credential.ID) < minCredentialIDBytes || len(credential.ID) > maxCredentialIDBytes ||
		len(credential.PublicKey) == 0 || len(credential.PublicKey) > maxPublicKeyBytes {
		return verifiedPasskey{}, auth.ErrSecondFactorVerificationFailed
	}
	return verifiedPasskey{
		credentialID: credential.ID, publicKey: credential.PublicKey, signCount: credential.Authenticator.SignCount,
		backupEligible: credential.Flags.BackupEligible, backupState: credential.Flags.BackupState,
		transports: reg.transports,
	}, nil
}

// assertionResult reports a verified assertion. counterRegressed marks a
// valid signature whose nonzero counter did not increase.
type assertionResult struct {
	received         uint32
	counterRegressed bool
	backupState      bool
}

// verifyAssertion proves an assertion for target, one of the account's active
// credentials, against the ceremony's challenge digest and the stored user
// handle. Every active credential is in the allowed list.
func (rp *RelyingParty) verifyAssertion(a *passkeyAssertion, challengeDigest, handle []byte, active []store.WebauthnCredential, target store.WebauthnCredential) (assertionResult, error) {
	parsed, err := a.response.Parse()
	if err != nil {
		return assertionResult{}, auth.ErrSecondFactorVerificationFailed
	}
	clientData := parsed.Response.CollectedClientData
	if err = rp.checkClientData(clientData, protocol.AssertCeremony, challengeDigest); err != nil {
		return assertionResult{}, err
	}
	user := passkeyUser{handle: handle}
	allowed := make([][]byte, 0, len(active))
	for _, stored := range active {
		allowed = append(allowed, stored.CredentialID)
		user.credentials = append(user.credentials, webauthn.Credential{
			ID: stored.CredentialID, PublicKey: stored.PublicKey,
			Flags: webauthn.CredentialFlags{
				UserPresent: true, UserVerified: true,
				BackupEligible: stored.BackupEligible, BackupState: stored.BackupState,
			},
			Authenticator: webauthn.Authenticator{SignCount: storedCounter(stored)},
		})
	}
	session := webauthn.SessionData{
		Challenge: clientData.Challenge, RelyingPartyID: rp.id, UserID: handle,
		AllowedCredentialIDs: allowed, UserVerification: protocol.VerificationRequired,
	}
	credential, err := rp.lib.ValidateLogin(user, session, parsed)
	if err != nil || !bytes.Equal(credential.ID, target.CredentialID) {
		return assertionResult{}, auth.ErrSecondFactorVerificationFailed
	}
	received := parsed.Response.AuthenticatorData.Counter
	stored := storedCounter(target)
	return assertionResult{
		received:         received,
		counterRegressed: (received != 0 || stored != 0) && received <= stored,
		backupState:      parsed.Response.AuthenticatorData.Flags.HasBackupState(),
	}, nil
}

// storedCounter narrows the database counter, which a check constraint keeps
// within the unsigned 32-bit range.
func storedCounter(credential store.WebauthnCredential) uint32 {
	if credential.SignCount < 0 || credential.SignCount > math.MaxUint32 {
		return math.MaxUint32
	}
	return uint32(credential.SignCount) //nolint:gosec // bounded to the uint32 range by the check above.
}

func (s *Service) random(n int) ([]byte, error) {
	b := make([]byte, n)
	if _, err := io.ReadFull(s.entropy, b); err != nil {
		return nil, fmt.Errorf("secondfactor: entropy: %w", err)
	}
	return b, nil
}

func sameID(a, b *uuid.UUID) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

func passkeyView(credential store.WebauthnCredential) auth.SecondFactorPasskey {
	return auth.SecondFactorPasskey{ID: credential.ID, CreatedAt: credential.CreatedAt, LastUsedAt: credential.LastUsedAt}
}

func passkeyViews(credentials []store.WebauthnCredential) []auth.SecondFactorPasskey {
	views := make([]auth.SecondFactorPasskey, 0, len(credentials))
	for _, credential := range credentials {
		views = append(views, passkeyView(credential))
	}
	return views
}
