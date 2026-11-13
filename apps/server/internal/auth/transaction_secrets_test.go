package auth_test

import (
	"bytes"
	"context"
	cryptorand "crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"

	"danny.vn/aboutme/apps/server/internal/auth"
)

type transactionBegin struct {
	name  string
	begin func(context.Context) (string, auth.Transaction, error)
}

func transactionBegins(ts *auth.TransactionStore, linkingUserID, resumeID uuid.UUID) []transactionBegin {
	var begins []transactionBegin
	for _, provider := range []auth.Provider{auth.ProviderGoogle, auth.ProviderLinkedIn, auth.ProviderGitHub} {
		redirectURI := testPublicOrigin + "/api/v1/auth/" + string(provider) + "/callback"
		for _, purpose := range []auth.Purpose{auth.PurposeLogin, auth.PurposeLink, auth.PurposeReauth} {
			linking := linkingUserID
			if purpose == auth.PurposeLogin {
				linking = uuid.Nil
			}
			begins = append(begins, transactionBegin{
				name: string(provider) + "/" + string(purpose),
				begin: func(ctx context.Context) (string, auth.Transaction, error) {
					return ts.Begin(ctx, provider, purpose, linking, redirectURI)
				},
			})
		}
		if provider == auth.ProviderGitHub {
			continue
		}
		begins = append(begins, transactionBegin{
			name: string(provider) + "/" + string(auth.PurposeView),
			begin: func(ctx context.Context) (string, auth.Transaction, error) {
				return ts.BeginViewForTest(ctx, provider, resumeID, redirectURI)
			},
		})
	}
	return begins
}

type namedSecret struct {
	name  string
	value string
}

func transactionSecrets(t *testing.T, handle string, tx auth.Transaction) []namedSecret {
	t.Helper()
	secrets := []namedSecret{
		{name: "handle", value: handle},
		{name: "state", value: tx.State},
		{name: "PKCE verifier", value: tx.PKCEVerifier},
	}
	if tx.Provider == auth.ProviderGitHub {
		if tx.Nonce != "" {
			t.Errorf("GitHub transaction nonce = %q, want none", tx.Nonce)
		}
		return secrets
	}
	return append(secrets, namedSecret{name: "OIDC nonce", value: tx.Nonce})
}

type entropyDraws struct {
	mu    sync.Mutex
	salt  [32]byte
	draws [][]byte
}

func recordEntropyDraws(t *testing.T) *entropyDraws {
	t.Helper()
	r := &entropyDraws{}
	if _, err := cryptorand.Read(r.salt[:]); err != nil {
		t.Fatalf("read draw salt: %v", err)
	}
	previous := cryptorand.Reader
	cryptorand.Reader = r
	t.Cleanup(func() { cryptorand.Reader = previous })
	return r
}

func (r *entropyDraws) Read(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	draw := make([]byte, 0, len(p)+sha256.Size)
	for block := uint64(0); len(draw) < len(p); block++ {
		seed := make([]byte, 0, len(r.salt)+16)
		seed = append(seed, r.salt[:]...)
		seed = binary.BigEndian.AppendUint64(seed, uint64(len(r.draws)))
		seed = binary.BigEndian.AppendUint64(seed, block)
		sum := sha256.Sum256(seed)
		draw = append(draw, sum[:]...)
	}
	draw = draw[:len(p)]
	r.draws = append(r.draws, draw)
	return copy(p, draw), nil
}

func (r *entropyDraws) index(value string) int {
	decoded, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return -1
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for i, draw := range r.draws {
		if bytes.Equal(draw, decoded) {
			return i
		}
	}
	return -1
}

func TestOAuthTransaction_UsesIndependentSecrets(t *testing.T) {
	q := newTestQueries(t)
	ts := auth.NewTransactionStore(q)
	begins := transactionBegins(ts, createTestUser(t, q), newViewFixture(t, q, true).id)

	t.Run("distinct values", func(t *testing.T) {
		for _, b := range begins {
			t.Run(b.name, func(t *testing.T) {
				handle, tx, err := b.begin(context.Background())
				if err != nil {
					t.Fatalf("begin transaction: %v", err)
				}
				secrets := transactionSecrets(t, handle, tx)
				for i, a := range secrets {
					if a.value == "" {
						t.Errorf("%s is empty", a.name)
						continue
					}
					for _, other := range secrets[i+1:] {
						if a.value == other.value {
							t.Errorf("%s equals %s (%q)", a.name, other.name, a.value)
						}
					}
				}
				for _, sent := range secrets[1:] {
					if sent.value == "" {
						continue
					}
					if strings.Contains(handle, sent.value) || strings.Contains(sent.value, handle) {
						t.Errorf("handle %q and %s %q overlap; a value sent to the provider must not reveal the handle",
							handle, sent.name, sent.value)
					}
				}
			})
		}
	})

	t.Run("separate entropy draws", func(t *testing.T) {
		for _, b := range begins {
			t.Run(b.name, func(t *testing.T) {
				draws := recordEntropyDraws(t)
				handle, tx, err := b.begin(context.Background())
				if err != nil {
					t.Fatalf("begin transaction: %v", err)
				}
				owners := make(map[int]string)
				for _, secret := range transactionSecrets(t, handle, tx) {
					i := draws.index(secret.value)
					if i < 0 {
						t.Errorf("%s %q is not one whole entropy draw", secret.name, secret.value)
						continue
					}
					if owner, taken := owners[i]; taken {
						t.Errorf("%s and %s reuse entropy draw %d", owner, secret.name, i)
						continue
					}
					owners[i] = secret.name
				}
			})
		}
	})
}
