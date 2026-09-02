# Task 00 — Authorities, design amendments, traceability, environment name

**Acceptance:** AC-AUTH-017, AC-AUTH-018, AC-SEC-006, AC-OPS-021, AC-OPS-022
rows created as PLANNED.

**Depends on:** the committed phase plan.

**Owned paths:** `docs/adr/0027-provider-login-flag.md`,
`docs/adr/0028-no-operator-surface.md`,
`docs/design/{decisions,product,web, security,api}.md`,
`docs/plans/traceability/{README,ac-auth,ac-sec,ac-ops}.md`,
`docs/plans/implementation-plan.md`, `.env.example`.

## Contract

Two short ADRs in the ADR 0025 shape (title, `Status: Accepted (date)`, Context,
Decision, Rejected alternatives, Consequences). Design pages gain the rules from
spec D1–D6 in their own words; the spec stays the phase's detailed text.
Traceability rows are added in each file's existing column order.

## Steps

- [ ] **Step 1: Write ADR 0027**

```markdown
# 0027 — Provider login behind a server flag

Status: Accepted (2026-09-02)

Amends the authentication scope in ADR 0025 and the Approved v4 product and
security design for the first release.

## Context

Version 1 ships email-and-password authentication. Google, GitHub, and LinkedIn
login are implemented and proven, but no production credentials exist for them
and the first community does not need them at launch. Leaving the routes
registered advertises dead sign-in paths and keeps three external redirect
surfaces reachable for no benefit.

## Decision

`PROVIDER_LOGIN_ENABLED` is a server flag, default `false`. When false, Go does
not register the provider start and callback routes or the authenticated
provider link and reauthentication starts; the paths return the uniform
not-found response of any unregistered route. The provider code, tests, mock
provider, and OpenAPI operations remain so a later release can turn the flag on
without a new phase. The web learns the flag through the unauthenticated
capabilities read and shows provider controls only when it is true.

## Rejected alternatives

- **Delete the provider code.** Throws away proven work and the local Google
  mock the HTTPS harness depends on.
- **Hide the buttons only.** Leaves the redirect surfaces reachable and lets the
  web and server disagree.
- **Mirror the flag into Nuxt runtime config.** Two sources of truth that can
  drift; every launcher would need a parity test.

## Consequences

- A provider-only account cannot sign in while the flag is off. No such account
  exists in production today.
- The native HTTPS harness sets the flag true because its proofs sign in through
  the local Google mock; the native HTTP stack and Compose leave it unset.
- Turning the flag on in production is a configuration change with its own
  review, not a code change.
```

- [ ] **Step 2: Write ADR 0028**

```markdown
# 0028 — No operator surface in the public application

Status: Accepted (2026-09-02)

## Context

The public application serves end users. Every route it exposes is reachable
from the internet through the same origin as resumes. A platform-admin page
would add a privileged session class, authorization code that must never fail
open, and a target that attracts credential attacks, for no v1 need.

## Decision

The public application has no platform-admin page, no privileged role, no
operator session class, and no route that reads or changes another account's
data. Operator actions run out of band with database credentials through the Go
commands under `apps/server/cmd/`. Infrastructure changes go through the
infrastructure-as-code phase. `/admin` stays a reserved public root that Caddy
denies. Any future operator need supersedes this ADR explicitly; it is never
added as a hidden or undocumented route.

## Rejected alternatives

- **A feature-flagged admin page.** A flag is one misconfiguration away from
  exposure and still ships the code to every deployment.
- **Admin routes on an internal listener.** Keeps privileged code in the same
  binary and process as the public surface; the boundary is a config line.

## Consequences

- Seeding, fixtures, migrations, and cleanup remain command-line tools that
  require a database URL and are guarded by database-name checks.
- The route table test keeps `/admin` denied; the design records the rule so a
  later phase cannot add an operator page without superseding this record.
```

