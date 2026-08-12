# Phase 0A — Contracts, budgets, traceability (implementation plan)

> **For agentic workers:** execute with superpowers:subagent-driven-development,
> one task per fresh subagent, Opus 5 review between tasks. Steps are `- [ ]`.

**Goal:** Freeze the contracts everything else depends on — the resume document
schema with generated Go/TS/Dart types, the OpenAPI envelope, numeric budgets,
and the traceability matrix.

**Base:** commit `c8ce9f3`. **Design:** [data](../design/data.md),
[API](../design/api.md), and [repository boundaries](../design/repository.md).
**Master plan:** `implementation-plan.md` (P0A = tasks 0.1, 0.1b, 0.8, 0.11).

**Why first:** the master plan's integration discipline says P0 freezes
contracts; every later phase regenerates from these files rather than redefining
shapes. A contract change after this phase is a dedicated reviewed commit.

## Environment facts (verified 2026-08-01)

- Go 1.26.5, Node 24.18.1, podman 5.8.4 available locally.
- **Mobile is deferred to a post-deployment phase (P11)** — no Dart generation,
  no Flutter stub, no Dart CI in this phase. The schema stays language-neutral
  so Dart types can be generated when P11 starts.
- Codegen tool: **quicktype** (npm, no sudo) — covers Go + TS from JSON Schema.
  Pin the exact version in `package.json`.

## Global constraints (inherited — apply to every task)

- Latest stable, then **pinned exactly**; commit the lockfile.
- Google style guides; `gofmt`/`goimports`; Google TS via ESLint.
- Generated code is **committed**; CI fails on drift.
- Determinism: no clock/RNG in generated output; stable key ordering.
- Conventional Commits; no AI/agent mentions in messages.
- `make docs-fmt` before committing any `.md`.

## File structure produced by this phase

| File                                                   | Responsibility                                              |
| ------------------------------------------------------ | ----------------------------------------------------------- |
| `packages/schema/resume.schema.json`                   | The resume document contract (single source of truth)       |
| `packages/schema/fixtures/*.json`                      | Valid + invalid documents used by every later phase's tests |
| `packages/schema/test/schema.test.ts`                  | Validates fixtures against the schema                       |
| `packages/schema/gen/{go,ts}/`                         | Committed generated types                                   |
| `packages/schema/package.json`, `scripts/generate.mjs` | Pinned codegen pipeline                                     |
| `docs/api/openapi.yaml`                                | `/api/v1` contract: envelope, errors, health                |
| `docs/plans/budgets.md`                                | Numeric performance/resource budgets                        |
| `docs/plans/traceability.md`                           | Spec statement → acceptance ID → owning phase               |

---

### Task 1: Budgets and traceability documents

**Files:**

- Create: `docs/plans/budgets.md`, `docs/plans/traceability.md`

**Interfaces:**

- Produces: acceptance ID scheme `AC-<AREA>-<NNN>` (areas: AUTH, DOC, SAVE, PUB,
  SEO, RT, PDF, PRIV, SEC, A11Y, OPS) used by every later phase plan and by the
  UAT report.

- [ ] **Step 1: Write `docs/plans/budgets.md`**

Content (exact values — these become CI/staging assertions in P7A/P9A):

```markdown
# Numeric budgets

Enforced in P7A (resource bounds) and P9A (staging rehearsal). A budget breach
fails the gate; changing a number requires a reviewed commit citing evidence.

| Budget                                   | Target                               | Where enforced |
| ---------------------------------------- | ------------------------------------ | -------------- |
| API p95 latency (read, warm)             | ≤ 150 ms                             | P9A synthetic  |
| API p95 latency (granular PATCH)         | ≤ 250 ms                             | P9A synthetic  |
| Public SSR page p95 (origin, uncached)   | ≤ 400 ms                             | P9A synthetic  |
| PDF render wall time                     | ≤ 20 s hard timeout, p95 ≤ 8 s       | P7A            |
| Concurrent renders                       | 1 (v1)                               | P7A            |
| Render queue depth                       | ≤ 8, then 503 + readiness unhealthy  | P7A            |
| Whole server task memory (Go + Chromium) | ≤ 512 MiB cgroup                     | P7A, P9A       |
| pgx pool size                            | ≤ 20, below Postgres max_connections | P0B config     |
| SSE concurrent connections per task      | ≤ 2000                               | P6A stress     |
| SSE file descriptors headroom            | ≥ 25% below ulimit                   | P6A stress     |
| SSE heartbeat interval                   | 25 s (< CloudFront idle timeout)     | P6A, P9A       |
| Request body                             | ≤ 256 KB                             | P0B middleware |
| Resume document total                    | ≤ 512 KB                             | P2A store      |
```

