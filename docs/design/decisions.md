# 10. Decision status

This design integrates the outcomes below. The ADR remains the rationale and
supersession record; these pages state the resulting design. Every record
through 0025 is accepted; 0014–0019 and 0021–0023 were accepted with the v4
approval on 2026-08-12, and 0025 with the password-authentication authority on
2026-08-16.

| ADR                                                                 | Status   | Integrated outcome                                                            |
| ------------------------------------------------------------------- | -------- | ----------------------------------------------------------------------------- |
| [0001](../adr/0001-agpl-3.0-license.md)                             | Accepted | Repository and hosted service use AGPL-3.0                                    |
| [0002](../adr/0002-go-api-nuxt-ssr-split.md)                        | Accepted | Go API and one shared Nuxt/Vue renderer                                       |
| [0003](../adr/0003-sse-over-websocket.md)                           | Accepted | HTTP autosave and SSE invalidation                                            |
| [0004](../adr/0004-resume-slug-only-urls.md)                        | Accepted | Globally unique resume slugs; users are not public                            |
| [0005](../adr/0005-draft-permissive-documents.md)                   | Accepted | Draft-permissive storage and publish-strict completeness                      |
| [0006](../adr/0006-schema-derived-codegen.md)                       | Accepted | Schema-derived code generation and cross-language conformance                 |
| [0007](../adr/0007-unversioned-health-endpoints.md)                 | Accepted | Root health and readiness routes                                              |
| [0008](../adr/0008-template-apply-semantics.md)                     | Accepted | Presets compute placement against current section keys                        |
| [0009](../adr/0009-section-order-authority.md)                      | Accepted | Layout arrays own section order                                               |
| [0010](../adr/0010-goose-only-migrations.md)                        | Accepted | Goose migrations are the relational schema source                             |
| [0011](../adr/0011-risk-tiered-delivery-gates.md)                   | Accepted | Risk-tiered task review and two phase gates                                   |
| [0012](../adr/0012-ssr-sanitizer-authority.md)                      | Accepted | Go owns SSR sanitizing; DOMPurify is client-only                              |
| [0013](../adr/0013-contact-detail-rendering.md)                     | Accepted | Array-ordered plain-text contacts with custom labels                          |
| [0014](../adr/0014-oauth-start-methods.md)                          | Accepted | GET login start; CSRF-protected POST link and reauth start                    |
| [0015](../adr/0015-session-rotation-delivery.md)                    | Accepted | One successor with bounded predecessor fallback until delivery                |
| [0016](../adr/0016-transactional-idempotency.md)                    | Accepted | Mutation and replay record commit together                                    |
| [0017](../adr/0017-resume-document-versioning.md)                   | Accepted | Pure read projection, CAS persistence, explicit converters                    |
| [0018](../adr/0018-bounded-rate-limiter.md)                         | Accepted | No active-bucket eviction under key churn                                     |
| [0019](../adr/0019-private-media-delivery.md)                       | Accepted | Private object storage behind live-gated Go reads                             |
| [0020](../adr/0020-uat-migration-baseline.md)                       | Accepted | First UAT freezes development migration history                               |
| [0021](../adr/0021-template-placement-order.md)                     | Accepted | Validate exact placement; order by selector then current position             |
| [0022](../adr/0022-public-artifact-revocation.md)                   | Accepted | Revalidate every public reuse; fence and drain generation leases              |
| [0023](../adr/0023-private-print-capability.md)                     | Accepted | One-use 256-bit, 60-second capability bound to snapshot and job               |
| [0024](../adr/0024-single-pass-delivery-gates.md)                   | Accepted | One author pass per task and one review per phase                             |
| [0025](../adr/0025-password-authentication-and-identity-linking.md) | Accepted | Email/password credential alongside providers; email is never an identity key |

## Remaining gates

| Gate                                                   | Owner                                     | Due                                                    |
| ------------------------------------------------------ | ----------------------------------------- | ------------------------------------------------------ |
| Per-asset font license, notice, and Reserved Font Name | Integration owner                         | As each P3 Task 5 asset is admitted, and before T5B    |
| Product name and trademark review                      | Human owner                               | Before P10 production promotion                        |
| Privacy and disclosure review                          | Qualified privacy counsel and human owner | Before P10 production promotion                        |
| Human authorization of cloud resources                 | Human owner                               | After local UAT passes, before any AWS or DNS mutation |

The font gate stays per asset because it is a legal check on exact bytes, not a
design question. The last two are legal and commercial reviews that only the
human owner can close; they gate production promotion, not development.

ADRs 0022 and 0023 carry the highest implementation complexity in the design and
belong to P5A and P7A. They are accepted as the target behavior. If their
mechanisms prove disproportionate when those phases are planned, simplify
through a superseding ADR rather than by quietly implementing less.

## Proposed v1 limits

The immutable v1 resume schema cannot add template identity, photo visibility,
section visibility, or a global section-icon toggle. V1 implements those limits
honestly. A later schema version may add them through the document-version
process; renderer code does not invent hidden, out-of-contract fields.

Font families are a catalog, not a fixed product identity. The catalog can grow
through reviewed schema and asset changes when the exact license permits free
self-hosting, redistribution, and PDF embedding without usage fees. A modified
asset also requires modification rights and compliance with naming conditions.

## Change process

No line in an accepted ADR is edited to make history look consistent. V4 is
approved, so a changed decision needs a new ADR or a v5 revision. Status words
are exact: “approved” means the decision is settled; “landed” describes
repository state and does not imply that a gate passed.
