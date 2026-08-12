// Code generated from resume.schema.json. DO NOT EDIT.

/**
 * Document-shape version. This schema validates version 2 only; see docs/design/data.md#document-versions.
 */
export type SchemaVersion = 2;
export type Uuid = string;
/**
 * Draft section selected by sectionType. Only sectionType and entries are required. See docs/design/data.md#resume-aggregate.
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
 * Optional section title; an empty string is an explicit draft value. See docs/design/data.md#draft-and-publish-validation.
 */
export type DisplayName = string;
/**
 * Kebab-case Lucide icon name. See docs/design/templates/contract.md.
 */
export type IconKey = string;
/**
 * Draft-permissive profile entry. See docs/design/data.md#resume-aggregate.
 */
export type ProfileEntry = EntryBase & {
  text?: RichText;
};
/**
 * Sanitized HTML. maxLength bounds code points; the aggregate validator enforces the 16 KiB UTF-8 limit. See docs/design/data.md#bounds-and-invariants.
 */
export type RichText = string;
/**
 * Draft-permissive work entry. See docs/design/data.md#resume-aggregate.
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
 * Optional draft link: absent, empty, or an exact lowercase https, mailto, or tel URI. See docs/design/data.md#draft-and-publish-validation.
 */
export type Link = string;
/**
 * Draft-permissive education entry. See docs/design/data.md#resume-aggregate.
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
 * Draft-permissive skill entry with an optional proficiency level from 0 to 5. See docs/design/data.md#resume-aggregate.
 */
export type SkillEntry = EntryBase & {
  name?: string;
  level?: number;
  infoHtml?: RichText;
};
/**
 * Draft-permissive language entry selected by its parent sectionType. See docs/design/data.md#resume-aggregate.
 */
export type LanguageEntry = EntryBase & {
  name?: string;
  level?: number;
};
/**
 * Draft-permissive certificate entry with an optional single year-month date. See docs/design/data.md#resume-aggregate.
 */
export type CertificateEntry = EntryBase & {
  title?: string;
  titleLink?: Link;
  issuer?: string;
  date?: YearMonth;
  description?: RichText;
};
/**
 * Draft-permissive project entry. See docs/design/data.md#resume-aggregate.
 */
export type ProjectEntry = EntryBase & {
  title?: string;
  link?: Link;
  dates?: DateRange;
  description?: RichText;
};
/**
 * Draft-permissive custom entry. See docs/design/data.md#resume-aggregate.
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
 * Built-in lowercase section key or UUID custom-section key, bounded to 36 characters. See docs/design/data.md#resume-aggregate.
 */
export type SectionKey = string;

/**
 * Current resume document shape. See docs/design/data.md for the aggregate and versioning contract.
 */
export interface Resume {
  schemaVersion: SchemaVersion;
  personalDetails: PersonalDetails;
  content: Content;
  customization: Customization;
}
/**
 * Draft personal details; fullName and details may be absent or empty. See docs/design/data.md#draft-and-publish-validation.
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
 * Resume-specific private photo object key and optional crop; the key is not a URL. See docs/design/security.md#untrusted-media.
 */
export interface Photo {
  /**
   * Server-owned private-media key with a bounded safe character set. Aggregate validation rejects traversal; see docs/design/security.md#untrusted-media.
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
 * One contact item; array order is display order. See docs/design/data.md#resume-aggregate.
 */
export interface PersonalDetail {
  id: Uuid;
  type: "email" | "phone" | "location" | "website" | "linkedin" | "github" | "twitter" | "custom";
  label?: string;
  /**
   * Draft contact value. URL contact types accept only an exact lowercase https URI or an empty string. See docs/adr/0013-contact-detail-rendering.md.
   */
  value: string;
  isHidden: boolean;
}
/**
 * Map of at most 24 section keys to sections. The store enforces document-wide entry ID uniqueness. See docs/design/data.md#resume-aggregate.
 */
export interface Content {
  [k: string]: Section;
}
/**
 * Draft entry base. Only id is required; absent and explicitly cleared fields remain distinct. See docs/design/data.md#draft-and-publish-validation.
 */
export interface EntryBase {
  id: Uuid;
  isHidden?: boolean;
}
/**
 * Date range with a required start. Present ranges have a null end; completed ranges have an end. The store checks ordering. See docs/design/data.md#bounds-and-invariants.
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
 * Resume presentation settings. See docs/design/templates/README.md.
 */
export interface Customization {
  font: {
    /**
     * Stable version-2 font catalog ID in manifest rank order. See docs/design/fonts.md#version-2-catalog.
     */
    family:
      | "be-vietnam-pro"
      | "inter"
      | "noto-sans"
      | "noto-serif"
      | "roboto"
      | "open-sans"
      | "plus-jakarta-sans"
      | "work-sans"
      | "nunito-sans"
      | "montserrat"
      | "fira-sans"
      | "barlow"
      | "alegreya"
      | "spectral"
      | "literata"
      | "newsreader"
      | "space-mono"
      | "crimson-pro"
      | "eb-garamond"
      | "aleo"
      | "cormorant-garamond"
      | "roboto-serif"
      | "roboto-mono"
      | "dm-sans"
      | "atkinson-hyperlegible-next"
      | "source-sans-3";
    baseSizePx: number;
  };
  colors: {
    primary: HexColor;
    text: HexColor;
    background: HexColor;
    accent?: HexColor;
    surface?: HexColor;
  };
  spacing: {
    sectionGap: number;
    entryGap: number;
    lineHeight: number;
    /**
     * Optional horizontal and vertical page margins in millimetres, each from 0 to 40. Absence renders as 15 mm. See docs/design/templates/print.md.
     */
    pageMargin?: {
      x: number;
      y: number;
    };
  };
  /**
   * Styles section headings. This is distinct from customization.header, which contains the resume top block and fullName. See docs/design/templates/contract.md.
   */
  heading: {
    style: "uppercase" | "titlecase" | "normal";
    showRule: boolean;
  };
  /**
   * Optional presentation of the top resume header containing the photo, fullName, headline, and contacts. It is distinct from customization.heading, which styles section headings. See docs/design/templates/contract.md.
   */
  header?: {
    /**
     * Horizontal alignment for the complete top block. See docs/design/templates/contract.md.
     */
    align: "left" | "center";
    /**
     * Displays contact details inline or stacked while preserving array order. See docs/design/templates/contract.md.
     */
    detailsLayout: "inline" | "stacked";
    /**
     * Contact-detail icon style for the top header: none or the Lucide outline glyph. See docs/design/templates/tokens.md.
     */
    iconStyle: "none" | "outline";
  };
  layout: {
    columns: 1 | 2;
    /**
     * Region filled by colors.surface. With layout.columns set to 1, sidebar renders as none; that degradation is not an error and does not rewrite the draft. See docs/design/templates/contract.md.
     */
    surfaceTarget?: "none" | "header" | "sidebar";
    /**
     * Orders section keys in main and sidebar columns. The store requires every content key exactly once across both arrays. See docs/design/data.md#resume-aggregate.
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
   * Display style for skill and language proficiency. See docs/design/templates/contract.md.
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
