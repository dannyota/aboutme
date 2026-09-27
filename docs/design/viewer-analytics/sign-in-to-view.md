# Sign in to view

An owner can require viewers to sign in with Google or LinkedIn before a resume
shows, only to keep out bots and anonymous automated reading. aboutme keeps
nothing about the viewer: the sign-in creates no account, the provider's claims
are discarded in the same request, and the owner never learns who viewed. The
only thing left is a pass cookie in the viewer's browser that names no person.
[ADR 0022](../../adr/0022-viewer-privacy-and-counting.md) records the choices.
Nothing here is built yet.

The gate offers Google and LinkedIn, each only while it is enabled for account
sign-in ([ADR 0016](../../adr/0016-sign-in-providers.md#enablement)). GitHub
never appears on the gate, even where it is enabled, because the gate text and
the privacy notice name only Google and LinkedIn.

## Setting

Each resume gains one switch, `signInToView`, in the publish dialog, off by
default: "Yêu cầu đăng nhập để xem" / "Require sign-in to view", with the line
"Người xem đăng nhập bằng Google hoặc LinkedIn. Bạn không thấy họ là ai." /
"Viewers sign in with Google or LinkedIn. You do not see who they are." The
setting is stored on the resume, is not part of the resume document, and never
changes the rendered resume. Counting runs the same with it on or off
([counting](counting.md)).

- **Request:** the publish request gains optional `signInToView`. Absent keeps
  the stored value, so a publish dialog loaded before the release still works.
  The resume response returns it. MCP has no publish tool, so agents cannot
  change it.
- **Server flag:** `SIGN_IN_TO_VIEW_ENABLED` (default false) lets owners turn
  the switch on; the capabilities read carries `signInToView` and the dialog
  shows the switch only when it is true. With the flag off, a request that turns
  the switch on is a publish issue and changes nothing; turning it off always
  works. Gating never depends on the flag: a resume with the switch on stays
  gated whatever the flag says.
- **Discovery:** the switch forces effective discovery off. The stored
  `seoGeoEnabled` value is kept, so turning sign-in off restores the owner's
  choice. The dialog shows the discovery switch disabled while sign-in is on.
- **Export:** the account export carries `signInToView` with the other publish
  settings. The pass epoch never leaves the server.

## Gate

A request for `/{slug}` without a valid pass for that resume gets the gate page
instead of the resume, with status 200, `Cache-Control: no-store`, and
`X-Robots-Tag: noindex, noarchive`. It shows:

- the public page title, so the viewer knows whose resume it is;
- the gate text in [legal](legal.md#sign-in-gate-text) (**Owner approval** V4);
- one button per offered provider, "Tiếp tục với Google" / "Continue with
  Google";
- a closed message after a failed or cancelled sign-in;
- what to do on refusal: "Không muốn đăng nhập? Hãy liên hệ trực tiếp chủ CV." /
  "Prefer not to sign in? Contact the resume owner directly."

The gate is rendered by Nuxt from a closed envelope Go sends (title, language,
offered providers, slug, link-preview text, and the message code), through the
same direct render path and page CSP as the resume. It contains no resume
content and no script: its buttons are plain links, it runs no counting, and it
never hydrates. Go validates the rendered gate with its own closed rule set, as
it does for the resume page.

The message comes from `?signin=cancelled` or `?signin=failed` on `/{slug}`; Go
reads only those two values into the envelope and ignores any other query. The
gate is never cached, so the query cannot reach another viewer.

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
| `POST /api/v1/public/resumes/{slug}/views/start` | The uniform public 404; not counted  |
| `/{slug}.md`, sitemap, `llms.txt`                | Absent: sign-in forces discovery off |
| Preview card and `og.png` alias                  | Served, as today                     |

Each gated handler reads the admitted public snapshot, which carries the switch
and the pass epoch, then checks the pass, and only then looks in the public
cache. Cached bytes are never served without a pass. A gated response for a
`sign_in` resume adds `private` to its `Cache-Control`, so no shared cache in
front of the origin may store it. Only pass holders reach view start, so gate
visits are not counted.

Turning the switch on for a live resume is a material publish state change: it
advances the public generation and waits for the revocation fence before the
setting returns ([ADR 0010](../../adr/0010-public-artifact-revocation.md)), so
no anonymous request admitted earlier, including an open live stream, completes
after success. Turning it off makes the resume public at once and needs no
fence.

The resume's pass epoch rises each time the switch is turned on and each time a
`sign_in` resume becomes live again after unpublishing, so passes from an
earlier period stop working. A pass survives a slug change, since it names the
resume ID.

## Sign-in flow

1. The gate button links to
   `GET /api/v1/auth/{provider}/start?purpose=view&slug={slug}`. `view` is a new
   unauthenticated purpose, allowed on `GET` like `login`
   ([ADR 0016](../../adr/0016-sign-in-providers.md)), and shares the anonymous
   start limiter. A malformed slug gets 400. A slug that is not live with the
   switch on redirects to `/{slug}` and creates no transaction. Otherwise the
   start stores the transaction with purpose `view` and the resume ID, and
   redirects to the provider.
2. The provider flow is unchanged: Google with PKCE, LinkedIn as
   [LinkedIn sign-in](../linkedin-sign-in.md#protocol) sets out, and ID token
   checks for both, with LinkedIn's nonce checked only when present
   ([security](../security.md#oauth-transaction)). The `view` start requests the
   scope `openid` only, so the provider returns no name or email (**Owner
   approval** V10, open; **Verify** that LinkedIn accepts `openid` alone).
3. The callback, for purpose `view`, never looks up, creates, links, or signs in
   an account and never creates a session. It verifies the ID token, re-reads
   the resume by ID, discards every claim, and then:
   - live with the switch on: sets a pass with the current epoch and redirects
     to the resume's current slug;
   - live with the switch off: redirects to the current slug without a pass;
   - not live: redirects to `/`.

   Nothing about the viewer is written to the database or logs. A failure after
   the transaction is read, including cancel, returns to `/{slug}?signin=failed`
   or `?signin=cancelled`. A failure before it is read (a missing or unknown
   transaction cookie) names no resume and goes to the login page, as for any
   purpose.

4. The resume renders; the counting script runs as for any viewer.

The owner is gated like any viewer: public routes never read the account session
to admit a request (**Owner approval** V9, open). The owner can read the resume
in the editor preview or sign in through the gate.

## Pass cookie

`__Host-view-pass` (`Secure`, `HttpOnly`, `SameSite=Lax`, `Path=/`) holds up to
10 passes, one per resume, separated by `.`. Each pass is base64url of
`resumeID (16 bytes) ‖ passEpoch (4) ‖ expiresAt Unix seconds (8) ‖ tag (32)`,
where the tag is HMAC-SHA-256 under `VIEW_PASS_KEY` over a fixed domain label
and the other fields. The whole cookie stays under 1 KiB.

- The key is 32 random bytes, loaded like the other runtime keys and never
  stored in the database. It survives deploys; rotating it ends every pass.
- A pass expires 7 days after sign-in and is never extended. Adding an 11th pass
  drops the one that expires first; expired and unreadable passes are dropped
  whenever the cookie is rewritten. The cookie's `Max-Age` is the latest
  remaining expiry.
- A pass names no person and aboutme keeps no copy, so it cannot be linked to
  the sign-in that made it. It authorizes only the gated reads of that resume;
  it reaches no account route, no CSRF token, and no agent route.

## What the owner sees

Nothing about viewers. The Views page shows counts as for any resume, and the
publish dialog shows the switch.

## Join invite

After sign-in the viewer goes straight to the resume. A small invitation to make
their own resume on aboutme.vn appears on that page.

- **Who:** only a viewer holding a pass on a `sign_in` resume, who has no
  aboutme session in this browser. Never on ordinary public pages (**Owner
  approval** V8).
- **How the page knows:** the resume HTML of a `sign_in` resume carries a marker
  from its render envelope and is cached under its own variant; only pass
  holders receive that HTML. View start answers `signedIn: true` when the
  request carries any valid account session. The invite shows only when the
  marker is present, start succeeded, and `signedIn` and `owner` are false.
  Start still authorizes nothing with the session.
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

## Release and rollback

The release ships with `SIGN_IN_TO_VIEW_ENABLED` false, so no resume can be
gated yet and a rollback is still safe. After the first production proof, the
operator raises the production release fence to the release's own number
([release fence](../passkey-release-fence.md#serialized-production-operation)),
then turns the flag on and redeploys the same tag. The deployer refuses an app
revision with the flag on while the fence is below that number (**Owner
approval** V11, open).

Once the fence is raised, `deploy.sh --rollback` below the release is refused,
because an older image would serve `sign_in` resumes publicly. Going lower is a
forward fix or privileged administration, as for the second-factor floors
(**Owner approval** V12, open).
