# Viewer tracking

In mode `ask`, a public page asks each viewer whether the owner may see details
of their views. Only after the viewer agrees does aboutme set a random viewer
cookie and record view events. A viewer who declines or ignores the popup reads
the resume normally and adds only to the anonymous count
([counting](counting.md)).

## Consent popup

The popup appears once the page has rendered, as a non-modal card: bottom right
on wide screens, a bottom sheet on phones. It never blocks scrolling, reading,
or the PDF button, and the page reserves bottom padding equal to its height on
phones, so no resume text sits under it. The two buttons have equal size,
weight, and color, with no preselected choice, as Decree 356/2025/NĐ-CP Article
6(3) requires ([legal](legal.md#consent)). Closing the popup without choosing
records nothing and shows it again on the next page load.

It renders in a shadow root mounted by `public-resume.mjs`, so resume template
CSS cannot restyle it and it cannot restyle the resume. Its language follows the
resume's language, like the rest of the public page chrome
([localization](../localization.md)). The approved text is in
[legal](legal.md#consent-popup-text) (**Owner approval** V4).

It does not appear to the signed-in owner, to viewers who already chose for this
resume under the current notice version, or on a `sign_in` resume, where the
gate replaces it ([sign in to view](sign-in-to-view.md)). At most one overlay
shows on a page.

## Cookies

Both cookies are set only by the consent and withdrawal endpoints, never by
script, and both are `Secure; HttpOnly; SameSite=Lax; Path=/` with no `Domain`.

| Cookie               | Set when                         | Value                                                                                           | Lifetime                                                    |
| -------------------- | -------------------------------- | ----------------------------------------------------------------------------------------------- | ----------------------------------------------------------- |
| `__Host-view-choice` | The viewer agrees or declines    | Up to 30 entries of `resumeRef.noticeVersion.choice`; about 20 bytes each; oldest dropped first | 90 days from the last choice                                |
| `__Host-view-id`     | The viewer agrees the first time | 16 random bytes, base64url                                                                      | 90 days from first agreement; never extended; kept on renew |

`resumeRef` is a random 8-byte value per resume, stored on the resume row and
used nowhere else, so the cookie reveals neither the resume ID nor its creation
time. `__Host-view-choice` is necessary: without it the popup would return on
every load. It holds no identifier, only which resumes the browser chose for.

When `__Host-view-id` expires, the next view asks again. A new agreement makes a
new cookie, so views before and after cannot be joined.

## View key

The stored key for a viewer on one resume is
`viewerKey = SHA-256("aboutme-view-v1" ‖ viewId ‖ resumeId)`, truncated to 16
bytes. It needs no server secret. Without the cookie value, nobody who reads the
database can join one viewer's rows across two resumes, and the owner of one
resume never learns what else the viewer read.

## Recording

With consent, a valid collect ([counting](counting.md#flow)) also appends one
view event. The fields are closed:

| Field        | Source                                                                                                                                 | Stored as                                                               |
| ------------ | -------------------------------------------------------------------------------------------------------------------------------------- | ----------------------------------------------------------------------- |
| Time         | Server clock at collect                                                                                                                | `timestamptz`                                                           |
| Time on page | A later `sendBeacon` at `pagehide` with the visible seconds, capped at 3,600                                                           | Integer seconds, one per view                                           |
| Device type  | Go classifies the `User-Agent` into `mobile`, `tablet`, `desktop`, or `other`; the string is not stored                                | Closed value                                                            |
| Place        | `CloudFront-Viewer-City` and `CloudFront-Viewer-Country`, added by the edge from the viewer's IP address; the IP address is not stored | City (≤ 64 bytes, NFC) and ISO country                                  |
| Source       | The script sends only the host of `document.referrer`; Go also reads in-app browser tokens in the `User-Agent`                         | `zalo`, `linkedin`, `facebook`, `email`, `search`, `other`, or `direct` |

Source rules, in order: an in-app token (`Zalo`, `LinkedInApp`, `FBAN` or `FBAV`
or `FB_IAB`; each **Verify**) wins; then the referrer host (`zalo.me`,
`chat.zalo.me`; `linkedin.com`, `lnkd.in`; `facebook.com`, `messenger.com`,
`m.me`; `mail.google.com`, `outlook.live.com`, `outlook.office.com`,
`mail.yahoo.com`; Google, Bing, and Cốc Cốc search hosts); then `other` for any
other host and `direct` for none. Most mail apps send no referrer, so `direct`
reads "Trực tiếp hoặc không rõ" / "Direct or unknown".

Nothing else is stored: no user agent, no IP address, no referrer path, no
screen or language data. A view event is recorded even when layer 7 deduplicates
the anonymous count, because the owner asked to see each visit; bot, hosting,
anomaly, and invalid outcomes record no event.

## What the owner sees

The resume page on `/app/views/{id}` adds a list for the last 90 days, newest
first: time (Asia/Ho_Chi_Minh), time on page, device, place, source, and a
viewer label, "Người xem 3" / "Viewer 3", numbered per resume by first view, so
the owner sees returns without an identifier. The owner can delete all recorded
viewer data for the resume at once.

## Viewer rights

The popup and the privacy notice link to `/privacy/viewer`, a Nuxt page under
the existing `/privacy` root. For the browser's cookies it lists, per resume,
the public title and every recorded view, then offers:

- **Withdraw and delete** for one resume: appends a `withdraw` consent record,
  deletes that resume's view events for the key at once, and sets the choice to
  declined;
- **Withdraw and delete everything**: the same for every agreed resume, then
  expires `__Host-view-id`.

Access, withdrawal, and deletion finish in the request, well inside the limits
of Decree 356/2025/NĐ-CP Article 5 ([legal](legal.md#viewer-rights)). A viewer
who lost the cookie cannot be matched by aboutme, since nothing else links the
rows to them; the notice says so and gives the contact address. Correction does
not apply: every field is measured, not supplied.

## Retention

- View events and their durations: deleted 90 days after the view by the daily
  privacy sweep, and at once on withdrawal, owner deletion, resume deletion, or
  account deletion.
- Consent and withdrawal records: 180 days after the record, as evidence of
  consent (Decree 356/2025/NĐ-CP Article 6(2)); they hold the view key, resume,
  notice version, choice, and time, and are deleted with the resume.
- Switching a resume from `ask` to `count` stops recording; recorded events
  expire on schedule unless the owner deletes them.

## Notice versions

The popup text has a version string, such as `view-2026-10`. A material change
to the popup or to the fields raises it; the choice cookie stores it, and a
viewer whose recorded choice has an older version sees the popup again. Earlier
events stay under the consent they were recorded with.
