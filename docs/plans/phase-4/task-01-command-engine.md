# Task 01: Domain types, response validation, and command engine

**Owner:** One high-judgment web author.

**Authorities:** `design.md` State model and Edit and save,
`mutation-contract.md` Command capture through Target and context projections,
ADRs 0005/0009/0017, and D1–D6.

**Acceptance:** AC-EDITOR-001, AC-EDITOR-003, and AC-EDITOR-007.

**Files:**

- Create: `apps/web/app/editor/types.ts`
- Create: `apps/web/app/editor/revision.ts`
- Create: `apps/web/app/editor/documentValidation.ts`
- Create: `apps/web/app/editor/commands.ts`
- Create: `apps/web/app/editor/reducer.ts`
- Create: `apps/web/app/editor/projections.ts`
- Create: `apps/web/app/editor/coalesce.ts`
- Create: `apps/web/test/editor/revision.test.ts`
- Create: `apps/web/test/editor/document-validation.test.ts`
- Create: `apps/web/test/editor/commands.test.ts`
- Create: `apps/web/test/editor/reducer.test.ts`
- Create: `apps/web/test/editor/projections.test.ts`
- Create: `apps/web/test/editor/coalesce.test.ts`
- Create: `apps/web/test/editor/fixture.ts`

**Interfaces:** `types.ts` is the only definition site for these cross-task
domain types:

```ts
export type Revision = string & { readonly __revision: unique symbol };
export type ParentETag = string & { readonly __parentETag: unique symbol };
export type SaveState =
  | "idle"
  | "dirty"
  | "saving"
  | "saved"
  | "offline"
  | "error"
  | "conflict"
  | "session-lost";
export type JsonValue =
  | null
  | boolean
  | number
  | string
  | readonly JsonValue[]
  | { readonly [key: string]: JsonValue };
export type Presence<T = unknown> =
  { readonly present: false } | { readonly present: true; readonly value: T };
export interface PlacementProjection {
  readonly main: readonly string[];
  readonly sidebar: readonly string[];
}
export type TemplateCustomizationProjection = Omit<
  Resume["customization"],
  "layout"
> & {
  readonly layout: Omit<Resume["customization"]["layout"], "sections">;
};
export interface TemplateTargetProjection {
  readonly placement: PlacementProjection;
  readonly customization: TemplateCustomizationProjection;
}
export type ContentIdentityProjection = readonly {
  readonly key: string;
  readonly sectionType: Section["sectionType"];
}[];
export type ProjectionValue =
  | JsonValue
  | ResumeSnapshot
  | Resume["personalDetails"]
  | Resume["customization"]
  | TemplateCustomizationProjection
  | Resume["content"]
  | Section
  | Section["entries"][number]
  | ReadonlyArray<Section["entries"][number]>
  | PersonalDetail
  | readonly PersonalDetail[]
  | Photo
  | PhotoCrop
  | DateRange
  | YearMonth
  | PlacementProjection
  | TemplateTargetProjection
  | ContentIdentityProjection;
export type ProjectionContextKey =
  | "resumeId"
  | "schemaVersion"
  | "ownerId"
  | "sectionKey"
  | "sectionType"
  | "entryId"
  | "membership"
  | "photoKey"
  | "placement"
  | "customization"
  | "contentIdentity"
  | `section:${string}:type`
  | `entry:${string}:membership`
  | `untouched:${string}:type`;
export interface Projection {
  readonly target: Presence<ProjectionValue>;
  readonly context: Readonly<
    Partial<Record<ProjectionContextKey, Presence<ProjectionValue>>>
  >;
}
export interface ResumeMetadata {
  readonly id: string;
  readonly title: string;
  readonly lng: string;
  readonly live: boolean;
  readonly slug: string | null;
  readonly schemaVersion: typeof CURRENT_VERSION;
  readonly createdAt: string;
  readonly updatedAt: string;
}
export interface ResumeSnapshot {
  readonly document: Resume;
  readonly metadata: ResumeMetadata;
}
export interface AcceptedResume extends ResumeSnapshot {
  readonly revision: Revision;
  readonly metadataFreshness: "complete" | "stale";
}
export interface EditorRuntime {
  nowEpochMs(): number;
  uuid(): string;
  delay(ms: number): Promise<void>;
}
export function parseRevision(value: unknown): Revision;
export function parseParentETag(value: string | null): ParentETag;
export function parentETag(revision: Revision): ParentETag;
export function compareRevision(left: Revision, right: Revision): -1 | 0 | 1;
export function parseCurrentDocument(value: unknown): Resume;
```

