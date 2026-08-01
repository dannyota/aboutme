// Code generated from resume.schema.json. DO NOT EDIT.

/**
 * Doc-shape version. This schema file validates schemaVersion 1 only; a future shape bump ships as resume/v2.
 */
export type SchemaVersion = 1;
export type Uuid = string;
/**
 * content[key]: oneOf dispatch on sectionType selecting the matching entry $def, so a section's entries array cannot mix entry shapes. Draft-permissive (design spec §3, revised 2026-08-01): only sectionType and entries are required — displayName and iconKey (section metadata) are optional and, when present, displayName may also be "" (cleared while retyping a section title), so a freshly created section persists before its title/icon are chosen. See fixtures/draft-cleared-name-empty-section.json.
 */
export type Section =
  | {
      sectionType: "profile";
      displayName?: DisplayName;
      iconKey?: IconKey;
      /**
       * @maxItems 64
       */
      entries: ProfileEntry[];
    }
  | {
      sectionType: "work";
      displayName?: DisplayName;
      iconKey?: IconKey;
      /**
       * @maxItems 64
       */
      entries: WorkEntry[];
    }
  | {
      sectionType: "education";
      displayName?: DisplayName;
      iconKey?: IconKey;
      /**
       * @maxItems 64
       */
      entries: EducationEntry[];
    }
  | {
      sectionType: "skill";
      displayName?: DisplayName;
      iconKey?: IconKey;
      /**
       * @maxItems 64
       */
      entries: SkillEntry[];
    }
  | {
      sectionType: "language";
      displayName?: DisplayName;
      iconKey?: IconKey;
      /**
       * @maxItems 64
       */
      entries: LanguageEntry[];
    }
  | {
      sectionType: "certificate";
      displayName?: DisplayName;
      iconKey?: IconKey;
      /**
       * @maxItems 64
       */
      entries: CertificateEntry[];
    }
  | {
      sectionType: "project";
      displayName?: DisplayName;
      iconKey?: IconKey;
      /**
       * @maxItems 64
       */
      entries: ProjectEntry[];
    }
  | {
      sectionType: "custom";
      displayName?: DisplayName;
      iconKey?: IconKey;
      /**
       * @maxItems 64
       */
      entries: CustomEntry[];
    };
/**
 * Section title. Draft-permissive (design spec §3, revised 2026-08-01): may be "" while the user retypes it — see the section $def.
 */
export type DisplayName = string;
/**
 * Lucide icon name, kebab-case (see §5 renderer: tree-shaken inline SVG via iconKey).
 */
export type IconKey = string;
/**
 * design spec §3 entry-fields table: profile. Draft-permissive — see entryBase.
 */
export type ProfileEntry = EntryBase & {
  text?: RichText;
};
/**
 * Sanitized HTML subset (see sanitizerAllowlistVersion). Byte bound mirrors spec §3's ≤16 KB rich text per entry — but JSON Schema's maxLength counts Unicode CODE POINTS, not bytes, so this is only a sound (never-false-reject) upper bound: any string that is truly ≤16384 UTF-8 bytes also has ≤16384 code points (each code point is ≥1 byte), so maxLength never rejects a genuinely valid document. It is not sufficient on its own — a 9,000-character string of 2-byte code points (e.g. 'é') is 9,000 code points (passes maxLength) but 18,000 UTF-8 bytes (over the real limit). The exact byte-accurate check is a store-layer aggregate validation (packages/schema/validation/store.ts validateRichTextByteLength / gen/go/store_validate.go ValidateRichTextByteLength), run on every write, with fixtures/store/invalid-oversize-richtext-bytes.json proving the rejection.
 */
export type RichText = string;
/**
 * design spec §3 entry-fields table: work. Draft-permissive — see entryBase.
 */
export type WorkEntry = EntryBase & {
  jobTitle?: string;
  employer?: string;
  employerLink?: Link;
  city?: string;
  country?: string;
  dates?: DateRange;
  description?: RichText;
};
/**
 * An optional, emptyable URL field (employerLink, schoolLink, titleLink, project link, …). Draft-permissive (design spec §3): the key may be absent ("never entered"), "" ("explicitly cleared"), or a valid URI — all three are distinguishable and preserved through round-trips (never collapsed to a sentinel). Scheme-restricted to exactly the three schemes in the sanitizer allowlist's urlSchemes (packages/schema/validation/sanitizer-allowlist.v1.json — https, mailto, tel; see sanitizerAllowlistVersion): the pattern requires an exact lowercase "https://", "mailto:", or "tel:" prefix, which as a side effect also rejects the obfuscations a hostile value would use to smuggle a dangerous scheme past a looser check — leading/embedded whitespace, mixed case ("JavaScript:"), and protocol-relative ("//evil.example") — none of which can match an anchored lowercase-literal prefix.
 */
