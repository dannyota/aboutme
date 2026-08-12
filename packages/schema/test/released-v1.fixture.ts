// Compile-time fixture for the retained v1 types and released-schema registry.
// released-versions.test.ts checks it with `tsc --noEmit --strict`; it is never
// executed. See docs/design/data.md#document-versions.
import type { Resume, Section, WorkEntry } from "../gen/ts/v1/resume";
import { CURRENT_VERSION, releasedSchema } from "../gen/ts/released";

// V1 root document shape.

function v1SectionKeys(doc: Resume): string[] {
  const version: 1 = doc.schemaVersion;
  void version;
  return Object.keys(doc.content);
}

// V1 section union narrowing.

function v1SectionSummary(section: Section): string {
  switch (section.sectionType) {
    case "work": {
      const entry: WorkEntry = section.entries[0];
      const employer: string | undefined = entry.employer;
      // @ts-expect-error: degree belongs to educationEntry, not workEntry.
      const degree = entry.degree;
      return String(employer) + String(degree);
    }
    case "education":
      return String(section.entries[0].degree);
    default:
      return section.sectionType;
  }
}

// Typed registry lookup fails by throwing.

const currentReleased: string = releasedSchema(CURRENT_VERSION).schema;

// @ts-expect-error: a released schema entry is readonly; callers cannot retarget it.
releasedSchema(CURRENT_VERSION).schema = "resume.v2.schema.json";

export { currentReleased, v1SectionKeys, v1SectionSummary };
