// Compile-time fixture for the generated discriminated union and
// draft-permissive entry fields. Only `id` is required; domain fields are
// optional. type-fidelity.test.ts checks this file with `tsc --strict`.
import type { EducationEntry, Section, WorkEntry } from "../gen/ts/resume";

// sectionType narrows entries to its matching entry type.

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
      // SkillEntry and LanguageEntry both use an optional numeric level.
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

// Draft-permissive entries require only id.

// A partially typed work entry must compile.
const draftPartial: WorkEntry = {
  id: "e1",
  jobTitle: "Engineer",
};

// Even more minimal: just id. Must also compile — there is no second
// required field.
const bareEntry: WorkEntry = {
  id: "e1",
};

// Drafts may omit employer.
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

// Section entries must match sectionType.

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

// Keep compile-only declarations referenced under noUnusedLocals.
void describeSection;
void draftPartial;
void bareEntry;
void noEmployer;
void missingId;
void fullWork;
void workSection;
void mismatchedSection;
