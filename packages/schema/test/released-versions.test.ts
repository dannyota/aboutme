import { execFileSync } from "node:child_process";
import { existsSync, readFileSync } from "node:fs";
import { describe, expect, it } from "vitest";

import {
  CURRENT_VERSION,
  RELEASED_SCHEMAS,
  isReleasedVersion,
  releasedSchema,
} from "../gen/ts/released";

// Each released document version keeps an immutable schema and retained Go and
// TypeScript types. The manifest is the only release registry, and unknown
// versions fail closed. See docs/design/data.md#document-versions.

interface ManifestEntry {
  version: number;
  schema: string;
  goPackage: string;
  tsTypes: string;
}

interface Manifest {
  currentVersion: number;
  versions: ManifestEntry[];
}

const manifest: Manifest = JSON.parse(
  readFileSync("released-versions.json", "utf8"),
);

describe("released-version manifest", () => {
  it("declares exactly version 1 as released, and 1 as current", () => {
    expect(manifest.versions.map((v) => v.version)).toEqual([1]);
    expect(manifest.currentVersion).toBe(1);
  });

  it("declares an explicit schema path per released version, never a discovered one", () => {
    for (const entry of manifest.versions) {
      expect(entry.schema).toBe(`resume.v${entry.version}.schema.json`);
      expect(existsSync(entry.schema), `${entry.schema} is missing`).toBe(true);
    }
  });

  it("names a current version that is itself released", () => {
    expect(manifest.versions.map((v) => v.version)).toContain(
      manifest.currentVersion,
    );
  });
});

describe("immutable released schema", () => {
  // A version bump adds a new snapshot and changes CURRENT_VERSION together.
  it("keeps resume.v1.schema.json byte-identical to resume.schema.json while CURRENT_VERSION is 1", () => {
    expect(CURRENT_VERSION).toBe(1);
    const current = readFileSync("resume.schema.json");
    const released = readFileSync("resume.v1.schema.json");
    expect(released.equals(current)).toBe(true);
  });

  it("retains version-scoped Go and TS output for every released version", () => {
    for (const entry of manifest.versions) {
      expect(
        existsSync(`${entry.goPackage}/resume.go`),
        `${entry.goPackage}/resume.go`,
      ).toBe(true);
      expect(
        existsSync(`${entry.goPackage}/rawschema.go`),
        `${entry.goPackage}/rawschema.go`,
      ).toBe(true);
      expect(existsSync(entry.tsTypes), entry.tsTypes).toBe(true);
    }
  });

  // Byte-identical inputs must produce identical bodies after the source
  // header. This also detects a hand edit to either generated file.
  it("derives the current TS output from the v1 schema while CURRENT_VERSION is 1", () => {
    const dropHeader = (path: string) =>
      readFileSync(path, "utf8").split("\n").slice(1).join("\n");
    expect(dropHeader("gen/ts/v1/resume.ts")).toBe(
      dropHeader("gen/ts/resume.ts"),
    );
    expect(readFileSync("gen/ts/v1/resume.ts", "utf8").split("\n")[0]).toBe(
      "// Code generated from resume.v1.schema.json. DO NOT EDIT.",
    );
  });

  it("embeds the v1 schema's exact bytes in the retained v1 Go package", () => {
    const source = readFileSync("gen/go/v1/rawschema.go", "utf8");
    const base64 = [...source.matchAll(/^\t"([A-Za-z0-9+/=]*)" \+$/gm)]
      .map((m) => m[1])
      .join("");
    expect(
      Buffer.from(base64, "base64").equals(
        readFileSync("resume.v1.schema.json"),
      ),
    ).toBe(true);
  });
});

describe("released-schema registry (TypeScript)", () => {
  it("contains exactly the manifest's released versions", () => {
    expect(RELEASED_SCHEMAS.map((s) => s.version)).toEqual(
      manifest.versions.map((v) => v.version),
    );
    expect(CURRENT_VERSION).toBe(manifest.currentVersion);
  });

  it("resolves a released version to its immutable schema and retained types", () => {
    const v1 = releasedSchema(1);
    expect(v1.schema).toBe("resume.v1.schema.json");
    expect(v1.goPackage).toBe("gen/go/v1");
    expect(v1.tsTypes).toBe("gen/ts/v1/resume.ts");
    expect(isReleasedVersion(1)).toBe(true);
  });

  it("fails closed on an unreleased, malformed, or out-of-range version", () => {
    for (const version of [
      0,
      2,
      -1,
      1.5,
      Number.NaN,
      Number.MAX_SAFE_INTEGER,
    ]) {
      expect(
        () => releasedSchema(version),
        `releasedSchema(${version})`,
      ).toThrow(/unknown released schema version/i);
      expect(isReleasedVersion(version), `isReleasedVersion(${version})`).toBe(
        false,
      );
    }
  });

  it("hands out a defensive copy, so a caller cannot mutate the registry", () => {
    const first = releasedSchema(1);
    (first as { schema: string }).schema = "tampered";
    expect(releasedSchema(1).schema).toBe("resume.v1.schema.json");
  });
});

describe("retained v1 TypeScript types", () => {
  // The application does not import retained v1 types, so compile them here.
  it("compile independently under tsc --strict", () => {
    expect(() =>
      execFileSync(
        "node_modules/.bin/tsc",
        ["--noEmit", "--strict", "test/released-v1.fixture.ts"],
        { stdio: "pipe" },
      ),
    ).not.toThrow();
  });
});
