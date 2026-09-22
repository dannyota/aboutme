package secondfactor

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/go-webauthn/webauthn/protocol/webauthncbor"
	"github.com/google/uuid"

	"github.com/dannyota/aboutme/apps/server/internal/auth"
	"github.com/dannyota/aboutme/apps/server/internal/store"
)

const (
	testOrigin = "https://aboutme.vn"
	testRPID   = "aboutme.vn"

	flagUP = 0x01
	flagUV = 0x04
	flagBE = 0x08
	flagBS = 0x10
	flagAT = 0x40
)

var b64 = base64.RawURLEncoding

// testAuthenticator is a software P-256 authenticator that produces the
// exact registration and assertion bodies the routes accept.
type testAuthenticator struct {
	t            *testing.T
	key          *ecdsa.PrivateKey
	credentialID []byte
	rpID         string
	origin       string
}

func newTestAuthenticator(t *testing.T) *testAuthenticator {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	id := make([]byte, 32)
	if _, err = rand.Read(id); err != nil {
		t.Fatalf("credential id: %v", err)
	}
	return &testAuthenticator{t: t, key: key, credentialID: id, rpID: testRPID, origin: testOrigin}
}

// coseKey hand-encodes the EC2 P-256 COSE key in canonical CBOR order:
// {1:2, 3:-7, -1:1, -2:x, -3:y}.
func (a *testAuthenticator) coseKey() []byte {
	point, err := a.key.PublicKey.Bytes()
	if err != nil {
		a.t.Fatalf("public key bytes: %v", err)
	}
	out := []byte{0xa5, 0x01, 0x02, 0x03, 0x26, 0x20, 0x01, 0x21, 0x58, 0x20}
	out = append(out, point[1:33]...)
	out = append(out, 0x22, 0x58, 0x20)
	return append(out, point[33:65]...)
}

func (a *testAuthenticator) authData(rpID string, flags byte, counter uint32, attested bool) []byte {
	rpHash := sha256.Sum256([]byte(rpID))
	out := append([]byte{}, rpHash[:]...)
	out = append(out, flags)
	out = binary.BigEndian.AppendUint32(out, counter)
	if attested {
		out = append(out, make([]byte, 16)...)
		out = binary.BigEndian.AppendUint16(out, uint16(len(a.credentialID)))
		out = append(out, a.credentialID...)
		out = append(out, a.coseKey()...)
	}
	return out
}

// clientDataSpec describes one clientDataJSON; zero fields take defaults.
type clientDataSpec struct {
	kind        string
	challenge   string
	origin      string
	crossOrigin bool
	topOrigin   string
}

func (c clientDataSpec) encode() []byte {
	fields := map[string]any{"type": c.kind, "challenge": c.challenge, "origin": c.origin}
	if c.crossOrigin {
		fields["crossOrigin"] = true
	}
	if c.topOrigin != "" {
		fields["topOrigin"] = c.topOrigin
	}
	out, err := json.Marshal(fields)
	if err != nil {
		panic(err)
	}
	return out
}

// registrationSpec varies one registration response.
type registrationSpec struct {
	client  clientDataSpec
	rpID    string
	flags   byte
	counter uint32
}

func (a *testAuthenticator) registrationBody(ceremonyID string, challenge []byte, mutate func(*registrationSpec)) []byte {
	spec := registrationSpec{
		client: clientDataSpec{kind: "webauthn.create", challenge: b64.EncodeToString(challenge), origin: a.origin},
		rpID:   a.rpID, flags: flagUP | flagUV | flagAT,
	}
	if mutate != nil {
		mutate(&spec)
	}
	attestation, err := webauthncbor.Marshal(map[string]any{
		"fmt": "none", "attStmt": map[string]any{}, "authData": a.authData(spec.rpID, spec.flags, spec.counter, true),
	})
	if err != nil {
		a.t.Fatalf("marshal attestation: %v", err)
	}
	id := b64.EncodeToString(a.credentialID)
	return mustJSON(a.t, map[string]any{
		"ceremonyId": ceremonyID,
		"credential": map[string]any{
			"id": id, "rawId": id, "type": "public-key",
			"response": map[string]any{
				"clientDataJSON":    b64.EncodeToString(spec.client.encode()),
				"attestationObject": b64.EncodeToString(attestation),
				"transports":        []string{"internal", "hybrid", "cable"},
			},
			"clientExtensionResults": map[string]any{},
		},
	})
}

