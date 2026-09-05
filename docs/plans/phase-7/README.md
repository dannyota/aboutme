# Phase 7: PDF and generated images

Status: Active, 2026-09-05. Base commit: `3baf732`.

Build owner PDF export, then live-gated public exports, using the shared Vue
renderer and bounded Chromium jobs. This phase runs locally and changes no AWS
resources.

## Authorities

- [System](../../design/system.md), [web](../../design/web.md),
  [print](../../design/templates/print.md), and
  [numeric budgets](../../design/budgets.md).
- [ADR 0022](../../adr/0022-public-artifact-revocation.md) owns public admission
  and revocation. [ADR 0023](../../adr/0023-private-print-capability.md) owns
  print capability and completion authority.
- [ADR 0024](../../adr/0024-single-pass-delivery-gates.md) owns delivery gates.
- [AC-PDF](../traceability/ac-pdf.md), [AC-PUB](../traceability/ac-pub.md), and
  [AC-SEC](../traceability/ac-sec.md) own acceptance rows.

## Delivery

| Task                           | Deliverable                                      | State                                               |
| ------------------------------ | ------------------------------------------------ | --------------------------------------------------- |
| [7.1](task-7.1-owner-print.md) | Bounded print jobs, private print SSR, owner PDF | Implemented; integration checks in progress         |
| 7.2                            | Public PDF and generated images                  | Implemented; browser and resource gates in progress |

The owner approved the proposed live-gated `1200×630` PNG at
`/api/v1/public/resumes/{slug}/og.png`, showing the top of the resume.
[ADR 0032](../../adr/0032-public-share-image.md) fixes that contract and
corrects the print target table's contradictory image-pagination statement.

## Execution rules

- Use subagent-driven development with the repository's overrides: one author
  per slice, test first, no per-task reviewer, one fresh Sol phase review before
  push. Workers never use Git.
- The integration owner keeps manifests, generated files, configuration,
  composition, gates, plans, and traceability. Workers own disjoint files.
- Run heavy commands serially using `flock --close .dev/phase-7/heavy.lock`.
  Full `make ci` runs alone. Preserve the shared database container.
- The unchanged candidate must pass [exit criteria](exit-criteria.md),
  `make ci`, and connected `make scan`. Remove this directory on phase exit;
  history keeps the plan.
