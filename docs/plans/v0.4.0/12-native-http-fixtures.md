# Native HTTP fixture verification brief

Status: implemented and verified in the isolated native checkout.

## Objective

Repair preexisting fixture drift without changing product behavior. The public
JSON projects schema version 4; the old frozen response still represented
version 2. Sitemap, robots, and llms output also predated the accepted public
discovery design.

The native development hydration bundle embeds checkout paths in Vue file
metadata. Two checkouts produced byte-identical bundles after replacing only
those path prefixes. Raw HTML fingerprints therefore differed across paths.

## Ownership

- Backend owns `apps/server/cmd/native-http-fixture/testdata/hashes.json`.
- Devops owns `scripts/native-http-capture.sh`,
  `scripts/native-http-hashes.mjs`, `scripts/test/native-http-capture-test.sh`,
  and `scripts/test/native-http-hashes.test.mjs`.
- The manager owns worktrees, native runtime commands, evidence, and
  integration.

Preserve every unrelated file. Do not change schema, product code, renderer
output, dependencies, or production infrastructure. No worker may start a stack
or browser while the persona uses the feature checkout.

## Required behavior and evidence

1. Compare retained main and feature captures against the current product
   contract before updating a frozen response.
2. Fetch the exact public hydration script referenced by the fixture HTML.
3. Require the script's raw SHA-256 prefix to match its served query
   fingerprint.
4. Canonicalize only the checkout-root prefix in Vue `__file` metadata. Preserve
   all other script bytes and all raw response evidence.
5. Replace only the validated hydration script fingerprint when comparing HTML.
   An ordinary link with the same URL must remain unchanged.
6. Freeze the canonical asset and HTML hashes. Changed script or HTML content
   must still fail the comparison.

The canonicalization unit cases and shell syntax checks pass. The manager ran
`make native-http-check` in `.dev/v0.4.0/native-worktree` at the localized web
commit with these fixture changes. It passed with evidence under
`.dev/native-http-evidence/run.lJtbYZ`.

The integrated release review covers the fixture changes and the preserved
fingerprint checks. Agent reports do not replace the manager's live check.
