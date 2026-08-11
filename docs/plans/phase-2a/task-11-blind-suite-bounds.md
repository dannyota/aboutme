# Task 11: Blind adversarial suite C — independently-derived size-bound limit+1 matrix

Same independence protocol as Tasks 9–10, a **third fresh instance** — not the
author of Task 9, not the author of Task 10, and not Task 5's author. Mandated
by the owner's B13 ruling: Task 5's own `bounds_test.go` and its schema-walk
completeness guard are both written by the same author who writes `validate.go`
— a self-consistent harness can still share the author's blind spot about which
bounds matter or how they're phrased. An independent author deriving the same
limit+1 matrix from the budget and the frozen schema alone, without reading Task
5's implementation or its own test file, is the check against that.

**Inputs the blind author gets:** `../budgets.md`, spec §3 (the size-bounds
bullet and the aggregate-invariant bullet), `packages/schema/resume.schema.json`
(the frozen schema itself, read directly for every `maxLength`/`maxItems`/
`maxProperties` declaration — not Task 5's inventory of them), and this plan's
Task 5 **Interfaces block only** (`ValidateForStore`, `AssembleCanonical`,
`MaxDocumentBytes`, `ValidationError` — signatures, not bodies). **Inputs
withheld:** `apps/server/internal/resume/bounds_test.go`,
`apps/server/internal/resume/validate.go`, and every other Task 5 implementation
file. The author of Task 5 must not edit this suite; weakening any assertion
requires Opus 5 review by name.

**Files:** create `apps/server/internal/resume/bounds_adversarial_test.go`.

Minimum matrix (the blind author may add, never subtract): one limit/limit+1
pair per bound found by independently walking `resume.schema.json` plus the
`../budgets.md` 512 KB total-document bound — total doc bytes, section count,
entries-per-section, personal-details count, and each distinct `maxLength` class
in the schema — each asserting `ValidateForStore` accepts the doc at the limit
and rejects it at limit+1 with a `*ValidationError`. A disagreement between this
suite's independently-derived matrix and Task 5's own bounds inventory (a bound
this suite finds that Task 5's completeness guard didn't, or vice versa) is
itself a blocking finding for Opus review, not something either author
reconciles unilaterally.

- [ ] **Step 1 (blind author): derive the matrix from `../budgets.md` / spec §3
      / `resume.schema.json` / Task 5's Interfaces block only; write the suite;
      run it** — expected mostly green if Task 5 is correct; any red, or any
      bound this suite exercises that Task 5's harness doesn't, is a real
      finding routed to Task 5's implementer, never fixed by the suite author.
- [ ] **Step 2: gate.** Pure unit tests, no DB:
      `cd apps/server && go build ./... && go vet ./... &&     go test ./internal/resume/... -run BoundsAdversarial -count=1 -v`,
      then the full `go test ./internal/resume/... -count=1`.
- [ ] **Step 3: commit** (blind author's own commit) —
      `git commit -m "test(resume): add independently-derived adversarial bounds suite" -- apps/server/internal/resume/bounds_adversarial_test.go`
