# Pull request CI path selection

[ADR 0024](../adr/0024-delivery-gates.md) records the owner's 2026-11-01 choice.
This contract reduces runner demand for prose pull requests. Keep all contracts
to avoid splitting their checks. A broad docs exemption or dependency-based lane
selection would need more reader tracking; neither is part of this change.

## Selection

Put the selector in `scripts/ci-path-selection.py`, using Python's standard
library. Preflight publishes `selection`, exactly `full` or `prose`, after its
existing comparison and immutable-artifact guards succeed. Only `pull_request`
may emit `prose`; all pushes and manual dispatches emit `full`. Keep workflow
triggers unfiltered. Branch dispatches stay full; a landing train is a pull
request and uses the same comparison over its combined change.

The complete initial allowlist is `docs/adr/[0-9][0-9][0-9][0-9]-*.md` and
`docs/plans/**/*.md`. Match repository-relative paths case-sensitively: `*`
cannot cross `/`; `**/` matches zero or more directories. Deny before allowing:
`docs/plans/traceability/**`, and any basename `AGENTS.md` or `CLAUDE.md`. Only
regular, nonexecutable Git blobs qualify on every existing side of a change
(mode `100644`). Symlinks, submodules, mode changes, and unknown paths select
`full`. Everything outside these patterns selects `full`, including `.github/`,
`scripts/`, `Makefile`, tool pins, lockfiles, and generated files.

Exclusions and repository reader evidence:

- All `docs/api/**`: `Makefile`'s `api-check`, web `openapi-gen.sh`, server
  `resumes_contract_test.go`, and public-render `worker-build.ts` consume it.
  `scripts/generate-web-e2e-source-manifest.sh` includes `openapi.yaml` too.
- All `docs/design/**`: server `bounds_http_test.go` and
  `photo_contract_test.go` read `budgets.md`; web `fonts.test.ts` and
  `app/assets/fonts/tools/generate.py` parse `fonts.md`. Exclude the whole tree
  initially, including the schema citation target `templates/colors.md`.
- `docs/adr/README.md`: `packages/schema/test/documentation-links.test.ts` reads
  the former-ADR map. Its existence checks on numbered ADRs remain in
  `contracts` through `schema-check`; they do not read those ADRs' contents.
- All other docs, including generated brand assets: web `render-brand.mjs` reads
  `docs/brand/src`; `scripts/test/check-doc-links.test.mjs` reads the docs
  indexes and `docs/design/decisions.md`. These paths are outside the list.
- Privacy copy and source hashes: `privacy.vue` imports `app/i18n/legal.ts`;
  server `privacynotice/manifest.go` hashes compiled notice copy. The two
  `packages/publicroots/*-build-sources.v1.json` inventories cover app sources.
  All are outside the list; no privacy content is exempted by its prose form.
- Traceability is excluded conservatively. Repository searches found only a
  route-table test comment citing it, not an executable reader. General links,
  lengths, and secret checks still cover plans; their lint exemption stays.

Use the published, validated `base_sha` and the checked-out candidate `HEAD`
(the merge commit on PR runs), resolved as commit IDs. Run
`git diff --no-ext-diff --no-textconv --name-status -z --find-renames BASE HEAD --`.
Parse NUL records as bytes; never split lines, evaluate filenames, filter paths,
or use a merge-base shortcut. Accept A/M/D and R0..R100; inspect both rename
paths and both tree modes. Control bytes, invalid UTF-8, other statuses, empty
diffs, malformed/truncated records, and diff/tree-read errors select `full`.
Abort selection above 16 MiB of diff output or 30 seconds. Catch selector errors
in preflight and publish `full`; comparison/guard failures still fail preflight.
Publish only the final enum, never raw paths, to outputs and the step summary.

## Jobs and aggregate

Until runtime-smoke integration, `prose` runs `preflight`, the entire
`contracts` job, and `ci`. It omits exactly `macos-tooling`, `vn-infra`,
`server`, `server-integration`, `server-resumeapi`, `web`, `web-e2e`, and
`dev-https-proofs`. Each retains `needs: [preflight]` and runs unless event is
`pull_request` and selection is `prose`; ordinary success dependency handling
stays. No web/server job is needed: contracts already runs docs lint, link
tests/checks using `base_sha`, lengths, full-history gitleaks, schema/API
checks, operational tests, and npm audits. Keep all those steps.

The accepted
[runtime-smoke addition](local-tooling-simplification.md#production-runtime-coverage)
adds `server-runtime-smoke` after `web`, with `needs: [preflight, server, web]`
and the same full/prose condition. At that integration, add it to `ci.needs`,
the expected job-key set, and the permitted prose skip set together, making nine
omissions. Until then, preserve the eight existing omissions. A smoke failure,
missing evidence, cancellation, or unexplained skip fails full CI.

Keep `ci` as the sole check with that name, `if: always()`, and `needs` equal to
every other job. Validate the complete expected job-key set and every result,
plus preflight success and a known selection value, before accepting skips.
Reject `prose` outside PRs even if every result is successful. Success is valid;
`skipped` is valid only for a named omission in PR `prose`. Missing/null
results, unknown values, failures, cancellations, and all other skips fail.
Never treat a missing output as permission to skip. Preserve shard evidence
downloads and coverage checks whenever `dev-https-proofs` succeeds; omit them
only when that job is legitimately omitted. Failed coverage still fails the
aggregate job. Keep reusable VN triggers, read-only permissions, and no
caller-cancelling group.

## Tests and delivery

Extend `scripts/test/workflow-contract.test.mjs` to execute the actual selector
and aggregate with fixtures. Cover both patterns, nested/root plans, exclusions,
mixed changes, unknown paths, deletions, both rename directions, Unicode/spaces,
newlines, symlinks, mode changes, bad status/NUL records, unavailable refs,
empty diffs, size/timeout errors, and fallback output. Use temporary Git
repositories. Assert every event's selection, all job conditions, and
guard-before-selection ordering. Test every result per job and missing whole job
keys, unknown/absent outputs, forged `prose` on push/dispatch, and
PR/fork/Dependabot/train contexts. Mutations removing event/name restrictions or
required needs must fail. Keep proof matrices, shard verification, and unique
`ci` tests.

Assert the explicit prose skip permission in
`scripts/test/workflow-safety-test.sh`, with negative mutations. Retain its
secret, guard, and browser dependency assertions. Replace the unconditional VN
caller assertion with the exact selection condition; its internal jobs remain
full whenever called. Run both suites in existing `operational-test-fast` CI.

Update `AGENTS.md`'s "GitHub CI is the full gate" bullet to link here; update
`instructions/verification.md`'s "Runs and evidence" every-job/skip rules and
branch table, and `instructions/workflow.md` Flow 8 to mention prose trains.
Update `ci.yml` and `vn-infra.yml` comments that promise every-trigger
execution. The `main-required-ci` ruleset and release rules stay: dispatch
cannot authorize landing; tagging/deploying requires a green main push on the
exact commit.

Independent Sol review precedes implementation. Ship selector, aggregate, tests,
and instructions together; the workflow edit selects full CI. No data, schema,
client, key, or credential migration occurs. Recovery is a reviewed change
forcing `full`. New readers of an allowed path must remove its exemption in the
same change. PR authors can edit the candidate gate itself, so selection is not
a sandbox: `.github/` changes select full, and gate edits need review before the
signed merge. Application, session/MCP, network-to-edge, container, database,
and backup attackers gain no CI authority. No production access or third-party
action is added. Main may still fail after a reduced PR; its full run remains
the release boundary.
