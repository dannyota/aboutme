# Task 04 — Implement encrypted transactional email outbox

**Acceptance:** AC-AUTH-014, AC-SEC-005.

**Depends on:** T01 generated outbox queries; T03 token digest contract.

**Owned paths:** T04 paths in `file-structure.md`.

## Contract

`authmail` implements D3 AEAD and closed payload types:

```go
type Kind string
const (
  KindVerify Kind = "verify"
  KindReset Kind = "reset"
  KindPasswordChanged Kind = "password_changed"
)

type Payload struct {
  Version int    `json:"version"`
  To      string `json:"to"`
  Link    string `json:"link,omitempty"`
}

type Sealed struct {
  KeyID string
  Nonce [12]byte
  Ciphertext []byte
}

type KeyRing struct {
  activeID string
  keys map[string][32]byte
  nonce io.Reader
}
func NewKeyRing(
  activeID string, keys map[string][32]byte, nonce io.Reader,
) (*KeyRing, error)
func (k *KeyRing) Seal(jobID uuid.UUID, kind Kind, p Payload) (Sealed, error)
func (k *KeyRing) Open(jobID uuid.UUID, kind Kind, s Sealed) (Payload, error)

type EnqueueRequest struct {
  JobID uuid.UUID
  Kind Kind
  RegistrationID *uuid.UUID
  ResetTokenID *uuid.UUID
  UserID *uuid.UUID
  TokenDigest *[32]byte
  Payload Payload
  ExpiresAt time.Time
}
type Outbox struct {
  ring *KeyRing
  clock func() time.Time
}
func NewOutbox(ring *KeyRing, clock func() time.Time) (*Outbox, error)
func (o *Outbox) EnqueueTx(context.Context, *store.Queries, EnqueueRequest) error
```

The constructors receive injected nonce entropy and clock. `Payload.To` must be
canonical D1 email. `Link` is absent for password-changed and otherwise must be
the hard-coded canonical origin plus D8 fragment route. Enqueue validates scope,
seals, inserts through qtx, and never opens its own transaction. `Sealed.KeyID`
is 1–64 printable ASCII, `Nonce` is exactly 12 bytes by type, and `Ciphertext`
is 1–4,112 bytes; `Open` validates all dynamic bounds before AEAD work.

## TDD cycle

- [ ] Write REDs for active/previous construction, exact 32-byte values,
      printable key IDs, at most two configured entries, repeated or
      unrecognized IDs, exact nonce, D3 AAD, deterministic fixture, random nonce
      inequality, and ciphertext bounds.
- [ ] Add tamper REDs for every job ID/kind/key ID/nonce/ciphertext/AAD byte,
      truncated tag, wrong key, swapped rows, unknown payload version, duplicate
      JSON key, unknown field, invalid email/link, and oversized plaintext.
- [ ] Write qtx fake/live REDs proving exact scope mapping, encrypted-only
      insert, transaction ownership, enqueue error propagation, and zero insert
      on validation/encryption failure.
- [ ] Run expected RED:

  ```sh
  cd apps/server && go test ./internal/authmail -race -count=1 \
    -run 'Test(KeyRing|Seal|Open|Outbox)'
  ```

- [ ] Implement with `crypto/aes`, `cipher.NewGCM`, strict JSON decode, and
      `subtle.ConstantTimeCompare` where digests are compared. Do not add a new
      crypto dependency.
- [ ] Rerun the focused suite and:

  ```sh
  make server-build server-vet
  ```

## Adversarial checklist

- Database rows and query capture never contain destination/link plaintext or
  raw token.
- Key rotation decrypts active/previous only; removed key fails closed.
- A job cannot be relabeled, reparented, or copied to another ID.
- Enqueue failure is returned before caller commit and tests prove the caller
  transaction rolls back.
- Error/log/panic sentinels prove no key, nonce, plaintext, email, token,
  ciphertext, or raw AEAD error leaks.

## Handoff

Report exported types, AAD bytes, deterministic fixture hash, RED/GREEN, qtx
contract, and T06/T08 integration notes. Suggested commit:
`feat(auth): encrypt authentication email jobs`.
