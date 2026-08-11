// Compile-time-only fixture: proves the RETAINED v1 type snapshot
// (gen/ts/v1/resume.ts) and the generated released-schema registry
// (gen/ts/released.ts) still compile on their own, independently of the
// current convenience output in gen/ts/resume.ts. Nothing in the running app
// imports the v1 snapshot yet — it exists so a future v2 has a real v1 type
// to convert from (design spec §3, "Wire-version compatibility") — so this
// file is the only thing that would notice it going stale.
//
// This file is never executed. released-versions.test.ts shells out to `tsc
// --noEmit --strict` on it, and a `@ts-expect-error` that does NOT actually
// error is itself a compile error (TypeScript's own semantics), so every
// assertion below is enforced rather than commentary.
import type { Resume, Section, WorkEntry } from "../gen/ts/v1/resume";
import { CURRENT_VERSION, releasedSchema } from "../gen/ts/released";

// --- the v1 root document shape ---

function v1SectionKeys(doc: Resume): string[] {
  const version: 1 = doc.schemaVersion;
  void version;
  return Object.keys(doc.content);
}

// --- v1's section union still narrows on sectionType ---

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

// --- the registry is typed, and its lookup is total-by-throwing ---

const currentReleased: string = releasedSchema(CURRENT_VERSION).schema;

// @ts-expect-error: a released schema entry is readonly; callers cannot retarget it.
releasedSchema(CURRENT_VERSION).schema = "resume.v2.schema.json";

export { currentReleased, v1SectionKeys, v1SectionSummary };
