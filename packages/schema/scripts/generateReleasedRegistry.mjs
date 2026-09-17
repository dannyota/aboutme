// Reads and validates released-versions.json, then emits the Go and
// TypeScript halves of the released-version registry it describes.

import { execFileSync } from "node:child_process";
import { existsSync, readFileSync, writeFileSync } from "node:fs";
import { join } from "node:path";

import {
  generatedHeader,
  GO_MODULE_PATH,
  manifestPath,
  packageRoot,
} from "./generatePaths.mjs";

// released-versions.json is the only release registry. Do not infer releases
// from filenames, because a stray file must not become a contract. Validate its
// structure here so failures name the manifest field, not generated output.
export function readReleasedManifest() {
  const manifest = JSON.parse(readFileSync(manifestPath, "utf8"));
  const versions = manifest.versions;
  if (!Array.isArray(versions) || versions.length === 0) {
    throw new Error(
      "generate.mjs: released-versions.json's `versions` is missing or empty.",
    );
  }

  let previous = 0;
  for (const entry of versions) {
    const { version, schema, goPackage, tsTypes } = entry ?? {};
    if (!Number.isInteger(version) || version < 1) {
      throw new Error(
        `generate.mjs: released-versions.json has a non-integer or non-positive version ${JSON.stringify(version)}.`,
      );
    }
    if (version <= previous) {
      throw new Error(
        `generate.mjs: released-versions.json's versions must ascend; ${version} follows ${previous}.`,
      );
    }
    previous = version;

    // The conventional paths are asserted, not derived: a released entry
    // states its own file explicitly, and this catches a typo pointing an
    // entry at another version's schema (which would silently regenerate v<N>
    // from v<M>'s bytes — the exact drift the immutability policy forbids).
    const expected = {
      schema: `resume.v${version}.schema.json`,
      goPackage: `gen/go/v${version}`,
      tsTypes: `gen/ts/v${version}/resume.ts`,
    };
    for (const [field, want] of Object.entries(expected)) {
      const got = { schema, goPackage, tsTypes }[field];
      if (got !== want) {
        throw new Error(
          `generate.mjs: released-versions.json version ${version} declares ${field} ${JSON.stringify(got)}, want ${JSON.stringify(want)}.`,
        );
      }
    }
    if (!existsSync(join(packageRoot, schema))) {
      throw new Error(
        `generate.mjs: released-versions.json version ${version} names ${schema}, which does not exist.`,
      );
    }
  }

  if (!versions.some((entry) => entry.version === manifest.currentVersion)) {
    throw new Error(
      `generate.mjs: released-versions.json's currentVersion ${JSON.stringify(manifest.currentVersion)} is not one of the released versions.`,
    );
  }

  const releasedVersions = new Set(versions.map((entry) => entry.version));
  for (const field of ["acceptedVersions", "emittedVersions"]) {
    const declared = manifest[field];
    if (!Array.isArray(declared) || declared.length === 0) {
      throw new Error(
        `generate.mjs: released-versions.json's ${field} must be a non-empty array.`,
      );
    }
    let previousDeclared = 0;
    for (const version of declared) {
      if (!Number.isInteger(version) || version < 1) {
        throw new Error(
          `generate.mjs: released-versions.json's ${field} contains invalid version ${JSON.stringify(version)}.`,
        );
      }
      if (version <= previousDeclared) {
        throw new Error(
          `generate.mjs: released-versions.json's ${field} must ascend without duplicates; ${version} follows ${previousDeclared}.`,
        );
      }
      if (!releasedVersions.has(version)) {
        throw new Error(
          `generate.mjs: released-versions.json's ${field} declares unreleased version ${version}.`,
        );
      }
      previousDeclared = version;
    }
    if (!declared.includes(manifest.currentVersion)) {
      throw new Error(
        `generate.mjs: released-versions.json's currentVersion ${manifest.currentVersion} is absent from ${field}.`,
      );
    }
  }

  return manifest;
}

