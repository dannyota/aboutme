# MCP guide page

Status: implemented (2026-09-28).

The MCP guide is a public, static page that shows a person how to connect an AI
assistant they already use to their aboutme.vn resumes through the Model Context
Protocol (MCP). It covers Claude first, names the one server URL,
`https://aboutme.vn/mcp`, and states what the assistant can and cannot do under
[ADR 0018](../adr/0018-mcp-agent-access.md). The page changes no MCP tool, OAuth
rule, schema, or API. Its full Vietnamese and English text is in
[MCP guide copy](mcp-guide-copy.md).

The Claude steps rely on
[MCP client compatibility](mcp-client-compatibility.md), which shipped in
v0.6.18. The guide ships after an owner-run proof that Claude on the web and
Claude Code connect in production.

## Route

The page lives at `/guide/mcp`.

`/mcp` cannot hold the page. It is the Go MCP endpoint, a fixed `go` root in the
public-root registry, and MCP clients probe it with `GET` and `POST`. Serving
HTML there would break clients and the protected-resource contract.

`/guide/mcp` adds one registry root, `guide`, with dispatch `nuxt`. The registry
moves to its next version, which regenerates the Caddy matcher, the Go
slug-claim set, the web root list, and the parity fixtures, as every new root
has. `guide` becomes a reserved slug. Before release, a read-only production
check confirms no resume holds or tombstones the slug `guide`; if one does, the
release stops for an owner decision.

The check runs as an operator-only ECS one-shot from the candidate server image.
It uses the existing jobs task's database connection and runtime-resolved app
password. In one repeatable-read, read-only transaction, it returns only the
`resumeOccupied` and `tombstoneOccupied` booleans from indexed existence reads.
Its syntax is `server check-public-root <root>`, with exactly one root that the
candidate image reserves. Exit 0 means neither table holds the root, 10 means
only a resume holds it, 11 means only a tombstone holds it, and 12 means both
do. Invalid invocation, configuration, or query failure exits 1. The command
logs no resume, account, title, time, database address, or secret. The deploy
script maps the fixed exit codes to the same two booleans, so it needs no
database or CloudWatch log access.

The operator runs the check once before deployment. The deploy runs it again
after maintenance is confirmed and the old app has stopped, before any
migration. The second check closes the interval in which the old app could
accept a new `guide` claim. An occupied result follows the existing
pre-migration recovery path and restores the old app without changing resume
data. The one-shot creates normal ECS task-definition, task, and CloudWatch log
records; only its database transaction is read-only.

`/guide` itself has no page and returns the normal not-found page. A later guide
takes another path under `/guide/` without a registry change.

## Navigation

- **Header.** A link labeled “Kết nối AI” or “Connect AI” sits directly after
  Library in the shell navigation, with `aria-current="page"` on `/guide/mcp`.
  Signed out, it shows from 44 rem, the width where the account buttons appear.
  Signed in, it shows from 64 rem, because the signed-in bar also carries
  Resumes, Views, and Settings. Below those widths it is hidden; phones reach
  the page from the landing page. The header must not overflow at 704, 768, or
  1024 px in either language, signed in or out.
- **Marketing CTA.** `/guide/mcp` joins the routes whose header button reads
  Create your resume.
- **Landing footer.** A link with the same label follows Verify.
- **Landing card.** The “Dùng trợ lý AI của bạn” / “Bring your own AI” card ends
  with a text link, “Xem cách kết nối” / “See how to connect”, to the page.
- **Settings.** The Connected agents section links the page from its empty state
  and under its list: “Xem cách kết nối trợ lý AI” / “See how to connect an AI
  assistant”.

## Page structure

The page is server-rendered, reads no API, and shows the same content signed in
or out. Sections, in order:

1. **Header block.** `h1`, lead, a one-paragraph MCP explainer, the labeled
   server URL in a copy block, and an account line linking Create account and
   Sign in.
2. **What your assistant can do.** Two lists, Can and Cannot, and one line on
   account-wide access.
3. **Connect Claude.** `h3` Claude on the web, desktop, and mobile, as an
   ordered list of six steps and a plan note. `h3` Claude Code, as three steps
   with two command blocks.
