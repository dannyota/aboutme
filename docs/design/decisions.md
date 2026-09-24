# 10. Decision status

This design integrates the outcomes below. Each ADR keeps its rationale and
supersession record; these pages state the resulting design. ADRs 0001–0049 are
accepted.

| ADR                                                                  | Status   | Integrated outcome                                                                                                     |
| -------------------------------------------------------------------- | -------- | ---------------------------------------------------------------------------------------------------------------------- |
| [0001](../adr/0001-agpl-3.0-license.md)                              | Accepted | Repository and hosted service use AGPL-3.0                                                                             |
| [0002](../adr/0002-go-api-nuxt-ssr-split.md)                         | Accepted | Go API and one shared Nuxt/Vue renderer                                                                                |
| [0003](../adr/0003-sse-over-websocket.md)                            | Accepted | HTTP autosave and SSE invalidation                                                                                     |
| [0004](../adr/0004-resume-slug-only-urls.md)                         | Accepted | Globally unique resume slugs; users are not public                                                                     |
| [0005](../adr/0005-draft-permissive-documents.md)                    | Accepted | Draft-permissive storage and publish-strict completeness                                                               |
| [0006](../adr/0006-schema-derived-codegen.md)                        | Accepted | Schema-derived code generation and cross-language conformance                                                          |
| [0007](../adr/0007-unversioned-health-endpoints.md)                  | Accepted | Root health and readiness routes                                                                                       |
| [0008](../adr/0008-template-apply-semantics.md)                      | Accepted | Presets compute placement against current section keys                                                                 |
| [0009](../adr/0009-section-order-authority.md)                       | Accepted | Layout arrays own section order                                                                                        |
| [0010](../adr/0010-goose-only-migrations.md)                         | Accepted | Goose migrations are the relational schema source                                                                      |
| [0011](../adr/0011-risk-tiered-delivery-gates.md)                    | Accepted | Author discipline and early browser checks remain; local full-gate and review clauses superseded by 0046               |
| [0012](../adr/0012-ssr-sanitizer-authority.md)                       | Accepted | Go owns SSR sanitizing; DOMPurify is client-only                                                                       |
| [0013](../adr/0013-contact-detail-rendering.md)                      | Accepted | Array-ordered contacts and custom labels; link and icon-label rules amended by 0040, 0041, and 0043                    |
| [0014](../adr/0014-oauth-start-methods.md)                           | Accepted | GET login start; CSRF-protected POST link and reauth start                                                             |
| [0015](../adr/0015-session-rotation-delivery.md)                     | Accepted | One successor with bounded predecessor fallback until delivery                                                         |
| [0016](../adr/0016-transactional-idempotency.md)                     | Accepted | Mutation and replay record commit together                                                                             |
| [0017](../adr/0017-resume-document-versioning.md)                    | Accepted | Pure read projection, CAS persistence, explicit converters                                                             |
| [0018](../adr/0018-bounded-rate-limiter.md)                          | Accepted | No active-bucket eviction under key churn                                                                              |
| [0019](../adr/0019-private-media-delivery.md)                        | Accepted | Private object storage behind live-gated Go reads                                                                      |
| [0020](../adr/0020-uat-migration-baseline.md)                        | Accepted | A committed marker freezes migration history                                                                           |
| [0021](../adr/0021-template-placement-order.md)                      | Accepted | Validate exact placement; order by selector then current position                                                      |
| [0022](../adr/0022-public-artifact-revocation.md)                    | Accepted | Revalidate every public reuse; fence and drain generation leases                                                       |
| [0023](../adr/0023-private-print-capability.md)                      | Accepted | One-use 256-bit, 60-second capability bound to snapshot and job                                                        |
| [0024](../adr/0024-single-pass-delivery-gates.md)                    | Accepted | One author pass per change remains; full-gate and review clauses superseded by 0046                                    |
| [0025](../adr/0025-password-authentication-and-identity-linking.md)  | Accepted | Email/password credential alongside providers; email is never an identity key                                          |
| [0026](../adr/0026-mcp-agent-access.md)                              | Accepted | Remote MCP endpoint and first-party OAuth 2.1 server; editor parity minus publish                                      |
| [0027](../adr/0027-provider-login-flag.md)                           | Accepted | Provider login behind `PROVIDER_LOGIN_ENABLED`; web reads capabilities; provider selection amended by 0039             |
| [0028](../adr/0028-no-operator-surface.md)                           | Accepted | No platform-admin page, privileged role, or operator route in the public app                                           |
| [0029](../adr/0029-application-ui-toolkit.md)                        | Accepted | Tailwind v4 and shadcn-vue chrome without Preflight; renderer stays isolated                                           |
| [0030](../adr/0030-stamped-document-visual-identity.md)              | Accepted | Stamped-document identity: seal red only for public state, signature ink actions, Be Vietnam Pro chrome                |
| [0031](../adr/0031-aws-cost-research-and-hosted-uat.md)              | Accepted | AWS cost research, OpenTofu, managed AWS services; hosted UAT superseded by 0037                                       |
| [0032](../adr/0032-public-share-image.md)                            | Accepted | One live-gated public PNG share image from the continuous resume renderer                                              |
| [0033](../adr/0033-public-image-builds-private-deployment.md)        | Accepted | Public ARM64 build/smoke; ECR publication superseded by 0037                                                           |
| [0034](../adr/0034-scheduled-uat-and-production-autoscaling.md)      | Accepted | Scheduled UAT and autoscaling; superseded by 0036 and 0037                                                             |
| [0035](../adr/0035-replica-coordination-and-uat-lifecycle.md)        | Accepted | Fleet coordination contract retained for a later second replica; runtime and hosted UAT superseded by 0037 and 0038    |
| [0036](../adr/0036-single-replica-launch-and-pipeline-migrations.md) | Accepted | One serving replica for the first release; migrations run as a deployment step; wake implementation retired            |
| [0037](../adr/0037-single-host-production-without-hosted-uat.md)     | Accepted | First release deploys straight to single-host production behind Cloudflare; no hosted UAT until about 500 users        |
| [0038](../adr/0038-single-baseline-and-plain-migrator.md)            | Accepted | One baseline migration with explicit app grants; plain goose migrator; replica runtime removed until a second replica  |
| [0039](../adr/0039-per-provider-login-enablement.md)                 | Accepted | `PROVIDER_LOGIN_ENABLED` enables providers one at a time; production can enable only Google                            |
| [0040](../adr/0040-contact-labels-beside-icons.md)                   | Accepted | Icons replace default contact labels; linked addresses display without scheme or trailing slash                        |
| [0041](../adr/0041-contact-link-display-and-body-justify.md)         | Accepted | Document v3: custom https links, per-detail link display, justified body text; GitHub and X brand marks                |
| [0042](../adr/0042-public-page-title-and-favicon.md)                 | Accepted | Owner-set public page title and one-emoji favicon as publication settings; exact server-computed head values           |
| [0043](../adr/0043-email-and-phone-links.md)                         | Accepted | Email and phone details link as `mailto:` and `tel:` only after a strict renderer check                                |
| [0044](../adr/0044-header-photo-position-and-project-subtitle.md)    | Accepted | Document v4: header photo on top, left, or right; optional project entry subtitle                                      |
| [0045](../adr/0045-pdf-download-name-and-metadata.md)                | Accepted | PDFs download as `<Full-Name>-Resume.pdf` with an RFC 5987 UTF-8 name; PDF Title and dates come from the revision      |
| [0046](../adr/0046-github-ci-delivery-gate.md)                       | Accepted | Narrow local checks and per-commit gitleaks; one fresh review before push; exact green GitHub CI commit before release |
| [0047](../adr/0047-bilingual-resume-workspace.md)                    | Accepted | Vietnamese and English resume workspace; interface toggles never change resume data or resume language                 |
| [0048](../adr/0048-passkey-second-factor-authentication.md)          | Accepted | Optional passkey second factor, recovery codes, authority epoch, exact wire contract, and release fence                |
| [0049](../adr/0049-totp-second-factor-authentication.md)             | Accepted | Optional authenticator-app second factor, sealed secrets with a derived-ID key ring, shared recovery, and floor v0.4.7 |

