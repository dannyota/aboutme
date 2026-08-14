# Phase 5A Publish and Public SSR Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use
> superpowers:subagent-driven-development (recommended) or
> superpowers:executing-plans to implement this plan task-by-task. Steps use
> checkbox (`- [ ]`) syntax for tracking.

**Goal:** Publish complete resumes at global slug URLs and serve privacy-safe,
server-rendered public JSON, photo, HTML, Markdown, sitemap, robots, and
`llms.txt` bytes with immediate origin revocation.

**Architecture:** PostgreSQL owns publish state, slug claims, tombstones, and a
durable discovery generation. Go owns admission, generation leases, exact bytes,
private caches, conditional requests, direct-render calls, and viewer
connections. Nuxt renders one closed public snapshot in a terminated-and-joined
worker; Caddy denies that route and all viewer artifacts pass through Go.

**Tech Stack:** Go 1.26, PostgreSQL 18, sqlc, OpenAPI 3.1, Nuxt 4, Vue 3,
TypeScript 6.0.3, Vite 8.2.0, `@vitejs/plugin-vue` 6.0.8, Node worker threads,
Caddy 2.11.4, and Podman.

## Global Constraints

- The approved authorities are `design.md`, `public-contract.md`,
  `public-formats.md`, `revocation.md`, Design v4, and ADRs 0004, 0005, 0016,
  0017, 0019, 0022, and 0024. They override this plan.
- Every public cache/conditional/media/render path acquires the current database
  generation lease before reuse, parsing, or I/O.
- Every existing revision mutation takes the non-draining base transition.
  Rename, unpublish, route disable, and slug-bearing delete cancel and join all
  affected leases before the transaction.
- One absolute five-second deadline covers every global/resume drain. Timeout
  begins no transaction and reopens unchanged state.
- Ambiguous commit remains closed until exact retained idempotency plus database
  proof resolves it. Mixed/unavailable evidence fails readiness.
- Slug claims use the generated v4 root registry, bytewise slug lock order,
  exact `aboutme.slug.v1:` advisory-lock domain, 180-day tombstones, and 30
  changed-slug attempts per account per rolling hour before availability detail.
- Public DTOs are dedicated closed types. They omit account/server identity,
  owner title, timestamps, private photo keys, hidden values, and disabled data.
  Go sanitizes every retained rich-text field again during projection.
- Public GET and HEAD share gate, generation, exact GET bytes, headers, status,
  and Content-Length. HEAD writes no body. Wrong methods on recognized public
  paths return public `405` before default body/rate middleware reads a body.
- Public ETags are quoted lowercase SHA-256 of exact unencoded bodies. Caddy
  alone adds/removes `-gzip` and `-zstd` suffixes.
- Private cache keys contain route class, representation, variant, resume ID,
  generation, format version, app digest, and renderer digest when rendered; TTL
  is at most 60 seconds.
- The direct renderer accepts only bounded `POST /internal-render/public`. Each
  render uses one worker thread. Abort or five seconds calls and awaits
  `worker.terminate()`; the handler returns only after worker exit.
- The Compose `render` network is internal and contains only server and web. It
  is not trusted, not host-published, and contains no Caddy, database, or media.
  Go binds only fixed edge address `10.90.0.2`, so web cannot reach Go on
  `render`; Go calls `http://web:3000` outbound.
- The closed v4 registry has the exact 14 Product v4 rows and dispatch values
  `reserved|go|nuxt|deny`. Generated Caddy dispatch handles deny, fixed Go,
  fixed Nuxt, reserved-only 404, valid dynamic slug/Markdown, then Nuxt fallback
  in that order.
- Local app/renderer digests are D1's SHA-256 over the two owned closed source
  manifests. Production uses the server/web image OCI digests from the release.
  Native and HTTPS temporary Caddyfiles inline the verified generated fragment
  and record source, fragment, and effective-byte hashes.
- Native direct origin is `http://127.0.0.1:20030`; native HTTPS direct origin
  is `http://127.0.0.1:20440`; ECS remains same-task loopback
  `http://127.0.0.1:3000`. No topology creates a trusted-web path.
- Phase 5A does not claim PDF/image export, SSE, print, P5B UI, P8 operations,
  or P9 HTTPS/443 UAT.
- Each task has one author who writes RED first and owns its adversarial cases.
  There is no per-task reviewer. One non-author performs the ADR 0024 phase
  review after master records are updated.
- Workers edit only exclusive task paths and never use Git. Owner windows
  serialize topology/Caddy/Makefile, migration/sqlc, OpenAPI/client,
  manifests/lockfiles, composition/native capture, and master records.
- At most two heavy checks run together. Full `make ci` and connected
  `make scan` run alone at one unchanged candidate commit.

---

## Plan documents

- [Decisions](decisions.md) contains the exact shared types and runtime choices.
- [File structure](file-structure.md) assigns every path once.
- [Integration handoffs](integration-handoffs.md) repeats producer/consumer
  signatures and fixes owner windows/reporting.
- [Exit criteria](exit-criteria.md) is the unchanged-candidate gate.

## Task index

