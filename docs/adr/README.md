# Architecture decision records

An architecture decision record (ADR) records one proposed or accepted choice,
its reason, and its consequences. The current design states the resulting rules
in [`../design/`](../design/README.md), and the
[decision status](../design/decisions.md) page maps each record to the design
pages that apply it.

Each record states the decisions in force for one topic. Its History section
names the former records it replaces and why each changed. To change a decision
in force, edit its record with the owner's approval and add a History line
naming what changed and why. A new record is only for a new topic. A proposed
record may change until accepted.

## Records

| ADR                                                      | Status   | Decides                                                                          |
| -------------------------------------------------------- | -------- | -------------------------------------------------------------------------------- |
| [0001](0001-agpl-3.0-license.md)                         | Accepted | AGPL-3.0-only license                                                            |
| [0002](0002-system-architecture.md)                      | Accepted | Go API, one Vue renderer, HTTP autosave, SSE refresh, root health routes         |
| [0003](0003-public-namespace-and-no-operator-surface.md) | Accepted | Globally unique resume slugs; no operator surface in the public app              |
| [0004](0004-resume-document-contract.md)                 | Accepted | Draft-permissive documents, schema codegen, section order, versions and losses   |
| [0005](0005-database-migrations.md)                      | Accepted | Goose migrations, one baseline, explicit grants, deploy-step migrator            |
| [0006](0006-transactional-idempotency.md)                | Accepted | Idempotency records commit with the mutation                                     |
| [0007](0007-bounded-rate-limiter.md)                     | Accepted | Bounded rate-limit state with overflow and expiry                                |
| [0008](0008-ssr-sanitizer-authority.md)                  | Accepted | Go sanitizes for SSR; DOMPurify is client-only                                   |
| [0009](0009-private-media-delivery.md)                   | Accepted | Private media behind live-gated reads, with durable deletion                     |
| [0010](0010-public-artifact-revocation.md)               | Accepted | Live-state gate before every public reuse; revocation fences                     |
| [0011](0011-print-capability-and-pdf-output.md)          | Accepted | One-use print capability; PDF file name, title, and dates                        |
| [0012](0012-template-placement.md)                       | Accepted | Template presets carry a placement rule with one total order                     |
| [0013](0013-resume-header-and-contacts.md)               | Accepted | Contact order, labels, and links; header photo; project subtitle; justified text |
| [0014](0014-public-page-head-and-link-preview.md)        | Accepted | Public page title and favicon; stored, versioned link-preview card               |
| [0015](0015-accounts-passwords-and-sessions.md)          | Accepted | Passwords beside providers; email is not an identity key; session rotation       |
| [0016](0016-sign-in-providers.md)                        | Accepted | OAuth start methods, per-provider enablement, the LinkedIn flow and its risk     |
| [0017](0017-second-factor-authentication.md)             | Accepted | Optional passkeys and TOTP, recovery codes, the epoch, the release fence         |
| [0018](0018-mcp-agent-access.md)                         | Accepted | Remote MCP endpoint and first-party OAuth 2.1; editor parity minus publish       |
| [0019](0019-application-ui-toolkit.md)                   | Accepted | Tailwind and shadcn-vue chrome, renderer isolation, guarded token edits          |
| [0020](0020-application-visual-identity.md)              | Accepted | Seal logo and stamp, aurora canvas, two blues, type scale, white resume          |
| [0021](0021-bilingual-resume-workspace.md)               | Accepted | Vietnamese and English workspace, separate from resume language                  |
| [0022](0022-viewer-privacy-and-counting.md)              | Accepted | Anonymous layered view counts; sign in to view with nothing stored               |
| [0023](0023-linkedin-import.md)                          | Accepted | LinkedIn import from the English Save to PDF, parsed in the browser              |
| [0024](0024-delivery-gates.md)                           | Accepted | GitHub CI is the full gate; one author pass and one fresh review                 |
| [0025](0025-single-host-production.md)                   | Accepted | One AWS host, no hosted UAT, CloudFront edge, Route 53 DNS                       |
| [0026](0026-replica-scaling.md)                          | Accepted | One serving replica; the designed path to a second                               |
| [0027](0027-vietnam-hosted-production.md)                | Accepted | Move production to Vietnam-hosted providers                                      |
| [0028](0028-deployment-transparency-observer.md)         | Accepted | An independent observer publishes the running image digests                      |
| [0029](0029-community-showcase.md)                       | Proposed | Opt-in, reviewed, uncached, never-indexed showcase of published resumes          |