// assertionSpec varies one assertion response.
type assertionSpec struct {
	client     clientDataSpec
	rpID       string
	flags      byte
	counter    uint32
	userHandle []byte
	badSig     bool
	key        *ecdsa.PrivateKey
}

func (a *testAuthenticator) assertionBody(ceremonyID string, challenge []byte, counter uint32, mutate func(*assertionSpec)) []byte {
	spec := assertionSpec{
		client: clientDataSpec{kind: "webauthn.get", challenge: b64.EncodeToString(challenge), origin: a.origin},
		rpID:   a.rpID, flags: flagUP | flagUV, counter: counter, key: a.key,
	}
	if mutate != nil {
		mutate(&spec)
	}
	authData := a.authData(spec.rpID, spec.flags, spec.counter, false)
	clientData := spec.client.encode()
	clientHash := sha256.Sum256(clientData)
	digest := sha256.Sum256(append(append([]byte{}, authData...), clientHash[:]...))
	signature, err := ecdsa.SignASN1(rand.Reader, spec.key, digest[:])
	if err != nil {
		a.t.Fatalf("sign: %v", err)
	}
	if spec.badSig {
		signature[len(signature)-1] ^= 0x01
	}
	var userHandle any
	if spec.userHandle != nil {
		userHandle = b64.EncodeToString(spec.userHandle)
	}
	id := b64.EncodeToString(a.credentialID)
	return mustJSON(a.t, map[string]any{
		"ceremonyId": ceremonyID,
		"credential": map[string]any{
			"id": id, "rawId": id, "type": "public-key",
			"response": map[string]any{
				"clientDataJSON":    b64.EncodeToString(clientData),
				"authenticatorData": b64.EncodeToString(authData),
				"signature":         b64.EncodeToString(signature),
				"userHandle":        userHandle,
			},
			"clientExtensionResults": map[string]any{},
		},
	})
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	out, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return out
}

func testRelyingParty(t *testing.T) *RelyingParty {
	t.Helper()
	rp, err := NewRelyingParty(testOrigin)
	if err != nil {
		t.Fatalf("NewRelyingParty() error = %v", err)
	}
	return rp
}

func testCeremonyID() (string, []byte) {
	token := bytes.Repeat([]byte{7}, ceremonyTokenBytes)
	digest := sha256.Sum256(token)
	return b64.EncodeToString(token), digest[:]
}

func challengeDigestOf(challenge []byte) []byte {
	sum := sha256.Sum256(challenge)
	return sum[:]
}

func TestNewRelyingParty_AcceptsOnlyStableHosts(t *testing.T) {
	accepted := map[string]string{
		"https://aboutme.vn":      "aboutme.vn",
		"https://localhost:20443": "localhost",
		"http://localhost:20080":  "localhost",
	}
	for origin, want := range accepted {
		rp, err := NewRelyingParty(origin)
		if err != nil || rp.ID() != want {
			t.Errorf("NewRelyingParty(%q) = %v, %v; want RP ID %q", origin, rp, err, want)
		}
	}
	for _, origin := range []string{
		"", "aboutme.vn", "http://aboutme.vn", "https://127.0.0.1", "https://[::1]:443", "https://Aboutme.vn",
		"https://aboutme.vn/app", "https://aboutme.vn?x=1", "https://user@aboutme.vn", "https://intranet",
		"https://-bad.example", "https://aboutme.vn#frag",
	} {
		if _, err := NewRelyingParty(origin); err == nil {
			t.Errorf("NewRelyingParty(%q) accepted an invalid relying party origin", origin)
		}
	}
}

