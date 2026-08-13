import { execFileSync } from "node:child_process";
import { createHash } from "node:crypto";
import { existsSync, readFileSync } from "node:fs";
import { describe, expect, it } from "vitest";

import {
  CURRENT_VERSION,
  ACCEPTED_VERSIONS,
  EMITTED_VERSIONS,
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
  acceptedVersions: number[];
  emittedVersions: number[];
  versions: ManifestEntry[];
}

const manifest: Manifest = JSON.parse(
  readFileSync("released-versions.json", "utf8"),
);

describe("released-version manifest", () => {
  it("declares versions 1 and 2 as released, and 2 as current", () => {
    expect(manifest.versions.map((v) => v.version)).toEqual([1, 2]);
    expect(manifest.currentVersion).toBe(2);
    expect(manifest.acceptedVersions).toEqual([1, 2]);
    expect(manifest.emittedVersions).toEqual([1, 2]);
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

  it("declares only released wire versions", () => {
    const releasedVersions = new Set(
      manifest.versions.map(({ version }) => version),
    );
    for (const [name, versions] of [
      ["acceptedVersions", manifest.acceptedVersions],
      ["emittedVersions", manifest.emittedVersions],
    ] as const) {
      expect(versions).not.toHaveLength(0);
      expect(new Set(versions).size, name).toBe(versions.length);
      for (const version of versions) {
        expect(releasedVersions.has(version), `${name}: ${version}`).toBe(true);
      }
      expect(versions, name).toContain(manifest.currentVersion);
    }
  });
});

describe("immutable released schema", () => {
  it("keeps retained generated types byte-identical after release", () => {
    const sha256 = (path: string) =>
      createHash("sha256").update(readFileSync(path)).digest("hex");
    const releasedHashes = new Map([
      [
        "gen/go/v1/resume.go",
        "e5221be71b5560cfb812f66c57516cc417c32d0529e8e0f1d915eb62d4b44703",
      ],
      [
        "gen/go/v1/rawschema.go",
        "8e29326a32081559c9989c43969094a1a8a507da6b7a4eaebb3088ba83501708",
      ],
      [
        "gen/ts/v1/resume.ts",
        "62851f1baad5d77bb138cd45da72e8f317c28875b6c01a999b62487e8f1213cb",
      ],
      [
        "gen/go/v2/resume.go",
        "402cd45ccf3fce44fd7fb2f1f841bbf0d11f1b35abbe7c2822d0f5585f05c313",
      ],
      [
        "gen/go/v2/rawschema.go",
        "b73319c9f11c41f22874fc3af474ade3198ef96bb6f456d30a1c6f77f7a703b5",
      ],
      [
        "gen/ts/v2/resume.ts",
        "d9978ca487c97c0e9945cc6120c53fbc234fadd7dcfe81ec8615007d10422a21",
      ],
    ]);
    for (const [path, expected] of releasedHashes) {
      expect(sha256(path), path).toBe(expected);
    }
  });

  // A version bump adds a new snapshot and changes CURRENT_VERSION together.
  it("keeps resume.v2.schema.json byte-identical to resume.schema.json while CURRENT_VERSION is 2", () => {
    expect(CURRENT_VERSION).toBe(2);
    const current = readFileSync("resume.schema.json");
    const released = readFileSync("resume.v2.schema.json");
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
  it("derives the current TS output from the v2 schema while CURRENT_VERSION is 2", () => {
    const dropHeader = (path: string) =>
      readFileSync(path, "utf8").split("\n").slice(1).join("\n");
    expect(dropHeader("gen/ts/v2/resume.ts")).toBe(
      dropHeader("gen/ts/resume.ts"),
    );
    expect(readFileSync("gen/ts/v2/resume.ts", "utf8").split("\n")[0]).toBe(
      "// Code generated from resume.v2.schema.json. DO NOT EDIT.",
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
    expect(ACCEPTED_VERSIONS).toEqual(manifest.acceptedVersions);
    expect(EMITTED_VERSIONS).toEqual(manifest.emittedVersions);
  });

  it("resolves a released version to its immutable schema and retained types", () => {
    const v1 = releasedSchema(1);
    expect(v1.schema).toBe("resume.v1.schema.json");
    expect(v1.goPackage).toBe("gen/go/v1");
    expect(v1.tsTypes).toBe("gen/ts/v1/resume.ts");
    expect(isReleasedVersion(1)).toBe(true);
    const v2 = releasedSchema(2);
    expect(v2.schema).toBe("resume.v2.schema.json");
    expect(v2.goPackage).toBe("gen/go/v2");
    expect(v2.tsTypes).toBe("gen/ts/v2/resume.ts");
    expect(isReleasedVersion(2)).toBe(true);
  });

  it("fails closed on an unreleased, malformed, or out-of-range version", () => {
    for (const version of [
      0,
      3,
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
