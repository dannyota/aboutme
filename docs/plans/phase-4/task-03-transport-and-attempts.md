# Task 03: Browser transport and frozen attempts

**Owner:** One high-judgment web author.

**Authorities:** current OpenAPI, `design.md` Route boundaries and Security,
`mutation-contract.md` Attempt construction through `412` reconciliation,
`editor-contract.md` Photo lifecycle, ADRs 0016/0017, and D2/D3/D7/D8.

**Acceptance:** AC-EDITOR-001, AC-EDITOR-005, AC-EDITOR-007, and AC-EDITOR-013.

**Files:**

- Create: `apps/web/app/editor/attempt.ts`
- Create: `apps/web/app/editor/resumeApi.ts`
- Create: `apps/web/test/editor/attempt.test.ts`
- Create: `apps/web/test/editor/resume-api.test.ts`

**Interfaces:** `attempt.ts` is the only definition site for transport result
types. Later tasks import them; they do not redeclare aliases.

```ts
export type ResumeOperation =
  | "createResume"
  | "updateResumeMetadata"
  | "upsertResumeEntry"
  | "deleteResumeEntry"
  | "updateResumeSection"
  | "updateResumeStructure"
  | "updateResumePersonalDetails"
  | "updateResumeCustomization"
  | "uploadResumePhoto"
  | "updateResumePhotoCrop"
  | "deleteResumePhoto"
  | "deleteResume";
export type AttemptPayload =
  | { readonly kind: "json"; readonly utf8: string }
  | { readonly kind: "empty" }
  | { readonly kind: "photo"; readonly file: File };
export interface FrozenAttempt {
  readonly id: string;
  readonly operation: ResumeOperation;
  readonly url: string;
  readonly method: "POST" | "PATCH" | "DELETE";
  readonly schemaVersion: typeof CURRENT_VERSION;
  readonly ifMatch?: ParentETag;
  readonly idempotencyKey: string;
  readonly payload: AttemptPayload;
  readonly firstDispatchAt: number;
  readonly retryCutoff: number;
  readonly automaticReplays: 0 | 1;
  readonly staleRebases: 0 | 1;
}
export interface ValidatedStaleWinner {
  readonly document: Resume;
  readonly revision: Revision;
}
export type AttemptFailureCode =
  | "bad_request"
  | "body_too_large"
  | "customization_path_denied"
  | "idempotency_key_invalid"
  | "idempotency_key_required"
  | "invalid_client_ip"
  | "media_invalid"
  | "media_not_found"
  | "media_too_large"
  | "media_type_unsupported"
  | "method_not_allowed"
  | "not_found"
  | "precondition_malformed"
  | "precondition_not_supported"
  | "precondition_required"
  | "request_invalid"
  | "response_invalid"
  | "resume_cap_exceeded"
  | "resume_not_found"
  | "unsupported_schema_version";
type GeneratedServerValidationIssue = NonNullable<
  NonNullable<components["schemas"]["Error"]["error"]["details"]>["issues"]
>[number];
export type ServerValidationIssue = Readonly<
  Pick<GeneratedServerValidationIssue, "path" | "code">
>;
export type AttemptResult =
  | {
      readonly kind: "complete";
      readonly status: 200 | 201;
      readonly accepted: AcceptedResume;
    }
  | {
      readonly kind: "child-ack";
      readonly status: 204;
      readonly scope: "entry" | "photo";
      readonly etag: ParentETag;
    }
  | { readonly kind: "resume-deleted"; readonly status: 204 }
  | {
      readonly kind: "stale";
      readonly status: 412;
      readonly winner: ValidatedStaleWinner;
    }
  | { readonly kind: "csrf-rejected" }
  | { readonly kind: "session-lost" }
  | {
      readonly kind: "validation-rejected";
      readonly issues: readonly ServerValidationIssue[];
    }
  | { readonly kind: "rate-limited"; readonly retryAfterMs: number | null }
  | { readonly kind: "media-busy"; readonly retryAfterMs: number | null }
  | { readonly kind: "idempotency-reuse" }
  | { readonly kind: "rejected"; readonly code: AttemptFailureCode }
  | { readonly kind: "unknown"; readonly reason: "transport" | "server" };
export interface ResumeSummary extends ResumeMetadata {
  readonly revision: Revision;
}
export type ResumeListResult =
  | { readonly kind: "ready"; readonly items: readonly ResumeSummary[] }
  | { readonly kind: "session-lost" }
  | { readonly kind: "rate-limited"; readonly retryAfterMs: number | null }
  | {
      readonly kind: "failed";
      readonly reason: "network" | "response-invalid";
    };
export type ResumeReadResult =
  | { readonly kind: "complete"; readonly accepted: AcceptedResume }
  | { readonly kind: "unavailable" }
  | { readonly kind: "session-lost" }
  | { readonly kind: "rate-limited"; readonly retryAfterMs: number | null }
  | {
      readonly kind: "failed";
      readonly reason: "network" | "response-invalid";
    };
export type ObjectETag = string & { readonly __objectETag: unique symbol };
export type OwnerPhotoReadResult =
  | {
      readonly kind: "bytes";
      readonly mime: "image/jpeg" | "image/png";
      readonly etag: ObjectETag;
      readonly bytes: Uint8Array;
    }
  | { readonly kind: "not-modified"; readonly etag: ObjectETag }
  | {
      readonly kind: "unavailable";
      readonly reason: "not-found" | "session-lost" | "invalid" | "network";
    };
```