func TestRegistrationOptions_ExactShape(t *testing.T) {
	rp := testRelyingParty(t)
	challenge := bytes.Repeat([]byte{1}, challengeBytes)
	handle := bytes.Repeat([]byte{2}, userHandleBytes)
	raw, err := rp.registrationOptions(challenge, handle, "a@example.com", "A", nil)
	if err != nil {
		t.Fatalf("registrationOptions() error = %v", err)
	}
	want := `{"challenge":"` + b64.EncodeToString(challenge) + `","rp":{"name":"aboutme","id":"aboutme.vn"},` +
		`"user":{"id":"` + b64.EncodeToString(handle) + `","name":"a@example.com","displayName":"A"},` +
		`"pubKeyCredParams":[{"type":"public-key","alg":-7},{"type":"public-key","alg":-257}],"timeout":300000,` +
		`"excludeCredentials":[],"authenticatorSelection":{"residentKey":"required","requireResidentKey":true,` +
		`"userVerification":"required"},"attestation":"none"}`
	if string(raw) != want {
		t.Fatalf("registration options =\n%s\nwant\n%s", raw, want)
	}
	stored := []store.WebauthnCredential{{CredentialID: []byte("0123456789abcdef"), Transports: []string{"internal", "usb"}}}
	raw, err = rp.registrationOptions(challenge, handle, "a@example.com", "A", stored)
	if err != nil || !strings.Contains(string(raw), `"excludeCredentials":[{"type":"public-key","id":"MDEyMzQ1Njc4OWFiY2RlZg","transports":["usb","internal"]}]`) {
		t.Fatalf("registration options with credential = %s, %v", raw, err)
	}
}

func TestAssertionOptions_NeverEmptyAndNoExtensions(t *testing.T) {
	rp := testRelyingParty(t)
	challenge := bytes.Repeat([]byte{3}, challengeBytes)
	if _, err := rp.assertionOptions(challenge, nil); !errors.Is(err, auth.ErrSecondFactorNotFound) {
		t.Fatalf("assertionOptions(no credentials) error = %v, want ErrSecondFactorNotFound", err)
	}
	stored := []store.WebauthnCredential{{CredentialID: []byte("0123456789abcdef")}}
	raw, err := rp.assertionOptions(challenge, stored)
	if err != nil {
		t.Fatalf("assertionOptions() error = %v", err)
	}
	want := `{"challenge":"` + b64.EncodeToString(challenge) + `","timeout":300000,"rpId":"aboutme.vn",` +
		`"allowCredentials":[{"type":"public-key","id":"MDEyMzQ1Njc4OWFiY2RlZg"}],"userVerification":"required"}`
	if string(raw) != want {
		t.Fatalf("assertion options =\n%s\nwant\n%s", raw, want)
	}
}