| Task                                          | Deliverable                                                                   | Acceptance                     | Owner                 |
| --------------------------------------------- | ----------------------------------------------------------------------------- | ------------------------------ | --------------------- |
| [00](task-00-render-topology.md)              | Render network, route registry/Caddy, native/HTTPS topology and static parity | OPS-005                        | Integration owner     |
| [01](task-01-public-state-storage.md)         | Public state, slug/tombstone and public snapshot SQL/sqlc                     | PUB-001/002/004                | Integration owner     |
| [02](task-02-generation-fences.md)            | Fences, leases, shared drain, recovery state                                  | PUB-004, SEC-001               | Fence author          |
| [03](task-03-idempotency-transition-seam.md)  | Serialized idempotency re-probe                                               | PUB-002/004                    | Idempotency author    |
| [04](task-04-openapi-public-contract.md)      | Publish/public wire authority and TypeScript client                           | PUB-001/002/003                | Integration owner     |
| [05](task-05-public-projection-json-photo.md) | Go DTO, origin, projection, cache, JSON/photo                                 | PUB-003/004, SEC-001           | Public read author    |
| [06](task-06-publish-policy.md)               | Publish decoder, completeness, slug policy and limiter                        | PUB-001/002                    | Publish policy author |
| [07](task-07-mutation-transitions-delete.md)  | Every mutation transition, publish/delete transaction and recovery            | PUB-001/002/004                | Transition author     |
| [08](task-08-public-formats.md)               | Markdown, discovery, JSON-LD and CSP bytes                                    | PUB-003/004                    | Format author         |
| [09](task-09-nuxt-render-worker.md)           | Closed worker-thread SSR and external hydration                               | PUB-003, OPS-005               | Web author            |
| [10](task-10-directrender-html.md)            | Go direct-render client and HTML gateway                                      | PUB-003/004                    | HTML author           |
| [11](task-11-public-router-readiness.md)      | Public dispatch, readiness, composition root                                  | PUB-003/004, OPS-005           | Integration owner     |
| [12](task-12-native-public-capture.md)        | Deterministic seed matrix and live native HTTP capture                        | PUB-003, OPS-005/012b, SEC-001 | Integration owner     |

## Frozen waves

The root integration owner enforces this cross-phase total order for shared
paths. It overrides phase-local parallelism:

1. Phase 4 T00 lands auth cache/validator and editor generated-client
   dependencies.
2. Phase 5A T00 lands topology, route registry, Caddy, and native parity.
3. Phase 5A T01 then T04 complete the serialized migration/sqlc and
   OpenAPI/generated-client windows before Phase 4 transport or public consumers
   edit their dependent surfaces.
4. Phase 5A T09/W4b runs after Phase 4 T00 and before either phase's final
   browser window.
5. Phase 4 T15 finishes the shared native HTTPS harness before Phase 5A T12 adds
   public capture while preserving every Phase 4 scenario.

Disjoint implementation work may run between these points. Only the root owner
releases a shared window, and the next owner starts from that integrated result.

| Wave | Tasks                 | Start condition                        | Heavy limit                                       |
| ---- | --------------------- | -------------------------------------- | ------------------------------------------------- |
| W0a  | 00                    | Phase 4 T00 shared window lands        | Owner alone; topology must pass before release    |
| W0b  | 01                    | T00 lands                              | Owner alone; one database container               |
| W0c  | 04                    | T01 serialized window lands            | Owner alone; OpenAPI/client generation            |
| W1   | 02, 06                | W0a–W0c land                           | Two disjoint Go checks                            |
| W2   | 03, 05                | T01/T02/T04 land                       | Two disjoint Go checks                            |
| W3   | 07, 08                | T03/T06 and T05 land respectively      | Two disjoint Go checks                            |
| W4   | 09                    | T04/T08 and Phase 4 T00 land           | Web alone; W4b owner dependency window            |
| W5   | 10                    | T05/T08/T09 land                       | One Go check                                      |
| W6   | 11                    | T02/T07/T10 land                       | Owner alone; router/composition window            |
| W7   | 12                    | Phase 4 T15 and all focused gates pass | Owner alone; live native evidence                 |
| W8   | Records, review, exit | T00–T12 reports accepted               | Records first, fresh review, then candidate gates |

Task 02's race catalog is the sole named-test owner list for all 22 ADR 0022
rows. Other task headers cite primitive prerequisites or integration coverage,
not duplicate ownership.

## Dispatch and completion

Before T00 dispatch, the root integration owner lands this Phase 5A plan and
updates `docs/plans/implementation-plan.md` plus the traceability index to mark
the approved task graph active. Those initial record edits are not deferred to
the candidate gate.

The owner dispatches one task with its exact task file, base commit,
authorities, acceptance IDs, and paths. The author observes the named RED
failure, implements the shown minimal GREEN skeleton, runs the exact focused
command, and returns the report in `integration-handoffs.md`. Authors suggest
Conventional Commit subjects but do not stage or commit.

After T12, the owner updates the master plan/index from active to its actual
completion state, adds architecture/runbook/evidence results, commits those
record changes locally, and reruns their focused checks. Only then does one
fresh non-author review the complete candidate. Findings return to the owning
task author and the same reviewer confirms fixes. The owner finally runs
`make ci` and connected `make scan` on one unchanged candidate before push.