- [ ] **Step 3: Index both ADRs in `docs/design/decisions.md`**

Append after the 0026 row, matching the table's column widths:

```markdown
| [0027](../adr/0027-provider-login-flag.md) | Accepted | Provider login behind
`PROVIDER_LOGIN_ENABLED`, off for v1; web reads capabilities | |
[0028](../adr/0028-no-operator-surface.md) | Accepted | No platform-admin page,
privileged role, or operator route in the public app |
```

Update the sentence "Every ADR through 0026 is accepted" in
`docs/design/README.md` to say 0028.

- [ ] **Step 4: Amend `docs/design/product.md`**

In "Core journeys", change journey 1 to "Sign in with email and password.
Provider sign-in (Google, GitHub, LinkedIn) is implemented but disabled in v1."
In the V1 scope table, change the Authentication row's decision to
"Email/password; provider login implemented behind a server flag that is off;
zero or one password credential per account". Add a "Landing and entry" section
before "Agent access":

```markdown
## Landing and entry

The home page introduces the product in a few lines and offers sign-in and
registration. It is static server-rendered text with no data fetch and no
application navigation for a visitor who is not signed in. Its copy names only
shipped behavior. Registration is public and verifies the email before an
account exists.
```

Add to "Product boundaries":

```markdown
- The public application has no operator or platform-admin surface. Operator
  actions run out of band with database credentials.
  [ADR 0028](../adr/0028-no-operator-surface.md) owns this rule.
```

- [ ] **Step 5: Amend `docs/design/web.md`**

Replace the paragraph starting "The login page keeps Google, GitHub, and
LinkedIn" with:

```markdown
The login page shows the email/password form always and the provider links only
when the capabilities read reports `providerLogin`. Registration, verification,
forgot-password, and reset-password are separate Nuxt pages. Verification and
reset strip the `#token=` fragment before any network call and load no
third-party resource. Account settings show whether a password is set and allow
add/change after recent reauthentication; the provider-linking block appears
only when `providerLogin` is true and the connected-agents block only when
`agentAccess` is true. Provider emails are never shown as a linkage decision.

The application shell renders two variants from the client-side session state:
signed out shows the brand, Sign in, Create account, and the theme toggle;
signed in shows Resumes, Settings, the account control, and the theme toggle.
Until the session read resolves, the shell renders the signed-out variant. The
editor route keeps its own top bar.

Nuxt reads `GET /api/v1/capabilities` only in the browser after hydration. A
failed read is treated as every capability false.
```

- [ ] **Step 6: Amend `docs/design/security.md`**

Append to "Provider identity":

```markdown
Provider login is gated by `PROVIDER_LOGIN_ENABLED`, default `false`. When
false, no provider start or callback route is registered and the settings
provider-link and provider-reauthentication starts are absent; each path returns
the uniform not-found response. [ADR 0027](../adr/0027-provider-login-flag.md)
records the decision.
```

Add a section after "Client address and rate limits":

```markdown
## No operator surface

