import { readFileSync, readdirSync } from "node:fs";
import { join } from "node:path";
import Ajv2020 from "ajv/dist/2020.js";
import addFormats from "ajv-formats";
import { describe, expect, it } from "vitest";

import { CURRENT_VERSION } from "../gen/ts/released";
import type { Resume } from "../gen/ts/resume";
import {
  FILLER_LANGUAGES,
  SAMPLES,
  loadFiller,
  loadSample,
} from "../gen/ts/samples";
import { TEMPLATES } from "../gen/ts/templates";
import { validateDocument } from "../validation/store";

// Sample resumes for the template gallery and "create from a sample"
// (samples/<templateId>.<vi|en>.json), and the generic filler shown for a
// template without its own sample (samples/_filler.<vi|en>.json). The server's
// create path re-validates every sample and must store it unchanged; that
// check lives in apps/server/internal/resumeapi.

const root = new URL("..", import.meta.url).pathname;
const directory = join(root, "samples");
const schema = JSON.parse(
  readFileSync(join(root, "resume.schema.json"), "utf8"),
);
const validate = addFormats(
  new Ajv2020({ allErrors: true, strict: false }),
).compile(schema);

const files = readdirSync(directory).filter((name) => name.endsWith(".json"));
const documents = files.map((name) => ({
  name,
  filler: name.startsWith("_filler."),
  document: JSON.parse(readFileSync(join(directory, name), "utf8")) as Resume,
}));
const templateIds = TEMPLATES.map((template) => template.id);

describe("sample files", () => {
  it("name a template and a language, with both languages per template", () => {
    const pattern = /^(?:_filler|[a-z0-9]+(?:-[a-z0-9]+)*)\.(?:vi|en)\.json$/u;
    expect(
      readdirSync(directory).filter((name) => !pattern.test(name)),
    ).toEqual([]);
    const byTemplate = new Map<string, string[]>();
    for (const { templateId, lng } of SAMPLES) {
      expect(templateIds).toContain(templateId);
      byTemplate.set(templateId, [...(byTemplate.get(templateId) ?? []), lng]);
    }
    expect(byTemplate.size).toBeGreaterThan(0);
    for (const languages of byTemplate.values()) {
      expect([...languages].sort()).toEqual(["en", "vi"]);
    }
    expect([...FILLER_LANGUAGES].sort()).toEqual(["en", "vi"]);
    expect(files).toHaveLength(SAMPLES.length + FILLER_LANGUAGES.length);
  });

  it.each(documents.map(({ name, document }) => [name, document] as const))(
    "%s is a valid current-version resume",
    (_name, document) => {
      expect(document.schemaVersion).toBe(CURRENT_VERSION);
      expect(validate(document), JSON.stringify(validate.errors)).toBe(true);
      expect(validateDocument(document as never)).toEqual([]);
    },
  );

  it.each(
    documents
      .filter(({ filler }) => !filler)
      .map(({ name, document }) => [name, document] as const),
  )("%s keeps its template's customization", (name, document) => {
    const template = TEMPLATES.find(({ id }) => name.startsWith(`${id}.`))!;
    const { layout, ...rest } = document.customization;
    const { layout: presetLayout, ...presetRest } = template.customization;
    expect(rest).toEqual(presetRest);
    expect(layout.columns).toBe(presetLayout.columns);
    expect(layout.surfaceTarget).toBe(presetLayout.surfaceTarget);
  });

  it.each(documents.map(({ name, document }) => [name, document] as const))(
    "%s holds only fictional contact data",
    (_name, document) => {
      expect(document.personalDetails.photo).toBeUndefined();
      for (const detail of document.personalDetails.details ?? []) {
        if (detail.type === "email") {
          expect(detail.value).toMatch(/^[a-z0-9.]+@example\.com$/u);
        }
        if (detail.type === "phone") {
          expect(detail.value).toMatch(/^\+84 90 000 \d{4}$/u);
        }
      }
      // Every link, in details or entries, points at a reserved example host.
      const hosts = [
        ...JSON.stringify(document).matchAll(/https?:\/\/([^/"\s<>]+)/gu),
      ].map((match) => match[1]!);
      expect(
        hosts.filter(
          (host) => !/^(?:[a-z0-9-]+\.)*example\.(?:com|org|edu)$/u.test(host),
        ),
      ).toEqual([]);
    },
  );
});

describe("sample loaders", () => {
  it("load every sample and filler as a fresh copy", async () => {
    for (const { templateId, lng } of SAMPLES) {
      const first = await loadSample(templateId, lng);
      const onDisk = documents.find(
        ({ name }) => name === `${templateId}.${lng}.json`,
      )!.document;
      expect(first).toEqual(onDisk);
      expect(await loadSample(templateId, lng)).not.toBe(first);
    }
    for (const lng of FILLER_LANGUAGES) {
      expect(await loadFiller(lng)).toEqual(
        documents.find(({ name }) => name === `_filler.${lng}.json`)!.document,
      );
    }
  });

  it("returns undefined for a template or language without a sample", async () => {
    expect(await loadSample("classic-serif", "vi")).toBeUndefined();
    expect(await loadSample("__proto__", "en")).toBeUndefined();
    expect(await loadSample("ats-plain", "fr" as never)).toBeUndefined();
  });
});
