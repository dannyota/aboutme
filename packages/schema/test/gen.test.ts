import { execFileSync } from "node:child_process";
import { readFileSync } from "node:fs";
import { describe, expect, it } from "vitest";

// Go and TypeScript outputs are committed; Dart remains deferred. The manifest
// supplies every retained version so a new release cannot be omitted from this
// drift check. rawschema.go is included because generation would otherwise
// overwrite stale embedded bytes before its Go content test sees them.
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
  // Codegen can exceed Vitest's five-second default under concurrent load.
  it("is byte-identical to a fresh generation", () => {
    const before = files.map((f) => readFileSync(f, "utf8"));
    execFileSync("node", ["scripts/generate.mjs"], { stdio: "inherit" });
    const after = files.map((f) => readFileSync(f, "utf8"));
    expect(after).toEqual(before);
  }, 30_000);

  it("marks every file as generated", () => {
    for (const f of files) {
      expect(readFileSync(f, "utf8")).toContain("DO NOT EDIT");
    }
  });

  // json-schema-to-typescript suffixes an alias when it fails to recognize an
  // existing $def. Reject the whole class of misleading duplicate aliases.
  it.each(["gen/ts/resume.ts", ...released.map((entry) => entry.tsTypes)])(
    "%s declares no counter-suffixed duplicate of another generated TS type",
    (path) => {
      const ts = readFileSync(path, "utf8");
      const declared = new Set(
        [
          ...ts.matchAll(
            /^export (?:type|interface) ([A-Za-z][A-Za-z0-9]*)\b/gm,
          ),
        ].map((m) => m[1]),
      );
      const duplicates = [...declared].filter((name) => {
        const stem = name.replace(/\d+$/, "");
        return stem !== name && declared.has(stem);
      });
      expect(
        duplicates,
        `duplicate aliases in ${path}: ${duplicates.join(", ")}`,
      ).toEqual([]);
    },
  );
});
