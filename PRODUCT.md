# Product

<!-- impeccable:product-schema 1 -->

## Platform

web

## Users

Primary: Vietnamese job seekers in tech (developers, designers, product and data
roles) applying to companies. They keep a small set of tailored resumes, share
one resume link with recruiters, and export the same resume as a PDF for job
portals. Resumes are written in Vietnamese or English; recruiters and HR staff
are the readers of the public page.

Secondary readers, not account holders: recruiters opening a shared link, search
crawlers and AI answer engines when a person has opted in, and an MCP-capable
agent the person connects to edit their own resumes.

## Product Purpose

aboutme is an open-source resume builder and hosted display service. A person
signs in with email and password, edits up to three resumes with a live preview
of the exact page layout, and publishes each one at `aboutme.vn/{slug}` with
explicit choices about PDF download and discovery. Success is a resume that
looks the same in the editor, on the public page, and in the PDF, reached by a
link the person controls and can withdraw immediately.

The intended product and architecture are defined in `docs/design/` (Approved
v4); `docs/design/product.md` is the product section of that authority.

## Positioning

The resume is public; the person is not. There is no profile page, no username,
and no account URL. Each resume has its own globally unique slug, discovery by
search engines and AI engines is off until the owner opts in, and unpublishing
or deleting revokes the public link at once. A connected agent can read and edit
resume content but can never publish. Neighboring builders sell a profile or a
platform; aboutme publishes documents.

## Operating Context

- The owner works in a browser on a laptop or phone: the editor is a four-region
  workspace (tool rail, section outline, page preview, inspector) that
  autosaves; phone widths switch between an edit view and a preview view.
- Publishing is a human decision in the web UI. The publish dialog exposes three
  independent choices: public resume, PDF download, and SEO and GEO (search and
  generative engine discovery), which defaults off.
- Recruiters read the public page as continuous HTML; the PDF is paginated by
  the print browser. Content, order, colors, and type must agree across editor
  preview, public page, and PDF; only page-break placement may differ.
- A person may connect an agent they already use (Claude, Codex, or any
  MCP-capable assistant). The agent speaks MCP to `aboutme.vn/mcp` after an
  OAuth 2.1 consent screen that names the client and the scopes; the settings
  page lists connected agents and revokes them.
- The v1 application interface is English. Vietnamese resume content is a
  first-class fixture; the bundled font catalog states measured script coverage
  instead of claiming universal coverage.
- Development is local-first: native Go, Nuxt, and Caddy at
  `http://localhost:20080` (`make dev-native`), an HTTPS harness at
  `https://localhost:20443` for authenticated browser proofs, and a seeded
  account (`make dev-seed`) with a sample resume.

## Capabilities and Constraints

- Eight section types (summary, experience, education, skills, languages,
  certifications, projects, custom), rich text with a versioned sanitizer, one
  or two columns, fonts, colors, spacing, heading styles, presets, and an
  optional photo.
- At most three resumes per account, enforced in PostgreSQL.
- Slug grammar `^[a-z0-9]+(-[a-z0-9]+)*$`, 4 to 30 characters, globally unique;
  released slugs enter a 180-day tombstone. Reserved roots cannot be claimed.
- Email and password authentication only in v1. Provider login (Google, GitHub,
  LinkedIn) is implemented behind a server flag that is off; the UI shows
  provider controls only when the capabilities read reports `providerLogin`.
- The resume renderer is pure: `(document, renderContext) -> HTML`. Application
  chrome must never change how the renderer's output looks; renderer golden HTML
  and screenshot suites are the proof.
- Strict Content Security Policy on every page; no third-party fonts, scripts,
  or CDNs at runtime. Fonts are self-hosted from a licensed catalog of 26
  families (all OFL-1.1); Be Vietnam Pro is rank 1 and Inter rank 2.
- Application UI toolkit: Tailwind CSS v4 and shadcn-vue primitives with reka-ui
  (ADR 0029). Decided 2026-09-04: UI work builds on branch `codex/phase-pu`
  rebased onto `main`, which adds the publish dialog. The rebase and its fresh
  phase review are the first UI task, not an assumption.
- Terminology: "resume" (never CV), "publish" and "unpublish", "public resume",
  "PDF download", "SEO and GEO", "connected agents", "signed-in devices",
  "slug". Buttons name the action they perform; copy is sentence case.
- Out of v1: cover letters, job tracker, first-party AI writing, custom domains,
  teams, analytics, a multilingual interface, collaborative editing, and any
  operator or admin surface.
- Undecided: the product name is in use as `aboutme` and the domain as
  `aboutme.vn`, but a name and trademark review is pending before production. No
  logo or wordmark exists beyond the lowercase word.

## Brand Commitments

- Name: `aboutme`, always lowercase, one word. Domain `aboutme.vn`.
- Voice: plain, exact, sentence case, no marketing filler. Copy names only
  shipped behavior. Errors say what happened and how to fix it. Deletion copy
  distinguishes immediate access revocation from delayed physical deletion.
- License: AGPL-3.0, stated on the landing page with a link to the repository.
- Binding constraint volunteered by the owner: the emerald "saved" and "active"
  accent and the zinc neutral scale are the current tokens; they are a starting
  point, not a requirement.

## Evidence on Hand

- Real resume fixture: the `full` fixture (Ada Lovelace, Analytical Engineer,
  Hanoi) in `apps/web/test/fixtures/` and the seeded sample resume; renders
  through the real renderer in every preset.
- Six named presets with golden screenshots in `apps/web/e2e/baselines/`.
- Font catalog manifest with measured Vietnamese coverage:
  `apps/web/app/assets/fonts/catalog.json`.
- Current-state screenshots (2026-09-04) under `.dev/design-qa/current/`.
- Absent, never to be invented: testimonials, customer logos, user counts,
  pricing, benchmarks, press, and any claim about hosted uptime or scale.

## Product Principles

1. Publish documents, not people. Every surface reinforces that a resume, not an
   account, is the public thing.
2. The page is the product. What the person sees in the preview is what a
   recruiter sees at the link and in the PDF.
3. Explicit over implicit. Discovery, download, agent access, and deletion are
   separate, named choices with plain consequences.
4. The person publishes; the agent only edits. Human control over public
   exposure is a design invariant, not a setting.
5. Local-first and open. Everything runs on a laptop before it runs in the
   cloud, and the code stays readable by the community it serves.

## Accessibility & Inclusion

Accessibility is a release requirement for the editor, publish flow, public
page, and generated artifacts: no serious or critical axe violation in light and
dark themes, keyboard-operable dialogs, menus, tabs, and rail with visible
focus, reduced motion respected, and a persisted theme choice. Vietnamese
diacritics must render correctly in every chrome typeface and every catalog font
at every weight used.
