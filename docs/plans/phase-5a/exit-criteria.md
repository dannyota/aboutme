# Phase 5A exit criteria

The integration owner checks every item at one unchanged candidate commit.
Failed or unsatisfiable items are corrected and rerun under ADR 0024.

## Topology, contract, and storage

- [ ] The v4 source regenerates byte-identical Go/Nuxt/Caddy/fixture consumers;
      its closed exact 14-row schema/dispatch order passes. Missing or drifted
      effective Caddy fragments stop every launch path.
- [ ] Both closed source manifests select exactly D1's regular files and local
      digest construction. Compose/native/HTTPS values match recomputation;
      release config uses server/web OCI digests. Native/temporary Caddyfiles
      contain verified fragment bytes, no relative import, and record raw
      source/fragment/effective SHA-256 values.
- [ ] Compose's internal render network contains only server/web, is untrusted
      and unexposed, and has no Caddy/database/media peer. Go binds only fixed
      edge `10.90.0.2`; web cannot reach Go or forge a trusted proxy. Caddy
      rejects both internal-render routes before Nuxt fallback and no backend
      receives them; this is a route-denial proof because Caddy and web share
      `frontend`.
- [ ] Route parity proves deny → fixed Go → fixed Nuxt → reserved-only 404 →
      valid dynamic slug/Markdown Go → Nuxt fallback, including invalid single
      segments and all 14 roots. Browser static/network, toolchain contract, CI
      adversarial, Make safety, generator, and route-table checks pass.
- [ ] Compose/native/HTTPS/ECS direct origins match D1. Every path supplies
      deterministic app/renderer digests, includes Web server worker sources,
      and passes literal topology/static tests before W0a release.
- [ ] Migration 00007 fresh up/down and concurrency tests pass; public state is
      one positive row; sqlc remains clean with the `public.citext` override.
- [ ] OpenAPI and generated TypeScript expose exact owner publish and public
      JSON/photo DTOs without treating HTML/text routes as JSON operations.

## Publish, mutation, and revocation

- [ ] Strict publish decoding, complete ordered/deduplicated issue catalog, slug
      grammar/registry, flags, recent reauth, claims/tombstones, exact 180-day
      boundary, bytewise locks, and 30 changed slugs/account/hour pass.
- [ ] Fresh publish/delete atomically store response, revision, claim/tombstone,
      discovery generation, and exact photo job. Exact delete replay precedes
      reauth/fence work; same-key contenders re-probe before CAS.
- [ ] Every existing revision mutation uses the non-draining base transition.
      Rename/unpublish/route-disable/slug-delete plus discovery changes use the
      required resume/global revoking drains.
- [ ] Revoking/global transitions cancel and join every retained generation
      under one absolute five-second deadline. Timeout starts no transaction and
      restores unchanged readability.
- [ ] Definite outcomes open only exact proof. Ambiguous outcomes stay closed
      until retained idempotency plus database proof resolves them; mixed or
      unavailable proof fails readiness and never reruns mutation.
- [ ] All 22 names in Task 02's sole catalog pass with deterministic blockers,
      including public response deadlines and release-based joins.

## Public representations and worker boundary

- [ ] Closed projection omits every account/owner/storage/private/hidden value,
      prunes layout, preserves absence/empty, and re-sanitizes retained rich
      text.
- [ ] JSON/photo/HTML/Markdown/sitemap/robots/llms exact success/error bytes,
      types, headers, methods, HEAD lengths, conditionals, uniform absence, and
      body-digest ETags match approved contracts.
- [ ] Real Caddy gzip/zstd suffixes and viewer/upstream conditional translation
      preserve body-digest identity and identity fallback.
- [ ] Admission precedes cache/conditional/media/render work. Private keys have
      every route/representation/variant/ID/generation/format/build dimension
      and renderer digest when needed; TTL is at most 60 seconds.
- [ ] Nuxt stream-caps/parses before spawn and builds a separate real Vue worker
      artifact. Each request uses one immutable four-field worker, accepts no
      ambient lookup capability, and caps exact request/HTML/origin/time values.
- [ ] Natural success resolves only after clean worker exit. Abort/exact five-
      second deadline calls terminate once, waits for observed exit, and rejects
      late output; a real noncooperative infinite worker proves kill-and-join.
- [ ] Discoverable HTML has deterministic JSON-LD and exactly one matching CSP
      hash; nondiscoverable HTML has neither; all hydration is external.
      Matching revisions hydrate existing SSR, differing revisions replace it,
      and client failure preserves the original accessible SSR document.

## Dispatch, readiness, and live evidence

- [ ] After health dispatch, recognized public paths dispatch before default
      BodyLimit/rate middleware. Hostile oversize wrong-method bodies receive
      exact public `405`/`Allow`; valid GET/HEAD receive public-specific
      controls.
- [ ] Readiness fails on invalid/missing public state, unresolved recovery,
      fence invariant, database failure, or direct Nuxt failure; `/healthz`
      remains independent and the composite is single-flight cached once.
- [ ] Fixed generation-41 native matrix covers discoverable+photo,
      nondiscoverable, private, renamed current, old tombstone, exact routes and
      hashes with exactly three resumes and the fixed D9 clock. Capture is
      read-only after seed. The isolated stack/database/media are removed,
      fixture cache/coordinator state dies with the process, and initially
      active normal native state restarts against untouched `aboutme_dev`.
- [ ] `make p5a-native-http-check` captures bounded secret-free viewer evidence
      through `http://localhost:20080`, including denial, readiness, cookies,
      compression, conditionals, ETags, and exact bytes.
- [ ] This live capture closes only AC-OPS-012b's first-public-JSON-route slice;
      it does not claim P5B UI, PDF/image, SSE, print, P8, or P9 HTTPS/443 work.

## Records, review, and unchanged candidate

- [ ] Every T00–T12 report matches the handoff format; shared edits and unrun
      checks are resolved or block exit.
- [ ] The owner updates and locally commits the master plan/index, architecture,
      runbook, and trace evidence before fresh review; focused record checks
      pass and the candidate includes that commit.
- [ ] One fresh non-author reviews the complete candidate and confirms all named
      security/concurrency/route/trace invariants; the same reviewer confirms
      fixes. There is no per-task reviewer.
- [ ] `make ci` passes alone, then connected `SEMGREP_APP_TOKEN` `make scan`
      passes alone on the same unchanged candidate.