```ts
export interface ResumeApi {
  list(): Promise<ResumeListResult>;
  read(id: string): Promise<ResumeReadResult>;
  dispatch(attempt: FrozenAttempt, csrfToken: string): Promise<AttemptResult>;
  readOwnerPhoto(id: string, etag?: ObjectETag): Promise<OwnerPhotoReadResult>;
}
export function freezeAttempt(
  command: AtomicEditorCommand,
  accepted: AcceptedResume,
  runtime: EditorRuntime,
): FrozenAttempt;
export function freezeCreateAttempt(
  intent: CreateResumeIntent,
  runtime: EditorRuntime,
): FrozenAttempt;
export function requestFromAttempt(
  attempt: FrozenAttempt,
  csrfToken: string,
): Request;
export function parseObjectETag(value: string | null): ObjectETag;
export function createResumeApi(fetcher?: typeof fetch): ResumeApi;
```

Request body aliases come from generated `operations`; opaque document/entry
members are narrowed with Task 01 types. UI code never calls `fetch`, `$fetch`,
or `createApiClient` directly.

- [ ] **Step 1: Write the frozen-attempt RED test**

Cover all 12 operations. Assert exact URL, method, schema header, quoted
`If-Match`, key, content type, and JSON text/empty/file payload. Build two
requests from one descriptor and compare semantic bytes and headers. Create has
no precondition. Existing-resume attempts use only accepted revision.

```ts
it("freezes metadata request bytes and varies only CSRF on replay", () => {
  const accepted = acceptedFixture();
  let uuid = 0;
  const runtime: EditorRuntime = {
    nowEpochMs: () => 100,
    uuid: () => `id-${++uuid}`,
    delay: async () => {},
  };
  const command = captureCommand(
    accepted,
    {
      resumeId: accepted.metadata.id,
      ownerId: "owner-1",
      sequence: 1,
      dependencyIds: [],
      intent: { kind: "metadataField", field: "title", value: "Ada" },
    },
    runtime,
  );
  const attempt = freezeAttempt(command, accepted, runtime);
  const first = requestFromAttempt(attempt, "csrf-a");
  const second = requestFromAttempt(attempt, "csrf-b");
  expect(attempt).toMatchObject({
    operation: "updateResumeMetadata",
    url: `/api/v1/resumes/${accepted.metadata.id}`,
    method: "PATCH",
    ifMatch: '"r1"',
  });
  expect(first.headers.get("Idempotency-Key")).toBe(
    second.headers.get("Idempotency-Key"),
  );
  expect(first.headers.get("X-Resume-Schema-Version")).toBe(
    String(CURRENT_VERSION),
  );
  expect(first.headers.get("X-CSRF-Token")).not.toBe(
    second.headers.get("X-CSRF-Token"),
  );
});
```

- [ ] **Step 2: Run the frozen-attempt test RED**

Run:

```sh
(cd apps/web && npx vitest run test/editor/attempt.test.ts)
```

Expected RED: FAIL because freeze functions do not exist.

- [ ] **Step 3: Implement minimal freeze/request construction**

Map the discriminant to a closed operation/path/method builder. Materialize
whole entry/personal payloads from `accepted` plus only the command intent.
Freeze JSON once:

```ts
const firstDispatchAt = runtime.nowEpochMs();
return Object.freeze({
  ...wire,
  id: command.id,
  idempotencyKey: runtime.uuid(),
  payload:
    wire.body === undefined
      ? { kind: "empty" }
      : { kind: "json", utf8: JSON.stringify(wire.body) },
  firstDispatchAt,
  retryCutoff: firstDispatchAt + 23 * 60 * 60 * 1000,
  automaticReplays: 0,
  staleRebases: 0,
});
```

Each dispatch creates a new `Request`. Empty bodies omit `Content-Type`.
Multipart recreates framing around the same accepted `File`; raw part bytes stay
unchanged.

- [ ] **Step 4: Rerun the frozen-attempt test GREEN**

Run the Step 2 command. Expected GREEN: PASS.

- [ ] **Step 5: Write the response/read RED test**

Use real `Response` objects. Cover every list/read result, complete `200`/`201`,
child and resume `204`, `401`, CSRF `403`, idempotency `409`, valid/malformed
`412`, `413`, `415`, validation `422`, `429`, media-busy `503`, other `5xx`, and
thrown transport errors. Reject any non-exact cache policy,
weak/missing/mismatched ETag, wrong schema header, body/ETag revision mismatch,
invalid document, and revision regression. For `422`, include a sentinel raw
`message` and assert the parsed `ServerValidationIssue` contains only `path` and
`code`.

