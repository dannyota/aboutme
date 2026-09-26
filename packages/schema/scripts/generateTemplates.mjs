// Reads and validates the committed template presets (packages/schema/templates
// or $ABOUTME_TEMPLATE_DIR), then emits the frozen TypeScript template
// registry consumed by apps/web.

import { readFileSync, readdirSync } from "node:fs";
import { basename, join } from "node:path";
import Ajv2020 from "ajv/dist/2020.js";
import addFormats from "ajv-formats";

import {
  generatedHeader,
  templateDirectory,
  writeGenerated,
} from "./generatePaths.mjs";

function failTemplate(file, message) {
  throw new Error(`generate.mjs: template ${basename(file)} ${message}`);
}

function exactKeys(file, value, required, optional = []) {
  if (!value || typeof value !== "object" || Array.isArray(value)) {
    failTemplate(file, "must be an object.");
  }
  const allowed = new Set([...required, ...optional]);
  const keys = Object.keys(value);
  const missing = required.filter((key) => !keys.includes(key));
  const unknown = keys.filter((key) => !allowed.has(key));
  if (missing.length > 0 || unknown.length > 0) {
    failTemplate(
      file,
      `has invalid keys (missing: ${missing.join(", ") || "none"}; unknown: ${unknown.join(", ") || "none"}).`,
    );
  }
}

export function readTemplatePresets(schema) {
  const files = readdirSync(templateDirectory)
    .filter((name) => name.endsWith(".json"))
    .sort()
    .map((name) => join(templateDirectory, name));
  if (files.length === 0) {
    throw new Error("generate.mjs: no template presets found.");
  }

  const fonts = new Set(
    schema.$defs?.customization?.properties?.font?.properties?.family?.enum ??
      [],
  );
  const sectionTypes = new Set(schema.$defs?.sectionType?.enum ?? []);
  const surfaceTargets = new Set(
    schema.$defs?.customization?.properties?.layout?.properties?.surfaceTarget
      ?.enum ?? [],
  );
  const ajv = addFormats(new Ajv2020({ allErrors: true, strict: true }));
  const validateCustomization = ajv.compile({
    $schema: schema.$schema,
    $defs: schema.$defs,
    $ref: "#/$defs/customization",
  });
  const ids = new Set();

  return files.map((file) => {
    let preset;
    try {
      preset = JSON.parse(readFileSync(file, "utf8"));
    } catch (error) {
      failTemplate(file, `is not valid JSON: ${error.message}.`);
    }
    exactKeys(file, preset, ["id", "name", "description", "customization"]);
    if (
      typeof preset.id !== "string" ||
      preset.id.length === 0 ||
      preset.id !== basename(file, ".json")
    ) {
      failTemplate(file, "must have an id equal to its filename.");
    }
    if (ids.has(preset.id)) {
      failTemplate(file, `duplicates id ${JSON.stringify(preset.id)}.`);
    }
    ids.add(preset.id);
    for (const key of ["name", "description"]) {
      if (typeof preset[key] !== "string" || preset[key].length === 0) {
        failTemplate(file, `must have a non-empty ${key}.`);
      }
    }

    const customization = preset.customization;
    exactKeys(
      file,
      customization,
      [
        "font",
        "colors",
        "spacing",
        "heading",
        "layout",
        "sectionDisplay",
        "pageFormat",
        "dateFormat",
      ],
      ["header"],
    );
    if (!fonts.has(customization?.font?.family)) {
      failTemplate(
        file,
        `uses unknown font ${JSON.stringify(customization?.font?.family)}.`,
      );
    }

    const layout = customization.layout;
    exactKeys(
      file,
      layout,
      ["columns", "placement"],
      ["sidebarSectionTypes", "surfaceTarget"],
    );
    if (
      layout.surfaceTarget !== undefined &&
      !surfaceTargets.has(layout.surfaceTarget)
    ) {
      failTemplate(
        file,
        `uses unknown surfaceTarget ${JSON.stringify(layout.surfaceTarget)}.`,
      );
    }
    if (layout.placement === "keep") {
      if (layout.sidebarSectionTypes !== undefined) {
        failTemplate(
          file,
          "must not define sidebarSectionTypes for placement keep.",
        );
      }
    } else if (layout.placement === "byType") {
      if (!Array.isArray(layout.sidebarSectionTypes)) {
        failTemplate(
          file,
          "must define sidebarSectionTypes for placement byType.",
        );
      }
      const selectors = layout.sidebarSectionTypes;
      if (new Set(selectors).size !== selectors.length) {
        failTemplate(file, "has duplicate sidebarSectionTypes.");
      }
      for (const selector of selectors) {
        if (!sectionTypes.has(selector) || selector === "custom") {
          failTemplate(
            file,
            `uses invalid section selector ${JSON.stringify(selector)}.`,
          );
        }
      }
    } else {
      failTemplate(
        file,
        `uses unknown placement ${JSON.stringify(layout.placement)}.`,
      );
    }

    const {
      placement: _placement,
      sidebarSectionTypes: _selectors,
      ...storedLayout
    } = layout;
    const storedCustomization = {
      ...customization,
      layout: { ...storedLayout, sections: { main: [], sidebar: [] } },
    };
    if (!validateCustomization(storedCustomization)) {
      failTemplate(
        file,
        `does not form a valid customization: ${ajv.errorsText(validateCustomization.errors)}.`,
      );
    }
    return preset;
  });
}

export function generateTemplatesTs(
  presets,
  sectionTypes,
  surfaceTargets,
  outFile,
) {
  const sectionTypeUnion = sectionTypes.map(JSON.stringify).join(" | ");
  const surfaceTargetUnion = surfaceTargets.map(JSON.stringify).join(" | ");
  const body = `${generatedHeader("templates/*.json and resume.schema.json")}

import type { Customization } from "./resume";

export type SectionType = ${sectionTypeUnion};

export interface TemplatePreset {
  readonly id: string;
  readonly name: string;
  readonly description: string;
  readonly customization: Omit<Customization, "layout"> & {
    readonly layout: {
      readonly columns: 1 | 2;
      readonly placement: "keep" | "byType";
      readonly sidebarSectionTypes?: readonly SectionType[];
      readonly surfaceTarget?: ${surfaceTargetUnion};
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

export const TEMPLATES: readonly Readonly<TemplatePreset>[] = deepFreeze(
  ${JSON.stringify(presets, null, 2)} satisfies TemplatePreset[],
);
`;
  writeGenerated(outFile, body, "prettier");
}
