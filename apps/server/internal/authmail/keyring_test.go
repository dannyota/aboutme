package authmail

import (
	"bytes"
	"crypto/rand"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestKeyRingNewValidActiveOnly(t *testing.T) {
	ring, err := NewKeyRing("k-active", map[string][32]byte{"k-active": fixedKey()}, bytes.NewReader(fixedNonce()))
	if err != nil {
		t.Fatalf("NewKeyRing: %v", err)
	}
	if ring.activeID != "k-active" {
		t.Fatalf("activeID = %q, want k-active", ring.activeID)
	}
	if len(ring.keys) != 1 {
		t.Fatalf("keys = %d, want 1", len(ring.keys))
	}
}

func TestKeyRingNewValidActiveAndPrevious(t *testing.T) {
	ring, err := NewKeyRing("k-active", map[string][32]byte{
		"k-active": fixedKey(),
		"k-prev":   keyAt(100),
	}, bytes.NewReader(fixedNonce()))
	if err != nil {
		t.Fatalf("NewKeyRing: %v", err)
	}
	if len(ring.keys) != 2 {
		t.Fatalf("keys = %d, want 2", len(ring.keys))
	}
}

func TestKeyRingNewRejectsEmptyKeys(t *testing.T) {
	if _, err := NewKeyRing("k-active", map[string][32]byte{}, bytes.NewReader(fixedNonce())); !errors.Is(err, ErrKeyRing) {
		t.Fatalf("err = %v, want ErrKeyRing", err)
	}
}

func TestKeyRingNewRejectsMoreThanTwoKeys(t *testing.T) {
	keys := map[string][32]byte{"k-a": fixedKey(), "k-b": keyAt(1), "k-c": keyAt(2)}
	if _, err := NewKeyRing("k-a", keys, bytes.NewReader(fixedNonce())); !errors.Is(err, ErrKeyRing) {
		t.Fatalf("err = %v, want ErrKeyRing", err)
	}
}

func TestKeyRingNewRejectsUnknownActiveID(t *testing.T) {
	keys := map[string][32]byte{"k-other": fixedKey()}
	if _, err := NewKeyRing("k-active", keys, bytes.NewReader(fixedNonce())); !errors.Is(err, ErrKeyRing) {
		t.Fatalf("err = %v, want ErrKeyRing", err)
	}
}

func TestKeyRingNewRejectsEmptyActiveID(t *testing.T) {
	keys := map[string][32]byte{"k-active": fixedKey()}
	if _, err := NewKeyRing("", keys, bytes.NewReader(fixedNonce())); !errors.Is(err, ErrKeyRing) {
		t.Fatalf("err = %v, want ErrKeyRing", err)
	}
}

func TestKeyRingNewRejectsControlCharKeyID(t *testing.T) {
	keys := map[string][32]byte{"k\tactive": fixedKey()}
	if _, err := NewKeyRing("k\tactive", keys, bytes.NewReader(fixedNonce())); !errors.Is(err, ErrKeyRing) {
		t.Fatalf("err = %v, want ErrKeyRing", err)
	}
}

func TestKeyRingNewRejectsLongKeyID(t *testing.T) {
	long := strings.Repeat("k", 65)
	if _, err := NewKeyRing(long, map[string][32]byte{long: fixedKey()}, bytes.NewReader(fixedNonce())); !errors.Is(err, ErrKeyRing) {
		t.Fatalf("err = %v, want ErrKeyRing", err)
	}
}

func TestKeyRingNewRejectsNilNonce(t *testing.T) {
	if _, err := NewKeyRing("k-active", map[string][32]byte{"k-active": fixedKey()}, nil); !errors.Is(err, ErrKeyRing) {
		t.Fatalf("err = %v, want ErrKeyRing", err)
	}
}

func TestKeyRingNewCopiesKeyMap(t *testing.T) {
	keys := map[string][32]byte{"k-active": fixedKey()}
	ring, err := NewKeyRing("k-active", keys, bytes.NewReader(fixedNonce()))
	if err != nil {
		t.Fatal(err)
	}
	// Mutating the caller's map after construction must not change the ring.
	delete(keys, "k-active")
	keys["k-intruder"] = keyAt(9)
	if _, ok := ring.keys["k-active"]; !ok {
		t.Fatal("ring lost active key after caller mutation")
	}
	if len(ring.keys) != 1 {
		t.Fatalf("ring keys = %d, want 1 (caller mutation leaked)", len(ring.keys))
	}
}

func TestSealOpenRoundtrip(t *testing.T) {
	ring := mustRing(t, "k-active", map[string][32]byte{"k-active": fixedKey()}, fixedNonce())
	p := validVerifyPayload()

	s, err := ring.Seal(fixedJobID(), KindVerify, p)
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}
	if s.KeyID != "k-active" {
		t.Fatalf("KeyID = %q, want k-active", s.KeyID)
	}
	if len(s.Ciphertext) == 0 {
		t.Fatal("ciphertext empty")
	}

	got, err := ring.Open(fixedJobID(), KindVerify, s)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if got != p {
		t.Fatalf("Open = %+v, want %+v", got, p)
	}
}

