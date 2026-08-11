# AC-API traceability rows

2 acceptance-criterion rows with the `AC-API-` prefix, in their original matrix
order. See [README.md](./README.md) for the matrix purpose, maintenance rules,
and the full prefix index.

| ID         | Spec clause | Statement                                                                                                                                                                                                                                                  | Phase/task | Test / UAT reference |
| ---------- | ----------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ---------- | -------------------- |
| AC-API-001 | §4          | The TypeScript API client is derived from `docs/api/openapi.yaml`: generated path/schema types are committed and drift-checked, a pinned typed transport consumes them, and the Nuxt client compiles and proves a representative request/response contract | P0F        | (pending)            |
| AC-API-002 | §4          | The Dart API client is generated from `docs/api/openapi.yaml`, committed and drift-checked, and compiles against the Flutter app without weakening the shared wire contract                                                                                | P11        | (pending)            |
