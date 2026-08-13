// Code generated from templates/*.json and resume.schema.json. DO NOT EDIT.

import type { Customization } from "./resume";

export type SectionType =
  | "profile"
  | "work"
  | "education"
  | "skill"
  | "language"
  | "certificate"
  | "project"
  | "custom";

export interface TemplatePreset {
  readonly id: string;
  readonly name: string;
  readonly description: string;
  readonly customization: Omit<Customization, "layout"> & {
    readonly layout: {
      readonly columns: 1 | 2;
      readonly placement: "keep" | "byType";
      readonly sidebarSectionTypes?: readonly SectionType[];
      readonly surfaceTarget?: "none" | "header" | "sidebar";
    };
  };
}

function deepFreeze<T>(value: T): Readonly<T> {
  if (value !== null && typeof value === "object") {
    for (const nested of Object.values(value)) {
      deepFreeze(nested);
    }
    Object.freeze(value);
  }
  return value;
}

export const TEMPLATES: readonly Readonly<TemplatePreset>[] = deepFreeze([
  {
    id: "academic-dense",
    name: "Academic Dense",
    description:
      "Long-form academic CV: one column, 9.75 pt body, year-only dates, and ruled section landmarks that stay findable across many pages.",
    customization: {
      font: {
        family: "source-sans-3",
        baseSizePx: 13,
      },
      colors: {
        primary: "#12293f",
        text: "#1a1e22",
        background: "#ffffff",
      },
      spacing: {
        sectionGap: 20,
        entryGap: 6,
        lineHeight: 1.3,
        pageMargin: {
          x: 20,
          y: 12,
        },
      },
      heading: {
        style: "uppercase",
        showRule: true,
      },
      header: {
        align: "left",
        detailsLayout: "inline",
        iconStyle: "none",
      },
      layout: {
        columns: 1,
        placement: "keep",
        surfaceTarget: "none",
      },
      sectionDisplay: {
        skill: {
          style: "text",
        },
        language: {
          style: "text",
        },
      },
      pageFormat: "a4",
      dateFormat: "YYYY",
    },
  },
  {
    id: "ats-plain",
    name: "ATS Plain",
    description:
      "One column, black on white, no icons or level widgets — what a text extractor reads is what the document stores.",
    customization: {
      font: {
        family: "source-sans-3",
        baseSizePx: 14,
      },
      colors: {
        primary: "#000000",
        text: "#000000",
        background: "#ffffff",
      },
      spacing: {
        sectionGap: 20,
        entryGap: 10,
        lineHeight: 1.45,
        pageMargin: {
          x: 18,
          y: 16,
        },
      },
      heading: {
        style: "normal",
        showRule: true,
      },
      header: {
        align: "left",
        detailsLayout: "stacked",
        iconStyle: "none",
      },
      layout: {
        columns: 1,
        placement: "keep",
        surfaceTarget: "none",
      },
      sectionDisplay: {
        skill: {
          style: "text",
        },
        language: {
          style: "text",
        },
      },
      pageFormat: "a4",
      dateFormat: "Mon YYYY",
    },
  },
  {
    id: "classic-serif",
    name: "Classic Serif",
    description:
      "Traditional one-column serif document: centred letterhead, ruled uppercase section headings, no fills or tints.",
    customization: {
      font: {
        family: "roboto-serif",
        baseSizePx: 13,
      },
      colors: {
        primary: "#12151a",
        text: "#23262c",
        background: "#ffffff",
        accent: "#1f3864",
      },
      spacing: {
        sectionGap: 22,
        entryGap: 12,
        lineHeight: 1.5,
        pageMargin: {
          x: 25,
          y: 20,
        },
      },
      heading: {
        style: "uppercase",
        showRule: true,
      },
      header: {
        align: "center",
        detailsLayout: "inline",
        iconStyle: "none",
      },
      layout: {
        columns: 1,
        placement: "keep",
        surfaceTarget: "none",
      },
      sectionDisplay: {
        skill: {
          style: "text",
        },
        language: {
          style: "text",
        },
      },
      pageFormat: "a4",
      dateFormat: "Mon YYYY",
    },
  },
  {
    id: "consulting-formal",
    name: "Consulting Formal",
    description:
      "One-column Letter document in navy and grey: left title block, title-case ruled headings, tight vertical rhythm, no fills.",
    customization: {
      font: {
        family: "inter",
        baseSizePx: 13,
      },
      colors: {
        primary: "#14304f",
        text: "#33383d",
        background: "#ffffff",
        accent: "#1c4e80",
      },
      spacing: {
        sectionGap: 15,
        entryGap: 9,
        lineHeight: 1.45,
        pageMargin: {
          x: 25,
          y: 15,
        },
      },
      heading: {
        style: "titlecase",
        showRule: true,
      },
      header: {
        align: "left",
        detailsLayout: "inline",
        iconStyle: "none",
      },
      layout: {
        columns: 1,
        placement: "keep",
        surfaceTarget: "none",
      },
      sectionDisplay: {
        skill: {
          style: "text",
        },
        language: {
          style: "text",
        },
      },
      pageFormat: "letter",
      dateFormat: "MM/YYYY",
    },
  },
  {
    id: "creative-accent",
    name: "Creative Accent",
    description:
      "One column under a tinted header block, with a single deep vermilion carrying the name, section headings, rules, links, and skill tags.",
    customization: {
      font: {
        family: "source-sans-3",
        baseSizePx: 15,
      },
      colors: {
        primary: "#c23a14",
        text: "#1f1a17",
        background: "#ffffff",
        accent: "#c23a14",
        surface: "#fff0eb",
      },
      spacing: {
        sectionGap: 20,
        entryGap: 10,
        lineHeight: 1.5,
        pageMargin: {
          x: 22,
          y: 18,
        },
      },
      heading: {
        style: "uppercase",
        showRule: true,
      },
      header: {
        align: "left",
        detailsLayout: "inline",
        iconStyle: "none",
      },
      layout: {
        columns: 1,
        placement: "keep",
        surfaceTarget: "header",
      },
      sectionDisplay: {
        skill: {
          style: "tag",
        },
        language: {
          style: "text",
        },
      },
      pageFormat: "a4",
      dateFormat: "Mon YYYY",
    },
  },
  {
    id: "designer-tag",
    name: "Designer Tag",
    description:
      "One-column portfolio sheet on bone paper: centred nameplate, no rules, and olive chips for skills and languages as the page's only fills.",
    customization: {
      font: {
        family: "be-vietnam-pro",
        baseSizePx: 15,
      },
      colors: {
        primary: "#2f3a1e",
        text: "#242220",
        background: "#f4f1ea",
        accent: "#54692a",
      },
      spacing: {
        sectionGap: 24,
        entryGap: 12,
        lineHeight: 1.5,
        pageMargin: {
          x: 26,
          y: 22,
        },
      },
      heading: {
        style: "uppercase",
        showRule: false,
      },
      header: {
        align: "center",
        detailsLayout: "inline",
        iconStyle: "outline",
      },
      layout: {
        columns: 1,
        placement: "keep",
        surfaceTarget: "none",
      },
      sectionDisplay: {
        skill: {
          style: "tag",
        },
        language: {
          style: "tag",
        },
      },
      pageFormat: "a4",
      dateFormat: "YYYY",
    },
  },
  {
    id: "editorial-wide",
    name: "Editorial Wide",
    description:
      "A one-column serif page at book proportions — wide margins, open leading, no rules — for a CV that is read rather than scanned.",
    customization: {
      font: {
        family: "alegreya",
        baseSizePx: 16,
      },
      colors: {
        primary: "#3a2c1e",
        text: "#24211c",
        background: "#faf8f3",
        accent: "#7a4a24",
      },
      spacing: {
        sectionGap: 32,
        entryGap: 16,
        lineHeight: 1.65,
        pageMargin: {
          x: 35,
          y: 22,
        },
      },
      heading: {
        style: "normal",
        showRule: false,
      },
      header: {
        align: "left",
        detailsLayout: "inline",
        iconStyle: "none",
      },
      layout: {
        columns: 1,
        placement: "keep",
        surfaceTarget: "none",
      },
      sectionDisplay: {
        skill: {
          style: "text",
        },
        language: {
          style: "text",
        },
      },
      pageFormat: "a4",
      dateFormat: "Mon YYYY",
    },
  },
  {
    id: "elegant-serif-two",
    name: "Elegant Serif Two-Column",
    description:
      "Two-column serif document with a warm ecru rail: claret title-case headings, credentials at the top of the rail, one ink throughout.",
    customization: {
      font: {
        family: "alegreya",
        baseSizePx: 14,
      },
      colors: {
        primary: "#6b2737",
        text: "#2b2622",
        background: "#ffffff",
        surface: "#f2ebdf",
      },
      spacing: {
        sectionGap: 24,
        entryGap: 12,
        lineHeight: 1.55,
        pageMargin: {
          x: 18,
          y: 16,
        },
      },
      heading: {
        style: "titlecase",
        showRule: true,
      },
      header: {
        align: "left",
        detailsLayout: "inline",
        iconStyle: "none",
      },
      layout: {
        columns: 2,
        placement: "byType",
        sidebarSectionTypes: ["certificate", "skill", "language"],
        surfaceTarget: "sidebar",
      },
      sectionDisplay: {
        skill: {
          style: "text",
        },
        language: {
          style: "dots",
        },
      },
      pageFormat: "a4",
      dateFormat: "Mon YYYY",
    },
  },
  {
    id: "engineer-compact",
    name: "Engineer Compact",
    description:
      "Dense two-column engineering CV: skill bars in an untinted sidebar, petrol accents on white, numeric dates.",
    customization: {
      font: {
        family: "source-sans-3",
        baseSizePx: 13,
      },
      colors: {
        primary: "#10394d",
        text: "#1a1f24",
        background: "#ffffff",
        accent: "#0d5a73",
      },
      spacing: {
        sectionGap: 12,
        entryGap: 6,
        lineHeight: 1.3,
        pageMargin: {
          x: 11,
          y: 12,
        },
      },
      heading: {
        style: "uppercase",
        showRule: true,
      },
      header: {
        align: "left",
        detailsLayout: "inline",
        iconStyle: "outline",
      },
      layout: {
        columns: 2,
        placement: "byType",
        sidebarSectionTypes: ["skill", "certificate", "language", "education"],
        surfaceTarget: "none",
      },
      sectionDisplay: {
        skill: {
          style: "bar",
        },
        language: {
          style: "dots",
        },
      },
      pageFormat: "a4",
      dateFormat: "MM/YYYY",
    },
  },
  {
    id: "executive-band",
    name: "Executive Band",
    description:
      "A filled navy masthead plate across the top of page one carries a large name and title; the pages below stay plain, uppercase-led, and ruleless.",
    customization: {
      font: {
        family: "be-vietnam-pro",
        baseSizePx: 15,
      },
      colors: {
        primary: "#16273d",
        text: "#1f2937",
        background: "#ffffff",
        accent: "#8a5a1e",
        surface: "#16273d",
      },
      spacing: {
        sectionGap: 24,
        entryGap: 12,
        lineHeight: 1.45,
        pageMargin: {
          x: 18,
          y: 14,
        },
      },
      heading: {
        style: "uppercase",
        showRule: false,
      },
      header: {
        align: "left",
        detailsLayout: "inline",
        iconStyle: "none",
      },
      layout: {
        columns: 1,
        placement: "keep",
        surfaceTarget: "header",
      },
      sectionDisplay: {
        skill: {
          style: "text",
        },
        language: {
          style: "dots",
        },
      },
      pageFormat: "a4",
      dateFormat: "Mon YYYY",
    },
  },
  {
    id: "government-formal",
    name: "Government Formal",
    description:
      "Single-column compliance document: one ink, a stacked contact block, ruled uppercase headings, Letter page at 25 mm margins and numeric MM/YYYY dates.",
    customization: {
      font: {
        family: "source-sans-3",
        baseSizePx: 14,
      },
      colors: {
        primary: "#000000",
        text: "#000000",
        background: "#ffffff",
      },
      spacing: {
        sectionGap: 24,
        entryGap: 14,
        lineHeight: 1.5,
        pageMargin: {
          x: 25,
          y: 25,
        },
      },
      heading: {
        style: "uppercase",
        showRule: true,
      },
      header: {
        align: "left",
        detailsLayout: "stacked",
        iconStyle: "none",
      },
      layout: {
        columns: 1,
        placement: "keep",
        surfaceTarget: "none",
      },
      sectionDisplay: {
        skill: {
          style: "text",
        },
        language: {
          style: "text",
        },
      },
      pageFormat: "letter",
      dateFormat: "MM/YYYY",
    },
  },
  {
    id: "graduate-friendly",
    name: "Graduate Friendly",
    description:
      "One-column warm sans at 15 px with a tinted identity band and wide entry spacing, so a short first-job history fills the page.",
    customization: {
      font: {
        family: "source-sans-3",
        baseSizePx: 15,
      },
      colors: {
        primary: "#7a3b1c",
        text: "#2f2a26",
        background: "#ffffff",
        accent: "#a04f16",
        surface: "#f7e8d9",
      },
      spacing: {
        sectionGap: 24,
        entryGap: 18,
        lineHeight: 1.55,
        pageMargin: {
          x: 20,
          y: 16,
        },
      },
      heading: {
        style: "titlecase",
        showRule: true,
      },
      header: {
        align: "left",
        detailsLayout: "stacked",
        iconStyle: "outline",
      },
      layout: {
        columns: 1,
        placement: "keep",
        surfaceTarget: "header",
      },
      sectionDisplay: {
        skill: {
          style: "tag",
        },
        language: {
          style: "tag",
        },
      },
      pageFormat: "a4",
      dateFormat: "YYYY",
    },
  },
  {
    id: "high-contrast",
    name: "High Contrast",
    description:
      "Maximum legibility: near-black on white at 13.5 pt, one AAA-contrast blue accent, one column.",
    customization: {
      font: {
        family: "inter",
        baseSizePx: 18,
      },
      colors: {
        primary: "#000000",
        text: "#141414",
        background: "#ffffff",
        accent: "#0b3fd6",
      },
      spacing: {
        sectionGap: 36,
        entryGap: 20,
        lineHeight: 1.6,
        pageMargin: {
          x: 18,
          y: 16,
        },
      },
      heading: {
        style: "uppercase",
        showRule: true,
      },
      header: {
        align: "left",
        detailsLayout: "stacked",
        iconStyle: "outline",
      },
      layout: {
        columns: 1,
        placement: "keep",
        surfaceTarget: "none",
      },
      sectionDisplay: {
        skill: {
          style: "text",
        },
        language: {
          style: "text",
        },
      },
      pageFormat: "a4",
      dateFormat: "Mon YYYY",
    },
  },
  {
    id: "international-lang",
    name: "International Languages",
    description:
      "Cross-border CV: languages lead an untinted rail as five-step bars, year-only dates, warm-neutral ink under a soft header band.",
    customization: {
      font: {
        family: "be-vietnam-pro",
        baseSizePx: 13,
      },
      colors: {
        primary: "#191614",
        text: "#2a2724",
        background: "#ffffff",
        accent: "#5a4a3a",
        surface: "#f0ede7",
      },
      spacing: {
        sectionGap: 18,
        entryGap: 9,
        lineHeight: 1.55,
        pageMargin: {
          x: 16,
          y: 14,
        },
      },
      heading: {
        style: "normal",
        showRule: true,
      },
      header: {
        align: "center",
        detailsLayout: "inline",
        iconStyle: "none",
      },
      layout: {
        columns: 2,
        placement: "byType",
        sidebarSectionTypes: ["language", "skill", "certificate"],
        surfaceTarget: "header",
      },
      sectionDisplay: {
        skill: {
          style: "text",
        },
        language: {
          style: "bar",
        },
      },
      pageFormat: "a4",
      dateFormat: "YYYY",
    },
  },
  {
    id: "minimal-air",
    name: "Minimal Air",
    description:
      "One column, no rules and no fills: wide margins and one spacing ladder carry the hierarchy.",
    customization: {
      font: {
        family: "inter",
        baseSizePx: 15,
      },
      colors: {
        primary: "#14161a",
        text: "#24272c",
        background: "#ffffff",
      },
      spacing: {
        sectionGap: 36,
        entryGap: 16,
        lineHeight: 1.65,
        pageMargin: {
          x: 30,
          y: 24,
        },
      },
      heading: {
        style: "uppercase",
        showRule: false,
      },
      header: {
        align: "left",
        detailsLayout: "inline",
        iconStyle: "none",
      },
      layout: {
        columns: 1,
        placement: "keep",
        surfaceTarget: "none",
      },
      sectionDisplay: {
        skill: {
          style: "text",
        },
        language: {
          style: "text",
        },
      },
      pageFormat: "a4",
      dateFormat: "Mon YYYY",
    },
  },
  {
    id: "modern-sidebar",
    name: "Modern Sidebar",
    description:
      "Two-column contemporary sans with a tinted sidebar carrying skills, languages and certificates.",
    customization: {
      font: {
        family: "be-vietnam-pro",
        baseSizePx: 14,
      },
      colors: {
        primary: "#0e3f52",
        text: "#1c2b33",
        background: "#ffffff",
        accent: "#0f6b7d",
        surface: "#e3ecf0",
      },
      spacing: {
        sectionGap: 20,
        entryGap: 10,
        lineHeight: 1.5,
        pageMargin: {
          x: 14,
          y: 13,
        },
      },
      heading: {
        style: "uppercase",
        showRule: true,
      },
      header: {
        align: "left",
        detailsLayout: "inline",
        iconStyle: "outline",
      },
      layout: {
        columns: 2,
        placement: "byType",
        sidebarSectionTypes: ["skill", "language", "certificate"],
        surfaceTarget: "sidebar",
      },
      sectionDisplay: {
        skill: {
          style: "bar",
        },
        language: {
          style: "dots",
        },
      },
      pageFormat: "a4",
      dateFormat: "Mon YYYY",
    },
  },
  {
    id: "mono-print",
    name: "Mono Print",
    description:
      "One-column pure black on white with no accent, no rules, no tints and no level widgets: every distinction survives a photocopier.",
    customization: {
      font: {
        family: "source-sans-3",
        baseSizePx: 14,
      },
      colors: {
        primary: "#000000",
        text: "#000000",
        background: "#ffffff",
      },
      spacing: {
        sectionGap: 26,
        entryGap: 12,
        lineHeight: 1.5,
        pageMargin: {
          x: 20,
          y: 18,
        },
      },
      heading: {
        style: "uppercase",
        showRule: false,
      },
      header: {
        align: "left",
        detailsLayout: "stacked",
        iconStyle: "none",
      },
      layout: {
        columns: 1,
        placement: "keep",
        surfaceTarget: "none",
      },
      sectionDisplay: {
        skill: {
          style: "text",
        },
        language: {
          style: "text",
        },
      },
      pageFormat: "a4",
      dateFormat: "Mon YYYY",
    },
  },
  {
    id: "nordic-muted",
    name: "Nordic Muted",
    description:
      "Cool blue-grey ink on white with a faint tinted sidebar panel, uppercase labels, and hairline rules.",
    customization: {
      font: {
        family: "source-sans-3",
        baseSizePx: 13,
      },
      colors: {
        primary: "#2f4557",
        text: "#333a42",
        background: "#ffffff",
        accent: "#4a6b80",
        surface: "#f1f3f5",
      },
      spacing: {
        sectionGap: 22,
        entryGap: 11,
        lineHeight: 1.55,
        pageMargin: {
          x: 18,
          y: 16,
        },
      },
      heading: {
        style: "uppercase",
        showRule: true,
      },
      header: {
        align: "left",
        detailsLayout: "inline",
        iconStyle: "outline",
      },
      layout: {
        columns: 2,
        placement: "byType",
        sidebarSectionTypes: ["skill", "language", "certificate"],
        surfaceTarget: "sidebar",
      },
      sectionDisplay: {
        skill: {
          style: "text",
        },
        language: {
          style: "dots",
        },
      },
      pageFormat: "a4",
      dateFormat: "Mon YYYY",
    },
  },
  {
    id: "one-page-tight",
    name: "One-Page Tight",
    description:
      "A4 two-column tuned to land a full senior career on a single sheet: cut margins, disciplined leading, no level widgets.",
    customization: {
      font: {
        family: "source-sans-3",
        baseSizePx: 13,
      },
      colors: {
        primary: "#22303c",
        text: "#1c2126",
        background: "#ffffff",
        accent: "#2f4858",
      },
      spacing: {
        sectionGap: 10,
        entryGap: 6,
        lineHeight: 1.25,
        pageMargin: {
          x: 12,
          y: 11,
        },
      },
      heading: {
        style: "uppercase",
        showRule: true,
      },
      header: {
        align: "left",
        detailsLayout: "inline",
        iconStyle: "none",
      },
      layout: {
        columns: 2,
        placement: "byType",
        sidebarSectionTypes: ["skill", "language", "certificate"],
        surfaceTarget: "none",
      },
      sectionDisplay: {
        skill: {
          style: "text",
        },
        language: {
          style: "text",
        },
      },
      pageFormat: "a4",
      dateFormat: "MM/YYYY",
    },
  },
  {
    id: "startup-bold",
    name: "Startup Bold",
    description:
      "Type-led one column for product, growth, and founding-team roles: the largest base in the set, uppercase black labels instead of rules, a 1:2.5:7.5 spacing ladder, and monochrome dot proficiency.",
    customization: {
      font: {
        family: "inter",
        baseSizePx: 16,
      },
      colors: {
        primary: "#0b0d10",
        text: "#2b3038",
        background: "#ffffff",
      },
      spacing: {
        sectionGap: 30,
        entryGap: 10,
        lineHeight: 1.35,
        pageMargin: {
          x: 26,
          y: 16,
        },
      },
      heading: {
        style: "uppercase",
        showRule: false,
      },
      header: {
        align: "left",
        detailsLayout: "inline",
        iconStyle: "outline",
      },
      layout: {
        columns: 1,
        placement: "keep",
        surfaceTarget: "none",
      },
      sectionDisplay: {
        skill: {
          style: "dots",
        },
        language: {
          style: "dots",
        },
      },
      pageFormat: "letter",
      dateFormat: "YYYY",
    },
  },
] satisfies TemplatePreset[]);
