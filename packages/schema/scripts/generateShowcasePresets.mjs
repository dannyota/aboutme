// Emits the Go preset table the community showcase uses to derive a resume's
// template (docs/design/showcase.md "What a listing shows"). Each entry holds
// the preset's customization in stored form, minus the leaves a template apply
// keeps or resets for the owner and minus placement, as canonical JSON. The Go
// side re-canonicalizes it and compares it to a resume's customization stripped
// the same way. apps/server/internal/showcase/presets_test.go rebuilds the
// table from templates/*.json and fails when this output is stale.

import { dirname, join } from "node:path";

import {
  generatedHeader,
  packageRoot,
  writeGenerated,
} from "./generatePaths.mjs";

export const showcasePresetsGoPath = join(
  dirname(dirname(packageRoot)),
  "apps",
  "server",
  "internal",
  "showcase",
  "presets.generated.go",
);

// canonicalJson serializes value with object keys in code-unit order, so equal
// customizations always produce equal text.
function canonicalJson(value) {
  if (Array.isArray(value)) {
    return `[${value.map(canonicalJson).join(",")}]`;
  }
  if (value !== null && typeof value === "object") {
    const keys = Object.keys(value).sort();
    return `{${keys
      .map((key) => `${JSON.stringify(key)}:${canonicalJson(value[key])}`)
      .join(",")}}`;
  }
  return JSON.stringify(value);
}

// matchForm drops the five leaves the showcase ignores (pageFormat, dateFormat,
// colorScheme, font.textAlign, header.photoPosition) and the preset's
// placement (layout.placement, layout.sidebarSectionTypes). A resume's own
// placement is layout.sections, which the Go side drops from its document.
export function showcaseMatchForm(preset) {
  if (preset.customization?.header === undefined) {
    throw new Error(
      `generateShowcasePresets: template ${preset.id} must define header; a preset without one cannot match a resume that kept its photo position.`,
    );
  }
  const customization = structuredClone(preset.customization);
  delete customization.pageFormat;
  delete customization.dateFormat;
  delete customization.colorScheme;
  delete customization.font.textAlign;
  delete customization.header.photoPosition;
  const {
    placement: _placement,
    sidebarSectionTypes: _sidebarSectionTypes,
    ...layout
  } = customization.layout;
  customization.layout = layout;
  return canonicalJson(customization);
}

export function generateShowcasePresetsGo(presets, outFile) {
  const rows = presets
    .map((preset) => ({ id: preset.id, match: showcaseMatchForm(preset) }))
    .sort((a, b) => (a.id < b.id ? -1 : a.id > b.id ? 1 : 0));
  for (const row of rows) {
    if (row.match.includes("`")) {
      throw new Error(
        `generateShowcasePresets: template ${row.id} holds a backtick.`,
      );
    }
  }
  const entries = rows
    .map((row) => `\t{ID: ${JSON.stringify(row.id)}, Match: \`${row.match}\`},`)
    .join("\n");
  const body = `${generatedHeader("templates/*.json")}

package showcase

// Preset is one template preset in showcase match form: its customization in
// stored form, without the leaves and the placement the showcase ignores, as
// canonical JSON.
type Preset struct {
	ID    string
	Match string
}

// Presets lists every template preset, ordered by ID.
var Presets = []Preset{
${entries}
}
`;
  writeGenerated(outFile, body, "go");
}