## Remaining gates

| Gate                                                   | Owner                                     | Due                            |
| ------------------------------------------------------ | ----------------------------------------- | ------------------------------ |
| Per-asset font license, notice, and Reserved Font Name | Integration owner                         | Whenever a font asset is added |
| Product name and trademark review                      | Human owner                               | Before the public announcement |
| Privacy and disclosure review                          | Qualified privacy counsel and human owner | Before the public announcement |
| SES production access                                  | Human owner                               | Before the public announcement |

The font gate stays per asset because it is a legal check on exact bytes.
[ADR 0037](../adr/0037-single-host-production-without-hosted-uat.md) is the
production approval; hosting cost, topology, and the release path follow it.

Known document limits, such as no template identity or section visibility, are
listed in [template limits](templates/limitations.md). A later document version
may lift one through [ADR 0017](../adr/0017-resume-document-versioning.md); the
renderer never invents an out-of-contract field.

## Change process

A changed decision needs a new ADR; a structural rewrite needs a v5 revision of
this design. Neither silently rewrites approved text, and no accepted ADR line
is edited to make history look consistent. A correction that fixes an error,
ambiguity, or contradiction without changing a decision is an ordinary edit.
Status words are exact: "approved" means the decision is settled; "landed"
describes repository state and does not imply that a gate passed.
