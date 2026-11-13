export const MAX_RICH_TEXT_BYTES = 16384;

export interface ValidationIssue {
  rule: string;
  path: string;
  message: string;
}

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

const MAX_OTHER_PATHS_IN_MESSAGE = 3;

function formatOtherPaths(sortedPaths: string[], path: string): string {
  const shown: string[] = [];
  for (const p of sortedPaths) {
    if (p === path) continue;
    if (shown.length === MAX_OTHER_PATHS_IN_MESSAGE) break;
    shown.push(p);
  }
  const total = sortedPaths.length - 1;
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

  for (const id of [...pathsById.keys()].sort()) {
    const paths = pathsById.get(id)!;
    if (paths.length <= 1) continue;
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

export function validateDetailIdUniqueness(
  personalDetails: PersonalDetails | undefined,
): ValidationIssue[] {
  const issues: ValidationIssue[] = [];
  const pathsById = new Map<string, string[]>();
  toArray<PersonalDetail>(personalDetails?.details).forEach((detail, index) => {
    const id = typeof detail?.id === "string" ? detail.id : "";
    const paths = pathsById.get(id) ?? [];
    paths.push(`personalDetails.details[${index}].id`);
    pathsById.set(id, paths);
  });
  for (const id of [...pathsById.keys()].sort()) {
    const paths = pathsById.get(id)!;
    if (paths.length <= 1) continue;
    const sortedPaths = [...paths].sort();
    for (const path of sortedPaths) {
      issues.push({
        rule: "duplicate-detail-id",
        path,
        message: `${path}: detail id "${id}" is not unique — also used at ${formatOtherPaths(sortedPaths, path)}`,
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

function yearMonthOrdinal(value: YearMonth): number {
  return value.y * 12 + (value.m ?? 1);
}

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

const DETAIL_TYPES_WITHOUT_URL_CONSTRAINT = new Set([
  "email",
  "phone",
  "location",
  "custom",
]);

interface PersonalDetail {
  id?: string;
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

// Go's RE2 has no lookahead, so the schema pattern cannot reject "..".
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

function compareIssues(a: ValidationIssue, b: ValidationIssue): number {
  if (a.path !== b.path) return a.path < b.path ? -1 : 1;
  if (a.rule !== b.rule) return a.rule < b.rule ? -1 : 1;
  if (a.message !== b.message) return a.message < b.message ? -1 : 1;
  return 0;
}

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
    ...validateDetailIdUniqueness(doc.personalDetails),
    ...validatePhotoKeyTraversal(doc.personalDetails),
  ];
  return issues.sort(compareIssues);
}
