import { existsSync, readdirSync, readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { join, resolve } from "node:path";
import { describe, expect, it } from "vitest";

const schemaRoot = fileURLToPath(new URL("..", import.meta.url));
const repositoryRoot = resolve(schemaRoot, "../..");
const colorAuthority = "docs/design/templates/colors.md";
const schemaFiles = readdirSync(schemaRoot)
  .filter(
    (name) =>
      name === "resume.schema.json" ||
      /^resume\.v\d+\.schema\.json$/.test(name),
  )
  .sort();

const stringsIn = (value: unknown): string[] => {
  if (typeof value === "string") return [value];
  if (Array.isArray(value)) return value.flatMap(stringsIn);
  if (value !== null && typeof value === "object") {
    return Object.values(value).flatMap(stringsIn);
  }
  return [];
};

describe.each(schemaFiles)("%s documentation citations", (schemaFile) => {
  it("uses existing current authority paths", () => {
    const schema = JSON.parse(
      readFileSync(join(schemaRoot, schemaFile), "utf8"),
    ) as unknown;
    const citations = [
      ...new Set(
        stringsIn(schema).flatMap(
          (text) => text.match(/\bdocs\/[A-Za-z0-9._/-]+\.md\b/g) ?? [],
        ),
      ),
    ];

    expect(
      citations.length,
      `${schemaFile} must retain its documentation citation`,
    ).toBeGreaterThan(0);

    for (const citation of citations) {
      expect(
        citation.startsWith("docs/specs/"),
        `${schemaFile} cites retired path ${citation}`,
      ).toBe(false);
      expect(
        existsSync(join(repositoryRoot, citation)),
        `${schemaFile} cites missing path ${citation}`,
      ).toBe(true);
    }

    expect(
      citations,
      `${schemaFile} must cite the color customization authority`,
    ).toContain(colorAuthority);
  });
});
