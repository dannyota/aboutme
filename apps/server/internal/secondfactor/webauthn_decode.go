package secondfactor

// Strict WebAuthn wire decoding: registration and assertion completion
// envelopes, canonical base64url members, ceremony IDs, and transport hints.
// Every rule runs before any CBOR or signature work.

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"unicode/utf8"

	"github.com/go-webauthn/webauthn/protocol"

	"github.com/dannyota/aboutme/apps/server/internal/auth"
)

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
// rules as decodeRegistration. userHandle is required and may be null or empty.
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
	// An empty user handle means absent in WebAuthn, exactly like null, so it
	// decodes to no handle and never matches the stored one.
	var userHandle []byte
	if !bytes.Equal(bytes.TrimSpace(fields["userHandle"]), []byte("null")) {
		if userHandle, err = base64Member(fields["userHandle"], 0, maxUserHandleBytes); err != nil {
			return nil, err
		}
		if len(userHandle) == 0 {
			userHandle = nil
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
