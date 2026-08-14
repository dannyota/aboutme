// Aggregate validation that JSON Schema cannot express or the Go write boundary
// does not execute: UTF-8 byte limits, cross-field placement and date rules,
// resume-wide ID uniqueness, contact URL schemes, and photo-key traversal.
// TypeScript and Go share fixtures so verdicts and issue order stay aligned.
// See docs/design/data.md#bounds-and-invariants and
// docs/design/security.md#untrusted-document-content.

/** Rich-text limit in UTF-8 bytes; schema maxLength counts code points. */
export const MAX_RICH_TEXT_BYTES = 16384;

export interface ValidationIssue {
  /** Stable machine-readable rule id, e.g. "rich-text-byte-length". */
  rule: string;
  /** Dotted/bracketed path into the document, e.g. "content.work.entries[0].description". */
  path: string;
  /** Human-readable explanation, safe to surface in a 422 response. */
  message: string;
}

/** UTF-8 byte length across Node and browsers, matching Go string bytes. */
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

// Rich-text fields by section discriminator. Own-property lookup is required:
// hostile values such as "constructor" and "__proto__" must not resolve
// through Object.prototype.
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
  return RICH_TEXT_FIELDS_BY_SECTION_TYPE[sectionType]!;
}

/**
 * Treats malformed non-arrays as empty so aggregate validation never throws.
 * Go's typed decoder rejects these shapes before this boundary.
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
 * Enforces that every content key appears exactly once across both layout
 * arrays. Duplicate placements, missing references, and orphan content use
 * distinct rule IDs. See docs/adr/0009-section-order-authority.md.
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
 * Enforces entry-ID uniqueness across the whole resume and reports every
 * occurrence. Missing or non-string IDs normalize to the empty Go string value
 * so malformed input has the same result in both implementations.
 */
// A valid document may contain 1,536 entries. Bound peer paths in each issue so
// one repeated ID cannot produce quadratic response text.
const MAX_OTHER_PATHS_IN_MESSAGE = 3;

// Formats a bounded, sorted list of the duplicate occurrence's peer paths.
function formatOtherPaths(sortedPaths: string[], path: string): string {
  const shown: string[] = [];
  for (const p of sortedPaths) {
    if (p === path) continue;
    if (shown.length === MAX_OTHER_PATHS_IN_MESSAGE) break;
    shown.push(p);
  }
  const total = sortedPaths.length - 1; // every path is unique, so exactly one entry equals `path`
  const remaining = total - shown.length;
  const suffix = remaining > 0 ? `, and ${remaining} more` : "";
  return shown.join(", ") + suffix;
}

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

  // Sort IDs and paths so emitted messages match Go regardless of map order.
  for (const id of [...pathsById.keys()].sort()) {
    const paths = pathsById.get(id)!;
    if (paths.length <= 1) continue;
    // Sort once per duplicated ID before emitting one issue per occurrence.
    const sortedPaths = [...paths].sort();
    for (const path of sortedPaths) {
      issues.push({
        rule: "duplicate-entry-id",
        path,
        message: `${path}: entry id "${id}" is not unique across the whole resume — also used at ${formatOtherPaths(sortedPaths, path)}`,
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

// Missing months compare as January, so two year-only dates compare equal.
function yearMonthOrdinal(value: YearMonth): number {
  return value.y * 12 + (value.m ?? 1);
}

/**
 * Enforces start<=end when end exists. A malformed missing start yields no
 * issue instead of throwing, matching Go's zero-value tolerance.
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

// Section types with date ranges. Certificates use one date, not a range.
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

// Only known plain-text contact types are exempt. URL types and malformed
// discriminators default to the https requirement. See
// docs/adr/0013-contact-detail-rendering.md.
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
 * Requires exact lowercase https:// for linkable or malformed contact types;
 * an empty value remains valid. This store check protects the Go boundary,
 * which does not execute JSON Schema. AJV separately checks full URI syntax.
 * See docs/adr/0013-contact-detail-rendering.md.
 */
export function validatePersonalDetailUrlSchemes(
  personalDetails: PersonalDetails | undefined,
): ValidationIssue[] {
  const issues: ValidationIssue[] = [];
  const details = toArray<PersonalDetail>(personalDetails?.details);
  details.forEach((detail, index) => {
    if (!detail || DETAIL_TYPES_WITHOUT_URL_CONSTRAINT.has(detail.type ?? "")) {
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
 * Rejects ".." in photo object keys. This stays outside the schema pattern
 * because negative lookahead is not portable to Go RE2; a substring check has
 * identical TypeScript and Go behavior. See docs/design/security.md#untrusted-media.
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
 * Locale-independent final order by path, rule, then message. Sorting once at
 * this boundary keeps caller-visible output aligned with Go regardless of each
 * rule's internal iteration order.
 */
function compareIssues(a: ValidationIssue, b: ValidationIssue): number {
  if (a.path !== b.path) return a.path < b.path ? -1 : 1;
  if (a.rule !== b.rule) return a.rule < b.rule ? -1 : 1;
  if (a.message !== b.message) return a.message < b.message ? -1 : 1;
  return 0;
}

/**
 * Runs every aggregate rule and returns all issues in canonical order. Null,
 * undefined, and other malformed top-level values yield no issues rather than
 * throwing at this untyped boundary.
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
