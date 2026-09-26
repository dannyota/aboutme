// Reads the committed sample resumes (packages/schema/samples) and emits the
// TypeScript sample registry consumed by apps/web: a manifest of which
// template has a sample in which language, and lazy loaders, so a page pulls
// only the sample it shows. test/samples.test.ts validates the documents.

import { readdirSync } from "node:fs";
import { join } from "node:path";

import {
  generatedHeader,
  packageRoot,
  writeGenerated,
} from "./generatePaths.mjs";

export const sampleDirectory = join(packageRoot, "samples");

const SAMPLE_FILE = /^([a-z0-9]+(?:-[a-z0-9]+)*)\.(vi|en)\.json$/u;
const FILLER_FILE = /^_filler\.(vi|en)\.json$/u;

/** Sample and filler files by name; throws on any other file. */
export function readSampleFiles(templateIds) {
  const known = new Set(templateIds);
  const samples = [];
  const fillers = [];
  for (const name of readdirSync(sampleDirectory).sort()) {
    const filler = FILLER_FILE.exec(name);
    if (filler) {
      fillers.push({ lng: filler[1], file: name });
      continue;
    }
    const sample = SAMPLE_FILE.exec(name);
    if (!sample) {
      throw new Error(`generate.mjs: sample file ${name} has an invalid name.`);
    }
    if (!known.has(sample[1])) {
      throw new Error(`generate.mjs: sample ${name} names no template.`);
    }
    samples.push({ templateId: sample[1], lng: sample[2], file: name });
  }
  return { samples, fillers };
}

export function generateSamplesTs({ samples, fillers }, outFile) {
  const loader = ({ file }) =>
    `  ${JSON.stringify(file.replace(/\.json$/u, ""))}: () =>\n` +
    `    import("../../samples/${file}"),`;
  const body = `${generatedHeader("samples/*.json")}
import type { Resume } from "./resume";

export type SampleLanguage = "vi" | "en";

export interface SampleRef {
  readonly templateId: string;
  readonly lng: SampleLanguage;
}

/** Every template that has its own sample, in each language it has. */
export const SAMPLES: readonly SampleRef[] = Object.freeze(
  ${JSON.stringify(samples.map(({ templateId, lng }) => ({ templateId, lng })))}
    .map((ref) => Object.freeze(ref as SampleRef)),
);

/** The languages with generic filler content for templates without one. */
export const FILLER_LANGUAGES: readonly SampleLanguage[] = Object.freeze(
  ${JSON.stringify(fillers.map(({ lng }) => lng))} as SampleLanguage[],
);

const loaders: Readonly<
  Record<string, () => Promise<{ readonly default: unknown }>>
> = {
${[...samples, ...fillers].map(loader).join("\n")}
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
  return load(\`\${templateId}.\${lng}\`);
}

/** Generic filler content for a template without its own sample. */
export function loadFiller(lng: SampleLanguage): Promise<Resume | undefined> {
  return load(\`_filler.\${lng}\`);
}
`;
  writeGenerated(outFile, body, "prettier");
}