export type Link = string;
/**
 * design spec §3 entry-fields table: education. Draft-permissive — see entryBase.
 */
export type EducationEntry = EntryBase & {
  degree?: string;
  school?: string;
  schoolLink?: Link;
  city?: string;
  country?: string;
  dates?: DateRange;
  description?: RichText;
};
/**
 * design spec §3 entry-fields table: skill. Draft-permissive — see entryBase (level was already the one optional field even before that rule; now every field here is optional).
 */
export type SkillEntry = EntryBase & {
  name?: string;
  level?: number;
  infoHtml?: RichText;
};
/**
 * design spec §3 entry-fields table: language. Draft-permissive — see entryBase. Note: languageEntry's field set ({name, level}) is a structural subset of skillEntry's ({name, level, infoHtml}) — see gen/go/section.go for why entries are not self-describing and the section's sectionType discriminator, not the entry's shape, is what defines its type.
 */
export type LanguageEntry = EntryBase & {
  name?: string;
  level?: number;
};
/**
 * design spec §3 entry-fields table: certificate. date is a single {y,m?}, not a range. Draft-permissive — see entryBase.
 */
export type CertificateEntry = EntryBase & {
  title?: string;
  titleLink?: Link;
  issuer?: string;
  date?: YearMonth;
  description?: RichText;
};
/**
 * design spec §3 entry-fields table: project. Draft-permissive — see entryBase.
 */
export type ProjectEntry = EntryBase & {
  title?: string;
  link?: Link;
  dates?: DateRange;
  description?: RichText;
};
/**
 * design spec §3 entry-fields table: custom. Draft-permissive — see entryBase.
 */
export type CustomEntry = EntryBase & {
  title?: string;
  titleLink?: Link;
  subtitle?: string;
  city?: string;
  dates?: DateRange;
  description?: RichText;
};
export type HexColor = string;
/**
 * content object property name: a built-in lowercase-letters key (e.g. "work") or a generated UUID key for a custom section. Shared by content.propertyNames and customization.layout.sections entries so both stay in lockstep. maxLength: 36 (phase-gate review finding M2): both pattern branches already imply <=36 chars (the UUID branch is exactly 36), so this only closes the hole where an unbounded ^[a-z]+$ built-in-style key could otherwise run arbitrarily long — a 50,000-char key previously validated, flowing verbatim into jsonb keys, store-layer issue messages, and logs.
 */
export type SectionKey = string;

/**
 * The resume document jsonb shape: personalDetails, content, customization plus schemaVersion. Single source of truth for generated Go/TS/Dart types and store-layer validation.
 */
export interface Resume {
  schemaVersion: SchemaVersion;
  personalDetails: PersonalDetails;
  content: Content;
  customization: Customization;
}
/**
 * Draft-permissive (design spec §3, revised 2026-08-01): fullName and details are both optional and may be empty/absent while editing — a cleared name or a not-yet-added details array must never block autosave. See fixtures/draft-cleared-name-empty-section.json.
 */
export interface PersonalDetails {
  fullName?: string;
  headline?: string;
  photo?: Photo;
  /**
   * @maxItems 16
   */
  details?: PersonalDetail[];
}
/**
 * Resume photos live per-doc (design spec §3: avatar_key is account-only; distinct from this). key is an S3 object key, not a URL.
 */
export interface Photo {
  /**
   * S3 object key (phase-gate review finding M6): restricted to AWS's documented 'safe' key-character set (alnum, !-_.*'()) plus "/" for the pseudo-directory delimiter our own upload path uses (see fixtures' "resumes/<user>/photo-original.jpg" keys). The FIRST character must be alphanumeric — this excludes a leading "."/"_"/"!" etc, e.g. ".hidden.jpg" or "_x.jpg" (phase-gate re-review finding NEW-M5, verified harmless against both committed fixture keys). This is a storage-key SAFETY bound (blocks an absolute URL like "https://evil.example.com/x.jpg", since ":" is not in the allowed set), not a key-construction naming CONVENTION — the design spec does not define one, so none is invented here (CLAUDE.md: 'do not invent a contract'). Path traversal (a ".." substring, e.g. "../../other-user/secret.jpg") is deliberately NOT rejected by this pattern — see phase-gate re-review finding NEW-M3: the natural regex form (a negative lookahead) is outside JSON Schema's portable regex subset and does not compile under Go's RE2 engine, which design spec §3 commits any future generated Go pattern-validator to using. validation/store.ts's validatePhotoKeyTraversal / gen/go/store_validate.go's ValidatePhotoKeyTraversal enforce the ".." rejection instead, as a plain substring check neither language needs a regex for.
   */
  key: string;
  crop?: PhotoCrop;
}
export interface PhotoCrop {
  x: number;
  y: number;
  width: number;
  height: number;
}
/**
 * One contact chip. Display order is the array order of personalDetails.details (no separate detailsOrder field — order lives where it's used, mirroring how customization.layout.sections orders content sections).
 */
