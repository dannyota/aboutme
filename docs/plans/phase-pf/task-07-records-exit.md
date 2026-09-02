# Task 07 — Records, review, exit, and plan deletion

**Acceptance:** every PF row PROVEN.

**Depends on:** T00–T06 accepted.

**Owned paths (integration owner):**
`docs/plans/traceability/{README,ac-auth, ac-sec,ac-ops}.md`,
`docs/architecture.md`, `docs/plans/implementation-plan.md`,
`docs/runbooks/native-development.md`, `docs/plans/phase-pf/` (deleted at exit).

## Steps

- [ ] **Step 1: Traceability**

Set the five rows to PROVEN with these references:

- AC-AUTH-017: `internal/config/config_test.go` `TestLoad_ProviderLoginFlag*`;
  `internal/auth/handlers_test.go` `*ProviderLoginDisabled*`;
  `apps/web/test/{login,sessions}.test.ts` gating cases;
  `deploy/dev-https-browser/entry.spec.ts` (flag-on branch).
- AC-AUTH-018: `docs/api/test/capabilities.test.ts`;
  `internal/api/capabilities_test.go`; `cmd/server/main_test.go`
  `TestCapabilitiesRegistrarReflectsConfig`.
- AC-SEC-006: ADR 0028; `internal/routetable/route_table_test.go` `/admin` cases
  (unchanged); `apps/web/test/sessions.test.ts` "never requests the grant list".
- AC-OPS-021: `cmd/dev-seed/seed_test.go`; `make operational-test`.
- AC-OPS-022: `apps/web/test/{app-chrome,landing}.test.ts`;
  `deploy/dev-https-browser/entry.spec.ts` via `make dev-https-entry-check`.

- [ ] **Step 2: Architecture**

In `docs/architecture.md`: add "landing page" to the Nuxt row of the component
table; in "Implemented HTTP surface" add a bullet "an unauthenticated
capabilities read that reports whether provider login and agent access are
enabled" and change the provider-login bullet to say those routes are registered
only when `PROVIDER_LOGIN_ENABLED` is true (off by default); add a paragraph
after the agent-access section: "The web shell renders a signed-out variant
(Sign in, Create account) until the session read resolves and a signed-in
variant (Resumes, Settings, account) afterward. The login and settings pages
show provider and connected-agent controls only when the capabilities read
enables them. The native development script seeds one account and one private
sample resume." Keep the file near 300 lines.

- [ ] **Step 3: Roadmap and runbook**

In `docs/plans/implementation-plan.md`, move PF to the "Complete and pushed"
sentence, remove its row from the state table and the phase index, and drop
`PF --> P9` from the graph. Confirm `docs/runbooks/native-development.md`
carries the seed credentials (T03) and `docs/runbooks/local-uat.md` the entry
check (T06).

- [ ] **Step 4: Fresh review**

Dispatch one non-author reviewer (Sonnet) over the integrated diff since the PF
base commit. The brief names the invariants to confirm: provider routes absent
when the flag is off and unchanged when on; capabilities is unauthenticated,
exact, and `no-store`; the seed refuses non-`aboutme_dev` databases and never
overwrites; no operator surface was added and `/admin` stays denied; Nuxt
performs no server-side fetch of Go; landing copy names no unshipped feature.
Fix findings; the same reviewer confirms.

- [ ] **Step 5: Exit checklist and gates**

Run every item in `exit-criteria.md` at the candidate commit, then:

```sh
make ci        # foreground chunks per the environment constraint
make scan      # with SEMGREP_APP_TOKEN
```

- [ ] **Step 6: Exit commit**

```sh
git rm -r docs/plans/phase-pf
git commit -m "docs: record phase PF exit" -- docs/plans/phase-pf docs/plans/implementation-plan.md docs/plans/traceability docs/architecture.md
```

The commit body lists the exit checklist result, the reviewer verdict, and the
`make ci` and `make scan` outcomes. Push after the commit.
