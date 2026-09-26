# Sign in to view

In mode `sign_in`, a viewer signs in with Google or LinkedIn before the resume
shows. The sign-in creates no aboutme account. The owner then sees who viewed.
The providers are those enabled for sign-in: Google today, and LinkedIn once
LinkedIn sign-in is on in production.
[ADR 0062](../../adr/0062-sign-in-to-view-without-an-account.md) records the
choices.

## Gate

A request for `/{slug}` without a valid viewer pass for that resume gets the
gate page instead of the resume, with status 200, `Cache-Control: no-store`, and
`X-Robots-Tag: noindex, noarchive`. It shows:

- the public page title, so the viewer knows whose resume it is;
- the gate text in [legal](legal.md#sign-in-gate-text) (**Owner approval** V4);
- one button per enabled provider, "Đồng ý và tiếp tục với Google" / "Agree and
  continue with Google", which the text above them explains;
- what to do on refusal: "Không muốn đăng nhập? Hãy liên hệ trực tiếp chủ CV." /
  "Prefer not to sign in? Contact the resume owner directly."

The gate is rendered by Nuxt from a closed envelope Go sends (title, language,
providers, notice version), through the same direct render path and page CSP as
the resume. It contains no resume content.

**Owner approval** V5: the page head keeps the link-preview title, description,
and card, so a shared link still previews
([link previews](../link-previews.md)).

## Gated routes

Every public representation of a `sign_in` resume requires a viewer pass for
that resume, except the preview card and its alias:

| Route                                            | Without a pass                         |
| ------------------------------------------------ | -------------------------------------- |
| `/{slug}`                                        | The gate                               |
| `GET /api/v1/public/resumes/{slug}` and `/photo` | The uniform public 404                 |
| `GET /api/v1/public/resumes/{slug}/pdf`          | The uniform public 404                 |
| `GET /api/v1/live/{slug}`                        | The uniform public 404                 |
| `/{slug}.md`, sitemap, `llms.txt`                | Absent: `sign_in` forces discovery off |
| Preview card and `og.png` alias                  | Served, as today                       |

The pass check runs before the public cache lookup, so cached bytes are never
served without a pass. Setting `sign_in` on a live resume is a material publish
state change: it advances the public generation and waits for the revocation
fence before the setting returns
([ADR 0022](../../adr/0022-public-artifact-revocation.md)), so no anonymous
request admitted earlier completes after success. Setting `count` or `ask`
revokes every viewer pass for the resume in the same transaction.

## Sign-in flow

1. The gate button is a link to
   `GET /api/v1/auth/{provider}/start?purpose=view&slug={slug}`. `view` is a new
   unauthenticated purpose, allowed on `GET` like `login`
   ([ADR 0014](../../adr/0014-oauth-start-methods.md)). The start checks that
   the slug is live and `sign_in`, then stores the transaction with purpose
   `view`, the resume ID, and the notice version, and redirects to the provider.
2. The provider flow is unchanged: Google with PKCE, LinkedIn as
   [LinkedIn sign-in](../linkedin-sign-in.md#protocol) sets out, nonce and ID
   token checks for both ([security](../security.md#oauth-transaction)).
3. The callback, for purpose `view`, never looks up, creates, links, or signs in
   an account and never creates a session. It re-checks that the resume is live
   and `sign_in`, upserts the viewer identity, appends a `sign_in` consent
   record, creates a viewer pass, sets the pass cookie, and redirects to
   `/{slug}`. Any failure, including cancel, returns to the gate with a closed
   message; nothing is stored.
4. The resume renders; the counting script runs as for any viewer, and a valid
   collect also appends a view event linked to the viewer identity
   ([tracking](tracking.md#recording)).

## Viewer identity

One row per resume, provider, and subject, so one owner's data never joins
another's and deleting a resume deletes its viewers:

| Field                 | Rule                                                                                |
| --------------------- | ----------------------------------------------------------------------------------- |
| Provider and subject  | The ID token `sub`; used only to recognize a returning viewer on this resume        |
| Name                  | The `name` claim, normalized and bounded to 200 characters                          |
| Email                 | Kept only when `email_verified` is true; otherwise absent and shown as "not shared" |
| First seen, last seen | Times                                                                               |

No picture, locale, headline, or provider token is kept. A known subject updates
name, email, and last seen. The row is deleted when its last view event expires,
90 days after the last view, or at once on withdrawal, owner deletion, or resume
deletion.

## Viewer pass

The cookie `__Host-view-pass` holds a 256-bit random value; PostgreSQL stores
only its SHA-256 hash, as sessions do ([security](../security.md#sessions)). A
pass row binds the hash to one resume and one viewer identity and expires 7 days
after sign-in, never extended. One browser can hold passes for several resumes
under the same cookie value. The pass authorizes only public reads of that
resume and the viewer data page; it reaches no account route, no CSRF token, and
no agent route.

## What the owner sees

The resume page on `/app/views/{id}` lists views as in
[tracking](tracking.md#what-the-owner-sees), with the viewer's name and email in
place of the viewer number, plus a list of people with their view counts and
last view. The owner never sees the provider subject or which provider was used.

## Viewer rights

`/privacy/viewer` also accepts a viewer pass: a signed-in viewer sees their
name, email, and every view of that resume, and can withdraw and delete, which
deletes the identity, its events, and every pass for it at once. A viewer
without a pass signs in through the gate again to reach it, or writes to the
contact address. Refusing sign-in stores nothing.

## Join invite

After sign-in the viewer goes straight to the resume. A small invitation to make
their own resume on aboutme.vn appears on that page.

- **Who:** only a viewer holding a pass on a `sign_in` resume, who has no
  aboutme session in this browser. Never on an `ask` or `count` resume, and
  never on ordinary public pages (**Owner approval** V8). The consent popup
  never shows on a `sign_in` resume, so the two never stack.
- **When:** once the viewer has scrolled past 60 percent of the resume or spent
  20 s of visible time on it, whichever comes first, and never in the first 5 s.
- **Where:** on screens at least 1,024 px wide whose right margin beside the
  resume is at least 360 px, a card 320 px wide at the bottom right with a 16 px
  margin. Everywhere else, a bar at most 56 px high along the bottom edge,
  inside the safe area; the page adds bottom padding of the same height, so the
  bar never covers resume text.
- **Close:** a labelled close button (`Đóng` / `Close`), also on `Escape`.
  Closing stores `aboutme.joinInvite.closedAt` in `localStorage` for 90 days, so
  it does not return on any resume in this browser. Following the link also
  stores it.
- **Accessibility:** a `region` landmark with a label, no focus steal, and
  `prefers-reduced-motion` respected.

Text, in the resume's language (**Owner approval** V4):

| Part   | Vietnamese                                                                            | English                                                                      |
| ------ | ------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------- |
| Body   | Tự tạo CV của bạn trên aboutme.vn: miễn phí, mã nguồn mở, chia sẻ bằng một đường dẫn. | Make your own resume on aboutme.vn: free, open source, shared with one link. |
| Button | Tạo CV miễn phí                                                                       | Create a free resume                                                         |
| Close  | Đóng                                                                                  | Close                                                                        |

The button opens `/register` in the same tab when password registration is on,
otherwise `/login`. The link carries no tracking parameter.