// TestDecodeRegistration_HostileCorpus is a frozen set of malformed bodies
// that must fail before any CBOR or signature work.
func TestDecodeRegistration_HostileCorpus(t *testing.T) {
	a := newTestAuthenticator(t)
	ceremonyID, _ := testCeremonyID()
	valid := string(a.registrationBody(ceremonyID, bytes.Repeat([]byte{1}, 32), nil))
	if _, err := decodeRegistration([]byte(valid)); err != nil {
		t.Fatalf("decodeRegistration(valid) error = %v", err)
	}
	id := b64.EncodeToString(a.credentialID)
	corpus := map[string]string{
		"trailing data":         valid + "{}",
		"top-level array":       "[" + valid + "]",
		"duplicate member":      strings.Replace(valid, `"ceremonyId":`, `"ceremonyId":"x","ceremonyId":`, 1),
		"unknown member":        strings.Replace(valid, `{"ceremonyId":`, `{"extra":1,"ceremonyId":`, 1),
		"missing transports":    strings.Replace(valid, `"transports":["internal","hybrid","cable"]`, `"x":[]`, 1),
		"null transports":       strings.Replace(valid, `["internal","hybrid","cable"]`, `null`, 1),
		"padded rawId":          strings.Replace(valid, `"rawId":"`+id+`"`, `"rawId":"`+id+`="`, 1),
		"id differs from raw":   strings.Replace(valid, `"id":"`+id+`"`, `"id":"`+b64.EncodeToString(bytes.Repeat([]byte{9}, 32))+`"`, 1),
		"wrong type":            strings.Replace(valid, `"type":"public-key"`, `"type":"password"`, 1),
		"extension output":      strings.Replace(valid, `"clientExtensionResults":{}`, `"clientExtensionResults":{"credProps":{"rk":true}}`, 1),
		"short ceremony id":     strings.Replace(valid, ceremonyID, ceremonyID[:42], 1),
		"null ceremony id":      strings.Replace(valid, `"`+ceremonyID+`"`, `null`, 1),
		"invalid utf8":          strings.Replace(valid, `"type":"public-key"`, "\"type\":\"public-key\xff\"", 1),
		"oversized client data": strings.Replace(valid, `"clientDataJSON":"`, `"clientDataJSON":"`+strings.Repeat("A", 5462)+`AA`, 1),
		"short credential id":   strings.ReplaceAll(valid, id, b64.EncodeToString([]byte("short"))),
		"line break base64":     strings.Replace(valid, `"rawId":"`+id, `"rawId":"`+id[:4]+`\n`+id[4:], 1),
	}
	for name, body := range corpus {
		if _, err := decodeRegistration([]byte(body)); !errors.Is(err, auth.ErrSecondFactorRequestInvalid) {
			t.Errorf("decodeRegistration(%s) error = %v, want ErrSecondFactorRequestInvalid", name, err)
		}
	}
}

func TestDecodeAssertion_HostileCorpus(t *testing.T) {
	a := newTestAuthenticator(t)
	ceremonyID, _ := testCeremonyID()
	valid := string(a.assertionBody(ceremonyID, bytes.Repeat([]byte{1}, 32), 1, nil))
	if _, err := decodeAssertion([]byte(valid)); err != nil {
		t.Fatalf("decodeAssertion(valid) error = %v", err)
	}
	corpus := map[string]string{
		"missing user handle":  strings.Replace(valid, `"userHandle":null`, `"x":null`, 1),
		"empty user handle":    strings.Replace(valid, `"userHandle":null`, `"userHandle":""`, 1),
		"long user handle":     strings.Replace(valid, `"userHandle":null`, `"userHandle":"`+b64.EncodeToString(make([]byte, 65))+`"`, 1),
		"numeric user handle":  strings.Replace(valid, `"userHandle":null`, `"userHandle":1`, 1),
		"null signature":       regexpReplaceValue(valid, "signature", "null"),
		"oversized signature":  regexpReplaceValue(valid, "signature", `"`+b64.EncodeToString(make([]byte, 1025))+`"`),
		"attestation member":   strings.Replace(valid, `"userHandle":null`, `"userHandle":null,"attestationObject":"AA"`, 1),
		"duplicate signature":  strings.Replace(valid, `"userHandle":null`, `"userHandle":null,"signature":"AA"`, 1),
		"noncanonical bits":    regexpReplaceValue(valid, "signature", `"AB"`),
		"missing extensions":   strings.Replace(valid, `"clientExtensionResults":{},`, ``, 1),
		"extensions not empty": strings.Replace(valid, `"clientExtensionResults":{}`, `"clientExtensionResults":{"appid":true}`, 1),
	}
	for name, body := range corpus {
		if _, err := decodeAssertion([]byte(body)); !errors.Is(err, auth.ErrSecondFactorRequestInvalid) {
			t.Errorf("decodeAssertion(%s) error = %v, want ErrSecondFactorRequestInvalid", name, err)
		}
	}
	withHandle := a.assertionBody(ceremonyID, bytes.Repeat([]byte{1}, 32), 1, func(s *assertionSpec) { s.userHandle = make([]byte, 64) })
	if _, err := decodeAssertion(withHandle); err != nil {
		t.Fatalf("decodeAssertion(64-byte user handle) error = %v", err)
	}
}

