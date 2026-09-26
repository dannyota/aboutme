// Embeds a schema file's exact bytes into a generated Go source constant.
// resume.schema.json (and each released version's schema) is outside gen/go's
// module, so go:embed cannot reach it — this is the substitute.

import { generatedHeader, writeGenerated } from "./generatePaths.mjs";

// Splits a base64 string into fixed-width lines so the generated Go source
// doesn't put resume.schema.json's ~29 KB of encoded bytes on a single line
// (readability/diffability only — Go itself doesn't care about line length
// here).
function chunkBase64(base64, width = 96) {
  const lines = [];
  for (let i = 0; i < base64.length; i += width) {
    lines.push(base64.slice(i, i + width));
  }
  return lines;
}

// Base64, not a Go string literal: resume.schema.json contains a literal
// backtick (inside a description string), which raw string literals cannot
// contain at all, and non-ASCII bytes that would need per-rune escaping in
// an interpreted string literal — both are exactly the kind of fragile,
// easy-to-get-subtly-wrong transcription this generator should not need to
// hand-roll. Base64's alphabet has neither problem, so the embedding is a
// straight, deterministic transcoding of rawSchemaBytes with no escaping
// logic at all.
export function generateRawSchema(
  rawSchemaBytes,
  outFile,
  { packageName, sourceName, verifiedBy },
) {
  const base64Lines = chunkBase64(rawSchemaBytes.toString("base64"));
  const literal = base64Lines.map((line) => `\t"${line}" +\n`).join("");

  const body = `${generatedHeader(sourceName)}

package ${packageName}

import "encoding/base64"

// rawSchemaBase64 is ${sourceName}'s exact bytes, base64-encoded at
// generation time (decision D2) — see this file's generator
// (scripts/generate.mjs's generateRawSchema) for why base64 instead of a
// plain Go string literal.
const rawSchemaBase64 = "" +
${literal}\t""

// RawSchema is ${sourceName}'s exact byte content, decoded once at
// package init from rawSchemaBase64 above. ${sourceName} lives outside
// this module (gen/go/go.mod), so go:embed cannot reach it directly — this
// generated constant is the substitute. ${verifiedBy}
var RawSchema = mustDecodeRawSchemaBase64()

func mustDecodeRawSchemaBase64() []byte {
	decoded, err := base64.StdEncoding.DecodeString(rawSchemaBase64)
	if err != nil {
		// Unreachable for a generated, unedited file: rawSchemaBase64 above is
		// produced by encoding/base64's own encoder in scripts/generate.mjs's
		// generateRawSchema (Node's Buffer#toString("base64") — the same
		// alphabet, no hand-editing in between).
		panic("schema: rawSchemaBase64 failed to decode: " + err.Error())
	}
	return decoded
}
`;

  writeGenerated(outFile, body, "go");
}
