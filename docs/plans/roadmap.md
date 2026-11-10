# Roadmap: v0.9 to v1.0

Status: proposed by the primary, 2026-11-10. Not yet accepted by the owner; the [open questions](#open-questions) need his answers. v0.8 is closed.

Direction (owner, 2026-11-09): create a resume, then prove your skills, then improve your career. Each minor version below has one vision. Each patch release ships one feature (AGENTS.md "Small releases"). A feature starts only after its named design or ADR is written and accepted.

## Fixed rules

These hold for every version and are not reopened by this plan.

- Free, open source (AGPL-3.0), private by default; the person controls and can withdraw the link (`PRODUCT.md:25-39`, `PRODUCT.md:99`).
- Bring your own AI: aboutme hosts no model and charges nothing for AI (`docs/design/product.md:55-56`). AI features are tools and prompts for the user's own assistant over MCP.
- Evidence, not claims: an assistant never invents an achievement, never confirms a skill on its own, and never publishes. MCP has no publish tool (`docs/adr/0018-mcp-agent-access.md:51-52`).
- Public URLs belong to a resume, not a person; an account has no public page (`docs/adr/0003-public-namespace-and-no-operator-surface.md:21-23`).
- A developer network (discovery, recruiters, connections) is a separate product. aboutme links to a profile there only with the user's consent.

## Facts that shape the order

- A live resume has no separate published snapshot. Public reads select the same row that saves write (`apps/server/internal/store/queries.sql.go:3031-3034`), and open pages refresh in place (`docs/design/realtime.md:24-25`). An MCP write to a published resume reaches viewers at once, with no owner review.
- No accept or reject of agent edits and no restorable history exist: none of the 15 MCP tools proposes or restores (`docs/design/mcp-owner-workflow.md:16-20`), and `revision` is only a compare-and-swap counter.
- A project is a print-sized entry: title, subtitle, link, dates, description (`packages/schema/resume.schema.json:230-247`). No project page exists.
- GitHub sign-in is built but off in production, with scope `user:email` and no API client (`apps/server/internal/auth/github.go:22-37`, `docs/design/product.md:9-10`).
- Only Claude Code is proven as an MCP client in production; ChatGPT is not named (`docs/design/mcp-guide.md:145-164`).
- GitHub: a token with no scope reads public repositories, pull requests and releases ([scopes](https://docs.github.com/en/apps/oauth-apps/building-oauth-apps/scopes-for-oauth-apps)). Limits are 5,000 REST requests an hour per user and 30 searches a minute ([REST limits](https://docs.github.com/en/rest/using-the-rest-api/rate-limits-for-the-rest-api), [search](https://docs.github.com/en/rest/search/search)). Pull requests and releases carry the author's numeric id; commits are linked by email only ([commit linking](https://docs.github.com/en/pull-requests/committing-changes-to-your-project/troubleshooting-commits/why-are-my-commits-linked-to-the-wrong-user)).
- ChatGPT: custom MCP servers with write actions are documented for Business and Enterprise or Edu, and read-only for Pro; Plus and Free are not verified (needs verification: the help page blocked direct reading, [article](https://help.openai.com/en/articles/12584461-developer-mode-and-mcp-apps-in-chatgpt)). A reviewed plugin directory exists for MCP-backed apps ([submission](https://developers.openai.com/apps-sdk/deploy/submission)). Using a visitor's ChatGPT plan on a hosted site needs an interest form and is not generally available ([token sharing](https://developers.openai.com/siwc/token-sharing-open-source)).

## Recommended order

Projects and evidence first, then reviewed changes, then assistant-led review. The owner's lean holds: projects and GitHub evidence need no AI and give value alone. Reviewed changes must ship before any assistant-led review, because today an assistant's edit to a live resume is public the moment it saves.

|Version|Vision|A developer can|
|-|-|-|
|v0.9|Show the work|Give each project its own page and attach GitHub evidence they confirmed|
|v0.10|Changes you approve|See what an assistant or import proposes, accept or reject each change, and restore an earlier version|
|v0.11|Review with your own AI|Ask their assistant to review a resume, compare it with a job post, and propose changes for approval|
|v0.12|Plan the next step|Ask their assistant for skill gaps, next steps, and interview practice grounded in their own evidence|
|v1.0|A stable base|Rely on frozen contracts; nothing new|

## v0.9: show the work

Vision: a project stops being three lines on a resume and becomes a page that proves the work.

Features, in shipping order:

1. Project detail: a project entry gains role, technologies, outcome, repository link and demo link. The printed resume stays one compact entry.
2. Project page: each visible project of a published resume has a public page under that resume, following the resume's publish, sign in to view, and indexing settings.
3. Projects index: the resume's public page links to a list of its project pages.
4. Connect GitHub: the owner links a GitHub account to their aboutme account for evidence only, separate from sign-in, with no scope beyond public data.
5. Evidence picker: the owner picks repositories, merged pull requests and releases to attach to a project. aboutme matches the author by numeric id and stores a snapshot (title, URL, date, counts) with the time it was checked.
6. Evidence on the page: attached evidence renders on the project page with its checked date. Disconnecting GitHub removes the "confirmed" mark, and the owner chooses whether the links stay.
7. MCP: assistants can read and edit project detail. They cannot attach evidence or connect accounts.

Out of scope: private repositories, commit-level evidence, contribution graphs, automatic skill inference, ranking, any score.

Done when: a developer with one published resume can publish two project pages, one with confirmed GitHub evidence, and the link preview, sign in to view, and unpublish behave as on the resume page.

Before code: an ADR amending the public namespace for project routes (ADR 0003); a design for project pages and their print mapping; a schema version for project detail (ADR 0004); a design and ADR for GitHub connection and evidence storage, including retention and removal.

## v0.10: changes you approve

Vision: nothing an assistant or an import writes reaches a live resume until the owner has seen it.

Features, in shipping order:

1. Version history: every save keeps a restorable version for a bounded time; the owner can view and restore one.
2. Proposed changes: a write from MCP to a published resume becomes a proposal, not a save. Unpublished resumes keep direct writes.
3. Review screen: the owner sees each proposed change against the current text and accepts or rejects it, one by one or all.
4. Owner setting: per resume, "assistant edits need my approval", on by default for published resumes.
5. MCP: tools to list a resume's pending proposals and their outcome, so an assistant can follow up.

Out of scope: collaborative editing, comments from other people, a separate draft copy of the whole resume.

Done when: with the setting on, no MCP tool can change what a public viewer sees, proven by a test; and an owner can restore yesterday's version in two steps.

Before code: a design and ADR for versions and proposals (storage, retention, limits, interaction with compare-and-swap revisions and realtime); an amendment to ADR 0018 and the MCP guide.

## v0.11: review with your own AI

Vision: the owner's own assistant becomes a careful reviewer that proposes, and the owner decides.

Features, in shipping order:

1. Review guide over MCP: a server-provided prompt and a read-only rubric (structure, evidence, length, language) drawn from the [Vietnam tech resume guide](../design/vietnam-tech-resumes.md), so any assistant reviews the same way.
2. Job post comparison: the owner pastes a job post to their assistant; a tool returns the resume in a form suited to comparison and records nothing about the post.
3. Evidence rule in tools: a proposal that adds an achievement or number must name the source the owner gave; the review screen shows it.
4. Wider client reach: prove hosted Claude and ChatGPT, and submit an aboutme app to the ChatGPT directory if its terms fit.

Out of scope: any hosted model, any score shown to other people, storing job posts.

Done when: with two named assistants, an owner can get a review and a job comparison and accept some proposals, and no proposal can add an unsourced claim without the screen marking it.

Before code: a design for the review prompt, rubric and comparison tool; a client compatibility proof plan; a decision on directory submission.

## v0.12: plan the next step

Vision: the same evidence that proves the work shows what to learn next.

Features, in shipping order:

1. Skill evidence view: the owner sees which listed skills have project or GitHub evidence and which have none. Private to the owner.
2. Career path prompt: a server-provided prompt that lets the assistant suggest gaps and next steps for a target role from that view.
3. Interview practice prompt: questions drawn from the resume, its projects and a pasted job post.

Out of scope: public skill badges, endorsements, a hosted interviewer, any ranking of people.

Done when: an owner can run both prompts with a named assistant and nothing from the session is stored by aboutme.

Before code: a design for the skill evidence view and both prompts.

## v1.0: a stable base

No features. The owner decides when to cut the first candidate; that commit records the supported contracts and adds `.v1-baseline`, which freezes migrations and released schemas (`AGENTS.md:99`, `docs/adr/0004-resume-document-contract.md:67-77`, `docs/adr/0005-database-migrations.md:40-49`). Launch gates stay in [backlog.md](backlog.md).

## Later

- Using a visitor's ChatGPT plan on the website: not available to hosted sites without OpenAI's approval. Not committed; never described as supported.
- The developer network: a separate product.
- The Flutter app: deferred beyond web v1 (`docs/design/product.md:39`).
- A publish scope for assistants: deferred, not designed away (`docs/adr/0018-mcp-agent-access.md:93-94`).

## Open questions

Each needs the owner's answer before the named version starts. The first option is the recommendation.

1. Project URL (v0.9). `aboutme.vn/{slug}/projects` and `aboutme.vn/{slug}/projects/{project}` under the resume, or a separate project namespace. Recommended: under the resume, because the resume already owns publish, privacy and indexing, and people stay without a public page.
2. Order of v0.10 and v0.11. Reviewed changes before assistant-led review, or review first. Recommended: reviewed changes first, because assistant edits to a live resume are public at once today.
3. GitHub connection (v0.9). A separate "connect for evidence" link with no scope, or turning on GitHub sign-in and reusing it. Recommended: separate connection, so sign-in policy and evidence stay independent.
4. Proposals by default (v0.10). On by default for published resumes, or off. Recommended: on.
5. ChatGPT directory (v0.11). Submit an aboutme app for review, or stay with user-added connectors. Recommended: decide after the client proofs, because documented custom connectors exclude most personal plans.

## Document conflicts found

Fix these in their own change; they do not block this plan.

- `PRODUCT.md:75` says production enables Google only; `docs/design/product.md:9-10` says Google and LinkedIn.
- `PRODUCT.md:91` lists analytics as out of v1; `docs/design/product.md:37` includes owner-only view counts.
- `docs/design/mcp-guide-copy.md:205-206` tells the user to review and then publish, which holds only for unpublished resumes.