// regexpReplaceValue replaces the quoted JSON string value of member name,
// quotes included, with value.
func regexpReplaceValue(body, name, value string) string {
	start := strings.Index(body, `"`+name+`":"`)
	if start < 0 {
		return body
	}
	open := start + len(name) + 3
	closing := open + 1 + strings.Index(body[open+1:], `"`)
	return body[:open] + value + body[closing+1:]
}

func TestVerifyRegistration_AcceptsValidAndRejectsHostileCorpus(t *testing.T) {
	rp := testRelyingParty(t)
	a := newTestAuthenticator(t)
	ceremonyID, _ := testCeremonyID()
	challenge := bytes.Repeat([]byte{4}, challengeBytes)
	handle := bytes.Repeat([]byte{5}, userHandleBytes)
	verify := func(mutate func(*registrationSpec)) (verifiedPasskey, error) {
		reg, err := decodeRegistration(a.registrationBody(ceremonyID, challenge, mutate))
		if err != nil {
			t.Fatalf("decodeRegistration() error = %v", err)
		}
		return rp.verifyRegistration(reg, challengeDigestOf(challenge), handle, "a@example.com", "A")
	}
	got, err := verify(func(s *registrationSpec) { s.counter = 3; s.flags |= flagBE })
	if err != nil {
		t.Fatalf("verifyRegistration(valid) error = %v", err)
	}
	if !bytes.Equal(got.credentialID, a.credentialID) || got.signCount != 3 || !got.backupEligible || got.backupState ||
		len(got.publicKey) == 0 || strings.Join(got.transports, ",") != "hybrid,internal" {
		t.Fatalf("verified passkey = %+v", got)
	}
	corpus := map[string]func(*registrationSpec){
		"assertion type":    func(s *registrationSpec) { s.client.kind = "webauthn.get" },
		"other challenge":   func(s *registrationSpec) { s.client.challenge = b64.EncodeToString(bytes.Repeat([]byte{6}, 32)) },
		"padded challenge":  func(s *registrationSpec) { s.client.challenge += "=" },
		"other origin":      func(s *registrationSpec) { s.client.origin = "https://evil.example" },
		"origin case":       func(s *registrationSpec) { s.client.origin = "https://ABOUTME.vn" },
		"origin with port":  func(s *registrationSpec) { s.client.origin = "https://aboutme.vn:443" },
		"cross origin":      func(s *registrationSpec) { s.client.crossOrigin = true },
		"top origin":        func(s *registrationSpec) { s.client.topOrigin = testOrigin },
		"other rp id hash":  func(s *registrationSpec) { s.rpID = "evil.example" },
		"no user presence":  func(s *registrationSpec) { s.flags &^= flagUP },
		"no verification":   func(s *registrationSpec) { s.flags &^= flagUV },
		"backup state only": func(s *registrationSpec) { s.flags |= flagBS },
	}
	for name, mutate := range corpus {
		if _, err := verify(mutate); !errors.Is(err, auth.ErrSecondFactorVerificationFailed) {
			t.Errorf("verifyRegistration(%s) error = %v, want ErrSecondFactorVerificationFailed", name, err)
		}
	}
}