`commands.ts` owns these data-only shapes. `StructureEdit` and
`CustomizationDelta` mirror the current OpenAPI operations but do not import
transport code.

<!-- prettier-ignore -->
```ts
export type StructureEdit =
  | { op: "createSection"; key: string; sectionType: Section["sectionType"];
      column: "main" | "sidebar"; index: number; displayName?: string; iconKey?: string }
  | { op: "deleteSection"; key: string }
  | { op: "moveSection"; key: string; column: "main" | "sidebar"; index: number }
  | { op: "reorderColumn"; column: "main" | "sidebar"; keys: readonly string[] };
export type EntryFieldPath =
  | "isHidden" | "text" | "jobTitle" | "employer" | "employerLink"
  | "city" | "country" | "dates" | "description" | "degree" | "school"
  | "schoolLink" | "name" | "level" | "infoHtml" | "title" | "titleLink"
  | "issuer" | "date" | "link" | "subtitle";
export type CustomizationSetPath =
  | "font.family" | "font.baseSizePx" | "colors.primary" | "colors.text"
  | "colors.background" | "colors.accent" | "colors.surface"
  | "spacing.sectionGap" | "spacing.entryGap" | "spacing.lineHeight"
  | "spacing.pageMargin.x" | "spacing.pageMargin.y" | "heading.style"
  | "heading.showRule" | "header.align" | "header.detailsLayout"
  | "header.iconStyle" | "layout.columns" | "layout.surfaceTarget"
  | "sectionDisplay.skill.style" | "sectionDisplay.language.style"
  | "pageFormat" | "dateFormat";
export type CustomizationUnsetPath =
  | "colors.accent" | "colors.surface" | "spacing.pageMargin" | "header"
  | "layout.surfaceTarget";
export type CustomizationDelta =
  | { op: "set"; path: CustomizationSetPath; value: JsonValue }
  | { op: "unset"; path: CustomizationUnsetPath };
interface CommandEnvelope {
  readonly id: string;
  readonly resumeId: string;
  readonly ownerId: string;
  readonly sequence: number;
  readonly targetKey: string;
  readonly base: Projection;
  readonly intended: Projection | null;
  readonly dependencyIds: readonly string[];
}
export type AtomicCommandIntent =
  | { kind: "metadataField"; field: "title"; value: string }
  | { kind: "metadataField"; field: "lng"; value: string | null }
  | { kind: "personalField"; path: "fullName" | "headline" | "details"; value: Presence }
  | { kind: "entryField"; sectionKey: string; entryId: string;
      path: EntryFieldPath; value: Presence }
  | { kind: "entryUpsert"; sectionKey: string; entry: Section["entries"][number] }
  | { kind: "entryDelete"; sectionKey: string; entryId: string }
  | { kind: "entryReorder"; sectionKey: string; entryIds: readonly string[] }
  | { kind: "sectionMetadata"; sectionKey: string; change:
        | { field: "displayName"; value: string }
        | { field: "iconKey"; value: string | null } }
  | { kind: "structure"; commands: readonly StructureEdit[] }
  | { kind: "customization"; deltas: readonly CustomizationDelta[] }
  | { kind: "photoCrop"; crop: PhotoCrop | null }
  | { kind: "photoUpload"; file: File }
  | { kind: "photoDelete" }
  | { kind: "resumeDelete"; confirmedTitle: string };
export type AtomicEditorCommand = CommandEnvelope & AtomicCommandIntent;
export interface CaptureCommandInput {
  readonly resumeId: string; readonly ownerId: string; readonly sequence: number;
  readonly dependencyIds: readonly string[]; readonly intent: AtomicCommandIntent;
}
export interface CreateResumeIntent {
  readonly kind: "resumeCreate"; readonly id: string; readonly ownerId: string;
  readonly sequence: number; readonly title: string; readonly lng?: string | null;
}
export function captureCommand(snapshot: ResumeSnapshot, input: CaptureCommandInput,
  runtime: EditorRuntime): AtomicEditorCommand;
export function applyIntent(snapshot: ResumeSnapshot,
  intent: AtomicCommandIntent): ResumeSnapshot;
export function replayCommand(snapshot: ResumeSnapshot,
  command: AtomicEditorCommand): ResumeSnapshot;
export function projectIntent(snapshot: ResumeSnapshot,
  intent: AtomicCommandIntent): Projection;
export function projectCommand(snapshot: ResumeSnapshot,
  command: AtomicEditorCommand): Projection;
export function equalProjection(left: Projection, right: Projection): boolean;
export function coalescePending(pending: readonly AtomicEditorCommand[],
  next: AtomicEditorCommand): readonly AtomicEditorCommand[];
export function acceptedFixture(
  overrides?: Partial<AcceptedResume>,
): AcceptedResume;
```

