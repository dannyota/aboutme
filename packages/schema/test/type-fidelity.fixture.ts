// Compile-time-only fixture: exercises gen/ts/resume.ts's discriminated
// union and each entry type's field shape under the draft-permissive
// contract (design spec §3, revised 2026-08-01): only `id` is required on
// every entry, every domain field is optional. This file is never run —
// type-fidelity.test.ts shells out to `tsc --noEmit --strict` on it. A
// `@ts-expect-error` line that does NOT actually error is itself flagged as
// a compile error (TypeScript's own semantics for the directive), so every
// assertion below is a real, enforced check, not an unverified comment.
import type { EducationEntry, Section, WorkEntry } from "../gen/ts/resume";

// --- switch (section.sectionType) narrows `entries` to the matching entry type ---
//
// This holds regardless of which fields are required: narrowing is about
// property *existence* per variant, not about which of those properties
// happen to be optional.

function describeSection(section: Section): string {
  switch (section.sectionType) {
    case "work": {
      const entry = section.entries[0];
      const jobTitle: string | undefined = entry.jobTitle;
      const employer: string | undefined = entry.employer;
      // @ts-expect-error: degree belongs to EducationEntry, not WorkEntry.
      const degree = entry.degree;
      return String(jobTitle) + String(employer) + String(degree);
    }
    case "education": {
      const entry = section.entries[0];
      const degree: string | undefined = entry.degree;
      // @ts-expect-error: jobTitle belongs to WorkEntry, not EducationEntry.
      const jobTitle = entry.jobTitle;
      return String(degree) + String(jobTitle);
    }
    case "skill": {
      const entry = section.entries[0];
      // level is optional on both skill and language now (every domain
      // field is), so this is no longer a distinguishing feature between
      // the two — see the LanguageEntry case below for the same type.
      const level: number | undefined = entry.level;
      return String(level);
    }
    case "language": {
      const entry = section.entries[0];
      const level: number | undefined = entry.level;
      return String(level);
    }
    case "certificate": {
      const entry = section.entries[0];
      const date: { y: number; m?: number } | undefined = entry.date;
      // @ts-expect-error: certificate has a single `date`, not a `dates` range like work/education/project/custom.
      const dates = entry.dates;
      return String(date) + String(dates);
    }
    case "project": {
      const entry = section.entries[0];
      const link: string | undefined = entry.link;
      return String(link);
    }
    case "custom": {
      const entry = section.entries[0];
      const subtitle: string | undefined = entry.subtitle;
      return String(subtitle);
    }
    case "profile": {
      const entry = section.entries[0];
      const text: string | undefined = entry.text;
      return String(text);
    }
  }
}

// --- draft-permissive: only `id` is required, every domain field (and
// isHidden) is optional ---

// The exact example from design spec §3 and fixtures/draft-partial.json: a
// work entry with only id and jobTitle typed so far. Must compile.
const draftPartial: WorkEntry = {
  id: "e1",
  jobTitle: "Engineer",
};

// Even more minimal: just id. Must also compile — there is no second
// required field.
const bareEntry: WorkEntry = {
  id: "e1",
};

// employer is no longer required (draft-permissive) — omitting it, unlike
// before this contract change, must NOT be an error.
const noEmployer: WorkEntry = {
  id: "e1",
  jobTitle: "Engineer",
  city: "Hanoi",
};

// id remains the one field required on every entry type.
// @ts-expect-error: id is required on WorkEntry even under the draft-permissive contract.
const missingId: WorkEntry = {
  jobTitle: "Engineer",
};

// A fully-specified entry must still compile (optional doesn't mean
// disallowed).
const fullWork: WorkEntry = {
  id: "e1",
  isHidden: false,
  jobTitle: "Engineer",
  employer: "Acme",
  employerLink: "",
  city: "Hanoi",
  country: "Vietnam",
  dates: { start: { y: 2020 }, end: null, present: true },
  description: "",
};

// --- Section itself: an entries array must match its own sectionType's entry type ---

const workSection: Section = {
  sectionType: "work",
  displayName: "Experience",
  iconKey: "briefcase",
  entries: [draftPartial],
};

// An EducationEntry-shaped element doesn't satisfy sectionType: "work"'s
// `entries: WorkEntry[]` — TypeScript reports the mismatch on the
// EducationEntry-only field (degree), not on the outer declaration. This
// is unrelated to optionality: "degree" doesn't exist on WorkEntry at all,
// required or not.
const mismatchedSection: Section = {
  sectionType: "work",
  displayName: "Experience",
  iconKey: "briefcase",
  entries: [
    {
      id: "e1",
      // @ts-expect-error: degree does not exist on WorkEntry, only on EducationEntry.
      degree: "BSc",
    } satisfies EducationEntry,
  ],
};

// Referenced so `tsc --noUnusedLocals`-style drift (should it ever be
// enabled) doesn't need updating this fixture; harmless no-op otherwise.
void describeSection;
void draftPartial;
void bareEntry;
void noEmployer;
void missingId;
void fullWork;
void workSection;
void mismatchedSection;
