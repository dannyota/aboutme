// Store-layer aggregate validation for the resume document (design spec §3
// "Relational constraints & store-layer invariants" / §5 sanitizer contract).
//
// resume.schema.json (JSON Schema) enforces everything expressible as a
// per-value or single-object constraint. These four rules are NOT
// expressible there, for the reasons documented at each rule's schema
// $def/description:
//
//   1. Byte-accurate rich-text length. JSON Schema's `maxLength` counts
//      Unicode code points, not bytes (see richText's description in
//      resume.schema.json) — a code point can be up to 4 UTF-8 bytes, so no
//      `maxLength` value can be both a tight AND a sound byte bound.
//   2. The layout aggregate invariant: every `content` section key appears
//      EXACTLY ONCE across `customization.layout.sections.main` +
//      `.sidebar` combined. This spans two different top-level document
//      fields (`content` and `customization`) — JSON Schema's `uniqueItems`
//      only dedups within a single array and cannot compare against a
//      sibling structure.
//   3. Date range ordering (`start` <= `end`). A cross-field numeric
//      comparison between two sibling object properties, which JSON
//      Schema's declarative keyword set cannot express (see dateRange's
//      description in resume.schema.json).
//   4. Entry-id uniqueness across the WHOLE resume (AC-DOC-002). JSON
//      Schema's `uniqueItems` only dedups within a single array; it cannot
//      catch the same id reused across two different `content` sections
//      (see content's description in resume.schema.json and
//      fixtures/store/invalid-duplicate-entry-id.json).
//
// This file is the TypeScript half of that store layer; gen/go/store_validate.go
// is the Go half — both apply the same four rules and are conformance-tested
// against the same fixtures/store/*.json corpus (see test/store-validation.test.ts
// and gen/go/store_validate_test.go). Neither half is generated: unlike
// resume.schema.json's types, these rules have no JSON Schema representation
// to generate FROM, so both are hand-written and kept in sync by that shared
// fixture corpus, the same pattern gen/go/section.go already uses for its own
// hand-written dispatch logic.
//
// Callers (apps/server's Go store layer, apps/web's autosave path) run
// validateDocument on every write, per design spec §3: "The fully assembled
// aggregate is validated on every write."

/** design spec §3 size bounds: ≤16 KB rich text per entry, byte-exact. */
export const MAX_RICH_TEXT_BYTES = 16384;

export interface ValidationIssue {
  /** Stable machine-readable rule id, e.g. "rich-text-byte-length". */
  rule: string;
  /** Dotted/bracketed path into the document, e.g. "content.work.entries[0].description". */
  path: string;
  /** Human-readable explanation, safe to surface in a 422 response. */
  message: string;
}

/** UTF-8 byte length of a string, computed the same way in every runtime (Node, browser, and — via len([]byte(s)) — Go's native string representation). */
export function utf8ByteLength(value: string): number {
  return new TextEncoder().encode(value).length;
}

export function validateRichTextByteLength(
  value: string,
  path: string,
  limit: number = MAX_RICH_TEXT_BYTES,
): ValidationIssue[] {
  const bytes = utf8ByteLength(value);
  if (bytes > limit) {
    return [
      {
        rule: "rich-text-byte-length",
        path,
        message:
          `${path}: ${bytes} UTF-8 bytes exceeds the ${limit}-byte limit ` +
          "(resume.schema.json's maxLength only bounds Unicode code points, not bytes)",
      },
    ];
  }
  return [];
}

// The rich-text field(s) each sectionType's entries carry — mirrors
// resume.schema.json's per-sectionType entry $defs (design spec §3's
// entry-fields table). "language" has no rich-text field.
//
// Phase-gate review finding I1: a plain object literal like this is indexed
// via the SAME lookup Object.prototype's own keys use, so
// TABLE[section.sectionType] for a hostile sectionType of "constructor",
// "__proto__", or "hasOwnProperty" resolved to an inherited Function
// instead of undefined — bypassing the `?? []` fallback below and crashing
// `for (const field of fields)` with "fields is not iterable" (a function
// has no iterator). Go's decode boundary rejects those same sectionType
// values before any of this code runs ("schema: unknown sectionType"), so
// this was a real TS-vs-Go behavior divergence, not just a theoretical one.
// Object.hasOwn checks OWN properties only, never the prototype chain, so it
// treats every hostile name exactly like any other unrecognized sectionType
// string: no table entry, zero rich-text fields, nothing to check.
const RICH_TEXT_FIELDS_BY_SECTION_TYPE: Record<string, string[]> = {
  profile: ["text"],
  work: ["description"],
  education: ["description"],
  skill: ["infoHtml"],
  language: [],
  certificate: ["description"],
  project: ["description"],
  custom: ["description"],
};