// TestVerifyAssertion_HostileCorpus is the frozen assertion corpus: every case
// is well formed and must fail verification without advancing anything.
func TestVerifyAssertion_HostileCorpus(t *testing.T) {
	rp := testRelyingParty(t)
	a := newTestAuthenticator(t)
	other := newTestAuthenticator(t)
	ceremonyID, _ := testCeremonyID()
	challenge := bytes.Repeat([]byte{4}, challengeBytes)
	handle := bytes.Repeat([]byte{5}, userHandleBytes)
	point := a.coseKey()
	target := store.WebauthnCredential{ID: uuid.New(), CredentialID: a.credentialID, PublicKey: point, SignCount: 10}
	otherStored := store.WebauthnCredential{ID: uuid.New(), CredentialID: other.credentialID, PublicKey: other.coseKey()}
	active := []store.WebauthnCredential{target, otherStored}
	verify := func(counter uint32, mutate func(*assertionSpec)) (assertionResult, error) {
		assertion, err := decodeAssertion(a.assertionBody(ceremonyID, challenge, counter, mutate))
		if err != nil {
			t.Fatalf("decodeAssertion() error = %v", err)
		}
		return rp.verifyAssertion(assertion, challengeDigestOf(challenge), handle, active, target)
	}
	got, err := verify(11, func(s *assertionSpec) { s.userHandle = handle })
	if err != nil || got.received != 11 || got.counterRegressed {
		t.Fatalf("verifyAssertion(valid) = %+v, %v", got, err)
	}
	if got, err = verify(10, nil); err != nil || !got.counterRegressed {
		t.Fatalf("verifyAssertion(equal counter) = %+v, %v; want a verified counter regression", got, err)
	}
	corpus := map[string]func(*assertionSpec){
		"bad signature":     func(s *assertionSpec) { s.badSig = true },
		"other key":         func(s *assertionSpec) { s.key = other.key },
		"registration type": func(s *assertionSpec) { s.client.kind = "webauthn.create" },
		"other challenge":   func(s *assertionSpec) { s.client.challenge = b64.EncodeToString(bytes.Repeat([]byte{6}, 32)) },
		"other origin":      func(s *assertionSpec) { s.client.origin = "https://aboutme.vn.evil.example" },
		"cross origin":      func(s *assertionSpec) { s.client.crossOrigin = true },
		"top origin":        func(s *assertionSpec) { s.client.crossOrigin, s.client.topOrigin = true, testOrigin },
		"other rp id hash":  func(s *assertionSpec) { s.rpID = "evil.example" },
		"no user presence":  func(s *assertionSpec) { s.flags &^= flagUP },
		"no verification":   func(s *assertionSpec) { s.flags &^= flagUV },
		"backup flip":       func(s *assertionSpec) { s.flags |= flagBE },
		"wrong user handle": func(s *assertionSpec) { s.userHandle = bytes.Repeat([]byte{9}, 32) },
		"extension data":    func(s *assertionSpec) { s.flags |= 0x80 },
	}
	for name, mutate := range corpus {
		if _, mutateErr := verify(12, mutate); !errors.Is(mutateErr, auth.ErrSecondFactorVerificationFailed) {
			t.Errorf("verifyAssertion(%s) error = %v, want ErrSecondFactorVerificationFailed", name, mutateErr)
		}
	}
	assertion, err := decodeAssertion(a.assertionBody(ceremonyID, challenge, 12, nil))
	if err != nil {
		t.Fatalf("decodeAssertion() error = %v", err)
	}
	if _, err = rp.verifyAssertion(assertion, challengeDigestOf(challenge), handle, active, otherStored); !errors.Is(err, auth.ErrSecondFactorVerificationFailed) {
		t.Fatalf("verifyAssertion(credential bound to another target) error = %v", err)
	}
	if _, err = rp.verifyAssertion(assertion, challengeDigestOf(challenge), handle, []store.WebauthnCredential{otherStored}, target); !errors.Is(err, auth.ErrSecondFactorVerificationFailed) {
		t.Fatalf("verifyAssertion(credential not active) error = %v", err)
	}
}

func TestVerifyAssertion_ZeroCountersStayZero(t *testing.T) {
	rp := testRelyingParty(t)
	a := newTestAuthenticator(t)
	ceremonyID, _ := testCeremonyID()
	challenge := bytes.Repeat([]byte{4}, challengeBytes)
	handle := bytes.Repeat([]byte{5}, userHandleBytes)
	target := store.WebauthnCredential{ID: uuid.New(), CredentialID: a.credentialID, PublicKey: a.coseKey()}
	assertion, err := decodeAssertion(a.assertionBody(ceremonyID, challenge, 0, nil))
	if err != nil {
		t.Fatalf("decodeAssertion() error = %v", err)
	}
	got, err := rp.verifyAssertion(assertion, challengeDigestOf(challenge), handle, []store.WebauthnCredential{target}, target)
	if err != nil || got.counterRegressed || got.received != 0 {
		t.Fatalf("verifyAssertion(zero counters) = %+v, %v", got, err)
	}
}
