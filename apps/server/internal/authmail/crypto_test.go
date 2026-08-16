package authmail

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/uuid"
)

// Fixed, non-secret test vectors shared across the package's tests. These are
// deliberately public: they pin the D3 AAD layout and the AES-256-GCM output
// without ever exercising a production key.
func fixedKey() [32]byte {
	var k [32]byte
	for i := range k {
		k[i] = byte(i)
	}
	return k
}

func keyAt(seed byte) [32]byte {
	var k [32]byte
	for i := range k {
		k[i] = seed + byte(i)
	}
	return k
}

func fixedNonce() []byte {
	n := make([]byte, 12)
	for i := range n {
		n[i] = byte(i)
	}
	return n
}

func fixedJobID() uuid.UUID {
	return uuid.MustParse("123e4567-e89b-12d3-a456-426614174000")
}

func mustRing(t *testing.T, activeID string, keys map[string][32]byte, nonce []byte) *KeyRing {
	t.Helper()
	ring, err := NewKeyRing(activeID, keys, bytes.NewReader(nonce))
	if err != nil {
		t.Fatalf("NewKeyRing: %v", err)
	}
	return ring
}

func validPayloadForKind(k Kind) Payload {
	p := Payload{Version: payloadVersion, To: "alice@example.com"}
	switch k {
	case KindVerify:
		p.Link = verifyLinkPrefix + "TESTTOKEN"
	case KindReset:
		p.Link = resetLinkPrefix + "TESTTOKEN"
	case KindPasswordChanged:
		p.Link = ""
	}
	return p
}

func validVerifyPayload() Payload { return validPayloadForKind(KindVerify) }

// errReader fails every read, simulating entropy exhaustion.
type errReader struct{}

func (errReader) Read([]byte) (int, error) { return 0, errors.New("no entropy") }

// sealWithKey seals arbitrary plaintext under a specific key and key ID, so
// tests can produce blobs sealed under a previous/removed key or with a
// deliberately invalid plaintext (unknown version, duplicate JSON key, ...).
func sealWithKey(t *testing.T, key [32]byte, keyID string, jobID uuid.UUID, kind Kind, plaintext []byte) Sealed {
	t.Helper()
	nonce := fixedNonce()
	var n [12]byte
	copy(n[:], nonce)
	ct, err := sealAEAD(key, nonce, plaintext, aad(jobID, kind, keyID))
	if err != nil {
		t.Fatalf("sealAEAD: %v", err)
	}
	return Sealed{KeyID: keyID, Nonce: n, Ciphertext: ct}
}

func sealRaw(t *testing.T, ring *KeyRing, jobID uuid.UUID, kind Kind, plaintext []byte) Sealed {
	t.Helper()
	return sealWithKey(t, ring.keys[ring.activeID], ring.activeID, jobID, kind, plaintext)
}

func TestAADIsExactD3Bytes(t *testing.T) {
	jobID := fixedJobID()
	const keyID = "k-active"

	got := aad(jobID, KindVerify, keyID)

	want := append([]byte("aboutme.auth-email.v1"), 0x00)
	want = append(want, jobID[:]...)
	want = append(want, 0x00)
	want = append(want, "verify"...)
	want = append(want, 0x00)
	want = append(want, keyID...)

	if !bytes.Equal(got, want) {
		t.Fatalf("AAD = %x, want %x", got, want)
	}
}

func TestSealAEADDeterministicRoundtrip(t *testing.T) {
	key := fixedKey()
	nonce := fixedNonce()
	plaintext := []byte(`{"version":1,"to":"alice@example.com","link":"` + verifyLinkPrefix + `TESTTOKEN"}`)
	a := aad(fixedJobID(), KindVerify, "k-active")

	ct1, err := sealAEAD(key, nonce, plaintext, a)
	if err != nil {
		t.Fatal(err)
	}
	ct2, err := sealAEAD(key, nonce, plaintext, a)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(ct1, ct2) {
		t.Fatal("sealAEAD is not deterministic for a fixed key/nonce/AAD")
	}

	got, err := openAEAD(key, nonce, ct1, a)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, plaintext) {
		t.Fatalf("roundtrip plaintext = %q, want %q", got, plaintext)
	}
}

func TestDeterministicCiphertextFixture(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "deterministic_verify.json"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}

	var fx struct {
		JobID            string `json:"job_id"`
		Kind             string `json:"kind"`
		KeyID            string `json:"key_id"`
		KeyHex           string `json:"key_hex"`
		NonceHex         string `json:"nonce_hex"`
		PlaintextHex     string `json:"plaintext_hex"`
		AADHex           string `json:"aad_hex"`
		CiphertextHex    string `json:"ciphertext_hex"`
		CiphertextSHA256 string `json:"ciphertext_sha256"`
	}
	if unmarshalErr := json.Unmarshal(raw, &fx); unmarshalErr != nil {
		t.Fatalf("parse fixture: %v", unmarshalErr)
	}

	jobID := uuid.MustParse(fx.JobID)
	key, err := hex.DecodeString(fx.KeyHex)
	if err != nil {
		t.Fatalf("key_hex: %v", err)
	}
	nonce, err := hex.DecodeString(fx.NonceHex)
	if err != nil {
		t.Fatalf("nonce_hex: %v", err)
	}
	plaintext, err := hex.DecodeString(fx.PlaintextHex)
	if err != nil {
		t.Fatalf("plaintext_hex: %v", err)
	}
	wantAAD, err := hex.DecodeString(fx.AADHex)
	if err != nil {
		t.Fatalf("aad_hex: %v", err)
	}
	wantCT, err := hex.DecodeString(fx.CiphertextHex)
	if err != nil {
		t.Fatalf("ciphertext_hex: %v", err)
	}

	if len(key) != 32 {
		t.Fatalf("key len = %d, want 32", len(key))
	}
	if len(nonce) != 12 {
		t.Fatalf("nonce len = %d, want 12", len(nonce))
	}

	var keyArr [32]byte
	copy(keyArr[:], key)

	gotAAD := aad(jobID, Kind(fx.Kind), fx.KeyID)
	if !bytes.Equal(gotAAD, wantAAD) {
		t.Fatalf("AAD mismatch:\n got %x\nwant %x", gotAAD, wantAAD)
	}

	gotCT, err := sealAEAD(keyArr, nonce, plaintext, gotAAD)
	if err != nil {
		t.Fatalf("sealAEAD: %v", err)
	}
	if !bytes.Equal(gotCT, wantCT) {
		t.Fatalf("ciphertext mismatch:\n got %x\nwant %x", gotCT, wantCT)
	}

	sum := sha256.Sum256(wantCT)
	if got := hex.EncodeToString(sum[:]); got != fx.CiphertextSHA256 {
		t.Fatalf("ciphertext sha256 = %s, want %s", got, fx.CiphertextSHA256)
	}

	opened, err := openAEAD(keyArr, nonce, wantCT, gotAAD)
	if err != nil {
		t.Fatalf("openAEAD: %v", err)
	}
	if !bytes.Equal(opened, plaintext) {
		t.Fatalf("opened plaintext = %q, want %q", opened, plaintext)
	}
}