function richTextFieldsFor(sectionType: string | undefined): string[] {
  if (
    sectionType === undefined ||
    !Object.hasOwn(RICH_TEXT_FIELDS_BY_SECTION_TYPE, sectionType)
  ) {
    return [];
  }
  return RICH_TEXT_FIELDS_BY_SECTION_TYPE[sectionType];
}

/**
 * Coerces an arbitrary JSON value to an array, returning [] for anything
 * that ISN'T one — not just `null`/`undefined`, which a bare `?? []` alone
 * does not catch.
 *
 * Phase-gate re-review finding NEW-I1: `?? []` only substitutes for
 * `null`/`undefined`, so a document where an array-shaped field instead
 * carries a non-array truthy value — `entries: {a:1}`, `details: "x"`,
 * `main: 5` — still reached `.forEach`/a spread and threw an uncaught
 * TypeError: the exact crash class I1 was raised about, reintroduced by
 * this file's own I1 fix (`validatePersonalDetailUrlSchemes`) and still
 * live at three older call sites. Go's typed decode rejects every one of
 * these shapes at the JSON boundary with a clean error before any of this
 * code runs (e.g. `json: cannot unmarshal object into Go struct field
 * ...details of type []schema.PersonalDetail`); `toArray` gives the
 * untyped TS half — which, per this file's header, receives "whatever a
 * caller passes, untyped JSON in practice" — the same "malformed input
 * becomes nothing to iterate, not a crash" tolerance, without trying to
 * make TS reject what Go's decoder already rejects earlier.
 */
function toArray<T>(value: unknown): T[] {
  return Array.isArray(value) ? (value as T[]) : [];
}

interface DocumentEntry {
  [key: string]: unknown;
}

interface DocumentSection {
  sectionType?: string;
  entries?: DocumentEntry[];
}

export function validateRichTextLengths(
  content: Record<string, DocumentSection> | undefined,
  limit: number = MAX_RICH_TEXT_BYTES,
): ValidationIssue[] {
  const issues: ValidationIssue[] = [];
  for (const [sectionKey, section] of Object.entries(content ?? {})) {
    const fields = richTextFieldsFor(section?.sectionType);
    if (fields.length === 0) continue;
    toArray<DocumentEntry>(section.entries).forEach((entry, entryIndex) => {
      for (const field of fields) {
        const value = entry?.[field];
        if (typeof value === "string") {
          issues.push(
            ...validateRichTextByteLength(
              value,
              `content.${sectionKey}.entries[${entryIndex}].${field}`,
              limit,
            ),
          );
        }
      }
    });
  }
  return issues;
}

interface LayoutSections {
  main?: string[];
  sidebar?: string[];
}

/**
 * The layout aggregate invariant (design spec §3): every content[key] must
 * appear exactly once across layout.main + layout.sidebar combined. Reports
 * three distinct failure modes so callers/tests can tell them apart:
 *   - "layout-exactly-once": a key placed more than once (within one array —
 *     already unreachable once resume.schema.json's uniqueItems is applied —
 *     or split across both arrays, which uniqueItems cannot catch).
 *   - "layout-missing-content-key": a key placed in the layout that has no
 *     matching entry in content.
 *   - "layout-orphan-content-key": a content key that isn't placed anywhere.
 */
