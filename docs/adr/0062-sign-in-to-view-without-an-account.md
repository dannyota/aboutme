# 0062: Sign in to view without an account

Status: Proposed (2026-09-26). The owner settled the feature; the gate text, the
join invite, and the preview-card choice wait for approval in the
[viewer analytics design](../design/viewer-analytics/README.md#owner-approval).

## Context

An owner may want to know exactly who read a resume. Viewers sign in with Google
or LinkedIn, the providers aboutme already uses, but must not get an aboutme
account or session by doing so. Every public representation of a resume is
served today without authentication, and the in-process public cache and the
revocation fence assume that ([ADR 0022](0022-public-artifact-revocation.md)).

## Decision

1. A resume in mode `sign_in` serves a gate at `/{slug}`. Its JSON, photo, PDF,
   and live stream need a viewer pass for that resume; discovery is forced off.
   The link-preview card and title stay public.
2. Sign-in uses the existing provider flows with a new unauthenticated OAuth
   purpose, `view`, bound to the resume. Its callback never reads, creates,
   links, or signs in an account and never creates a session.
3. Viewer identities are separate rows per resume, provider, and subject,
   holding only name and a verified email, deleted 90 days after the last view.
4. A viewer pass is a `__Host-view-pass` cookie with a 256-bit value, stored as
   a hash, bound to one resume, valid 7 days, never extended. It authorizes
   public reads of that resume and the viewer's own data only.
5. Turning `sign_in` on advances the public generation and waits for the
   revocation fence; turning it off revokes every pass for the resume.
6. The release raises the production release fence to itself, so a rollback
   cannot silently make `sign_in` resumes public.
7. After sign-in the viewer goes straight to the resume, where a dismissible
   join invite may appear; the consent popup never shows on a `sign_in` resume.

## Rejected

| Option                                         | Why not                                                                                 |
| ---------------------------------------------- | --------------------------------------------------------------------------------------- |
| Create a light aboutme account for each viewer | The owner ruled it out; it would put viewers under the account terms and deletion rules |
| One global viewer identity across resumes      | Lets aboutme's data join one viewer across owners; per-resume rows keep them apart      |
| An interstitial join page after sign-in        | The owner chose to show the resume at once                                              |

## Consequences

- The OAuth transaction table gains a purpose value and a resume column.
- Every gated public route checks the pass before the public cache.
- LinkedIn appears on the gate only once LinkedIn sign-in is enabled.
- Operators must switch `sign_in` resumes to `count` before lowering the fence.