- [ ] **Step 2: Write `docs/plans/traceability.md`**

Seed the matrix with the acceptance-ID scheme and one row per normative spec
statement currently known. Header + first rows exactly:

```markdown
# Spec traceability matrix

**Completion target:** one row per normative statement in the linked design
clause below. Every phase plan must resolve its rows (owning task + test
reference) before independent approval. Acceptance IDs are stable and referenced
by the UAT report.

| ID          | Design clause                                                                                                                                  | Statement                                                                         | Phase/task | Test / UAT reference |
| ----------- | ---------------------------------------------------------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------- | ---------- | -------------------- |
| AC-DOC-001  | [Data: relational model](../design/data.md#relational-model)                                                                                   | Max 3 resumes per user, DB-enforced                                               | P2A        | (pending)            |
| AC-DOC-002  | [Data: resume aggregate](../design/data.md#resume-aggregate)                                                                                   | Entry ids unique across the whole resume                                          | P2A        | (pending)            |
| AC-DOC-003  | [Data: resume aggregate](../design/data.md#resume-aggregate)                                                                                   | Date range: start ≤ end; present ⇒ end null                                       | P2A        | (pending)            |
| AC-DOC-004  | [Data: bounds and invariants](../design/data.md#bounds-and-invariants)                                                                         | Size bounds rejected at limit+1                                                   | P2A        | (pending)            |
| AC-AUTH-001 | [Security: provider identity](../design/security.md#provider-identity)                                                                         | No automatic email merge across providers                                         | P1         | (pending)            |
| AC-AUTH-002 | [Security: provider identity](../design/security.md#provider-identity)                                                                         | LinkedIn registration without verified email rejected                             | P1         | (pending)            |
| AC-AUTH-003 | [Security: OAuth transaction](../design/security.md#oauth-transaction)                                                                         | GitHub receives no OIDC nonce/iss checks                                          | P1         | (pending)            |
| AC-AUTH-004 | [Security: sessions](../design/security.md#sessions)                                                                                           | Session rotation >24h is atomic with grace interval                               | P1         | (pending)            |
| AC-AUTH-005 | [Security: sessions](../design/security.md#sessions)                                                                                           | Device list, per-session revoke, logout-everywhere, Clear-Site-Data               | P1         | (pending)            |
| AC-SAVE-001 | [API: resume write safety](../design/api.md#resume-write-safety)                                                                               | Stale If-Match returns 412 with current revision                                  | P2B        | (pending)            |
| AC-SAVE-002 | [API: resume write safety](../design/api.md#resume-write-safety)                                                                               | Idempotent replay returns stored response; different body rejected                | P2B        | (pending)            |
| AC-PUB-001  | [Product: public namespace](../design/product.md#public-namespace)                                                                             | Unpublish keeps the slug; only rename/delete release it                           | P5A        | (pending)            |
| AC-PUB-002  | [Product: public namespace](../design/product.md#public-namespace)                                                                             | Released slugs tombstoned 180 days                                                | P5A        | (pending)            |
| AC-PUB-003  | [Product: publish controls](../design/product.md#publish-controls)                                                                             | Publish-state matrix: live=false ⇒ all surfaces 404/410                           | P5A        | (pending)            |
| AC-PUB-004  | [Product: publish controls](../design/product.md#publish-controls)                                                                             | seo_geo off ⇒ X-Robots-Tag noindex, .md 404, excluded from sitemap                | P5A        | (pending)            |
| AC-PUB-005  | [Product: publish controls](../design/product.md#publish-controls)                                                                             | download_enabled gates the public PDF endpoint                                    | P7B        | (pending)            |
| AC-SEC-001  | [Security: document content](../design/security.md#untrusted-document-content)                                                                 | Hostile corpus neutralized by both sanitizers + browser                           | P3         | (pending)            |
| AC-SEC-002  | [Security: CSRF and origin](../design/security.md#csrf-and-canonical-origin)                                                                   | CSRF: token + exact Origin, fail closed                                           | P1         | (pending)            |
| AC-RT-001   | [Realtime: delivery semantics](../design/realtime.md#delivery-semantics)                                                                       | SSE reconnect always refetches (NOTIFY not durable)                               | P6A        | (pending)            |
| AC-RT-002   | [Realtime: delivery semantics](../design/realtime.md#delivery-semantics)                                                                       | Public stream closes immediately on unpublish                                     | P6B        | (pending)            |
| AC-PDF-001  | [Web: pure renderer](../design/web.md#pure-renderer); [operations: resource budgets](../design/operations.md#resource-and-performance-budgets) | Renders bounded: 1 concurrent, timeout kill, readiness on saturation, no outbound | P7A        | (pending)            |
| AC-PRIV-001 | [Operations: privacy lifecycle](../design/operations.md#privacy-lifecycle)                                                                     | DELETE /me purges account, resumes, media, sessions; creates tombstones           | P8-priv    | (pending)            |
| AC-OPS-001  | [Deployment: database and releases](../design/deployment.md#database-and-releases)                                                             | Two-runner migration is advisory-locked                                           | P9A        | (pending)            |
| AC-OPS-002  | [Deployment: production topology](../design/deployment.md#production-topology)                                                                 | Origin rejects requests lacking the CloudFront secret                             | P9A        | (pending)            |
```

