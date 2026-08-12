# 10. Decision status

This draft integrates the outcomes below. The ADR remains the rationale and
supersession record; these pages state the resulting design. ADRs 0014–0019 are
proposed with Draft v4 and require the same explicit approval.

| ADR                                                 | Status   | Integrated outcome                                             |
| --------------------------------------------------- | -------- | -------------------------------------------------------------- |
| [0001](../adr/0001-agpl-3.0-license.md)             | Accepted | Repository and hosted service use AGPL-3.0                     |
| [0002](../adr/0002-go-api-nuxt-ssr-split.md)        | Accepted | Go API and one shared Nuxt/Vue renderer                        |
| [0003](../adr/0003-sse-over-websocket.md)           | Accepted | HTTP autosave and SSE invalidation                             |
| [0004](../adr/0004-resume-slug-only-urls.md)        | Accepted | Globally unique resume slugs; users are not public             |
| [0005](../adr/0005-draft-permissive-documents.md)   | Accepted | Draft-permissive storage and publish-strict completeness       |
| [0006](../adr/0006-schema-derived-codegen.md)       | Accepted | Schema-derived code generation and cross-language conformance  |
| [0007](../adr/0007-unversioned-health-endpoints.md) | Accepted | Root health and readiness routes                               |
| [0008](../adr/0008-template-apply-semantics.md)     | Accepted | Presets compute placement against current section keys         |
| [0009](../adr/0009-section-order-authority.md)      | Accepted | Layout arrays own section order                                |
| [0010](../adr/0010-goose-only-migrations.md)        | Accepted | Goose migrations are the relational schema source              |
| [0011](../adr/0011-risk-tiered-delivery-gates.md)   | Accepted | Risk-tiered task review and two phase gates                    |
| [0012](../adr/0012-ssr-sanitizer-authority.md)      | Accepted | Go owns SSR sanitizing; DOMPurify is client-only               |
| [0013](../adr/0013-contact-detail-rendering.md)     | Accepted | Array-ordered plain-text contacts with custom labels           |
| [0014](../adr/0014-oauth-start-methods.md)          | Proposed | GET login start; CSRF-protected POST link and reauth start     |
| [0015](../adr/0015-session-rotation-delivery.md)    | Proposed | One successor with bounded predecessor fallback until delivery |
| [0016](../adr/0016-transactional-idempotency.md)    | Proposed | Mutation and replay record commit together                     |
| [0017](../adr/0017-resume-document-versioning.md)   | Proposed | Pure read projection, CAS persistence, explicit converters     |
| [0018](../adr/0018-bounded-rate-limiter.md)         | Proposed | No active-bucket eviction under key churn                      |
| [0019](../adr/0019-private-media-delivery.md)       | Proposed | Private object storage behind live-gated Go reads              |

## Open approval gates

| Gate                          | Owner                             | Deadline or phase                         |
| ----------------------------- | --------------------------------- | ----------------------------------------- |
| Draft v4 design approval      | Design owner                      | After independent design and plan reviews |
| Template contract approval    | Design owner                      | Before Phase 3 implementation             |
| Product name/trademark review | Human owner                       | Before production launch approval         |
| Privacy and disclosure review | Qualified counsel and human owner | Before production launch approval         |

The retired monolithic design carried a draft marker from its first commit. ADRs
0008 and 0009 later called that file frozen, but the repository has no dated
approval record for the whole document. This revision does not infer one. Those
two accepted ADRs still control their named decisions; Draft v4 requires the
explicit approval defined in the design index.

## Proposed v1 limits

The immutable v1 resume schema cannot add template identity, photo visibility,
section visibility, or a global section-icon toggle. V1 implements those limits
honestly. A later schema version may add them through the document-version
process; renderer code does not invent hidden, out-of-contract fields.

Font families are a catalog, not a fixed product identity. The catalog can grow
through reviewed schema and asset changes when the exact license permits free
self-hosting, redistribution, and PDF embedding without usage fees. A modified
asset also requires modification rights and compliance with naming conditions.

## Approval and change process

No line in an accepted ADR is edited to make history look consistent. This draft
corrects the current design and cites each accepted or proposed decision. Once
v4 is explicitly approved, changed decisions require a new ADR or design
revision. Status words are exact: “draft” means mutable; “approved” means
frozen; “landed” describes repository state and does not imply either approval
or gate success.