export interface PersonalDetail {
  id: Uuid;
  type: "email" | "phone" | "location" | "website" | "linkedin" | "github" | "twitter" | "custom";
  label?: string;
  /**
   * Draft-permissive (design spec §3, revised 2026-08-01): may be explicitly cleared ("") while the user retypes it, same rule as every other free-text field. For type in {website, linkedin, github, twitter} the allOf below additionally restricts this to an https:// URL (or "") — see its description for why (phase-gate review finding C1).
   */
  value: string;
  isHidden: boolean;
}
/**
 * Ordered map sectionKey → section (design spec §3). ≤24 sections per size bounds; entry ids are unique across the whole resume, enforced at the store layer (AC-DOC-002, see fixtures/store/).
 */
export interface Content {
  [k: string]: Section;
}
/**
 * Draft-permissive (design spec §3, revised 2026-08-01): id is the only field required on every entry, so a half-typed entry from an autosaving editor persists and reloads exactly as typed. Never fabricate a sentinel value for an absent field — absence ("never entered") and "" ("explicitly cleared") are distinct and both round-trip unchanged. Publish-time completeness (e.g. work needs jobTitle+employer) is a separate, later validation layer, not enforced here.
 */
export interface EntryBase {
  id: Uuid;
  isHidden?: boolean;
}
/**
 * present ⇒ end===null and ¬present ⇒ end≠null are both enforced here (design spec §3). start≤end is a cross-field numeric comparison JSON Schema cannot express cleanly and is left to the store layer (AC-DOC-003), matching how duplicate-entry-id (AC-DOC-002) is deferred to fixtures/store/.
 */
export interface DateRange {
  start: YearMonth;
  end: YearMonth | null;
  present: boolean;
}
export interface YearMonth {
  y: number;
  m?: number;
}
/**
 * Mirrors design spec §3: font, colors, spacing, heading, layout (with sections order arrays), per-type display configs, pageFormat, date formats.
 */
export interface Customization {
  font: {
    family: "Be Vietnam Pro" | "Inter" | "Source Sans 3" | "Alegreya" | "Roboto Serif";
    baseSizePx: number;
  };
  colors: {
    primary: HexColor;
    text: HexColor;
    background: HexColor;
    accent?: HexColor;
  };
  spacing: {
    sectionGap: number;
    entryGap: number;
    lineHeight: number;
  };
  heading: {
    style: "uppercase" | "titlecase" | "normal";
    showRule: boolean;
  };
  layout: {
    columns: 1 | 2;
    /**
     * Placement of content's section keys into the two layout columns. JSON Schema bounds each array's size and forbids a repeat WITHIN one array (maxItems/uniqueItems below); it cannot express the cross-field aggregate invariant (design spec §3): every content key must appear exactly once across main+sidebar COMBINED — no duplicate across the two arrays, no key absent from content, no content key placed nowhere. That combined rule is store-layer aggregate validation (packages/schema/validation/store.ts validateLayoutSections / gen/go/store_validate.go ValidateLayoutSections), run on every write; see fixtures/store/invalid-layout-duplicate-across-arrays.json, fixtures/store/invalid-layout-missing-content-key.json, fixtures/store/invalid-layout-orphan-content-key.json.
     */
    sections: {
      /**
       * @maxItems 24
       */
      main: SectionKey[];
      /**
       * @maxItems 24
       */
      sidebar: SectionKey[];
    };
  };
  /**
   * Per-type display configs. Scoped to skill/language, the two builtin types where a proficiency-display style is meaningful.
   */
  sectionDisplay: {
    skill: {
      style: "text" | "tag" | "bar" | "dots";
    };
    language: {
      style: "text" | "tag" | "bar" | "dots";
    };
  };
  pageFormat: "a4" | "letter";
  dateFormat: "MM/YYYY" | "Mon YYYY" | "YYYY";
}