`AtomicEditorCommand` excludes template groups. Task 02 defines that distinct
queue-item type once. Create is separate because it has no resume ID or
addressable base before a validated `201`. Only `photoUpload` has
`intended: null`: its server-owned photo key is opaque until the same frozen
attempt succeeds. Every other command has a projected intended value.

`test/editor/fixture.ts` is the only shared test-fixture producer. It parses
`../../packages/schema/fixtures/minimal.json`, returns fresh structured clones,
and uses fixed owner/metadata plus `parseRevision("1")`; later tests import it
instead of inventing an incompatible snapshot.

- [ ] **Step 1: Write the revision/document-validation RED tests**

Accept `1`, `42`, and `9223372036854775807`. Reject zero, leading zeroes, signs,
whitespace, exponent/decimal text, overflow, numbers, and weak or malformed
ETags. Validate `minimal.json`; reject wrong-version, schema-invalid,
layout-invalid, duplicate-entry, unsafe-contact, and traversal fixtures.

```ts
it.each(["1", "42", "9223372036854775807"])("accepts revision %s", (value) => {
  expect(parseRevision(value)).toBe(value);
  expect(parseParentETag(`"r${value}"`)).toBe(`"r${value}"`);
});
it.each([
  0,
  "0",
  "01",
  "+1",
  " 1",
  "1.0",
  "1e2",
  "9223372036854775808",
  'W/"r1"',
])("rejects non-canonical revision %j", (value) =>
  expect(() => parseRevision(value)).toThrow(),
);
it("validates only current-v2 complete documents", () => {
  expect(parseCurrentDocument(structuredClone(minimalFixture))).toEqual(
    minimalFixture,
  );
  expect(() =>
    parseCurrentDocument({ ...minimalFixture, schemaVersion: 1 }),
  ).toThrow();
});
```

- [ ] **Step 2: Run the revision/document tests RED**

Run:

```sh
(cd apps/web && npx vitest run \
  test/editor/revision.test.ts test/editor/document-validation.test.ts)
```

Expected RED: FAIL because the modules do not exist.

- [ ] **Step 3: Implement the minimal validators and shared fixture**

Compile the exported current schema once with strict Ajv 2020-12 and formats.
After JSON Schema passes, require `CURRENT_VERSION` and zero aggregate
`validateDocument` issues. Compare revision length then lexical text against the
signed-64-bit maximum. `parseParentETag` accepts only `/^"r([1-9][0-9]*)"$/` and
calls `parseRevision` on the capture.

