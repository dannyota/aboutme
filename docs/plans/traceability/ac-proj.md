# AC-PROJ traceability rows

Twelve acceptance-criterion rows use the `AC-PROJ-` prefix, for the account
project library and project pages ([design](../../design/projects.md),
[contract](../../design/projects-contract.md),
[ADR 0029](../../adr/0029-account-project-library.md)). The design is proposed
and awaits the owner's approval, so every row is `PLANNED` and names no test
yet. See [README.md](./README.md) for matrix rules and the full prefix index.

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
| AC-PROJ-009 | Representations | Public JSON, Markdown, JSON-LD, sitemap, `llms.txt`, showcase and the live stream are unchanged by projects; project pages are `noindex` unless SEO and GEO is on | v0.9 | PLANNED | None yet |
| AC-PROJ-010 | Agents | Library tools need `projects:read` or `projects:write`; a grant made before those scopes answers `scope_denied`; no tool publishes, attaches evidence or connects an account | v0.9 | PLANNED | None yet |
| AC-PROJ-011 | Print | In the public page and the PDF, a linked available entry's title links to its project page; layout and pagination of a resume without linked projects are unchanged | v0.9 | PLANNED | None yet |
| AC-PROJ-012 | Export and deletion | The account export includes the library, and account deletion removes the library and its slug holds | v0.9 | PLANNED | None yet |
