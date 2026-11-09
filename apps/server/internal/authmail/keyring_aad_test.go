package authmail

import (
	"errors"
	"testing"

	"github.com/google/uuid"
)

func TestKeyRing_AuthenticatesNullableScopePresence(t *testing.T) {
	zero := uuid.Nil
	for _, tc := range []struct {
		name  string
		scope JobScope
	}{
		{"user_id", JobScope{UserID: &zero}},
		{"registration_id", JobScope{RegistrationID: &zero}},
		{"reset_token_id", JobScope{ResetTokenID: &zero}},
		{"token_digest", JobScope{TokenDigest: []byte{}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ring := testRing(t, 1)
			sealed, err := ring.Seal(fixedJobID(), KindPasswordChanged, JobScope{}, validPayloadForKind(KindPasswordChanged))
			if err != nil {
				t.Fatal(err)
			}
			if _, err := ring.Open(fixedJobID(), KindPasswordChanged, JobScope{}, sealed); err != nil {
				t.Fatalf("open unchanged scope: %v", err)
			}
			if _, err := ring.Open(fixedJobID(), KindPasswordChanged, tc.scope, sealed); !errors.Is(err, ErrAuthentication) {
				t.Fatalf("open present %s = %v, want ErrAuthentication", tc.name, err)
			}
		})
	}
}

func TestKeyRing_RejectsV1AAD(t *testing.T) {
	ring := testRing(t, 1)
	sealed := sealV1Payload(t, ring, fixedJobID(), KindVerify, validVerifyPayload())
	if _, err := ring.Open(fixedJobID(), KindVerify, JobScope{}, sealed); !errors.Is(err, ErrAuthentication) {
		t.Fatalf("open v1 = %v, want ErrAuthentication", err)
	}
}

func TestKeyRing_AuthenticatesScopeColumn(t *testing.T) {
	id := fixedJobID()
	cases := []struct {
		name  string
		scope JobScope
	}{
		{"user_id", JobScope{UserID: &id}},
		{"registration_id", JobScope{RegistrationID: &id}},
		{"reset_token_id", JobScope{ResetTokenID: &id}},
	}
	for _, from := range cases {
		t.Run(from.name, func(t *testing.T) {
			ring := testRing(t, 1)
			sealed, err := ring.Seal(id, KindPasswordChanged, from.scope, validPayloadForKind(KindPasswordChanged))
			if err != nil {
				t.Fatal(err)
			}
			for _, to := range cases {
				_, openErr := ring.Open(id, KindPasswordChanged, to.scope, sealed)
				if from.name == to.name {
					if openErr != nil {
						t.Fatalf("open unchanged %s: %v", from.name, openErr)
					}
				} else if !errors.Is(openErr, ErrAuthentication) {
					t.Fatalf("move %s to %s = %v, want ErrAuthentication", from.name, to.name, openErr)
				}
			}
		})
	}
}
