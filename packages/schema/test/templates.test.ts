import { execFileSync } from "node:child_process";
import {
  cpSync,
  mkdtempSync,
  readFileSync,
  readdirSync,
  rmSync,
  writeFileSync,
} from "node:fs";
import { tmpdir } from "node:os";
import { basename, join } from "node:path";
import Ajv2020 from "ajv/dist/2020.js";
import addFormats from "ajv-formats";
import { afterEach, describe, expect, it } from "vitest";

import { TEMPLATES } from "../gen/ts/templates";
import { generateTemplatesTs } from "../scripts/generate.mjs";

const root = new URL("..", import.meta.url).pathname;
const templatesDir = join(root, "templates");
const schema = JSON.parse(
  readFileSync(join(root, "resume.schema.json"), "utf8"),
);
const ajv = addFormats(new Ajv2020({ allErrors: true, strict: true }));
const validateCustomization = ajv.compile({
  $schema: schema.$schema,
  $defs: schema.$defs,
  $ref: "#/$defs/customization",
});
const temporaryDirectories: string[] = [];

afterEach(() => {
  for (const path of temporaryDirectories.splice(0)) {
    rmSync(path, { recursive: true, force: true });
  }
});

describe("template registry", () => {
  it("matches every committed preset and validates its rule and customization", () => {
    const fileIDs = readdirSync(templatesDir)
      .filter((name) => name.endsWith(".json"))
      .map((name) => basename(name, ".json"))
      .sort();
    const registryIDs = TEMPLATES.map((preset) => preset.id).sort();

    expect(registryIDs).toEqual(fileIDs);
    expect(new Set(registryIDs).size).toBe(registryIDs.length);
    expect(Object.isFrozen(TEMPLATES)).toBe(true);

    const fonts =
      schema.$defs.customization.properties.font.properties.family.enum;
    const sectionTypes = schema.$defs.sectionType.enum;
    for (const preset of TEMPLATES) {
      expect(Object.isFrozen(preset)).toBe(true);
      expect(fonts).toContain(preset.customization.font.family);

      const { placement, sidebarSectionTypes, ...layout } =
        preset.customization.layout;
      if (placement === "keep") {
        expect(sidebarSectionTypes).toBeUndefined();
      } else {
        expect(placement).toBe("byType");
        expect(sidebarSectionTypes).toBeDefined();
        expect(new Set(sidebarSectionTypes).size).toBe(
          sidebarSectionTypes?.length,
        );
        for (const sectionType of sidebarSectionTypes ?? []) {
          expect(sectionType).not.toBe("custom");
          expect(sectionTypes).toContain(sectionType);
        }
      }

      const customization = {
        ...preset.customization,
        layout: { ...layout, sections: { main: [], sidebar: [] } },
      };
      expect(
        validateCustomization(customization),
        `${preset.id}: ${ajv.errorsText(validateCustomization.errors)}`,
      ).toBe(true);
    }
  });

  it.each([
    [
      "unknown font",
      (preset: any) =>
        (preset.customization.font.family = "missing-font"),
    ],
    [
      "unknown selector",
      (preset: any) =>
        (preset.customization.layout.sidebarSectionTypes = ["missing-type"]),
    ],
    [
      "duplicate selector",
      (preset: any) =>
        (preset.customization.layout.sidebarSectionTypes = ["skill", "skill"]),
    ],
    [
      "custom selector",
      (preset: any) =>
        (preset.customization.layout.sidebarSectionTypes = ["custom"]),
    ],
    [
      "selector on keep",
      (preset: any) => {
        preset.customization.layout.placement = "keep";
        preset.customization.layout.sidebarSectionTypes = ["skill"];
      },
    ],
  ])("generation rejects a preset with an %s", (_name, mutate) => {
    const fixtureDir = mkdtempSync(join(tmpdir(), "aboutme-template-test-"));
    temporaryDirectories.push(fixtureDir);
    cpSync(templatesDir, fixtureDir, { recursive: true });
    const fixturePath = join(fixtureDir, "modern-sidebar.json");
    const preset = JSON.parse(readFileSync(fixturePath, "utf8"));
    mutate(preset);
    writeFileSync(fixturePath, `${JSON.stringify(preset, null, 2)}\n`);

    expect(() =>
      execFileSync("node", ["scripts/generate.mjs"], {
        cwd: root,
        env: { ...process.env, ABOUTME_TEMPLATE_DIR: fixtureDir },
        stdio: "pipe",
      }),
    ).toThrow();
  });

  it("derives the emitted surfaceTarget union from the schema enum", () => {
    const fixtureDir = mkdtempSync(join(tmpdir(), "aboutme-template-ts-"));
    temporaryDirectories.push(fixtureDir);
    const outFile = join(fixtureDir, "templates.ts");

    generateTemplatesTs(
      [],
      ["profile"],
      ["none", "header", "sidebar", "experimental"],
      outFile,
    );

    expect(readFileSync(outFile, "utf8")).toContain(
      'readonly surfaceTarget?: "none" | "header" | "sidebar" | "experimental";',
    );
  });
});
