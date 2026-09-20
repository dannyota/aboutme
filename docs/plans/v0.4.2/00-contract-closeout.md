# Passkey contract close-out brief

Role: architect. Model: `gpt-5.6-sol`.

## Objective and authority

After the top manager accepts the independently reviewed security design, turn
the proposal into complete design, ADR, budget, wire, deployment, and acceptance
authorities that implementation can follow without inventing an interface. Read
`AGENTS.md`, `docs/plans/v0.4-roadmap.md`, all affected design files, ADRs 0024
through 0028 and 0033 through 0039, ADR 0046, the production runbook, the
current OpenAPI, and the accepted second-factor design.

## Owned paths

- Add `docs/design/second-factor-authentication.md` to this release base.
- Create `docs/design/passkey-second-factor-contract.md`.
- Create `docs/design/passkey-release-fence.md`.
- Create `docs/adr/0048-passkey-second-factor-authentication.md`.
- Modify `docs/adr/README.md`.
- Modify `docs/design/README.md`.
- Modify `docs/design/decisions.md`.
- Modify `docs/design/security.md`.
- Modify `docs/design/data.md`.
- Modify `docs/design/api.md`.
- Modify `docs/design/budgets.md`.
- Modify `docs/design/deployment.md`.

Do not edit product code, OpenAPI source, migrations, generated files,
infrastructure, plans, or the production runbook. Do not implement TOTP.

## Required contract

- State the exact v0.4.2 API request, response, status, error, cookie, CSRF,
  return-path, cache, and one-time download shapes. State which TOTP routes and
  capability fields are absent until v0.4.3.
- Define pending primary reauthentication for password and providers, including
  session binding and browser redirect behavior.
- Define WebAuthn creation and request JSON conversion, canonical base64url
  fields, optional fields, extension handling, and the exact
  `github.com/go-webauthn/webauthn` v0.18.2 dependency verified against the
  primary official source.
- Define every security mail event and data field without factor secrets.
- Define the single v0.4.2 passkey migration and confirm that no TOTP table or
  migration exists. State OAuth code cleanup ordering, the compatibility
  trigger, and mixed-version startup behavior.
- Put every count, byte, lifetime, rate, cleanup, and attempt limit in the
  budget table before implementation starts.
- Define the DynamoDB table name, key and item attributes, semantic-version
  encoding, consistent reads, conditional monotonic update, encryption,
  retention and deletion protection, deploy role, operator entrypoint, and
  failure behavior.
- Give the manager the final clauses needed to define acceptance rows for
  pending isolation, epoch enforcement, passkey verification, recovery,
  notifications, flag behavior, and the release fence.

## Checks and report

Run after writing the sources:

```bash
node_modules/.bin/prettier --check docs/design/second-factor-authentication.md docs/design/passkey-second-factor-contract.md docs/design/passkey-release-fence.md docs/adr/0048-passkey-second-factor-authentication.md docs/adr/README.md docs/design/README.md docs/design/decisions.md docs/design/security.md docs/design/data.md docs/design/api.md docs/design/budgets.md docs/design/deployment.md
npx markdownlint-cli2 docs/design/second-factor-authentication.md docs/design/passkey-second-factor-contract.md docs/design/passkey-release-fence.md docs/adr/0048-passkey-second-factor-authentication.md docs/adr/README.md docs/design/README.md docs/design/decisions.md docs/design/security.md docs/design/data.md docs/design/api.md docs/design/budgets.md docs/design/deployment.md
scripts/check-lengths.sh docs/design/second-factor-authentication.md docs/design/passkey-second-factor-contract.md docs/design/passkey-release-fence.md docs/adr/0048-passkey-second-factor-authentication.md docs/adr/README.md docs/design/README.md docs/design/decisions.md docs/design/security.md docs/design/data.md docs/design/api.md docs/design/budgets.md docs/design/deployment.md
```

Definition of done: no product, wire, budget, dependency, migration, or fence
choice remains for an implementer. Obtain a fresh Sol review of the complete
contract set. Report exact files, checks and results, skipped checks with
reason, review findings and confirmation, and open items. Do not perform Git
operations. Use short plain text with no em dash.