## Former numbers

On 2026-09-27 the 65 records then in the repository were merged into the set
above and renumbered. Commit messages, baselined migrations, released schema
files, and code generated from those schemas still cite the former numbers; this
table resolves them. Git history keeps every former record.

| Former | Former file                                          | Now                                                      |
| ------ | ---------------------------------------------------- | -------------------------------------------------------- |
| 0001   | `agpl-3.0-license`                                   | [0001](0001-agpl-3.0-license.md)                         |
| 0002   | `go-api-nuxt-ssr-split`                              | [0002](0002-system-architecture.md)                      |
| 0003   | `sse-over-websocket`                                 | [0002](0002-system-architecture.md)                      |
| 0004   | `resume-slug-only-urls`                              | [0003](0003-public-namespace-and-no-operator-surface.md) |
| 0005   | `draft-permissive-documents`                         | [0004](0004-resume-document-contract.md)                 |
| 0006   | `schema-derived-codegen`                             | [0004](0004-resume-document-contract.md)                 |
| 0007   | `unversioned-health-endpoints`                       | [0002](0002-system-architecture.md)                      |
| 0008   | `template-apply-semantics`                           | [0012](0012-template-placement.md)                       |
| 0009   | `section-order-authority`                            | [0004](0004-resume-document-contract.md)                 |
| 0010   | `goose-only-migrations`                              | [0005](0005-database-migrations.md)                      |
| 0011   | `risk-tiered-delivery-gates`                         | [0024](0024-delivery-gates.md)                           |
| 0012   | `ssr-sanitizer-authority`                            | [0008](0008-ssr-sanitizer-authority.md)                  |
| 0013   | `contact-detail-rendering`                           | [0013](0013-resume-header-and-contacts.md)               |
| 0014   | `oauth-start-methods`                                | [0016](0016-sign-in-providers.md)                        |
| 0015   | `session-rotation-delivery`                          | [0015](0015-accounts-passwords-and-sessions.md)          |
| 0016   | `transactional-idempotency`                          | [0006](0006-transactional-idempotency.md)                |
| 0017   | `resume-document-versioning`                         | [0004](0004-resume-document-contract.md)                 |
| 0018   | `bounded-rate-limiter`                               | [0007](0007-bounded-rate-limiter.md)                     |
| 0019   | `private-media-delivery`                             | [0009](0009-private-media-delivery.md)                   |
| 0020   | `uat-migration-baseline`                             | [0005](0005-database-migrations.md)                      |
| 0021   | `template-placement-order`                           | [0012](0012-template-placement.md)                       |
| 0022   | `public-artifact-revocation`                         | [0010](0010-public-artifact-revocation.md)               |
| 0023   | `private-print-capability`                           | [0011](0011-print-capability-and-pdf-output.md)          |
| 0024   | `single-pass-delivery-gates`                         | [0024](0024-delivery-gates.md)                           |
| 0025   | `password-authentication-and-identity-linking`       | [0015](0015-accounts-passwords-and-sessions.md)          |
| 0026   | `mcp-agent-access`                                   | [0018](0018-mcp-agent-access.md)                         |
| 0027   | `provider-login-flag`                                | [0016](0016-sign-in-providers.md)                        |
| 0028   | `no-operator-surface`                                | [0003](0003-public-namespace-and-no-operator-surface.md) |
| 0029   | `application-ui-toolkit`                             | [0019](0019-application-ui-toolkit.md)                   |
| 0030   | `stamped-document-visual-identity`                   | [0020](0020-application-visual-identity.md)              |
| 0031   | `aws-cost-research-and-hosted-uat`                   | [0025](0025-single-host-production.md)                   |
| 0032   | `public-share-image`                                 | [0014](0014-public-page-head-and-link-preview.md)        |
| 0033   | `public-image-builds-private-deployment`             | [0025](0025-single-host-production.md)                   |
| 0034   | `scheduled-uat-and-production-autoscaling`           | [0026](0026-replica-scaling.md)                          |
| 0035   | `replica-coordination-and-uat-lifecycle`             | [0026](0026-replica-scaling.md)                          |
| 0036   | `single-replica-launch-and-pipeline-migrations`      | [0026](0026-replica-scaling.md)                          |
| 0037   | `single-host-production-without-hosted-uat`          | [0025](0025-single-host-production.md)                   |
| 0038   | `single-baseline-and-plain-migrator`                 | [0005](0005-database-migrations.md)                      |
| 0039   | `per-provider-login-enablement`                      | [0016](0016-sign-in-providers.md)                        |
| 0040   | `contact-labels-beside-icons`                        | [0013](0013-resume-header-and-contacts.md)               |
| 0041   | `contact-link-display-and-body-justify`              | [0013](0013-resume-header-and-contacts.md)               |
| 0042   | `public-page-title-and-favicon`                      | [0014](0014-public-page-head-and-link-preview.md)        |
| 0043   | `email-and-phone-links`                              | [0013](0013-resume-header-and-contacts.md)               |
| 0044   | `header-photo-position-and-project-subtitle`         | [0013](0013-resume-header-and-contacts.md)               |
| 0045   | `pdf-download-name-and-metadata`                     | [0011](0011-print-capability-and-pdf-output.md)          |
| 0046   | `github-ci-delivery-gate`                            | [0024](0024-delivery-gates.md)                           |
| 0047   | `bilingual-resume-workspace`                         | [0021](0021-bilingual-resume-workspace.md)               |
| 0048   | `passkey-second-factor-authentication`               | [0017](0017-second-factor-authentication.md)             |
| 0049   | `totp-second-factor-authentication`                  | [0017](0017-second-factor-authentication.md)             |
| 0050   | `aurora-application-identity`                        | [0020](0020-application-visual-identity.md)              |
| 0051   | `vietnam-hosted-production`                          | [0027](0027-vietnam-hosted-production.md)                |
| 0052   | `guarded-token-edits-to-generated-primitives`        | [0019](0019-application-ui-toolkit.md)                   |
| 0053   | `public-pdf-tab-renders-the-download-in-the-browser` | Removed: rejected; public pages have no PDF tab          |
| 0054   | `cloudfront-edge-for-single-host-production`         | [0025](0025-single-host-production.md)                   |
| 0055   | `stored-link-preview-card`                           | [0014](0014-public-page-head-and-link-preview.md)        |
| 0056   | `route-53-production-dns`                            | [0025](0025-single-host-production.md)                   |
| 0057   | `deployment-transparency-observer`                   | [0028](0028-deployment-transparency-observer.md)         |
| 0058   | `linkedin-sign-in-in-production`                     | [0016](0016-sign-in-providers.md)                        |
| 0059   | `linkedin-import-in-the-browser`                     | [0023](0023-linkedin-import.md)                          |
| 0060   | `viewer-data-controller-and-consent`                 | [0022](0022-viewer-privacy-and-counting.md)              |
| 0061   | `layered-human-view-counting`                        | [0022](0022-viewer-privacy-and-counting.md)              |
| 0062   | `sign-in-to-view-without-an-account`                 | [0022](0022-viewer-privacy-and-counting.md)              |
| 0063   | `linkedin-sign-in-without-a-nonce-claim`             | [0016](0016-sign-in-providers.md)                        |
| 0064   | `linkedin-import-from-save-to-pdf`                   | [0023](0023-linkedin-import.md)                          |
| 0065   | `seal-identity`                                      | [0020](0020-application-visual-identity.md)              |