func TestSealOpenRandomNonceInequality(t *testing.T) {
	ring, err := NewKeyRing("k-active", map[string][32]byte{"k-active": fixedKey()}, rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	p := validVerifyPayload()

	s1, err := ring.Seal(fixedJobID(), KindVerify, p)
	if err != nil {
		t.Fatal(err)
	}
	s2, err := ring.Seal(fixedJobID(), KindVerify, p)
	if err != nil {
		t.Fatal(err)
	}
	if s1.Nonce == s2.Nonce {
		t.Fatal("two seals reused the same nonce")
	}
	if bytes.Equal(s1.Ciphertext, s2.Ciphertext) {
		t.Fatal("two seals produced identical ciphertext")
	}
}

func TestSealRejectsUnknownKind(t *testing.T) {
	ring := mustRing(t, "k-active", map[string][32]byte{"k-active": fixedKey()}, fixedNonce())
	if _, err := ring.Seal(fixedJobID(), Kind("bogus"), validVerifyPayload()); !errors.Is(err, ErrInvalidKind) {
		t.Fatalf("err = %v, want ErrInvalidKind", err)
	}
}

func TestSealRejectsOversizedPlaintext(t *testing.T) {
	ring := mustRing(t, "k-active", map[string][32]byte{"k-active": fixedKey()}, fixedNonce())
	p := validVerifyPayload()
	p.Link = verifyLinkPrefix + strings.Repeat("t", 5000)
	if _, err := ring.Seal(fixedJobID(), KindVerify, p); !errors.Is(err, ErrOversizedPayload) {
		t.Fatalf("err = %v, want ErrOversizedPayload", err)
	}
}

func TestSealRejectsNonceFailure(t *testing.T) {
	ring, err := NewKeyRing("k-active", map[string][32]byte{"k-active": fixedKey()}, errReader{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ring.Seal(fixedJobID(), KindVerify, validVerifyPayload()); !errors.Is(err, ErrNonce) {
		t.Fatalf("err = %v, want ErrNonce", err)
	}
}

func TestOpenRejectsEmptyCiphertext(t *testing.T) {
	ring := mustRing(t, "k-active", map[string][32]byte{"k-active": fixedKey()}, fixedNonce())
	_, err := ring.Open(fixedJobID(), KindVerify, Sealed{KeyID: "k-active", Ciphertext: []byte{}})
	if !errors.Is(err, ErrCiphertext) {
		t.Fatalf("err = %v, want ErrCiphertext", err)
	}
}

func TestOpenRejectsOversizedCiphertext(t *testing.T) {
	ring := mustRing(t, "k-active", map[string][32]byte{"k-active": fixedKey()}, fixedNonce())
	_, err := ring.Open(fixedJobID(), KindVerify, Sealed{KeyID: "k-active", Ciphertext: make([]byte, maxCiphertextBytes+1)})
	if !errors.Is(err, ErrCiphertext) {
		t.Fatalf("err = %v, want ErrCiphertext", err)
	}
}

func TestOpenAcceptsBoundCiphertext(t *testing.T) {
	ring := mustRing(t, "k-active", map[string][32]byte{"k-active": fixedKey()}, fixedNonce())
	// Exactly the upper bound passes the size gate and fails authentication.
	_, err := ring.Open(fixedJobID(), KindVerify, Sealed{KeyID: "k-active", Ciphertext: make([]byte, maxCiphertextBytes)})
	if !errors.Is(err, ErrAuthentication) {
		t.Fatalf("err = %v, want ErrAuthentication (bound accepted, auth failed)", err)
	}
}

func TestOpenRejectsUnknownKeyID(t *testing.T) {
	ring := mustRing(t, "k-active", map[string][32]byte{"k-active": fixedKey()}, fixedNonce())
	_, err := ring.Open(fixedJobID(), KindVerify, Sealed{KeyID: "k-removed", Ciphertext: make([]byte, 16)})
	if !errors.Is(err, ErrUnknownKey) {
		t.Fatalf("err = %v, want ErrUnknownKey", err)
	}
}

func TestOpenRejectsInvalidKeyID(t *testing.T) {
	ring := mustRing(t, "k-active", map[string][32]byte{"k-active": fixedKey()}, fixedNonce())
	_, err := ring.Open(fixedJobID(), KindVerify, Sealed{KeyID: "k\tbad", Ciphertext: make([]byte, 16)})
	if !errors.Is(err, ErrInvalidKeyID) {
		t.Fatalf("err = %v, want ErrInvalidKeyID", err)
	}
}

func TestOpenRejectsTamperedCiphertext(t *testing.T) {
	ring := mustRing(t, "k-active", map[string][32]byte{"k-active": fixedKey()}, fixedNonce())
	s, err := ring.Seal(fixedJobID(), KindVerify, validVerifyPayload())
	if err != nil {
		t.Fatal(err)
	}
	s.Ciphertext[len(s.Ciphertext)-1] ^= 0x01
	if _, err := ring.Open(fixedJobID(), KindVerify, s); !errors.Is(err, ErrAuthentication) {
		t.Fatalf("err = %v, want ErrAuthentication", err)
	}
}

func TestOpenRejectsTruncatedTag(t *testing.T) {
	ring := mustRing(t, "k-active", map[string][32]byte{"k-active": fixedKey()}, fixedNonce())
	s, err := ring.Seal(fixedJobID(), KindVerify, validVerifyPayload())
	if err != nil {
		t.Fatal(err)
	}
	s.Ciphertext = s.Ciphertext[:len(s.Ciphertext)-1]
	if _, err := ring.Open(fixedJobID(), KindVerify, s); !errors.Is(err, ErrAuthentication) {
		t.Fatalf("err = %v, want ErrAuthentication", err)
	}
}

func TestOpenRejectsWrongNonce(t *testing.T) {
	ring := mustRing(t, "k-active", map[string][32]byte{"k-active": fixedKey()}, fixedNonce())
	s, err := ring.Seal(fixedJobID(), KindVerify, validVerifyPayload())
	if err != nil {
		t.Fatal(err)
	}
	s.Nonce[0] ^= 0xff
	if _, err := ring.Open(fixedJobID(), KindVerify, s); !errors.Is(err, ErrAuthentication) {
		t.Fatalf("err = %v, want ErrAuthentication", err)
	}
}

func TestOpenRejectsWrongJobID(t *testing.T) {
	ring := mustRing(t, "k-active", map[string][32]byte{"k-active": fixedKey()}, fixedNonce())
	s, err := ring.Seal(fixedJobID(), KindVerify, validVerifyPayload())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ring.Open(uuid.New(), KindVerify, s); !errors.Is(err, ErrAuthentication) {
		t.Fatalf("err = %v, want ErrAuthentication", err)
	}
}

func TestOpenRejectsWrongKind(t *testing.T) {
	ring := mustRing(t, "k-active", map[string][32]byte{"k-active": fixedKey()}, fixedNonce())
	s, err := ring.Seal(fixedJobID(), KindVerify, validVerifyPayload())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ring.Open(fixedJobID(), KindReset, s); !errors.Is(err, ErrAuthentication) {
		t.Fatalf("err = %v, want ErrAuthentication", err)
	}
}

func TestOpenRejectsWrongKeyMaterial(t *testing.T) {
	// Same key ID, different key bytes: the GCM tag cannot verify.
	good := mustRing(t, "k-active", map[string][32]byte{"k-active": fixedKey()}, fixedNonce())
	bad := mustRing(t, "k-active", map[string][32]byte{"k-active": keyAt(200)}, fixedNonce())

	s, err := good.Seal(fixedJobID(), KindVerify, validVerifyPayload())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := bad.Open(fixedJobID(), KindVerify, s); !errors.Is(err, ErrAuthentication) {
		t.Fatalf("err = %v, want ErrAuthentication", err)
	}
}

func TestOpenDecryptsPreviousKeyAfterRotation(t *testing.T) {
	prev := keyAt(100)
	active := fixedKey()
	ring := mustRing(t, "k-active", map[string][32]byte{
		"k-prev":   prev,
		"k-active": active,
	}, fixedNonce())

	plaintext := []byte(`{"version":1,"to":"alice@example.com","link":"` + verifyLinkPrefix + `tok"}`)
	s := sealWithKey(t, prev, "k-prev", fixedJobID(), KindVerify, plaintext)

	got, err := ring.Open(fixedJobID(), KindVerify, s)
	if err != nil {
		t.Fatalf("Open previous-key job: %v", err)
	}
	if got.To != "alice@example.com" || got.Link != verifyLinkPrefix+"tok" {
		t.Fatalf("opened = %+v, want previous-key plaintext", got)
	}
}

func TestOpenRemovedKeyFailsClosed(t *testing.T) {
	ring := mustRing(t, "k-active", map[string][32]byte{"k-active": fixedKey()}, fixedNonce())
	plaintext := []byte(`{"version":1,"to":"a@b.c","link":"` + verifyLinkPrefix + `tok"}`)
	s := sealWithKey(t, keyAt(50), "k-removed", fixedJobID(), KindVerify, plaintext)

	if _, err := ring.Open(fixedJobID(), KindVerify, s); !errors.Is(err, ErrUnknownKey) {
		t.Fatalf("err = %v, want ErrUnknownKey", err)
	}
}

func TestOpenRejectsUnknownPayloadVersion(t *testing.T) {
	ring := mustRing(t, "k-active", map[string][32]byte{"k-active": fixedKey()}, fixedNonce())
	s := sealRaw(t, ring, fixedJobID(), KindVerify, []byte(`{"version":2,"to":"alice@example.com","link":"`+verifyLinkPrefix+`tok"}`))
	if _, err := ring.Open(fixedJobID(), KindVerify, s); !errors.Is(err, ErrUnknownVersion) {
		t.Fatalf("err = %v, want ErrUnknownVersion", err)
	}
}

func TestOpenRejectsDuplicateJSONKey(t *testing.T) {
	ring := mustRing(t, "k-active", map[string][32]byte{"k-active": fixedKey()}, fixedNonce())
	s := sealRaw(t, ring, fixedJobID(), KindVerify, []byte(`{"version":1,"version":1,"to":"alice@example.com"}`))
	if _, err := ring.Open(fixedJobID(), KindVerify, s); !errors.Is(err, ErrStrictJSON) {
		t.Fatalf("err = %v, want ErrStrictJSON", err)
	}
}

func TestOpenRejectsUnknownField(t *testing.T) {
	ring := mustRing(t, "k-active", map[string][32]byte{"k-active": fixedKey()}, fixedNonce())
	s := sealRaw(t, ring, fixedJobID(), KindVerify, []byte(`{"version":1,"to":"alice@example.com","evil":1}`))
	if _, err := ring.Open(fixedJobID(), KindVerify, s); !errors.Is(err, ErrStrictJSON) {
		t.Fatalf("err = %v, want ErrStrictJSON", err)
	}
}

func TestOpenRejectsInvalidEmailInPlaintext(t *testing.T) {
	ring := mustRing(t, "k-active", map[string][32]byte{"k-active": fixedKey()}, fixedNonce())
	s := sealRaw(t, ring, fixedJobID(), KindVerify, []byte(`{"version":1,"to":"not-an-email"}`))
	if _, err := ring.Open(fixedJobID(), KindVerify, s); !errors.Is(err, ErrInvalidEmail) {
		t.Fatalf("err = %v, want ErrInvalidEmail", err)
	}
}

func TestOpenRejectsInvalidLinkInPlaintext(t *testing.T) {
	ring := mustRing(t, "k-active", map[string][32]byte{"k-active": fixedKey()}, fixedNonce())
	s := sealRaw(t, ring, fixedJobID(), KindVerify, []byte(`{"version":1,"to":"alice@example.com","link":"https://evil.example/x"}`))
	if _, err := ring.Open(fixedJobID(), KindVerify, s); !errors.Is(err, ErrInvalidLink) {
		t.Fatalf("err = %v, want ErrInvalidLink", err)
	}
}
