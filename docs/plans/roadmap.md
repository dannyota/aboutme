# Roadmap: v0.9 to v1.x

Status: in progress. Accepted by the owner on 2026-11-10 and amended on 2026-11-15 with the [decisions](#owner-decisions) below. v0.8 and v0.9 are closed; v0.10 is next.

Direction (owner, 2026-11-09): create a resume, then prove your skills, then improve your career. Each minor version below has one vision. Each patch release ships one feature (AGENTS.md "Small releases"). A feature starts only after its named design or ADR is written and accepted.

Direction (owner, 2026-11-15): build it for the owner's own use first. After v1.0, aboutme grows into a place that helps junior developers and new graduates build a career: their resume, their knowledge, and their record of progress.

## Fixed rules

These hold for every version and are not reopened by this plan.

- Free, open source (AGPL-3.0), private by default; the person controls and can withdraw the link (`PRODUCT.md:25-39`, `PRODUCT.md:99`).
- Bring your own AI: aboutme hosts no model and charges nothing for AI (`docs/design/product.md:55-56`). AI features are tools and prompts for the user's own assistant over MCP.
- Evidence, not claims (proposed content rule, not yet enforced): an assistant never invents an achievement and never confirms a skill on its own. An assistant never publishes: MCP has no publish tool (`docs/adr/0018-mcp-agent-access.md:51-52`).
- Public URLs belong to a resume, not a person; an account has no public page (`docs/adr/0003-public-namespace-and-no-operator-surface.md:21-23`).
- Discovery, recruiters and connections are a separate product. aboutme links to a profile there only with the user's consent. Review of a resume by other signed-in people joins aboutme in [v1.x](#v1x-a-place-to-build-a-career).

## Facts that shape the order

- A live resume has no separate published snapshot. Public reads select the same row that saves write (`GetPublicRealtimeResume` in `apps/server/sql/queries.sql`), and open pages refresh in place (`docs/design/realtime.md:24-25`). An MCP write to a published resume is public on the next request, with no owner review; an already open page follows within its refresh interval (`docs/design/realtime.md:18-25`).
- No accept or reject of agent edits and no restorable history exist: none of the 22 MCP tools proposes or restores ([owner workflow](../design/mcp-owner-workflow.md)), and `revision` is only a compare-and-swap counter.
- Projects live once in an account library, each resume chooses which it shows, and each chosen project of a published resume has a public page ([projects contract](../design/projects-contract.md)). An assistant's edit to a library project reaches every live resume that shows it at once.
- GitHub sign-in is built but off in production, with scope `user:email`; its client reads only the profile and email, with no repository or evidence client, and provider tokens are discarded after sign-in (`githubScopes` in `apps/server/internal/auth/github.go`, [sign-in security](../design/security-sign-in.md#oauth-transaction), [product](../design/product.md)).
- Only Claude Code is proven as an MCP client in production; ChatGPT is not named (`docs/design/mcp-guide.md:145-164`).
- GitHub: a token with no scope reads public repositories, pull requests and releases ([scopes](https://docs.github.com/en/apps/oauth-apps/building-oauth-apps/scopes-for-oauth-apps)). Limits are 5,000 REST requests an hour per user and 30 non-code searches a minute ([REST limits](https://docs.github.com/en/rest/using-the-rest-api/rate-limits-for-the-rest-api), [search](https://docs.github.com/en/rest/search/search)). Pull requests and releases carry the author's numeric id; commits are linked by email only ([commit linking](https://docs.github.com/en/pull-requests/committing-changes-to-your-project/troubleshooting-commits/why-are-my-commits-linked-to-the-wrong-user)).
- ChatGPT: custom MCP servers with write actions are documented for Business and Enterprise or Edu, and read-only for Pro; Plus and Free are not verified (needs verification: the help page blocked direct reading, [article](https://help.openai.com/en/articles/12584461-developer-mode-and-mcp-apps-in-chatgpt)). A reviewed plugin directory exists for MCP-backed apps ([submission](https://developers.openai.com/apps-sdk/deploy/submission)). Using a visitor's ChatGPT plan on a hosted site needs an interest form and is not generally available ([token sharing](https://developers.openai.com/siwc/token-sharing-open-source)).

## Recommended order

Projects and evidence first, then reviewed changes, then assistant-led review. The owner's lean holds: projects and GitHub evidence need no AI and give value alone. Reviewed changes must ship before any write-capable assistant review, because today an assistant's edit to a live resume is public the moment it saves. v0.9 keeps that exposure for assistant edits to project detail until v0.10.

|Version|Vision|A developer can|
|-|-|-|
|v0.9|Show the work|Write a project once, choose which resumes show it, give it its own page, and attach GitHub evidence they confirmed|
|v0.10|Changes you approve|See what an assistant or import proposes, accept or reject each change, and restore an earlier version|
|v0.11|Review with your own AI|Ask their assistant to review a resume, compare it with a job post, and propose changes for approval|
|v0.12|Plan the next step|Ask their assistant for skill gaps, next steps, and interview practice grounded in their own evidence|
|v1.0|A stable base|Rely on frozen contracts; nothing new|
|v1.x|A place to build a career|Use a mobile app, keep a record of progress, learn from guidance, and get their resume reviewed by others|

## v0.9: show the work

Vision: a project stops being three lines on a resume and becomes a page that proves the work.

Release numbers (owner, 2026-11-12): the first release in which a person can write a project and show it on a resume is v0.9.0; each later feature of this list is v0.9.1 and up. An urgent fix released before v0.9.0 stays in v0.8.x.

All ten features have shipped. Feature numbers are stable and are not renumbered.

Features:

1. Project library: projects live once per account, private, with no public URL of their own. A project has role, technologies, outcome, repository link and demo link, with its text held per resume language.
2. Projects on a resume: each resume chooses which library projects it shows and in what order. Nothing is shown by default; creating a project asks which resumes get it. The printed resume keeps one compact entry per chosen project.
3. Project page: each chosen project of a published resume has a public page under that resume, in the resume's language, following its publish, sign in to view, and indexing settings.
4. Viewer entry points: the resume's public page shows a Projects link and links each project entry to its page; in the PDF the same title is a link, so a reader of the PDF can reach the projects the owner chose to show. A printed address for paper readers is not planned (owner, 2026-11-11).
5. Connect GitHub: the owner links a GitHub account to their aboutme account for evidence only, separate from sign-in, with no scope beyond public data.
6. Evidence picker: the owner picks repositories, merged pull requests and releases to attach to a project. aboutme checks each item against the connected account (author id for pull requests and releases; owner for repositories, as the [design](../design/github-evidence-picker.md) defines) and stores a snapshot (title, URL, date) with the time it was checked. Evidence belongs to the library project, so every resume that shows the project shows it.
7. Evidence on the page: attached evidence renders on the project page with its checked date. Disconnecting GitHub removes the "confirmed" mark, and the owner chooses whether the links stay.
8. MCP: assistants can read and edit library projects and a resume's selection. They cannot attach evidence or connect accounts.
9. Home page (owner, 2026-11-15): a new home page that reads as made by a person, warm and colorful, with a new palette if the chosen direction needs one. Every claim on it stays true.
10. Web layout for the public resume (owner, 2026-11-15): the public page gets a layout made for screens, phones first, with contact actions and readable text. The PDF and the print layout keep the template.

Out of scope: private repositories, commit-level evidence, contribution graphs, automatic skill inference, ranking, any score.

Done when: a developer with two resumes can write a project once, show it on one resume and not the other, publish two project pages, one with confirmed GitHub evidence, reach them from the resume page and from the title links in its PDF, and print the resume with compact project entries; hiding, deleting or renaming a project, sign in to view, indexing opt-out, link previews and unpublish revoke or gate the project pages exactly as the resume page; and a test shows no MCP tool can attach evidence or connect an account.

Before code for 9 and 10: the owner picks a direction from mockups; an amendment to [ADR 0020](../adr/0020-application-visual-identity.md) for any identity rule the direction changes; a design for the public web layout that keeps every public representation under ADR 0010 revocation and ADR 0022 viewer privacy. Claude subagents do UI design and frontend work ([workflow](../../instructions/workflow.md#models-and-ownership)).

Before code for the rest: an amendment to ADR 0003 for project routes; a design and ADR for the account project library (a new aggregate beside the resume document, its per-language text, limits, export and deletion, and how existing project entries migrate); a design for project pages, viewer entry points and their print mapping that covers every public representation (page, Markdown, JSON, previews, indexing) under ADR 0010 revocation and ADR 0022 viewer privacy; a schema version for project detail (ADR 0004); a design and ADR for GitHub connection and evidence that decide account binding, token lifetime, storage and revocation, what "confirmed" means per item type, snapshot behavior when the upstream item is deleted or made private, retention and removal.

## Foundation work

These run beside the features, ship as their own patch releases, and add nothing a user sees.

- Remove the undeployed AWS code ([ADR 0026](../adr/0026-vietnam-hosted-production.md)).
- Move JavaScript dependencies to one [pnpm workspace](../design/pnpm-workspace.md).
- Slim the server runtime image, then carry [third-party licences](../design/third-party-licences.md) in every release image.
- Small removals: the second test DOM and the [approval probe](../design/production-pull-deploy/README.md#release-order-and-owner-decisions).

## v0.10: changes you approve

Vision: with approval on, nothing an assistant writes reaches a live resume until the owner has accepted it.

Features, in shipping order:

1. Version history: saves keep restorable versions for a bounded time; the owner can view and restore one. The design decides what a restore includes (text, photo, evidence, publish settings).
2. Proposed changes: a write from MCP to a published resume becomes a proposal, not a save. Unpublished resumes keep direct writes.
3. Review screen: the owner sees each proposed change against the current text and accepts or rejects it, one by one or all.
4. Owner setting: per resume, "assistant edits need my approval", on by default for published resumes.
5. MCP: tools to list a resume's pending proposals and their outcome, so an assistant can follow up.

Out of scope: collaborative editing, comments from other people, a separate draft copy of the whole resume, merging an import into an existing resume (LinkedIn import keeps creating a new resume after its own review, `docs/design/linkedin-import.md:43-49`).

Done when: with the setting on, a test shows no MCP tool, including delete and photo tools, can change what a public viewer sees; an owner can accept one proposed change, reject another, and see a stale proposal refused after a newer save; and an owner can restore an earlier version within the retention the design sets.

Before code: the owner accepts the [change review](../design/change-review.md) design and [ADR 0031](../adr/0031-reviewed-assistant-changes.md) after an independent review; ADR 0018 and the MCP guide are amended on acceptance.

## v0.11: review with your own AI

Vision: the owner's own assistant becomes a careful reviewer that proposes, and the owner decides.

Features, in shipping order:

1. Review guide over MCP: a server-provided prompt and a read-only rubric (structure, evidence, length, language) drawn from the [Vietnam tech resume guide](../design/vietnam-tech-resumes.md), so any assistant reviews the same way.
2. Job post comparison: the owner pastes a job post to their assistant; a tool returns the resume in a form suited to comparison and records nothing about the post.
3. Evidence rule in tools: a proposal that adds an achievement or number names the source the owner gave; a proposal without one is accepted by the tool and marked unsourced on the review screen. The source field never holds job post text.
4. Wider client reach: prove hosted Claude and ChatGPT, and submit an aboutme app to the ChatGPT directory if its terms fit.

Out of scope: any hosted model, any score shown to other people, storing job posts.

Done when: with two named assistants, an owner can get a review and a job comparison and accept some proposals; every proposal that adds a claim without a source is marked unsourced on the review screen; and no tool input or stored proposal contains job post text.

Before code: a design for the review prompt, rubric and comparison tool that treats a pasted job post as untrusted instructions and limits a review to the one resume the owner named; the provenance change to the v0.10 proposal contract; a client compatibility proof plan; a decision on directory submission.

## v0.12: plan the next step

Vision: the same evidence that proves the work shows what to learn next.

Features, in shipping order:

1. Skill evidence view: the owner sees which listed skills have project or GitHub evidence and which have none. Private to the owner.
2. Career path prompt: a server-provided prompt that lets the assistant suggest gaps and next steps for a target role from that view.
3. Interview practice prompt: questions drawn from the resume, its projects and a pasted job post.

Out of scope: public skill badges, endorsements, a hosted interviewer, any ranking of people.

Done when: an owner can open the skill evidence view and see each listed skill with or without evidence, run both prompts with a named assistant, and nothing from the session is stored by aboutme.

Before code: a design for the skill evidence view and both prompts.

## v1.0: a stable base

No features. The owner decides when to cut the first candidate; that commit records the supported contracts and adds `.v1-baseline`, which freezes migrations and released schemas (`AGENTS.md:99`, `docs/adr/0004-resume-document-contract.md:67-77`, `docs/adr/0005-database-migrations.md:40-49`). Launch gates stay in [backlog.md](backlog.md).

## v1.x: a place to build a career

Vision (owner, 2026-11-15): after v1.0, aboutme helps a junior developer or new graduate build a career, not only write a resume. Nothing here is designed or ordered yet; each item needs its design and decisions before code.

- Mobile app: the Flutter app, deferred until web v1 (`docs/design/product.md:39`), starts here.
- Knowledge: information and guidance for developers starting out, beginning from the [Vietnam tech resume guide](../design/vietnam-tech-resumes.md).
- Tracks: a private record of what a person learns and does over time, like a diary, that can feed projects, evidence and the resume.
- Shared review: signed-in people can read and review another person's resume when its owner allows it. Review is on by default for a resume with open access, and the owner can turn it off.

Before code: an amendment to ADR 0003 for whatever a reviewer sees of the owner and of other reviewers; a design for review that covers who may review, moderation, reporting and removal, abuse limits, what a review stores and for how long, and the privacy notice; a decision on whether tracks are ever public; and the mobile contract against the frozen v1 API.

## Later

- Using a visitor's ChatGPT plan on the website: documented for open-source apps that run locally; a hosted site must apply through an interest form, so approval is assumed to be required (inference). Not committed; never described as supported.
- Discovery, recruiters and connections: a separate product.
- A publish scope for assistants: deferred, not designed away (`docs/adr/0018-mcp-agent-access.md:93-94`).

## Owner decisions

Decided on 2026-11-10; the designs named under each version follow them.

1. Projects (v0.9): written once in an account library; each resume chooses which projects it shows. Project pages live under the resume, as `aboutme.vn/{slug}/projects` and `aboutme.vn/{slug}/projects/{project}`. The resume shows viewers where to find the chosen projects.
2. Order: reviewed changes (v0.10) before assistant-led review (v0.11).
3. GitHub (v0.9): a separate connection for evidence with no scope, independent of sign-in.
4. Proposals (v0.10): on by default for published resumes.
5. ChatGPT directory (v0.11): decide after the client proofs.

Decided on 2026-11-15:

1. Build for the owner's own use first.
2. v0.9 adds the home page and the public resume's web layout, ahead of GitHub evidence.
3. UI design and frontend work go to Claude subagents, not `gpt-6-astra`.
4. The undeployed AWS code is removed; dependencies move to pnpm with a one-day minimum release age; release images carry third-party licences, with a written source offer for base-layer GPL software.
5. v1.x follows v1.0 with the mobile app, knowledge, tracks and shared review, as [above](#v1x-a-place-to-build-a-career).