// The Go half of the released-version registry. It lives in the CURRENT
// package (gen/go, package schema) and imports one retained package per
// released version, so `go build ./...` in that module is itself the proof
// that every retained snapshot still compiles.
//
// Lookup fails closed: an unreleased version returns ErrUnknownSchemaVersion
// rather than the nearest or newest match, mirroring docmigrate's identity
// projector, which errors on a stored schema_version it has no converter for
// instead of passing the document through unconverted.
export function generateReleasedGo(manifest, outFile) {
  const imports = manifest.versions
    .map(
      (entry) =>
        `\tschemav${entry.version} "${GO_MODULE_PATH}/v${entry.version}"`,
    )
    .join("\n");

  const entries = manifest.versions
    .map(
      (entry) => `\t{
\t\tVersion:   ${entry.version},
\t\tSchema:    "${entry.schema}",
\t\tGoPackage: "${entry.goPackage}",
\t\tTSTypes:   "${entry.tsTypes}",
\t\tRawSchema: schemav${entry.version}.RawSchema,
\t},`,
    )
    .join("\n");

  const body = `${generatedHeader("released-versions.json")}

package schema

import (
	"bytes"
	"errors"
	"fmt"

${imports}
)

// CurrentVersion is the document-shape version resume.schema.json currently
// describes, and the version every stored resume is projected to on read and
// persisted at on write. It matches apps/server's docmigrate.CurrentVersion
// because the production projector consumes this generated declaration.
const CurrentVersion = ${manifest.currentVersion}

var acceptedVersions = []int{${manifest.acceptedVersions.join(", ")}}
var emittedVersions = []int{${manifest.emittedVersions.join(", ")}}

// AcceptedVersions returns the independently declared wire versions accepted
// by production. The returned slice is a copy.
func AcceptedVersions() []int { return append([]int(nil), acceptedVersions...) }

// EmittedVersions returns the independently declared wire versions emitted by
// production. The returned slice is a copy.
func EmittedVersions() []int { return append([]int(nil), emittedVersions...) }

// ReleasedSchema is one released document-shape version: its immutable schema
// file and the retained generated types derived from that file. Released
// entries are append-only (design spec §3, "Wire-version compatibility"), so
// a value of this type describes a contract that can never change, only be
// superseded.
type ReleasedSchema struct {
	// Version is the released schema_version this entry describes.
	Version int
	// Schema is the immutable schema file's path, relative to packages/schema.
	Schema string
	// GoPackage is the retained Go types' path, relative to packages/schema.
	GoPackage string
	// TSTypes is the retained TypeScript types' path, relative to
	// packages/schema.
	TSTypes string
	// RawSchema is the schema file's exact byte content.
	RawSchema []byte
}

// ErrUnknownSchemaVersion is returned for a version this build has no
// released schema for. Callers must treat it as a hard failure: a document
// claiming an unreleased version cannot be validated, converted, or emitted,
// and guessing a nearby version would persist a document under a contract
// nothing describes.
var ErrUnknownSchemaVersion = errors.New("schema: unknown released schema version")

// releasedSchemas is generated from released-versions.json, ascending by
// version.
var releasedSchemas = []ReleasedSchema{
${entries}
}

// ReleasedVersions returns every released document-shape version in
// ascending order. The returned slice is freshly allocated, so a caller
// cannot reorder or truncate the registry for everyone else.
func ReleasedVersions() []int {
	versions := make([]int, len(releasedSchemas))
	for i, released := range releasedSchemas {
		versions[i] = released.Version
	}
	return versions
}

// ReleasedSchemaFor returns the released schema for version. It fails closed:
// an unreleased version yields an error wrapping ErrUnknownSchemaVersion and
// a zero ReleasedSchema, never a fallback to the current or nearest version.
// The returned RawSchema is a copy, so a caller cannot mutate the immutable
// bytes the retained package holds.
func ReleasedSchemaFor(version int) (ReleasedSchema, error) {
	for _, released := range releasedSchemas {
		if released.Version == version {
			released.RawSchema = bytes.Clone(released.RawSchema)
			return released, nil
		}
	}
	return ReleasedSchema{}, fmt.Errorf("%w: %d", ErrUnknownSchemaVersion, version)
}
`;

  writeFileSync(outFile, body);
  execFileSync("gofmt", ["-w", outFile], { stdio: "inherit" });
}

// The TypeScript half of the released-version registry. It carries the
// manifest rather than the schema bytes: apps/web imports this package for
// types, and inlining every released schema's ~36 KB into the bundle to
// satisfy a build-time registry would be a real cost for no runtime gain. The
// Go side (gen/go/released.go) is where the bytes live, because that is the
// side that actually compiles and validates documents.
export function generateReleasedTs(manifest, outFile) {
  const entries = manifest.versions
    .map(
      (entry) => `  Object.freeze({
    version: ${entry.version},
    schema: "${entry.schema}",
    goPackage: "${entry.goPackage}",
    tsTypes: "${entry.tsTypes}",
  }),`,
    )
    .join("\n");

  const body = `${generatedHeader("released-versions.json")}

/**
 * One released document-shape version: its immutable schema file and the
 * retained generated types derived from that file. Paths are relative to
 * packages/schema. Released entries are append-only (design spec §3,
 * "Wire-version compatibility").
 */
export interface ReleasedSchema {
  readonly version: number;
  readonly schema: string;
  readonly goPackage: string;
  readonly tsTypes: string;
}

/**
 * The document-shape version resume.schema.json currently describes.
 */
export const CURRENT_VERSION = ${manifest.currentVersion};

/** Wire versions accepted by production, authored independently of releases. */
export const ACCEPTED_VERSIONS: readonly number[] = Object.freeze([${manifest.acceptedVersions.join(", ")}]);

/** Wire versions emitted by production, authored independently of releases. */
export const EMITTED_VERSIONS: readonly number[] = Object.freeze([${manifest.emittedVersions.join(", ")}]);

/**
 * Every released version, ascending. Frozen: the registry is a contract, not
 * a mutable cache.
 */
export const RELEASED_SCHEMAS: readonly ReleasedSchema[] = Object.freeze([
${entries}
]);

/**
 * Reports whether \`version\` has been released. Non-integer, negative, and
 * NaN inputs are simply not released, so callers need no separate guard.
 */
export function isReleasedVersion(version: number): boolean {
  return RELEASED_SCHEMAS.some((released) => released.version === version);
}

/**
 * Resolves a released version. Fails closed: an unreleased version throws
 * rather than falling back to the current or nearest one, because a document
 * claiming an unreleased version cannot be validated or converted at all.
 * The result is a copy, so a caller cannot retarget the registry.
 */
export function releasedSchema(version: number): ReleasedSchema {
  const released = RELEASED_SCHEMAS.find((candidate) => candidate.version === version);
  if (released === undefined) {
    throw new Error(\`schema: unknown released schema version \${version}\`);
  }
  return { ...released };
}
`;

  writeFileSync(outFile, body);
}