export function validateLayoutSections(
  content: Record<string, DocumentSection> | undefined,
  layoutSections: LayoutSections | undefined,
): ValidationIssue[] {
  const issues: ValidationIssue[] = [];
  const contentKeys = new Set(Object.keys(content ?? {}));

  const placements: string[] = [
    ...toArray<string>(layoutSections?.main),
    ...toArray<string>(layoutSections?.sidebar),
  ];
  const counts = new Map<string, number>();
  for (const key of placements) {
    counts.set(key, (counts.get(key) ?? 0) + 1);
  }

  for (const [key, count] of counts) {
    if (count > 1) {
      issues.push({
        rule: "layout-exactly-once",
        path: "customization.layout.sections",
        message: `section key "${key}" appears ${count} times across layout.sections.main+sidebar combined (must appear exactly once)`,
      });
    }
    if (!contentKeys.has(key)) {
      issues.push({
        rule: "layout-missing-content-key",
        path: "customization.layout.sections",
        message: `layout.sections references section key "${key}", which does not exist in content`,
      });
    }
  }

  for (const key of contentKeys) {
    if (!counts.has(key)) {
      issues.push({
        rule: "layout-orphan-content-key",
        path: "customization.layout.sections",
        message: `content section "${key}" is not placed in layout.sections.main or .sidebar`,
      });
    }
  }

  return issues;
}

/**
 * AC-DOC-002 (design spec §3): entry ids are client-generated uuids that
 * must be unique ACROSS THE WHOLE RESUME, not merely within one section.
 * resume.schema.json has no per-array `uniqueItems` on any section's
 * `entries` — even if it did, that would only dedup entries WITHIN one
 * section's own array; it could never catch the same id reused across two
 * DIFFERENT sections (e.g. one `work` entry and one `skill` entry sharing an
 * id), which is exactly the cross-structure gap this rule closes — the same
 * category of gap validateLayoutSections closes for
 * customization.layout.sections. See
 * fixtures/store/invalid-duplicate-entry-id.json.
 *
 * Reports one issue per OCCURRENCE of a duplicated id (not just the first
 * repeat), so a caller can point at every offending entry, not only the
 * second one found.
 *
 * A missing or non-string `id` (unreachable through ajv, which requires
 * `id` and constrains it to `format: "uuid"`, but this file's inputs are
 * "untyped JSON in practice" per this file's header) is treated as the
 * empty string "" — the same value Go's ID field defaults to for an absent
 * JSON key, since Go has no separate "missing" state for a required
 * `string` field. This keeps the two halves' set of REACHABLE documents
 * (a JSON body with the `id` key entirely absent) behaving identically,
 * rather than picking an arbitrary TS-only tolerance for a case Go can
 * actually receive.
 */
export function validateEntryIdUniqueness(
  content: Record<string, DocumentSection> | undefined,
): ValidationIssue[] {
  const issues: ValidationIssue[] = [];
  const pathsById = new Map<string, string[]>();

  for (const [sectionKey, section] of Object.entries(content ?? {})) {
    toArray<DocumentEntry>(section?.entries).forEach((entry, entryIndex) => {
      const id = typeof entry?.id === "string" ? entry.id : "";
      const path = `content.${sectionKey}.entries[${entryIndex}].id`;
      const paths = pathsById.get(id) ?? [];
      paths.push(path);
      pathsById.set(id, paths);
    });
  }

  for (const [id, paths] of pathsById) {
    if (paths.length <= 1) continue;
    for (const path of paths) {
      const others = paths.filter((p) => p !== path).sort();
      issues.push({
        rule: "duplicate-entry-id",
        path,
        message: `${path}: entry id "${id}" is not unique across the whole resume — also used at ${others.join(", ")}`,
      });
    }
  }

  return issues;
}

interface YearMonth {
  y: number;
  m?: number;
}

interface DateRange {
  start: YearMonth;
  end: YearMonth | null;
  present: boolean;
}

// Sortable key for a {y, m?} value. A missing month is treated as month 1
// (start of year) on both sides of the comparison — a documented
// simplification: it means start={y:2020} vs. end={y:2020} (same year, both
// months unknown) compares equal rather than ambiguous, which is the
// permissive-but-safe choice (never flags two same-year, both-month-absent
// dates as reversed).
function yearMonthOrdinal(value: YearMonth): number {
  return value.y * 12 + (value.m ?? 1);
}

