# 10. Decision status

This design integrates the outcomes below. Each ADR keeps its rationale and
history; these pages state the resulting design. ADRs 0001 to 0029 are accepted.
The [ADR index](../adr/README.md) maps every former ADR number to its current
record.

| ADR                                                             | Status   | Outcome                                                                                                                             | Design                                                                                                                      |
| --------------------------------------------------------------- | -------- | ----------------------------------------------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------- |
| [0001](../adr/0001-agpl-3.0-license.md)                         | Accepted | Repository and hosted service use AGPL-3.0-only                                                                                     | [Repository](repository.md)                                                                                                 |
| [0002](../adr/0002-system-architecture.md)                      | Accepted | Go API and one shared Vue renderer; HTTP autosave and SSE invalidation; root health routes                                          | [System](system.md), [realtime](realtime.md), [API](api.md)                                                                 |
| [0003](../adr/0003-public-namespace-and-no-operator-surface.md) | Accepted | Globally unique resume slugs; users are not public; no operator surface                                                             | [Product](product.md), [security](security.md)                                                                              |
| [0004](../adr/0004-resume-document-contract.md)                 | Accepted | Draft-permissive storage, schema-derived codegen, layout arrays own order, adjacent converters with declared losses                 | [Data](data.md)                                                                                                             |
| [0005](../adr/0005-database-migrations.md)                      | Accepted | Goose migrations are the schema source; one baseline with explicit grants; plain migrator as a deploy step                          | [Data](data.md), [deployment](deployment.md)                                                                                |
| [0006](../adr/0006-transactional-idempotency.md)                | Accepted | Mutation and replay record commit together                                                                                          | [API](api.md)                                                                                                               |
| [0007](../adr/0007-bounded-rate-limiter.md)                     | Accepted | No active-bucket eviction under key churn; shared overflow bucket                                                                   | [Security](security.md), [budgets](budgets.md)                                                                              |
| [0008](../adr/0008-ssr-sanitizer-authority.md)                  | Accepted | Go owns SSR sanitizing; DOMPurify is client-only                                                                                    | [Web](web.md#rich-text)                                                                                                     |
| [0009](../adr/0009-private-media-delivery.md)                   | Accepted | Private object storage behind live-gated Go reads; durable exact-key deletion                                                       | [API](api.md), [operations](operations.md)                                                                                  |
| [0010](../adr/0010-public-artifact-revocation.md)               | Accepted | Revalidate every public reuse; fence and drain generation leases                                                                    | [System](system.md), [operations](operations.md)                                                                            |
| [0011](../adr/0011-print-capability-and-pdf-output.md)          | Accepted | One-use 60-second print capability; `<Full-Name>-Resume.pdf`, title, and revision dates                                             | [Web](web.md), [print](templates/print.md)                                                                                  |
| [0012](../adr/0012-template-placement.md)                       | Accepted | Presets carry a placement rule; one deterministic total order                                                                       | [Template contract](templates/contract.md)                                                                                  |
| [0013](../adr/0013-resume-header-and-contacts.md)               | Accepted | Array-ordered contacts, icon labels, checked `https`, `mailto:`, and `tel:` links, photo position, project subtitle, justified text | [Template contract](templates/contract.md), [tokens](templates/tokens.md)                                                   |
| [0014](../adr/0014-public-page-head-and-link-preview.md)        | Accepted | Owner-set page title and emoji favicon; stored, versioned link-preview card                                                         | [Link previews](link-previews.md)                                                                                           |
| [0015](../adr/0015-accounts-passwords-and-sessions.md)          | Accepted | Password beside providers; email is never an identity key; bounded session rotation                                                 | [Security](security.md)                                                                                                     |
| [0016](../adr/0016-sign-in-providers.md)                        | Accepted | POST for privileged starts; per-provider enablement; LinkedIn without PKCE or a returned nonce, risk accepted                       | [Security](security.md), [LinkedIn sign-in](linkedin-sign-in.md)                                                            |
| [0017](../adr/0017-second-factor-authentication.md)             | Accepted | Optional passkeys and TOTP, recovery codes, authentication epoch, release fence                                                     | [Second factor](second-factor-authentication.md), [release fence](passkey-release-fence.md)                                 |
| [0018](../adr/0018-mcp-agent-access.md)                         | Accepted | Remote MCP endpoint and first-party OAuth 2.1 server; editor parity minus publish                                                   | [Security](security.md), [MCP workflow](mcp-owner-workflow.md)                                                              |
| [0019](../adr/0019-application-ui-toolkit.md)                   | Accepted | Tailwind v4 and shadcn-vue without Preflight; renderer isolated; guarded token edits                                                | [Web](web.md), [UI tokens](ui/typography-and-tokens.md)                                                                     |
| [0020](../adr/0020-application-visual-identity.md)              | Accepted | Seal logo and stamp, aurora canvas, two blues, burnt-orange destructive, resume ground colors                                       | [UI identity](ui/identity-and-seal.md), [UI tokens](ui/typography-and-tokens.md), [public page theme](public-page-theme.md) |
| [0021](../adr/0021-bilingual-resume-workspace.md)               | Accepted | Vietnamese and English workspace; interface toggles never change resume data                                                        | [Localization](localization.md)                                                                                             |
| [0022](../adr/0022-viewer-privacy-and-counting.md)              | Accepted | No viewer data kept; seven-layer anonymous counts; sign in to view with a signed pass                                               | [Viewer analytics](viewer-analytics/README.md)                                                                              |
| [0023](../adr/0023-linkedin-import.md)                          | Accepted | Import reads the English Save to PDF with pdf.js in a worker; the file never leaves the browser                                     | [LinkedIn import](linkedin-import.md)                                                                                       |
| [0024](../adr/0024-delivery-gates.md)                           | Accepted | Failing test first, one fresh review, exact green GitHub CI commit before tag and deploy                                            | [`AGENTS.md`](../../AGENTS.md)                                                                                              |
| [0025](../adr/0025-single-host-production.md)                   | Accepted | One AWS Singapore host, no hosted UAT until about 500 users, CloudFront edge, Route 53 DNS                                          | [Single-host production](single-host-production.md), [CloudFront edge](cloudfront-edge.md)                                  |
| [0026](../adr/0026-replica-scaling.md)                          | Accepted | One serving replica; the scaling design returns before a second                                                                     | [Scaling](scaling/README.md)                                                                                                |
| [0027](../adr/0027-vietnam-hosted-production.md)                | Accepted | Vietnam-hosted production on GreenNode and Bizfly; AWS becomes test only                                                            | [Vietnam production](vietnam-production.md)                                                                                 |
| [0028](../adr/0028-deployment-transparency-observer.md)         | Accepted | An off-host observer publishes running digests checked against signed provenance; `/verify` shows them                              | [Deployment transparency](deployment-transparency/README.md)                                                                |
| [0029](../adr/0029-community-showcase.md)                       | Accepted | Opt-in showcase of published resumes, listed without review; uncached listing; never indexed                                        | [Community showcase](showcase.md)                                                                                           |

## Remaining gates

| Gate                                                   | Owner                                    | Due                            |
| ------------------------------------------------------ | ---------------------------------------- | ------------------------------ |
| Per-asset font license, notice, and Reserved Font Name | Integration owner                        | Whenever a font asset is added |
| Privacy and disclosure text                            | Human owner approves agent-reviewed text | Before the public announcement |

The font gate stays per asset because it is a legal check on exact bytes.
[ADR 0025](../adr/0025-single-host-production.md) is the production approval;
hosting cost, topology, and the release path follow it.

Known document limits, such as no template identity or section visibility, are
listed in [template limits](templates/limitations.md). A later document version
may lift one through [ADR 0004](../adr/0004-resume-document-contract.md); the
renderer never invents an out-of-contract field.

## Change process

A changed decision needs a new ADR; a structural rewrite needs a v5 revision of
this design. Neither silently rewrites approved text, and no accepted ADR line
is edited to make history look consistent. A correction that fixes an error,
ambiguity, or contradiction without changing a decision is an ordinary edit.
Status words are exact: "approved" means the decision is settled; "landed"
describes repository state and does not imply that a gate passed.
