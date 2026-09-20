# Account and sensitive-action brief

Role: backend. Model: `gpt-5.6-terra`.

## Objective and authority

Apply the accepted factor boundary to account deletion, export, slug release,
and other non-auth-package sensitive actions. Read `AGENTS.md`,
`docs/design/second-factor-authentication.md`,
`docs/design/passkey-second-factor-contract.md`, the privacy and public-artifact
designs, ADR 0022, ADR 0048, and the verified storage and session reports before
editing.

## Owned paths

- Modify `apps/server/internal/accountapi/delete.go`.
- Modify `apps/server/internal/accountapi/delete_test.go`.
- Modify `apps/server/internal/accountapi/delete_race_test.go`.
- Modify `apps/server/internal/accountapi/export.go`.
- Modify `apps/server/internal/accountapi/export_test.go`.
- Modify `apps/server/internal/resumeapi/persist.go`.
- Modify `apps/server/internal/resumeapi/persist_test.go`.
- Modify `apps/server/internal/resumeapi/resumes_publish.go`.
- Modify `apps/server/internal/resumeapi/writesafety.go`.
- Modify `apps/server/internal/resumeapi/writesafety_transaction.go`.
- Modify `apps/server/internal/resumeapi/writesafety_test.go`.
- Modify `apps/server/internal/resumeapi/completeness_test.go`.
- Modify `apps/server/internal/resumeapi/transition_test.go`.
- Modify `apps/server/internal/resumeapi/routes_test.go`.

Do not edit auth, factor, OAuth, mail, OpenAPI, web, migration, or
infrastructure files.

## Required behavior

- Account deletion, rename, unpublish, delete, and every other slug release
  revalidate the concrete current session, account epoch, primary proof, and
  factor proof under the accepted transaction lock order.
- Account deletion cascades every policy, credential, recovery, pending, and
  ceremony row without changing public-artifact revocation safety.
- Portable export contains no factor policy, credential, public key, transport,
  challenge, pending token, CSRF secret, recovery digest, or recovery code.
- Existing account and resume bounds, no-oracle responses, rate limits, and
  ambiguous-commit behavior remain unchanged.
- Concurrent deletion, slug release, factor change, session rotation, and export
  cannot bypass the epoch boundary or deadlock.

## Test-first cycle and checks

Start with `git status --short`. Add failing authorization, export-absence,
cascade, stale-session, rename, unpublish, delete, slug-release, and concurrency
cases first. The top manager must grant the database and normal test lanes
before these commands run:

```bash
(cd apps/server && go test -count=1 ./internal/accountapi ./internal/resumeapi)
(cd apps/server && golangci-lint run ./internal/accountapi/... ./internal/resumeapi/...)
make server-build server-vet server-test
```

Definition of done: account and resume sensitive actions enforce both recent
proofs when enrolled, and privacy surfaces expose no factor material. Report
exact files, checks and results, skipped checks with command and reason, race
evidence, and open items. Do not perform Git operations. Use short plain text
with no em dash.