/**
 * design spec §3: start <= end. Only meaningful when `end` is non-null —
 * present:true documents already have end:null enforced by
 * resume.schema.json's dateRange allOf/if-then, so there is nothing to
 * compare.
 *
 * Phase-gate review finding I1: `start` is required by resume.schema.json,
 * but this function receives whatever a caller passes — untyped JSON in
 * practice — so a hostile/malformed `dates` object carrying `end`+`present`
 * but no `start` used to null-deref `dates.start.y` inside
 * yearMonthOrdinal. Go's DateRange.Start is a value type (not a pointer), so
 * the equivalent Go input decodes to a zero YearMonth{Y:0} (ordinal 1)
 * instead of erroring — this guard mirrors that same tolerance instead of
 * throwing, rather than trying to make TS reject what Go silently accepts.
 */
export function validateDateRange(
  dates: DateRange | null | undefined,
  path: string,
): ValidationIssue[] {
  if (!dates || dates.end === null || dates.end === undefined) return [];
  if (!dates.start || typeof dates.start.y !== "number") return [];
  if (yearMonthOrdinal(dates.start) > yearMonthOrdinal(dates.end)) {
    const fmt = (ym: YearMonth) =>
      ym.m ? `${ym.y}-${String(ym.m).padStart(2, "0")}` : `${ym.y}`;
    return [
      {
        rule: "date-range-order",
        path,
        message: `${path}: start (${fmt(dates.start)}) is after end (${fmt(dates.end)})`,
      },
    ];
  }
  return [];
}

// sectionTypes whose entries carry a `dates` range (design spec §3's
// entry-fields table). certificate has a single `date` {y,m?}, not a range,
// so there is nothing to order-check there; skill/language/profile have no
// date field at all.
const DATE_RANGE_SECTION_TYPES = new Set([
  "work",
  "education",
  "project",
  "custom",
]);

export function validateDateRanges(
  content: Record<string, DocumentSection> | undefined,
): ValidationIssue[] {
  const issues: ValidationIssue[] = [];
  for (const [sectionKey, section] of Object.entries(content ?? {})) {
    if (!DATE_RANGE_SECTION_TYPES.has(section?.sectionType ?? "")) continue;
    toArray<DocumentEntry>(section.entries).forEach((entry, entryIndex) => {
      const dates = entry?.dates as DateRange | undefined;
      if (dates) {
        issues.push(
          ...validateDateRange(
            dates,
            `content.${sectionKey}.entries[${entryIndex}].dates`,
          ),
        );
      }
    });
  }
  return issues;
}

// Detail types explicitly exempted from the https-scheme requirement below
// (design spec §3: no design-spec-defined value format for these — see
// resume.schema.json's personalDetail $def description). Every OTHER
// `type` — the four URL types (website/linkedin/github/twitter) AND any
// out-of-enum/missing/wrong-case string — is treated as requiring https,
// fail-closed. Phase-gate re-review finding NEW-M1: an earlier version of
// this rule allowlisted exactly the four URL type strings
// (`DETAIL_TYPES_REQUIRING_HTTPS.has(type)`), so a one-character
// perturbation of `type` — "WEBSITE", "url", "", or a missing key — fell
// through to "not a URL type, skip" and let `javascript:` straight past
// the store layer, even though this rule's own doc comment (below) justifies
// its existence specifically for documents that reach Go's write path
// without ever running ajv (where such a perturbed `type` is exactly the
// input ajv is not there to catch). Default-deny closes that: only a type
// this file KNOWS has no URL semantics is exempt.
const DETAIL_TYPES_WITHOUT_URL_CONSTRAINT = new Set([
  "email",
  "phone",
  "location",
  "custom",
]);

interface PersonalDetail {
  type?: string;
  value?: string;
}

interface Photo {
  key?: string;
}

interface PersonalDetails {
  details?: PersonalDetail[];
  photo?: Photo;
}

