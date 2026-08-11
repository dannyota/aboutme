import { execFileSync } from "node:child_process";
import { readFileSync } from "node:fs";
import { describe, expect, it } from "vitest";

// Dart generation is out of scope for Phase 0A (mobile deferred); only
// Go and TS are generated. gen/go/rawschema.go (decision D2) is included here
// too — this is the only check that would catch a committed rawschema.go
// going stale relative to resume.schema.json without this file's own
// regeneration step silently overwriting the drift first (see
// gen/go/rawschema_test.go's header for the complementary, narrower content
// check: it proves RawSchema's bytes match resume.schema.json, but only
// AFTER whatever ran immediately before it — if that was this very test,
// the drift is already gone by the time rawschema_test.go looks).
//
// The retained per-version snapshots (gen/go/v<N>/*, gen/ts/v<N>/resume.ts)
// and the two generated registries are derived from released-versions.json
// rather than listed here, so releasing a version cannot leave its snapshot
// out of the drift check by omission — the manifest is the same input
// scripts/generate.mjs itself reads. AC-DOC-012 requires the retained output
// to stay mechanically derived from its immutable schema; this is what proves
// it still is.
interface ReleasedEntry {
  version: number;
  schema: string;
  goPackage: string;
  tsTypes: string;
}

const released: ReleasedEntry[] = JSON.parse(
  readFileSync("released-versions.json", "utf8"),
).versions;

const files = [
  "gen/go/resume.go",
  "gen/ts/resume.ts",
  "gen/go/rawschema.go",
  "gen/go/released.go",
  "gen/ts/released.ts",
  ...released.flatMap((entry) => [
    `${entry.goPackage}/resume.go`,
    `${entry.goPackage}/rawschema.go`,
    entry.tsTypes,
  ]),
];

describe("generated code", () => {
  it("is byte-identical to a fresh generation", () => {
    const before = files.map((f) => readFileSync(f, "utf8"));
    execFileSync("node", ["scripts/generate.mjs"], { stdio: "inherit" });
    const after = files.map((f) => readFileSync(f, "utf8"));
    expect(after).toEqual(before);
  });

  it("marks every file as generated", () => {
    for (const f of files) {
      expect(readFileSync(f, "utf8")).toContain("DO NOT EDIT");
    }
  });

  // json-schema-to-typescript names a schema node it cannot recognise as an
  // already-seen $def by appending a counter: `HexColor1` beside `HexColor`,
  // `Link1` beside `Link`. Both are dangling exported aliases for one type,
  // and a property pointed at the duplicate reads as if it were a different
  // type than its siblings. Two separate schema shapes have caused this so
  // far — `link`'s `type`-plus-`anyOf` pair (buildSharedCodegenSchema fix 3)
  // and a `$ref` carrying a sibling `description` (buildTsCodegenSchema) —
  // so this asserts the *class* is absent rather than either instance.
  it.each(["gen/ts/resume.ts", ...released.map((entry) => entry.tsTypes)])(
    "%s declares no counter-suffixed duplicate of another generated TS type",
    (path) => {
      const ts = readFileSync(path, "utf8");
      const declared = new Set(
        [...ts.matchAll(/^export (?:type|interface) ([A-Za-z][A-Za-z0-9]*)\b/gm)].map(
          (m) => m[1],
        ),
      );
      const duplicates = [...declared].filter((name) => {
        const stem = name.replace(/\d+$/, "");
        return stem !== name && declared.has(stem);
      });
      expect(duplicates, `duplicate aliases in ${path}: ${duplicates.join(", ")}`).toEqual([]);
    },
  );
});
