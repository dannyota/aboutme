# TOTP cryptography brief

Role: backend. Model: Sonnet (Codex: `gpt-5.6-terra`).

## Objective and authority

Implement the isolated TOTP profile, provisioning encoder, and authenticated
secret key ring without database or HTTP behavior. Read `AGENTS.md`, the
accepted TOTP contract, accepted ADR 0049, the budget design, and the existing
auth-mail key-ring implementation for repository conventions.

## Owned paths

- Create `apps/server/internal/secondfactor/totp.go`.
- Create `apps/server/internal/secondfactor/totp_test.go`.
- Create `apps/server/internal/secondfactor/totp_crypto.go`.
- Create `apps/server/internal/secondfactor/totp_crypto_test.go`.
- Create `apps/server/internal/secondfactor/totp_provisioning.go`.
- Create `apps/server/internal/secondfactor/totp_provisioning_test.go`.

Do not edit services, handlers, stores, SQL, mail, config, commands, OpenAPI,
web, dependencies, infrastructure, or design. Use Go standard-library
cryptography only.

## Required behavior

- Generate and verify the accepted RFC 6238 profile with injected entropy and
  clock. Compare all eligible candidates in constant time and return the
  greatest match without logging material.
- Enforce six ASCII digits, nonnegative time, one-step skew, canonical unpadded
  Base32, and exact grouped display.
- Build the exact issuer, label, percent encoding, query order, and 2,048-byte
  provisioning URI.
- Seal and open only exact 20-byte secrets with AES-256-GCM, fresh 12-byte
  nonces, the accepted domain-separated associated data, one active key, and at
  most one previous key. Seal takes the caller's row ID; it never creates one.
- Derive each 26-byte `tk1_` key ID from its key value exactly as the
  key-management design states. Reject a ring whose two keys derive one ID.
- Return typed unknown-key and authenticated-decryption failures that lifecycle
  code maps to a per-row 503 without exposing a key, record, or ciphertext
  value.
- Prove RFC 6238 SHA-1 vectors, six-digit leading zeroes, step boundaries,
  duplicate matches, negative time, malformed code and secret input, URI
  encoding, nonce failure, tamper, truncation, row and account swaps,
  record-kind swaps, version swaps, unknown keys, key-ID derivation vectors,
  equal active and previous keys, and active versus previous behavior.

## Hosted checks and report

Write the failing vector and hostile-input tests first. Do not run local tests,
builds, lint, installs, database writes, browsers, or stacks. Report these as
unrun pending exact-candidate GitHub CI:

```bash
(cd apps/server && go test ./internal/secondfactor)
(cd apps/server && golangci-lint run ./internal/secondfactor/...)
make server-build server-vet server-test
```

Definition of done: pure TOTP, provisioning, and encryption APIs enforce every
accepted bound and expose no database or HTTP policy. Report exact files, checks
and results, skipped commands and reason, vector evidence, and open items. Do
not perform Git operations. Use short plain text with no em dash. Code, tests,
comments, and living docs never cite plans, tasks, phases, or review findings;
cite the design, ADR, or `AC-*` ID instead.