- [ ] **Step 3: Verify formatting**

Run: `make docs-fmt` Expected: `Summary: 0 issues in 0 files`

- [ ] **Step 4: Commit**

```bash
git add docs/plans/budgets.md docs/plans/traceability.md
git commit -m "docs: add numeric budgets and spec traceability matrix"
```

---

### Task 2: Resume document JSON Schema + fixtures

**Files:**

- Create: `packages/schema/resume.schema.json`,
  `packages/schema/fixtures/{minimal,full,invalid-*}.json`,
  `packages/schema/package.json`, `packages/schema/test/schema.test.ts`

**Interfaces:**

- Produces: `$id: https://aboutme.vn/schema/resume/v1`; top-level object
  `{schemaVersion: integer, personalDetails, content, customization}`;
  `sectionType` enum
  `["profile","work","education","skill","language", "certificate","project","custom"]`;
  `DateRange` =
  `{start:{y:integer,m?:integer}, end:{y:integer,m?:integer}|null, present:boolean}`;
  sanitizer allowlist version constant `SANITIZER_ALLOWLIST_VERSION = 1`. Later
  phases import these names verbatim.

- [ ] **Step 1: Create the package manifest with pinned tooling**

`packages/schema/package.json`:

```json
{
  "name": "@aboutme/schema",
  "private": true,
  "type": "module",
  "scripts": {
    "test": "vitest run",
    "generate": "node scripts/generate.mjs"
  }
}
```

Then pin exact dev dependencies (no ranges):

```bash
cd packages/schema
npm install --save-dev --save-exact ajv ajv-formats vitest quicktype
```

- [ ] **Step 2: Write the failing fixture-validation test**

`packages/schema/test/schema.test.ts`:

```ts
import { readFileSync, readdirSync } from "node:fs";
import { join } from "node:path";
import Ajv2020 from "ajv/dist/2020.js";
import addFormats from "ajv-formats";
import { describe, expect, it } from "vitest";

const root = new URL("..", import.meta.url).pathname;
const schema = JSON.parse(
  readFileSync(join(root, "resume.schema.json"), "utf8"),
);
const ajv = addFormats(new Ajv2020({ allErrors: true, strict: true }));
const validate = ajv.compile(schema);

const fixture = (name: string) =>
  JSON.parse(readFileSync(join(root, "fixtures", name), "utf8"));

const names = readdirSync(join(root, "fixtures"));

describe("resume schema", () => {
  it("accepts every valid fixture", () => {
    for (const name of names.filter((n) => !n.startsWith("invalid-"))) {
      expect(
        validate(fixture(name)),
        `${name}: ${ajv.errorsText(validate.errors)}`,
      ).toBe(true);
    }
  });

  it("rejects every invalid fixture", () => {
    for (const name of names.filter((n) => n.startsWith("invalid-"))) {
      expect(validate(fixture(name)), `${name} should be invalid`).toBe(false);
    }
  });

  it("pins the sanitizer allowlist version", () => {
    expect(schema.$defs.sanitizerAllowlistVersion.const).toBe(1);
  });

  it("requires present entries to omit an end date", () => {
    expect(
      validate({
        ...fixture("minimal.json"),
        content: {
          work: {
            sectionType: "work",
            displayName: "Experience",
            iconKey: "briefcase",
            entries: [
              {
                id: "018f0000-0000-7000-8000-000000000001",
                isHidden: false,
                jobTitle: "Engineer",
                employer: "Acme",
                dates: { start: { y: 2020 }, end: { y: 2022 }, present: true },
              },
            ],
          },
        },
      }),
    ).toBe(false);
  });
});
```

- [ ] **Step 3: Run the test to verify it fails**