```ts
it("drops raw validation messages at the transport boundary", async () => {
  const fetcher = vi.fn().mockResolvedValue(
    new Response(
      JSON.stringify({
        error: {
          code: "document_invalid",
          message: "raw envelope",
          details: {
            issues: [
              {
                path: "content.work",
                code: "required",
                message: "sentinel raw issue",
              },
            ],
          },
        },
      }),
      {
        status: 422,
        headers: {
          "Cache-Control": "no-store, no-transform",
          "Content-Type": "application/json",
        },
      },
    ),
  );
  const intent: CreateResumeIntent = {
    kind: "resumeCreate",
    id: "create-1",
    ownerId: "owner-1",
    sequence: 0,
    title: "Fixture",
  };
  const runtime: EditorRuntime = {
    nowEpochMs: () => 0,
    uuid: () => "key-1",
    delay: async () => {},
  };
  const result = await createResumeApi(fetcher).dispatch(
    freezeCreateAttempt(intent, runtime),
    "csrf",
  );
  expect(result).toEqual({
    kind: "validation-rejected",
    issues: [{ path: "content.work", code: "required" }],
  });
  expect(JSON.stringify(result)).not.toContain("sentinel");
});
```

- [ ] **Step 6: Run the response/read test RED**

Run:

```sh
(cd apps/web && npx vitest run test/editor/resume-api.test.ts)
```

Expected RED: FAIL because response parsing is absent.

- [ ] **Step 7: Implement minimal closed response parsing**

All requests set `credentials: 'include'` and `cache: 'no-store'`. Before a
status branch, require exact `Cache-Control: no-store, no-transform`. Parse as:

```ts
if (status === 200 || status === 201) return parseComplete(response);
if (status === 204) return parseBodyless(response, attempt.operation);
if (status === 412) return parseValidatedWinner(response);
const error = await parseErrorEnvelope(response);
if (status === 503 && error.code === "media_busy") return parseMediaBusy(error);
if (status >= 500) return { kind: "unknown", reason: "server" };
return parseClosedFailure(status, error);
```

Map the generated wire issue into the distinct UI-safe type:

```ts
const safeIssues = (
  issues: readonly GeneratedServerValidationIssue[] | undefined,
): readonly ServerValidationIssue[] =>
  (issues ?? []).map(({ path, code }) => Object.freeze({ path, code }));
```

`parseComplete` validates body, schema header, and parent tag agreement.
`parseBodyless` rejects bytes/content type and requires a tag only for entry or
photo child operations. `parseValidatedWinner` accepts only the `412` details'
document and revision; it neither requires an ETag nor fabricates summary
metadata. Never return or log a raw server message.

- [ ] **Step 8: Rerun the response/read test GREEN**

Run the Step 6 command. Expected GREEN: PASS.

- [ ] **Step 9: Write the owner-photo RED test**

Require `200` JPEG/PNG plus strong object tag and bytes; `304` plus the same
strong tag and no body; or the closed unavailable result. Reject HTML, SVG,
wrong/missing MIME, weak/missing tag, unexpected `304` body, and distinguish no
wrong-owner versus missing detail.

```ts
const resumeId = "resume-1";
const pngBytes = new Uint8Array([0x89, 0x50, 0x4e, 0x47]);
const fetcher = vi.fn();
it.each([
  [200, { "Content-Type": "image/png", ETag: '"photo-1"' }, "bytes"],
  [304, { ETag: '"photo-1"' }, "not-modified"],
  [200, { "Content-Type": "image/svg+xml", ETag: '"photo-1"' }, "unavailable"],
] as const)("maps owner photo %s to %s", async (status, headers, kind) => {
  fetcher.mockResolvedValue(
    new Response(status === 200 ? pngBytes : null, { status, headers }),
  );
  expect((await createResumeApi(fetcher).readOwnerPhoto(resumeId)).kind).toBe(
    kind,
  );
});
```

- [ ] **Step 10: Run the owner-photo test RED**

Run the Step 6 command. Expected RED: FAIL because `readOwnerPhoto` is absent.

- [ ] **Step 11: Implement minimal owner-photo read**

Send `If-None-Match` only for a validated prior tag. Copy accepted bytes into a
new `Uint8Array`; transport creates no object/data URL.

```ts
const bytes = new Uint8Array(await response.arrayBuffer());
return {
  kind: "bytes",
  mime,
  etag: parseObjectETag(response.headers.get("ETag")),
  bytes: bytes.slice(),
};
```

- [ ] **Step 12: Rerun the owner-photo test GREEN**

Run the Step 10 command. Expected GREEN: PASS.

- [ ] **Step 13: Run the final task gate and report**

```sh
(cd apps/web && npx vitest run test/editor/{attempt,resume-api}.test.ts)
(cd apps/web && npx eslint app/editor/{attempt,resumeApi}.ts \
  test/editor/{attempt,resume-api}.test.ts)
(cd apps/web && npx vue-tsc --build --noEmit)
```

Suggested commit: `feat(editor): add frozen resume transport`.
