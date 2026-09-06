# 0032 - Public resumes expose one share image

Status: Accepted (2026-09-05).

## Context

The approved design calls for generated images but does not specify a public
path, format, viewport, crop, or eligibility rule. Its print target table also
assigns page fragmentation to images, while the determinism section specifies a
viewport capture. Phase 7 needs one exact contract.

## Decision

The owner approved continuing with the proposed live-gated share image:

- `GET /api/v1/public/resumes/{slug}/og.png` returns `image/png`.
- `HEAD` and conditional reads follow the existing public response contract.
- The image is exactly 1200 by 630 pixels at device scale 1. Capture the top
  viewport of the shared continuous resume renderer, with an opaque white page
  background. Do not use PDF page fragmentation, full-page screenshots, or
  client-selected sizes. Content below the viewport is cropped.
- Eligibility is `live=true` at the requested current slug. The image does not
  depend on `download_enabled` or `seo_geo_enabled`.
- Public HTML includes Open Graph and Twitter image metadata using the canonical
  absolute image URL and fixed dimensions. Discovery-disabled pages retain their
  existing noindex policy. Social sharing does not enable indexing.
- There is one image variant. The API accepts no format, width, height, page, or
  crop query parameter.

The image uses the same frozen, privacy-filtered document, normalized inline
photo, fonts, Chromium environment, and one-use print authority as PDF export.
Go owns completion and publication. ADR 0022's live-state gate, generation
lease, revocation drain, entity tag, and cache rules apply before any image
reuse.

## Consequences

The share image shows a recognizable part of the resume without promising a
complete document. A resume may extend below it. PDF remains the full-document
export. Unpublish, rename, and deletion revoke new image admission together with
the other public representations.

Phase 7 records a bounded PNG output size and tests exact dimensions, forbidden
variants, download/discovery independence, and revocation of cached and running
image jobs.