Run: `cd packages/schema && npm test` Expected: FAIL — `resume.schema.json` does
not exist (ENOENT).

- [ ] **Step 4: Write `resume.schema.json`**

JSON Schema 2020-12. Required shape (implement exactly; expand entry types per
the [resume aggregate](../design/data.md#resume-aggregate)): `$id`, `$schema`,
`type: object`, `additionalProperties: false`,
`required: ["schemaVersion","personalDetails","content","customization"]`.

`$defs` must include: `sanitizerAllowlistVersion` (`const: 1`), `uuid`
(`type: string`, `format: uuid`), `richText` (`type: string`,
`maxLength: 16384`), `dateRange` (object: `start`
`{y: integer 1900..2100, m?: integer 1..12}`, `end` same or `null`,
`present: boolean`; enforce `present ⇒ end === null` with `if/then`),
`entryBase` (`{id: uuid, isHidden: boolean}`), and one `$def` per `sectionType`
extending `entryBase` with the fields listed in the
[resume aggregate's entry table](../design/data.md#resume-aggregate).

`content` is an object with `propertyNames` matching `^[a-z]+$|^[0-9a-f-]{36}$`
(built-in keys or custom uuid keys), each value
`{sectionType, displayName, iconKey, entries[]}` with `maxItems: 64`, and
`oneOf` dispatch on `sectionType` selecting the matching entry `$def`.

`customization` mirrors the
[resume aggregate](../design/data.md#resume-aggregate) (font, colors, spacing,
heading, layout with `sections` order arrays, per-type display configs,
`pageFormat`, date formats).

- [ ] **Step 5: Write the fixtures**

`fixtures/minimal.json` (smallest valid doc), `fixtures/full.json` (every
section type, both date forms, custom section, hidden entry — this becomes the
golden-render fixture in P3), and invalid fixtures:
`invalid-present-with-end.json`, `invalid-duplicate-entry-id.json`,
`invalid-unknown-section-type.json`, `invalid-oversize-richtext.json`,
`invalid-missing-required.json`.

Note: duplicate-entry-id cannot be expressed in JSON Schema — that fixture
exists for the P2A store test (`AC-DOC-002`); keep it in a separate
`fixtures/store/` directory so the schema test does not consume it.

- [ ] **Step 6: Run the test to verify it passes**

Run: `cd packages/schema && npm test` Expected: PASS, all four tests.

- [ ] **Step 7: Commit**

```bash
git add packages/schema
git commit -m "feat(schema): add resume document JSON Schema with fixtures"
```

---

### Task 3: Code generation (Go/TS) + drift check

**Files:**

- Create: `packages/schema/scripts/generate.mjs`,
  `packages/schema/gen/{go,ts,dart}/` (generated, committed),
  `packages/schema/test/gen.test.ts`
- Modify: `Makefile`, `.github/workflows/ci.yml`

**Interfaces:**

- Consumes: `resume.schema.json` from Task 2.
- Produces: Go package `schema` at `packages/schema/gen/go/resume.go`; TS types
  at `gen/ts/resume.ts`; Dart at `gen/dart/resume.dart`. Go module path
  `github.com/dannyota/aboutme/packages/schema/gen/go` (imported by
  `apps/server` in P0B).

- [ ] **Step 1: Write the generator**

`packages/schema/scripts/generate.mjs` runs quicktype three times with
deterministic settings (`--src-lang schema`, no timestamps, stable ordering),
writing into `gen/go`, `gen/ts`, `gen/dart`. Go output gets `package schema`;
Dart gets a library declaration. Emit a header comment
`// Code generated from resume.schema.json. DO NOT EDIT.` in each file.

- [ ] **Step 2: Write the failing drift test**

`packages/schema/test/gen.test.ts`:

```ts
import { execFileSync } from "node:child_process";
import { readFileSync } from "node:fs";
import { describe, expect, it } from "vitest";

const files = ["gen/go/resume.go", "gen/ts/resume.ts", "gen/dart/resume.dart"];

describe("generated code", () => {
  it("is byte-identical to a fresh generation", () => {
    const before = files.map((f) => readFileSync(f, "utf8"));
    execFileSync("node", ["scripts/generate.mjs"], { stdio: "inherit" });
    const after = files.map((f) => readFileSync(f, "utf8"));
    expect(after).toEqual(before);
  });

  it("marks every file as generated", () => {
    for (const f of files) {
      expect(readFileSync(f, "utf8")).toContain("DO NOT EDIT");
    }
  });
});
```

- [ ] **Step 3: Run to verify it fails**

Run: `cd packages/schema && npm test` Expected: FAIL — generated files do not
exist.

- [ ] **Step 4: Generate and verify Go compiles**

```bash
cd packages/schema && npm run generate
cd gen/go && go mod init github.com/dannyota/aboutme/packages/schema/gen/go && go build ./...
```

Expected: builds with no errors.

- [ ] **Step 5: Run tests to verify they pass**

Run: `cd packages/schema && npm test` Expected: PASS.

- [ ] **Step 6: Wire Make + CI**

Add to root `Makefile`:

```make
schema-gen: ## Regenerate schema types (Go/TS)
 cd packages/schema && npm run generate

schema-check: ## Fail if generated types drift from the schema
 cd packages/schema && npm ci && npm test
```

Add a `schema` job to `.github/workflows/ci.yml` running `make schema-check`
plus `go build ./...` in `packages/schema/gen/go`.

- [ ] **Step 7: Commit**

```bash
git add packages/schema Makefile .github/workflows/ci.yml
git commit -m "feat(schema): generate committed Go/TS types with drift check"
```

---

### Task 4: OpenAPI contract for `/api/v1`

**Files:**

- Create: `docs/api/openapi.yaml`, `docs/api/test/openapi.test.ts` (or a Go test
  in P0B — pick the JS harness here since no Go module exists yet)
- Modify: root `package.json` (pin `@redocly/cli`), `Makefile`, CI

**Interfaces:**

- Produces: the response envelope schemas `Envelope` (`{data: object}`) and
  `Error` (`{error: {code: string, message: string}}`); the `Revision` header
  convention (`ETag: "r<n>"`, `If-Match`); documented `412` and `409` semantics;
  health paths `/healthz`, `/readyz`. Every later phase **adds** paths to this
  file; it is never rewritten.

- [ ] **Step 1: Write `docs/api/openapi.yaml`**

OpenAPI 3.1. `servers: [{url: https://aboutme.vn/api/v1}]`. Components:
`Envelope`, `Error`, `Revision` (string), parameters `IfMatch`,
`IdempotencyKey`. Paths for this phase: `GET /healthz` (200 plain),
`GET /readyz` (200 / 503 with `Error`). Document in `description` that all
mutating endpoints require `If-Match` and return `412` on staleness, `409` only
for domain conflicts ([API conventions](../design/api.md#conventions)).

- [ ] **Step 2: Write the failing validation test**

Pin the linter and assert the document is valid and contains the invariants:

```bash
npm install --save-dev --save-exact @redocly/cli
```

`docs/api/test/openapi.test.ts`:

```ts
import { execFileSync } from "node:child_process";
import { readFileSync } from "node:fs";
import { parse } from "yaml";
import { describe, expect, it } from "vitest";

const doc = parse(readFileSync("docs/api/openapi.yaml", "utf8"));

describe("openapi contract", () => {
  it("lints clean", () => {
    execFileSync("npx", ["redocly", "lint", "docs/api/openapi.yaml"], {
      stdio: "inherit",
    });
  });

  it("serves /api/v1", () => {
    expect(doc.servers[0].url).toMatch(/\/api\/v1$/);
  });

  it("defines the error envelope", () => {
    expect(doc.components.schemas.Error.properties.error.required).toEqual([
      "code",
      "message",
    ]);
  });

  it("documents 412 for stale writes", () => {
    expect(JSON.stringify(doc)).toContain("412");
  });
});
```

- [ ] **Step 3: Run to verify it fails, then passes**

Run: `npx vitest run docs/api/test/openapi.test.ts` Expected: FAIL before the
YAML exists → PASS after Step 1 is complete.

- [ ] **Step 4: Wire Make + CI**

```make
api-check: ## Lint and test the OpenAPI contract
 npx redocly lint docs/api/openapi.yaml && npx vitest run docs/api/test
```

Add to the CI `docs`/`schema` job.

- [ ] **Step 5: Commit**

```bash
git add docs/api Makefile package.json package-lock.json .github/workflows/ci.yml
git commit -m "feat(api): add OpenAPI contract with envelope and health paths"
```

---

## Phase exit criteria

- [ ] `packages/schema` validates all fixtures; generated Go/TS committed and
      drift-checked; Go package compiles.
- [ ] `docs/api/openapi.yaml` lints clean and documents envelope + `412`/`409`.
- [ ] `docs/plans/budgets.md` and `docs/plans/traceability.md` exist with the
      acceptance-ID scheme in use.
- [ ] All CI jobs green; `make docs-lint` clean.
- [ ] Opus 5 has reviewed every task diff; blocking findings resolved.