```ts
// revision.ts
const DECIMAL = /^[1-9][0-9]*$/;
const MAX_REVISION = "9223372036854775807";
export function parseRevision(value: unknown): Revision {
  if (typeof value !== "string" || !DECIMAL.test(value))
    throw new Error("invalid revision");
  if (
    value.length > MAX_REVISION.length ||
    (value.length === MAX_REVISION.length && value > MAX_REVISION)
  )
    throw new Error("revision out of range");
  return value as Revision;
}
// documentValidation.ts
export function parseCurrentDocument(value: unknown): Resume {
  if (
    !validateSchema(value) ||
    value.schemaVersion !== CURRENT_VERSION ||
    validateDocument(value).length
  )
    throw new Error("invalid current document");
  return value;
}
// test/editor/fixture.ts
import { readFileSync } from "node:fs";
const minimalFixture = JSON.parse(
  readFileSync("../../packages/schema/fixtures/minimal.json", "utf8"),
) as unknown;
const fixedMetadata: ResumeMetadata = {
  id: "resume-1",
  title: "Fixture",
  lng: "en",
  live: false,
  slug: null,
  schemaVersion: CURRENT_VERSION,
  createdAt: "2026-01-01T00:00:00Z",
  updatedAt: "2026-01-01T00:00:00Z",
};
export const acceptedFixture = (
  overrides: Partial<AcceptedResume> = {},
): AcceptedResume => ({
  document: structuredClone(parseCurrentDocument(minimalFixture)),
  metadata: structuredClone(fixedMetadata),
  revision: parseRevision("1"),
  metadataFreshness: "complete",
  ...overrides,
});
```

- [ ] **Step 4: Rerun the revision/document tests GREEN**

Run the Step 2 command. Expected GREEN: PASS.

- [ ] **Step 5: Write the command/reducer/projection RED tests**

For every union member, capture from optimistic state before apply. Prove
immutable deterministic replay and the contract's target/context table. Cover
absence versus `undefined`, `null`, `""`, zero, array order, whole-entry
materialization, new-entry document-wide binding, placement order, photo-key
context, opaque upload, authenticated owner identity, and destructive target
capture.

```ts
it("captures before replay and preserves absence distinctly", () => {
  const accepted = acceptedFixture();
  const runtime: EditorRuntime = {
    nowEpochMs: () => 10,
    uuid: () => "command-1",
    delay: async () => {},
  };
  const command = captureCommand(
    accepted,
    {
      resumeId: accepted.metadata.id,
      ownerId: "owner-1",
      sequence: 1,
      dependencyIds: [],
      intent: {
        kind: "personalField",
        path: "headline",
        value: { present: false },
      },
    },
    runtime,
  );
  expect(command.base.target).toEqual({
    present: true,
    value: accepted.document.personalDetails.headline,
  });
  expect(command.intended!.target).toEqual({ present: false });
  expect(
    replayCommand(accepted, command).document.personalDetails,
  ).not.toHaveProperty("headline");
  expect(accepted.document.personalDetails).toHaveProperty("headline");
});
```

- [ ] **Step 6: Run the command/reducer/projection tests RED**

Run:

```sh
(cd apps/web && npx vitest run \
  test/editor/{commands,reducer,projections}.test.ts)
```

Expected RED: FAIL because capture, replay, and projection exports are absent.

- [ ] **Step 7: Implement minimal pure capture/replay/projection**

Implement the state transition in this order:

```ts
const before = projectIntent(snapshot, input.intent);
const next = applyIntent(snapshot, input.intent);
const intended =
  input.intent.kind === "photoUpload"
    ? null
    : projectIntent(next, input.intent);
return deepFreeze({
  ...input.intent,
  id: runtime.uuid(),
  resumeId: input.resumeId,
  ownerId: input.ownerId,
  sequence: input.sequence,
  targetKey: targetKey(input.intent, input.sequence),
  base: before,
  intended,
  dependencyIds: input.dependencyIds,
});
```