/**
 * personalDetails.details[] chip URL-scheme hardening (design spec §5;
 * phase-gate review finding C1). resume.schema.json's personalDetail $def
 * now restricts `value` to an https:// URL (or "") when `type` is one of
 * website/linkedin/github/twitter — the same hardening $defs/link already
 * applies to employerLink/schoolLink/titleLink/project link. That schema
 * check is ajv-only (TypeScript); unlike resume.schema.json's types, Go has
 * no JSON-Schema pattern validator at all (gen/go/resume.go's
 * PersonalDetail.Value is a bare, unconstrained string, and its `Type` is a
 * bare `string`-backed type with no `UnmarshalJSON` enum check), so this is
 * the one rule in this file that duplicates something JSON Schema can
 * already express on its own — every other rule here exists BECAUSE JSON
 * Schema can't express it (see this file's header comment). Without a
 * store-layer copy, a document that reaches Go's write path without first
 * passing through ajv (or a future write path that decodes straight into
 * the Go Resume type) would let a hostile "javascript:"/"data:"/"vbscript:"
 * value straight into content.details — and since that same document also
 * never saw ajv's `type: {enum: [...]}` check, `type` cannot be trusted to
 * be one of the eight known values either (NEW-M1): this rule is
 * fail-closed on `type`, not an allowlist of the four URL types.
 *
 * Phase-gate re-review finding NEW-M2: this checks ONLY the scheme prefix —
 * a case-sensitive, anchored "https://" literal — NOT full URI
 * well-formedness. resume.schema.json's `then` branch is `pattern` AND
 * `format: "uri"`; ajv's `format: "uri"` additionally rejects things this
 * function accepts, e.g. an embedded newline/CR/U+2028, embedded whitespace,
 * or a non-ASCII IRI host. That gap is intentionally NOT closed here: the
 * security-relevant half — rejecting a dangerous scheme — is this function's
 * job and is portable (a plain string prefix check, no dependency on a
 * `format` implementation), where chasing exact RE2/JS URI-parser parity
 * would not meaningfully improve security and risks its own divergence bugs.
 * "" (explicitly cleared) stays accepted, draft-permissive.
 */
export function validatePersonalDetailUrlSchemes(
  personalDetails: PersonalDetails | undefined,
): ValidationIssue[] {
  const issues: ValidationIssue[] = [];
  const details = toArray<PersonalDetail>(personalDetails?.details);
  details.forEach((detail, index) => {
    if (
      !detail ||
      DETAIL_TYPES_WITHOUT_URL_CONSTRAINT.has(detail.type ?? "")
    ) {
      return;
    }
    const value = detail.value;
    if (typeof value !== "string" || value === "") return;
    if (!value.startsWith("https://")) {
      issues.push({
        rule: "personal-detail-url-scheme",
        path: `personalDetails.details[${index}].value`,
        message:
          `personalDetails.details[${index}].value: type "${detail.type}" ` +
          'requires an https:// URL (or ""), got a value that does not ' +
          'start with "https://"',
      });
    }
  });
  return issues;
}

/**
 * personalDetails.photo.key path-traversal guard (phase-gate re-review
 * finding NEW-M3). resume.schema.json's photo.key pattern is
 * `^[A-Za-z0-9][A-Za-z0-9!_.*'()/-]*$` — a lookahead-free character class
 * that compiles under both ECMA 262 (ajv) and Go's RE2 (design spec §3
 * commits the publish policy to being "generated into Go and TS like the
 * storage schema — never a hand-written validator", so a future Go-side
 * pattern check derived from this file must be able to compile every
 * pattern here). The pattern USED to also forbid a ".." substring via a
 * negative lookahead (`(?!.*\.\.)`), which RE2 cannot compile at all
 * ("invalid or unsupported Perl syntax: `(?!`") — RE2 deliberately excludes
 * backtracking constructs for its linear-time matching guarantee, and JSON
 * Schema 2020-12's own "Regular Expressions" guidance asks authors to stay
 * inside a portable subset for exactly this reason. Since neither language
 * needs a regex to check "does this string contain two consecutive dots" —
 * a plain substring check expresses it directly — the ".." rejection lives
 * here (and in gen/go/store_validate.go's ValidatePhotoKeyTraversal)
 * instead of in the schema pattern.
 */
