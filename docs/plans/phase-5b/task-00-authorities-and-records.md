# P5B T00 — authorities and records

## Contract

Create the dispatchable P5B plan before code changes. Resolve the existing
traceability defect by removing P5B SSE ownership from `AC-PUB-003`; P6 owns SSE
through `AC-RT-001` and `AC-RT-002`. Mark `AC-PUB-003` `LANDED` until P7B closes
the remaining public PDF/image surface.

Add P5B-owned rows `AC-PUB-006` through `AC-PUB-010`, activate P5B in the
roadmap, and create the task, adversarial, and exit files in this directory.
This task changes delivery records only; it does not claim implementation.

## Ownership

- `docs/plans/implementation-plan.md`
- `docs/plans/traceability/ac-pub.md`
- `docs/plans/traceability/README.md`
- `docs/plans/phase-5b/**`

Owner: integration owner. Acceptance rows: planning state for `AC-PUB-006`
through `AC-PUB-010`. Numeric budgets: none exercised.

## Verification

Run:

```sh
npx prettier --check --ignore-path /dev/null \
  docs/plans/implementation-plan.md \
  docs/plans/traceability/ac-pub.md \
  docs/plans/traceability/README.md \
  docs/plans/phase-5b/*.md
npx markdownlint-cli2 \
  docs/plans/implementation-plan.md \
  docs/plans/traceability/ac-pub.md \
  docs/plans/traceability/README.md \
  'docs/plans/phase-5b/*.md'
```

Before commit, inspect only the owned diff and staged file list. Run gitleaks
for the commit under the repository's established commit workflow.
