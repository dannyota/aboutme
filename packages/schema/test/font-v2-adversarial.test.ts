import { createHash } from "node:crypto";
import { readdirSync, readFileSync } from "node:fs";
import { join } from "node:path";
import { describe, expect, it } from "vitest";

import {
  ACCEPTED_VERSIONS,
  CURRENT_VERSION,
  EMITTED_VERSIONS,
} from "@aboutme/schema/released";

interface CatalogEntry {
  id: string;
  v1Family: string;
}

interface Catalog {
  catalogVersion: number;
  entries: CatalogEntry[];
}

interface ReleasedManifest {
  currentVersion: number;
  acceptedVersions: number[];
  emittedVersions: number[];
  versions: Array<{ version: number }>;
}

const root = new URL("..", import.meta.url).pathname;
const catalog: Catalog = JSON.parse(
  readFileSync(
    join(root, "../../apps/web/app/assets/fonts/catalog.json"),
    "utf8",
  ),
);
const currentSchema = JSON.parse(
  readFileSync(join(root, "resume.schema.json"), "utf8"),
);
const released: ReleasedManifest = JSON.parse(
  readFileSync(join(root, "released-versions.json"), "utf8"),
);

const fontEnum =
  currentSchema.$defs.customization.properties.font.properties.family.enum;
const catalogIDs = catalog.entries.map((entry) => entry.id);

describe("document v2 font catalog release", () => {
  it("releases the full catalog in manifest order", () => {
    expect(catalog.catalogVersion).toBe(2);
    expect(catalogIDs).toHaveLength(26);
    expect(new Set(catalogIDs).size).toBe(catalogIDs.length);
    expect(fontEnum).toEqual(catalogIDs);
  });

  it("declares current, accepted, and emitted versions independently", () => {
    expect(released.versions.map(({ version }) => version)).toEqual([1, 2]);
    expect(released.currentVersion).toBe(2);
    expect(released.acceptedVersions).toEqual([1, 2]);
    expect(released.emittedVersions).toEqual([1, 2]);
    expect(CURRENT_VERSION).toBe(2);
    expect(ACCEPTED_VERSIONS).toEqual([1, 2]);
    expect(EMITTED_VERSIONS).toEqual([1, 2]);
    expect(Object.isFrozen(ACCEPTED_VERSIONS)).toBe(true);
    expect(Object.isFrozen(EMITTED_VERSIONS)).toBe(true);
  });

  it("keeps the immutable v1 schema bytes unchanged", () => {
    const digest = createHash("sha256")
      .update(readFileSync(join(root, "resume.v1.schema.json")))
      .digest("hex");
    expect(digest).toBe(
      "879858284bc3cb4092d1d671466a9c620e27abf134aecedce070b6f21e4e5866",
    );
  });

  it("gives every catalog entry a valid explicit v1 fallback", () => {
    const v1Families = new Set([
      "Be Vietnam Pro",
      "Inter",
      "Source Sans 3",
      "Alegreya",
      "Roboto Serif",
    ]);
    expect(catalog.entries.map(({ v1Family }) => v1Family)).toHaveLength(26);
    for (const entry of catalog.entries) {
      expect(v1Families.has(entry.v1Family), entry.id).toBe(true);
    }
  });

  it("updates all 20 presets to stable catalog IDs", () => {
    const templateDir = join(root, "templates");
    const templates = readdirSync(templateDir)
      .filter((name) => name.endsWith(".json"))
      .sort();
    expect(templates).toHaveLength(20);
    for (const name of templates) {
      const preset = JSON.parse(readFileSync(join(templateDir, name), "utf8"));
      expect(catalogIDs, name).toContain(preset.customization.font.family);
    }
  });
});