export function validatePhotoKeyTraversal(
  personalDetails: PersonalDetails | undefined,
): ValidationIssue[] {
  const key = personalDetails?.photo?.key;
  if (typeof key !== "string" || !key.includes("..")) return [];
  return [
    {
      rule: "photo-key-path-traversal",
      path: "personalDetails.photo.key",
      message: `personalDetails.photo.key: "${key}" contains ".." — not a valid S3 object key path segment`,
    },
  ];
}

interface ResumeDocument {
  content?: Record<string, DocumentSection>;
  customization?: { layout?: { sections?: LayoutSections } };
  personalDetails?: PersonalDetails;
}

/**
 * Total order over ValidationIssue, by (path, rule, message) — plain
 * codepoint comparison (`<`/`>`), not `localeCompare`, so the ordering is
 * locale-independent and matches Go's byte-wise string comparison exactly
 * for every value this codebase actually produces (rule ids and paths are
 * fixed ASCII identifiers; message text embeds only ASCII-constrained
 * values — section keys, dates — with one exception: NEW-M1's fail-closed
 * `personal-detail-url-scheme` rule can embed an arbitrary, possibly
 * non-ASCII `type` string in its message. UTF-8 byte order equals Unicode
 * codepoint order by construction, and JS's `<`/`>` on strings compares
 * UTF-16 code units, which equals codepoint order for every character in
 * the Basic Multilingual Plane — the only case where the two engines could
 * disagree is a supplementary-plane codepoint (surrogate pair) landing
 * exactly at a tie-breaking position, an edge case not worth a dependency
 * to close).
 *
 * Phase-gate re-review finding M1 (integration-owner ruling): TS iterated
 * a `Map` in placement order while gen/go/store_validate.go iterated
 * `sortedKeys`/`sortedIntKeys`, so the two halves emitted the SAME set of
 * issues in a DIFFERENT order for the same hostile document. Rather than
 * rewrite each rule function's own internal iteration (touching layout,
 * rich-text, and date-range logic independently, with more surface for a
 * new divergence), this sorts the FINAL combined list once, at the
 * validator's return boundary — the two halves' sub-validators keep
 * whatever internal order they already had; only the caller-visible result
 * is canonicalized. gen/go/store_validate.go's ValidateDocument applies the
 * identical (path, rule, message) sort via sort.SliceStable.
 */
function compareIssues(a: ValidationIssue, b: ValidationIssue): number {
  if (a.path !== b.path) return a.path < b.path ? -1 : 1;
  if (a.rule !== b.rule) return a.rule < b.rule ? -1 : 1;
  if (a.message !== b.message) return a.message < b.message ? -1 : 1;
  return 0;
}

/**
 * Runs every store-layer aggregate rule against a full resume document and
 * returns every violation found (not just the first) — mirrors ajv's
 * allErrors: true behavior in test/schema.test.ts, so a single failing write
 * can report every offending field at once instead of one round trip per
 * error.
 *
 * Phase-gate re-review finding NEW-I1: `doc.content` on a `null`/`undefined`
 * (or any non-object) `doc` throws before any per-field guard below ever
 * runs — the top of the crash class, not just another instance of it. Accepts
 * `null`/`undefined` explicitly (unlike the rest of this file's `| undefined`
 * convention) because this is the one function a caller invokes directly
 * with a whole parsed document, which — same as every other input in this
 * file — is untyped JSON in practice, not a value TypeScript's `ResumeDocument`
 * type can actually guarantee at runtime.
 */
export function validateDocument(
  doc: ResumeDocument | null | undefined,
): ValidationIssue[] {
  if (!doc || typeof doc !== "object") return [];
  const issues = [
    ...validateRichTextLengths(doc.content),
    ...validateLayoutSections(doc.content, doc.customization?.layout?.sections),
    ...validateDateRanges(doc.content),
    ...validateEntryIdUniqueness(doc.content),
    ...validatePersonalDetailUrlSchemes(doc.personalDetails),
    ...validatePhotoKeyTraversal(doc.personalDetails),
  ];
  return issues.sort(compareIssues);
}