The public application has no privileged role, operator session, or route that
reads or changes another account's data. `/admin` is a reserved public root that
Caddy denies. Operator actions are command-line tools that require a database
URL and a database-name guard. [ADR 0028](../adr/0028-no-operator-surface.md)
owns this boundary.
```

- [ ] **Step 7: Amend `docs/design/api.md`**

Add a row to the endpoint groups table after the `/me` row:

```markdown
| `GET /capabilities` | Unauthenticated read of optional surfaces:
`providerLogin`, `agentAccess` |
```

Add after the table: "Provider start and callback operations are registered only
when `PROVIDER_LOGIN_ENABLED` is true; the OpenAPI description on each says so.
The capabilities read is `security: []`, returns two required booleans, and uses
`Cache-Control: no-store`."

- [ ] **Step 8: Add traceability rows**

Append to `docs/plans/traceability/ac-auth.md` (state PLANNED, reference
"(pending)"), matching the table columns:

```markdown
| AC-AUTH-017 | ADR 0027; security: provider identity | With
`PROVIDER_LOGIN_ENABLED` false, every provider start/callback and provider
link/reauth start is unregistered and returns the uniform 404; no page offers
provider sign-in or linking; with it true the provider suites pass unchanged |
PF T01/T05/T06 | PLANNED | (pending) | | AC-AUTH-018 | API: capabilities |
`GET /capabilities` is unauthenticated, returns exactly the required
`providerLogin` and `agentAccess` booleans reflecting configuration, rejects
other methods, and is `no-store` | PF T02 | PLANNED | (pending) |
```

Update the intro sentence to "Eighteen acceptance-criterion rows".

Append to `docs/plans/traceability/ac-sec.md`:

```markdown
| AC-SEC-006 | ADR 0028; security: no operator surface | No privileged role,
operator session, or cross-account route exists; `/admin` stays denied; the
settings page requests only surfaces the capabilities read enables | PF T00/T05
| PLANNED | (pending) |
```

Update its intro to "Six acceptance-criterion rows".

Append to `docs/plans/traceability/ac-ops.md`:

```markdown
| AC-OPS-021 | Development seed | `dev-seed` is idempotent, refuses any database
not named `aboutme_dev` or not on loopback, never overwrites an existing
credential or document, and runs only from the native development script | PF
T03 | PLANNED | (pending) | | AC-OPS-022 | Entry flow proof | Headless
trusted-browser proof: landing renders the D5 heading and both buttons without
app navigation; the seed user signs in and lands on the resume list with the
signed-in shell; console, page, certificate, and external-request errors are
zero | PF T04/T06 | PLANNED | (pending) |
```

Update its intro to "23 acceptance-criterion rows". In
`docs/plans/traceability/README.md`, change the matrix index counts (`AC-AUTH`
18, `AC-SEC` 6, `AC-OPS` 23, total 129) and the Ownership sentence to "Phases PM
and PF are active; see [PM](../phase-pm/README.md) and
[PF](../phase-pf/README.md)."

- [ ] **Step 9: Add the environment name**

In `.env.example`, after the `MCP_ENABLED=` block:

```dotenv
# Provider (Google, GitHub, LinkedIn) login. Leave false or blank to keep the
# provider start and callback routes unregistered; v1 ships password-only
# sign-in (docs/design/security.md, "Provider identity").
PROVIDER_LOGIN_ENABLED=
```

- [ ] **Step 10: Update the roadmap**

In `docs/plans/implementation-plan.md`, confirm the PF row of the state table
links to this phase's `README.md` and reads
`Planned: flag, landing, seed, capabilities, no-operator ADR` in its state cell;
change nothing else in the roadmap during this phase until T07.

- [ ] **Step 11: Format, lint, and check links**

````sh
make docs-fmt
python3 - <<'PY'
import os,re,subprocess
root=subprocess.check_output(["git","rev-parse","--show-toplevel"],text=True).strip()
bad=0
for f in subprocess.check_output(["git","ls-files","*.md","**/*.md"],cwd=root,text=True).split():
    if "node_modules" in f: continue
    t=re.sub(r"```.*?```","",open(os.path.join(root,f),encoding="utf-8").read(),flags=re.S)
    for m in re.finditer(r"\[[^\]]*\]\(([^)\s]+)\)",t):
        u=m.group(1)
        if u.startswith(("http","mailto:","#")): continue
        p=os.path.normpath(os.path.join(root,os.path.dirname(f),u.split("#")[0]))
        if not os.path.exists(p): print(f,u); bad+=1
print("broken",bad); raise SystemExit(bad)
PY
````

Expected: `make docs-fmt` reports 0 issues; the link script prints `broken 0`.

## Handoff

Report the files changed and the two check outputs. Suggested commit:
`docs: adopt v1 entry experience authorities`.
