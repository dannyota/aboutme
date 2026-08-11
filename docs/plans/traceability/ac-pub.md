# AC-PUB traceability rows

5 acceptance-criterion rows with the `AC-PUB-` prefix, in their original matrix
order. See [README.md](./README.md) for the matrix purpose, maintenance rules,
and the full prefix index.

| ID         | Spec clause | Statement                                                          | Phase/task | Test / UAT reference |
| ---------- | ----------- | ------------------------------------------------------------------ | ---------- | -------------------- |
| AC-PUB-001 | §3          | Unpublish keeps the slug; only rename/delete release it            | P5A        | (pending)            |
| AC-PUB-002 | §3          | Released slugs tombstoned 180 days                                 | P5A        | (pending)            |
| AC-PUB-003 | §4          | Publish-state matrix: live=false ⇒ all surfaces 404/410            | P5A        | (pending)            |
| AC-PUB-004 | §4          | seo_geo off ⇒ X-Robots-Tag noindex, .md 404, excluded from sitemap | P5A        | (pending)            |
| AC-PUB-005 | §4          | download_enabled gates the public PDF endpoint                     | P7B        | (pending)            |