4. **Try a request.** Three example requests and one line on reviewing and
   publishing in the editor.
5. **Privacy and control.** Five points: approval, approving only your own
   request, no password or session for the assistant, where read content goes,
   and revoking.
6. **Other apps.** What a client needs, what cannot connect, and one line per
   client proven to connect.
7. **Troubleshooting.** Six question and answer pairs.

### What the page may claim

Every claim maps to shipped behavior:

| Claim                                    | Source                                          |
| ---------------------------------------- | ----------------------------------------------- |
| 15 tools; no publish or public read      | [MCP owner workflow](mcp-owner-workflow.md)     |
| Read and write scopes, account-wide      | [Security](security.md), ADR 0018               |
| Delete of a published resume unpublishes | [API](api.md#agent-access-and-the-bearer-world) |
| Three resumes per account                | [Product](product.md)                           |
| Ten live grants; eleventh refused        | [Budgets](budgets.md)                           |
| Revoke kills access and refresh tokens   | [Security](security.md)                         |
| No PDF export, view counts, or import    | The tool list has none of them                  |

The page never promises a response time, a specific model, or that changes
appear live in an open editor.

Claude step 5 and privacy point 1 name where approval returns, and
troubleshooting pair 3 quotes the grant-limit message, matching the consent page
copy that shipped with items 6 and 7 of
[MCP client compatibility](mcp-client-compatibility.md).

### Other clients

Claude comes first because Anthropic documents remote MCP with OAuth for its
hosted apps ([custom connectors][claude-connectors]) and Claude Code ([Claude
Code MCP][claude-code-mcp]). The page names another client only after a proof
shows it connects, reads, writes, refreshes after one hour, and stops after
revoke.

- **Visual Studio Code** [documents][vscode-mcp] remote HTTP servers, OAuth with
  dynamic client registration, and the redirects `http://127.0.0.1:33418` and
  `https://vscode.dev/redirect`, both inside aboutme's redirect grammar. Its
  registration body is not documented, so it may hit the same gaps as Claude.
  Candidate for the list after proof.
- **ChatGPT** is not named. This design could not confirm from OpenAI's own
  documentation which plans add a remote MCP server with OAuth.
- **Cursor and other apps with custom-scheme redirects** cannot register:
  aboutme accepts only `https` redirects and `http` loopback redirects. The
  Other apps section says so without naming apps.

## Layout

The page uses the marketing shell on the Aurora canvas, like `/verify`, and only
existing tokens and primitives. Nothing uses seal red, which means public; the
Cannot list must not look like an error or a public state.

| Measure        | Below 42 rem (390 px)     | From 42 rem (1280 px)  |
| -------------- | ------------------------- | ---------------------- |
| Column         | Full width, 16 px gutters | 60 rem, 24 px gutters  |
| Top and bottom | 32 px, 64 px              | 48 px, 96 px           |
| Section gap    | 32 px                     | 40 px                  |
| `h1`           | 24 px, weight 700         | 32 px, weight 700      |
| Card padding   | 20 px                     | 28 px                  |
| Can and Cannot | One column, Can first     | Two columns, 32 px gap |
| Claude parts   | Stacked                   | Stacked                |

- The header block uses the Library header: `--surface-blue` tint, border, and
  the 20 px feature radius. The lead is 16 px `--muted-foreground`, at most 40
  rem wide.
- Every other section is a `--card` card with a border, the 20 px feature
  radius, and `--shadow-product`. Section headings are `h2` at 18 px weight 600;
  `h3` is 15 px weight 600.
- The URL and command blocks use the verify page's command block look: `--muted`
  fill, border, 10 px radius, 13 px monospace, a 32 px copy button at the top
  right, sideways scroll, no `$` prompt on the URL.
- Steps are an `ol` with 24 px number disks: `--surface-blue` fill, 1 px
  `--border` ring, `--foreground` 13 px weight 600 numerals. Inline UI names are
  weight 600. Inline code uses `--muted` fill and 13 px monospace.
- Can items take a 16 px Lucide `check` in `--link`. Cannot items take a 16 px
  Lucide `minus` in `--muted-foreground`.
- Example requests sit in a `--muted` panel with a 2 px `--brand-indigo` left
  rule.
- Troubleshooting is a `dl`: the question as a 15 px weight 600 `dt`, the answer
  as a 15 px `dd`, 16 px between pairs.
- Both themes come from the tokens alone. The page adds no theme value.

## Accessibility

- One `h1`, then `h2` per section and `h3` inside Connect Claude. The DOM order
  is the visual order at every width.
- The copy buttons are real buttons named “Sao chép địa chỉ” / “Copy URL” and
  “Sao chép lệnh” / “Copy command”. The result is announced through one polite
  live region on the page. Failure says to select the text by hand.
- Each scrolling block is focusable with `tabindex="0"` and an accessible name.
  URL and command text carry `translate="no"`.
- Third-party UI names stay in English in both languages, because that is what
  Claude shows. The Vietnamese text says so once.
- Links name their destination. External links open in the same tab and carry
  `rel="noopener noreferrer"`.
- Number disks and icons keep a visible border or shape under
  `forced-colors: active`. Icons are `aria-hidden`; the text carries meaning.
- No motion. Text meets WCAG AA on each tint in both themes, measured on the
  built page.
- The page renders in the site language and joins the localized route list, so
  the header shows the language toggle.

## SEO

The page is indexable, uses `useSiteSeo` with path `/guide/mcp`, and joins the
sitemap and `llms.txt` site page list as “Connect an AI assistant with MCP”. The
canonical URL is `https://aboutme.vn/guide/mcp`, with the site Open Graph image.
It adds no structured data. The title and description are in the copy file.

## Security, privacy, and size

The page is static text: no data fetch, no form, no cookie, no new script
source, and no CSP change. It shows the production URL from the site origin
constant, so local stacks show `https://aboutme.vn/mcp` too. The copy teaches
the one check a person can make: approve only a request they just started, from
the app they meant. The consent page change in
[MCP client compatibility](mcp-client-compatibility.md) makes that check
stronger by showing where approval returns.

The page adds one route chunk of a few kilobytes and no dependency. It stores
nothing, so there is no migration or loss rule. An older cached page without the
header link keeps working.

If production ever turns MCP off, the header, footer, card, and settings links
and the page must come down in the same release.

## Owner approvals

1. **Route `/guide/mcp`, reserving `guide`.** Approved (2026-09-27): it cannot
   collide with the MCP endpoint and leaves room for later guides. Alternative:
   `/ai`, shorter but vague for search and for a later guide.
2. **Header label “Kết nối AI” / “Connect AI”, hidden on phones.** Approved
   (2026-09-27): people know “AI” better than “MCP”, and the phone header
   already drops the account buttons for space. Alternative: “MCP” on every
   width, shorter but unclear to most visitors.
3. **Links from the footer, the landing card, and Connected agents.** Approved
   (2026-09-27): they are the phone paths and the place a person looks after
   connecting.
4. **Indexable, in the sitemap and `llms.txt`.** Approved (2026-09-27): an
   assistant that reads `llms.txt` learns the endpoint, and search brings people
   who look for “Claude resume”.
5. **Claude first; other clients only after proof.** Approved (2026-09-27): one
   working path beats a list that fails. Visual Studio Code is the next
   candidate.
6. **Public copy says “trợ lý AI”; Settings keeps “Tác nhân đã kết nối”.**
   Approved (2026-09-27): the landing page and privacy policy already say “trợ
   lý AI”; the guide quotes the Settings label as the screen shows it.
7. **Ship the compatibility fix first, as its own release, and the guide after
   an owner-run Claude proof in production.** Approved (2026-09-27): the guide
   must not teach steps that fail. Alternative: ship the guide with only the
   Other apps section, which helps almost nobody.
8. **Link Anthropic's custom connector help page from the Claude section.**
   Approved (2026-09-27): it covers plan changes the guide cannot track.

[claude-connectors]:
  https://support.claude.com/en/articles/11175166-get-started-with-custom-connectors-using-remote-mcp
[claude-code-mcp]: https://code.claude.com/docs/en/mcp
[vscode-mcp]: https://code.visualstudio.com/api/extension-guides/ai/mcp