Reducers clone only the changed path and use exhaustive `never` switches.
Projection walks placement arrays explicitly and represents absence with
`{ present: false }`; it never relies on `Object.keys(content)`. No function
reads ambient clock, random, network, Pinia, or Nuxt state.

- [ ] **Step 8: Rerun the command/reducer/projection tests GREEN**

Run the Step 6 command. Expected GREEN: PASS.

- [ ] **Step 9: Write the coalescing RED test**

Prove only adjacent unsent, non-destructive value commands with equal
`targetKey` and `kind` coalesce. The result retains the first base,
dependencies, ID, and sequence, then takes the last intended projection and
value. Upload, delete, structure, and entry-upsert commands never coalesce;
create bypasses the function.

```ts
const coalesceFixtures = (): readonly [
  AtomicEditorCommand,
  AtomicEditorCommand,
  AtomicEditorCommand,
] => {
  const accepted = acceptedFixture();
  let id = 0;
  const runtime: EditorRuntime = {
    nowEpochMs: () => 0,
    uuid: () => `c-${++id}`,
    delay: async () => {},
  };
  const input = (value: string, sequence: number): CaptureCommandInput => ({
    resumeId: accepted.metadata.id,
    ownerId: "owner-1",
    sequence,
    dependencyIds: [],
    intent: { kind: "metadataField", field: "title", value },
  });
  const first = captureCommand(accepted, input("A", 1), runtime);
  const last = captureCommand(
    replayCommand(accepted, first),
    input("B", 2),
    runtime,
  );
  const destructive = captureCommand(
    accepted,
    {
      resumeId: accepted.metadata.id,
      ownerId: "owner-1",
      sequence: 3,
      dependencyIds: [],
      intent: { kind: "resumeDelete", confirmedTitle: accepted.metadata.title },
    },
    runtime,
  );
  return [first, last, destructive];
};
it("coalesces only an adjacent unsent value command", () => {
  const [first, last, destructive] = coalesceFixtures();
  const merged = coalescePending([first], last);
  expect(merged).toEqual([
    { ...last, id: first.id, sequence: first.sequence, base: first.base },
  ]);
  expect(coalescePending(merged, destructive)).toEqual([
    ...merged,
    destructive,
  ]);
  expect(first).toEqual(coalesceFixtures()[0]);
});
```

- [ ] **Step 10: Run the coalescing test RED**

Run:

```sh
(cd apps/web && npx vitest run test/editor/coalesce.test.ts)
```

Expected RED: FAIL because `coalescePending` is absent.

- [ ] **Step 11: Implement minimal coalescing**

Use one left-to-right pass. Replace only the last output element when the
admission predicate passes; otherwise append the command unchanged. Never mutate
an input command.

```ts
const output = [...pending];
const previous = output.at(-1);
if (previous && isCoalescible(previous, next))
  output[output.length - 1] = {
    ...next,
    id: previous.id,
    sequence: previous.sequence,
    base: previous.base,
  };
else output.push(next);
return output;
```

- [ ] **Step 12: Rerun the coalescing test GREEN**

Run the Step 10 command. Expected GREEN: PASS.

- [ ] **Step 13: Run the final task gate and report**

```sh
(cd apps/web && npx vitest run \
  test/editor/{revision,document-validation,commands,reducer,projections,coalesce}.test.ts)
(cd apps/web && npx eslint \
  app/editor/{types,revision,documentValidation,commands,reducer,projections,coalesce}.ts \
  test/editor/{revision,document-validation,commands,reducer,projections,coalesce}.test.ts \
  test/editor/fixture.ts)
(cd apps/web && npx vue-tsc --build --noEmit)
```

Suggested commit: `feat(editor): add validated command domain`.
