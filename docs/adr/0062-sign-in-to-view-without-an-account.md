# 0062: Sign in to view without an account or stored identity

Status: Accepted (2026-09-26) for a later release than view counts. The owner
chose the no-storage model: sign-in only keeps bots and anonymous automated
reading off a resume, and nobody, the owner included, learns who viewed.

## Context

Some owners want only people to read a resume, not scrapers. Viewers can sign in
with Google or LinkedIn, the providers aboutme already uses, but must not get an
aboutme account or session by doing so. aboutme keeps no data about viewers
([ADR 0060](0060-viewer-data-controller-and-consent.md)). Every public
representation of a resume is served today without authentication, and the
in-process public cache and the revocation fence assume that
([ADR 0022](0022-public-artifact-revocation.md)).

## Decision

1. A resume with sign-in to view on serves a gate at `/{slug}`. Its JSON, photo,
   PDF, and live stream need a pass for that resume; discovery is forced off.
   The link-preview card and title stay public.
2. Sign-in uses the existing provider flows with a new unauthenticated OAuth
   purpose, `view`, bound to the resume. Its callback never reads, creates,
   links, or signs in an account, never creates a session, and discards every
   claim once the ID token verifies. Nothing about the viewer is stored or
   logged, and the owner is not told who viewed.
3. The pass is a `__Host-view-pass` cookie holding signed passes that name a
   resume, its pass epoch, and an expiry 7 days out, never extended. aboutme
   keeps no copy. It authorizes public reads of that resume only.
4. Turning sign-in on advances the public generation and waits for the
   revocation fence; turning it on again raises the resume's pass epoch, which
   ends older passes.
5. The release raises the production release fence to itself, so a rollback
   cannot silently make gated resumes public.
6. After sign-in the viewer goes straight to the resume, where a dismissible
   join invite may appear.

## Rejected

| Option                                         | Why not                                                                                    |
| ---------------------------------------------- | ------------------------------------------------------------------------------------------ |
| Show the owner who viewed                      | The owner ruled out tracking; it would make viewer identities sensitive data aboutme keeps |
| Create a light aboutme account for each viewer | Would put viewers under the account terms and deletion rules                               |
| A random pass stored as a hash in the database | A stored row links a browser to a sign-in; a signed pass needs no row                      |
| An interstitial join page after sign-in        | The owner chose to show the resume at once                                                 |

## Consequences

- The OAuth transaction table gains a purpose value and a resume column; the
  resumes table gains the switch and the pass epoch.
- Every gated public route checks the pass before the public cache.
- A pass cannot be revoked one by one; raising the epoch or rotating the pass
  key ends passes together.
- LinkedIn appears on the gate only once LinkedIn sign-in is enabled.
- Operators must turn the switch off on every resume before lowering the fence.
