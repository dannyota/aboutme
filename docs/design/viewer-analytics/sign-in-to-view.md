# Sign in to view

An owner can require viewers to sign in with Google or LinkedIn before a resume
shows, only to keep out bots and anonymous automated reading. aboutme keeps
nothing about the viewer: the sign-in creates no account, the name and email the
provider returns are discarded in the same request, and the owner never learns
who viewed. The only thing left is a pass cookie in the viewer's browser that
names no person. The providers are those enabled for sign-in: Google today, and
LinkedIn once LinkedIn sign-in is on in production.
[ADR 0062](../../adr/0062-sign-in-to-view-without-an-account.md) records the
choices. This is a later release than view counts; nothing here is built yet.

## Setting

Each resume gains one switch, `signInToView`, in the publish dialog, off by
default: "Yêu cầu đăng nhập để xem" / "Require sign-in to view", with the line
"Người xem đăng nhập bằng Google hoặc LinkedIn. Bạn không thấy họ là ai." /
"Viewers sign in with Google or LinkedIn. You do not see who they are." The
setting is stored on the resume, is not part of the resume document, and never
changes the rendered resume. Counting runs the same with it on or off
([counting](counting.md)).

## Gate

A request for `/{slug}` without a valid pass for that resume gets the gate page
instead of the resume, with status 200, `Cache-Control: no-store`, and
`X-Robots-Tag: noindex, noarchive`. It shows:

- the public page title, so the viewer knows whose resume it is;
- the gate text in [legal](legal.md#sign-in-gate-text) (**Owner approval** V4);
- one button per enabled provider, "Tiếp tục với Google" / "Continue with
  Google";
- what to do on refusal: "Không muốn đăng nhập? Hãy liên hệ trực tiếp chủ CV." /
  "Prefer not to sign in? Contact the resume owner directly."

The gate is rendered by Nuxt from a closed envelope Go sends (title, language,
providers), through the same direct render path and page CSP as the resume. It
contains no resume content.

**Owner approval** V5: the page head keeps the link-preview title, description,
and card, so a shared link still previews
([link previews](../link-previews.md)).

## Gated routes

Every public representation of a `sign_in` resume requires a pass for that
resume, except the preview card and its alias:

| Route                                            | Without a pass                       |
| ------------------------------------------------ | ------------------------------------ |
| `/{slug}`                                        | The gate                             |
| `GET /api/v1/public/resumes/{slug}` and `/photo` | The uniform public 404               |
| `GET /api/v1/public/resumes/{slug}/pdf`          | The uniform public 404               |
| `GET /api/v1/live/{slug}`                        | The uniform public 404               |
| `/{slug}.md`, sitemap, `llms.txt`                | Absent: sign-in forces discovery off |
| Preview card and `og.png` alias                  | Served, as today                     |

The pass check runs before the public cache lookup, so cached bytes are never
served without a pass. Turning the switch on for a live resume is a material
publish state change: it advances the public generation and waits for the
revocation fence before the setting returns
([ADR 0022](../../adr/0022-public-artifact-revocation.md)), so no anonymous
request admitted earlier completes after success. Turning it on again after it
was off raises the resume's pass epoch, so passes from the earlier period stop
working.

## Sign-in flow

1. The gate button links to
   `GET /api/v1/auth/{provider}/start?purpose=view&slug={slug}`. `view` is a new
   unauthenticated purpose, allowed on `GET` like `login`
   ([ADR 0014](../../adr/0014-oauth-start-methods.md)). The start checks that
   the slug is live and requires sign-in, stores the transaction with purpose
   `view` and the resume ID, and redirects to the provider.
2. The provider flow is unchanged: Google with PKCE, LinkedIn as
   [LinkedIn sign-in](../linkedin-sign-in.md#protocol) sets out, nonce and ID
   token checks for both ([security](../security.md#oauth-transaction)).
3. The callback, for purpose `view`, never looks up, creates, links, or signs in
   an account and never creates a session. It verifies the ID token, re-checks
   that the resume is live and requires sign-in, discards every claim, sets the
   pass cookie, and redirects to `/{slug}`. Nothing about the viewer is written
   to the database or logs. Any failure, including cancel, returns to the gate
   with a closed message.
4. The resume renders; the counting script runs as for any viewer.

## Pass cookie

`__Host-view-pass` (`Secure`, `HttpOnly`, `SameSite=Lax`, `Path=/`, 7 days)
holds up to 10 passes, one per resume. Each pass is base64url of
`resumeID ‖ passEpoch ‖ expiresAt ‖ HMAC-SHA-256 tag`, signed with a server key
that survives deploys and is loaded like the other runtime keys, never stored in
the database. A pass names no person and aboutme keeps no copy, so it cannot be
linked to the sign-in that made it. It expires 7 days after sign-in and is never
extended. It authorizes only public reads of that resume; it reaches no account
route, no CSRF token, and no agent route.

Turning the switch off makes the resume public again; turning it on later raises
the pass epoch. Rotating the pass key ends every pass at once.

## What the owner sees

Nothing about viewers. The Views page shows counts as for any resume, and the
publish dialog shows the switch.

## Join invite

After sign-in the viewer goes straight to the resume. A small invitation to make
their own resume on aboutme.vn appears on that page.

- **Who:** only a viewer holding a pass on a `sign_in` resume, who has no
  aboutme session in this browser. Never on ordinary public pages (**Owner
  approval** V8).
- **When:** once the viewer has scrolled past 60 percent of the resume or spent
  20 s of visible time on it, whichever comes first, and never in the first 5 s.
- **Where:** on screens at least 1,024 px wide whose right margin beside the
  resume is at least 360 px, a card 320 px wide at the bottom right with a 16 px
  margin. Everywhere else, a bar at most 56 px high along the bottom edge,
  inside the safe area; the page adds bottom padding of the same height, so the
  bar never covers resume text.
- **Close:** a labeled close button (`Đóng` / `Close`), also on `Escape`.
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
