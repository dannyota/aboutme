# Task 00 — Reconcile authorities, budgets, traceability, and public roots

**Acceptance:** AC-AUTH-008, AC-AUTH-015, AC-OPS-020.

**Depends on:** Integrated Phase 4 and Phase 5A; approved Phase PA plan.

**Owned paths:** T00 paths in `file-structure.md`. This is a serialized
integration-owner window.

## Contract

Approved v4 must describe the D1–D10 password/account/session/mail model and
must no longer say the product stores no passwords. ADR 0025 records why
application-owned credentials coexist with provider identities and why email is
not a provider-link key. `budgets.md` receives every numeric D1–D7 bound.

The closed root registry becomes `public-roots.v5.json` and adds these Nuxt
roots without changing any existing row or dispatch:

```json
{"root":"forgot-password","dispatch":"nuxt"}
{"root":"register","dispatch":"nuxt"}
{"root":"reset-password","dispatch":"nuxt"}
{"root":"verify-email","dispatch":"nuxt"}
```

The generator rejects a v4 source once the v5 contract lands and regenerates all
current Go, Nuxt, Caddy, and fixture consumers from v5. T00 atomically updates
both build-source manifests plus every runtime/static reference in
`scripts/dev-native.sh`, `scripts/dev-https.sh`, `scripts/dev-https-test.sh`,
and `deploy/dev-https-browser/static-test.sh`. Historical Phase 5A plan text
remains unchanged.

## TDD cycle

- [ ] Add failing design/ADR contract tests or focused source assertions that
      require password credentials, subject-first provider resolution,
      user-fenced session creation, exact forced session replacement, encrypted
      outbox, and no password removal/email change.
- [ ] Add failing budget assertions for every D1–D7 value, including the 4 KiB
      body, 254-byte email, Argon2id/admission, token, HIBP cache/deadline,
      outbox ciphertext/key ring, leases/batches/retries, and capture bounds.
- [ ] Change the public-root source test first. Run:

  ```sh
  node --test packages/publicroots/public-roots.test.mjs
  ```

  Confirm RED names the missing v5 source and four roots.

- [ ] Write `docs/adr/0025-password-authentication-and-identity-linking.md` with
      Status Accepted, context, decision, rejected alternatives, migration
      order, session fence, provider-link rules, and consequences.
- [ ] Update Approved v4 product, security, data, API, deployment, web,
      decisions, and README text. Keep existing provider/session/CSRF
      guarantees. State that cloud SES activation stays authorization-gated.
- [ ] Add the numeric budget rows and explanations without changing unrelated
      phase values.
- [ ] Rename the source to v5, update generator authority/version/path checks,
      regenerate consumers through the documented generator, and prove old
      registry drift fails closed. Update every nonhistorical v4 reference in
      the two build manifests, three lifecycle scripts, and browser static test
      in the same owner window.
- [ ] Update trace links/statements for the new planned criteria. Do not mark a
      behavior LANDED or PROVEN before its implementation/evidence exists.
- [ ] Run the narrow GREEN checks:

  ```sh
  node scripts/generate-public-roots.mjs --check
  make public-roots-check route-table-test
  npx prettier --check --ignore-path /dev/null \
    docs/design docs/adr/0025-password-authentication-and-identity-linking.md \
    docs/plans/budgets.md docs/plans/traceability
  npx markdownlint-cli2 \
    'docs/design/**/*.md' \
    docs/adr/0025-password-authentication-and-identity-linking.md \
    docs/plans/budgets.md 'docs/plans/traceability/*.md'
  ```

- [ ] Scan the owned diff for stale `provider-only`, old v4 registry paths,
      missing bounds, accidental route reclassification, and secret examples.

## Adversarial checklist

- Duplicate/unknown/reordered root rows and v4/v5 path disagreement fail.
- All existing route dispatch values remain byte-identical.
- No design permits auto email merge, provider email synchronization, password
  removal, account-email change, plaintext mail storage, or a session issuer
  outside the user fence.
- No example contains a real email, token, key, AWS account, sender identity, or
  internal hostname.

## Handoff

Report changed authority clauses, ADR decision, exact budget rows, registry
source/output hashes, RED/GREEN commands, and any downstream name change.
Suggested commit: `docs(auth): adopt password authentication authority`.
