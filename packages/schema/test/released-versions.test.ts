import { execFileSync } from "node:child_process";
import { existsSync, readFileSync } from "node:fs";
import { describe, expect, it } from "vitest";

import {
  CURRENT_VERSION,
  RELEASED_SCHEMAS,
  isReleasedVersion,
  releasedSchema,
} from "../gen/ts/released";

// Design spec §3, "Wire-version compatibility" (AC-DOC-012): every released
// schema_version keeps an IMMUTABLE schema file plus generated types
// (resume.v<N>.schema.json). The policy applies from v1, before a second
// version exists — that is the whole point of this file. It proves four
// things that together make the append-only policy enforceable today:
//
//  1. resume.v1.schema.json is the released snapshot of the bytes the
//     current contract actually ships. While CURRENT_VERSION is 1 it must
//     byte-equal resume.schema.json, so nobody can quietly edit the working
//     file and leave the released one behind (or vice versa).
//  2. Every released version carries version-scoped Go and TS output, so a
//     released schema can never exist without retained types.
//  3. The registry enumerates exactly the released versions — no implicit
//     newest-file discovery anywhere in the pipeline.
//  4. An unknown version fails CLOSED (throws), never silently degrading to
//     "probably the current one". docmigrate's identity projector already
//     fails closed on an unrecognised stored version; this is the same rule
//     applied to the schema registry.
//
// The complementary Go-side assertions (schema.RawSchema derives from v1,
// ReleasedSchemaFor fails closed) live in gen/go/released_test.go, and the
// regenerate-and-byte-compare drift check covering every file below lives in
// gen.test.ts.

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

const manifest: Manifest = JSON.parse(readFileSync("released-versions.json", "utf8"));

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
    expect(manifest.versions.map((v) => v.version)).toContain(manifest.currentVersion);
  });
});

describe("immutable released schema", () => {
  // The one moment resume.schema.json and the current released snapshot are
  // allowed to differ is a version bump, and a version bump adds a new
  // resume.v<N>.schema.json in the same change — so this holds at every
  // commit, not just this one.
  it("keeps resume.v1.schema.json byte-identical to resume.schema.json while CURRENT_VERSION is 1", () => {
    expect(CURRENT_VERSION).toBe(1);
    const current = readFileSync("resume.schema.json");
    const released = readFileSync("resume.v1.schema.json");
    expect(released.equals(current)).toBe(true);
  });

  it("retains version-scoped Go and TS output for every released version", () => {
    for (const entry of manifest.versions) {
      expect(existsSync(`${entry.goPackage}/resume.go`), `${entry.goPackage}/resume.go`).toBe(
        true,
      );
      expect(
        existsSync(`${entry.goPackage}/rawschema.go`),
        `${entry.goPackage}/rawschema.go`,
      ).toBe(true);
      expect(existsSync(entry.tsTypes), entry.tsTypes).toBe(true);
    }
  });

  // Derivation, not coincidence: the current convenience output and the v1
  // snapshot come off the same generator over byte-identical inputs, so
  // everything below the "generated from <file>" header line must match
  // exactly. If someone hand-edits gen/ts/resume.ts, this fails even though
  // both files would still regenerate deterministically on their own.
  it("derives the current TS output from the v1 schema while CURRENT_VERSION is 1", () => {
    const dropHeader = (path: string) => readFileSync(path, "utf8").split("\n").slice(1).join("\n");
    expect(dropHeader("gen/ts/v1/resume.ts")).toBe(dropHeader("gen/ts/resume.ts"));
    expect(readFileSync("gen/ts/v1/resume.ts", "utf8").split("\n")[0]).toBe(
      "// Code generated from resume.v1.schema.json. DO NOT EDIT.",
    );
  });

  it("embeds the v1 schema's exact bytes in the retained v1 Go package", () => {
    const source = readFileSync("gen/go/v1/rawschema.go", "utf8");
    const base64 = [...source.matchAll(/^\t"([A-Za-z0-9+/=]*)" \+$/gm)].map((m) => m[1]).join("");
    expect(Buffer.from(base64, "base64").equals(readFileSync("resume.v1.schema.json"))).toBe(true);
  });
});

describe("released-schema registry (TypeScript)", () => {
  it("contains exactly the manifest's released versions", () => {
    expect(RELEASED_SCHEMAS.map((s) => s.version)).toEqual(manifest.versions.map((v) => v.version));
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
    for (const version of [0, 2, -1, 1.5, Number.NaN, Number.MAX_SAFE_INTEGER]) {
      expect(() => releasedSchema(version), `releasedSchema(${version})`).toThrow(
        /unknown released schema version/i,
      );
      expect(isReleasedVersion(version), `isReleasedVersion(${version})`).toBe(false);
    }
  });

  it("hands out a defensive copy, so a caller cannot mutate the registry", () => {
    const first = releasedSchema(1);
    (first as { schema: string }).schema = "tampered";
    expect(releasedSchema(1).schema).toBe("resume.v1.schema.json");
  });
});

describe("retained v1 TypeScript types", () => {
  // gen/ts/v1/resume.ts is a retained snapshot: nothing in the running app
  // imports it yet, so without this nothing would notice if it stopped
  // compiling. Same mechanism as type-fidelity.test.ts — `tsc --noEmit
  // --strict` fails both on an unexpected error and on a @ts-expect-error
  // that does not actually error.
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
