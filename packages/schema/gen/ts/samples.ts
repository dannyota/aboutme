// Code generated from samples/*.json. DO NOT EDIT.
import type { Resume } from "./resume";

export type SampleLanguage = "vi" | "en";

export interface SampleRef {
  readonly templateId: string;
  readonly lng: SampleLanguage;
}

/** Every template that has its own sample, in each language it has. */
export const SAMPLES: readonly SampleRef[] = Object.freeze(
  [
    { templateId: "ats-plain", lng: "en" },
    { templateId: "ats-plain", lng: "vi" },
    { templateId: "engineer-compact", lng: "en" },
    { templateId: "engineer-compact", lng: "vi" },
    { templateId: "executive-band", lng: "en" },
    { templateId: "executive-band", lng: "vi" },
    { templateId: "graduate-friendly", lng: "en" },
    { templateId: "graduate-friendly", lng: "vi" },
    { templateId: "modern-sidebar", lng: "en" },
    { templateId: "modern-sidebar", lng: "vi" },
  ].map((ref) => Object.freeze(ref as SampleRef)),
);

/** The languages with generic filler content for templates without one. */
export const FILLER_LANGUAGES: readonly SampleLanguage[] = Object.freeze([
  "en",
  "vi",
] as SampleLanguage[]);

const loaders: Readonly<
  Record<string, () => Promise<{ readonly default: unknown }>>
> = {
  "ats-plain.en": () => import("../../samples/ats-plain.en.json"),
  "ats-plain.vi": () => import("../../samples/ats-plain.vi.json"),
  "engineer-compact.en": () => import("../../samples/engineer-compact.en.json"),
  "engineer-compact.vi": () => import("../../samples/engineer-compact.vi.json"),
  "executive-band.en": () => import("../../samples/executive-band.en.json"),
  "executive-band.vi": () => import("../../samples/executive-band.vi.json"),
  "graduate-friendly.en": () =>
    import("../../samples/graduate-friendly.en.json"),
  "graduate-friendly.vi": () =>
    import("../../samples/graduate-friendly.vi.json"),
  "modern-sidebar.en": () => import("../../samples/modern-sidebar.en.json"),
  "modern-sidebar.vi": () => import("../../samples/modern-sidebar.vi.json"),
  "_filler.en": () => import("../../samples/_filler.en.json"),
  "_filler.vi": () => import("../../samples/_filler.vi.json"),
};

async function load(name: string): Promise<Resume | undefined> {
  if (!Object.hasOwn(loaders, name)) return undefined;
  const module = await loaders[name]!();
  // A fresh copy, so a caller can change it without touching other pages.
  return structuredClone(module.default) as Resume;
}

/** The sample for a template in a language, or undefined when none exists. */
export function loadSample(
  templateId: string,
  lng: SampleLanguage,
): Promise<Resume | undefined> {
  return load(`${templateId}.${lng}`);
}

/** Generic filler content for a template without its own sample. */
export function loadFiller(lng: SampleLanguage): Promise<Resume | undefined> {
  return load(`_filler.${lng}`);
}
