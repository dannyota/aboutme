# AC-PROJ traceability rows

Eighteen acceptance-criterion rows use the `AC-PROJ-` prefix, for the account
project library and project pages ([design](../../design/projects.md),
[contract](../../design/projects-contract.md),
[ADR 0029](../../adr/0029-account-project-library.md)), and for GitHub
evidence ([design](../../design/github-evidence.md),
[ADR 0030](../../adr/0030-github-evidence.md), proposed). The owner approved
the design on 2026-11-11; nothing is implemented, so every row is `PLANNED` and
names no test yet. See [README.md](./README.md) for matrix rules and the full prefix index.

| ID | Design clause | Statement | Phase/task | State | Test/UAT reference |
| - | - | - | - | - | - |
| AC-PROJ-001 | Library | A library project is private to its account: no route returns it without that account's session or grant, and its id never appears in any public response | v0.9 | PLANNED | None yet |
| AC-PROJ-002 | Selection | Every resume write rejects a `projectId` of another account or a second entry naming the same project, on create, entry upsert, import and agent writes | v0.9 | PLANNED | None yet |
| AC-PROJ-003 | Selection | Linking prefills the entry from the chosen language text and creates the project section when the resume has none; later library edits do not change the entry | v0.9 | PLANNED | None yet |
| AC-PROJ-004 | Language | A project page uses the text for the resume language's primary subtag, or else the project's default language; a resume with `und` uses the default | v0.9 | PLANNED | None yet |
| AC-PROJ-005 | Project pages | `/{slug}/projects` and `/{slug}/projects/{project-slug}` are served only for a live resume and a visible linked entry; every other state answers as an unknown resume | v0.9 | PLANNED | None yet |
| AC-PROJ-006 | Generations and revocation | Unpublish, sign in to view, hide, unlink, project deletion and slug rename take effect on the next request, and a cached page is never reused across them | v0.9 | PLANNED | None yet |
| AC-PROJ-007 | Library mutations | Deleting a project unlinks it from every resume of the account in one transaction, leaves each document valid, advances each revision, and is replayed by its idempotency key | v0.9 | PLANNED | None yet |
| AC-PROJ-008 | Library mutations | A released project slug cannot be claimed again within the account for 180 days | v0.9 | PLANNED | None yet |
| AC-PROJ-009 | Representations | Public JSON gains only `projectLinks` and no project id; Markdown, JSON-LD, sitemap, `llms.txt`, showcase and the live stream protocol are unchanged by projects; project pages are `noindex` unless SEO and GEO is on | v0.9 | PLANNED | None yet |
| AC-PROJ-010 | Agents | Library tools need `projects:read` or `projects:write`; a grant made before those scopes answers `scope_denied`; no tool publishes, attaches evidence or connects an account | v0.9 | PLANNED | None yet |
| AC-PROJ-011 | Print | In the public page and the PDF, a linked available entry's title links to its project page; layout and pagination of a resume without linked projects are unchanged | v0.9 | PLANNED | None yet |
| AC-PROJ-012 | Export and deletion | The account export includes the library, and account deletion removes the library and its slug holds | v0.9 | PLANNED | None yet |
| AC-PROJ-013 | GitHub evidence: enablement and connection | With evidence credentials unset every evidence route answers 404 and sign-in is unchanged; a connection is made only by the user who started it, with an unchanged connection generation, and never signs in or links an identity | v0.9 | PLANNED | None yet |
| AC-PROJ-014 | GitHub evidence: session | A token response with a non-empty or missing scope is refused; the token exists only in server memory for one session of at most 30 minutes and appears in no response, log, export or database row | v0.9 | PLANNED | None yet |
| AC-PROJ-015 | GitHub evidence: items | An item is attached only when its rule holds for the connected account id; a login match, contributor listing or commit never confirms; a disconnect that commits during an attach leaves nothing attached | v0.9 | PLANNED | None yet |
| AC-PROJ-016 | GitHub evidence: outbound | Outbound requests go only to the fixed GitHub API host within the time, size and count bounds; a pasted link is rebuilt, redirects are refused, and recheck follows a renamed repository by id | v0.9 | PLANNED | None yet |
| AC-PROJ-017 | GitHub evidence: page and writes | Evidence shows its label and checked date, follows the project page's gating and revocation, is unchanged by a project `PUT` or agent write, and no request reaches GitHub when a page is viewed | v0.9 | PLANNED | None yet |
| AC-PROJ-018 | GitHub evidence: removal | Disconnect and account deletion leave no connection or session, and no bearer token or agent tool can connect, attach, recheck or remove evidence | v0.9 | PLANNED | None yet |
