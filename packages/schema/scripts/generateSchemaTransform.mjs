// In-memory schema transforms shared by (or specific to) the Go and
// TypeScript codegen paths. Only the copy fed to the generators is touched —
// resume.schema.json on disk is never modified, and AJV
// (packages/schema/test/schema.test.ts) still validates the real file at
// runtime. Everything here is about type *shape*, not validation.

// The $def backing the (otherwise-unreferenced) SectionType enum — see
// deriveSectionVariants for the per-sectionType entry list, which is
// derived from the schema rather than named here.
export const SECTION_TYPE_DEF = ["sectionType", "SectionType"];

export function toPascalCase(key) {
  return key.charAt(0).toUpperCase() + key.slice(1);
}

// Derive entry definitions from the schema, in declaration order. Fail when
// section.oneOf and sectionType.enum disagree instead of generating a partial
// contract.
export function deriveSectionVariants(schema) {
  const oneOf = schema.$defs?.section?.oneOf;
  if (!Array.isArray(oneOf) || oneOf.length === 0) {
    throw new Error(
      "generate.mjs: resume.schema.json's $defs.section.oneOf is missing or empty.",
    );
  }

  const variants = oneOf.map((branch, index) => {
    const sectionType = branch?.properties?.sectionType?.const;
    const itemsRef = branch?.properties?.entries?.items?.$ref;
    if (typeof sectionType !== "string" || typeof itemsRef !== "string") {
      throw new Error(
        `generate.mjs: $defs.section.oneOf[${index}] doesn't have the expected shape ` +
          "(properties.sectionType.const + properties.entries.items.$ref).",
      );
    }
    const match = itemsRef.match(/^#\/\$defs\/([A-Za-z0-9_]+)$/);
    if (!match) {
      throw new Error(
        `generate.mjs: unexpected entries.items.$ref "${itemsRef}" on sectionType "${sectionType}".`,
      );
    }
    return { sectionType, defKey: match[1], typeName: toPascalCase(match[1]) };
  });

  const fromOneOf = new Set(variants.map((v) => v.sectionType));
  const fromEnum = new Set(schema.$defs?.sectionType?.enum ?? []);
  const disagreement = [
    ...[...fromOneOf].filter((v) => !fromEnum.has(v)),
    ...[...fromEnum].filter((v) => !fromOneOf.has(v)),
  ];
  if (disagreement.length > 0) {
    throw new Error(
      "generate.mjs: $defs.section.oneOf and $defs.sectionType.enum disagree on sectionType " +
        `values: ${disagreement.join(", ")}.`,
    );
  }

  return variants;
}

// Shared preprocessing for both languages.
export function buildSharedCodegenSchema(schema) {
  const clone = structuredClone(schema);

  // Neither generator evaluates JSON Schema 2020-12 unevaluatedProperties.
  // Removing it from the type-only copy lets both merge entry allOf branches;
  // AJV still enforces it against the source schema.
  const stripUnevaluatedProperties = (node) => {
    if (Array.isArray(node)) {
      node.forEach(stripUnevaluatedProperties);
      return;
    }
    if (node && typeof node === "object") {
      delete node.unevaluatedProperties;
      for (const value of Object.values(node)) {
        stripUnevaluatedProperties(value);
      }
    }
  };
  stripUnevaluatedProperties(clone);

  // dateRange allOf contains if/then validation, not type composition. jstt
  // otherwise collapses its properties to unknown. The store layer enforces
  // the relationship against real values.
  for (const def of Object.values(clone.$defs ?? {})) {
    if (
      def &&
      typeof def === "object" &&
      Array.isArray(def.allOf) &&
      def.allOf.some(
        (branch) => branch && typeof branch === "object" && "if" in branch,
      )
    ) {
      delete def.allOf;
    }
  }

  // link's anyOf refines string validation but not its generated type. Use a
  // plain string in the type-only copy to avoid duplicate intersection aliases.
  if (clone.$defs?.link) {
    clone.$defs.link = {
      description: clone.$defs.link.description,
      type: "string",
      maxLength: clone.$defs.link.maxLength,
    };
  }

  // Give every reusable definition a stable name. quicktype otherwise derives
  // some names from the temporary input filename.
  for (const [key, def] of Object.entries(clone.$defs ?? {})) {
    if (
      def &&
      typeof def === "object" &&
      !("title" in def) &&
      !("const" in def)
    ) {
      def.title = toPascalCase(key);
    }
  }

  // jstt prefers the schema title over compile()'s requested root name.
  clone.title = "Resume";

  return clone;
}

// Go-only: content's map values point at `section`, an eight-way oneOf.
// Go has no representation for that beyond structural collapse (see this
// file's header comment), so section.go hand-writes the real `Section` type
// instead. This function retargets `content`'s value schema at a trivial
// empty placeholder titled "Section", so quicktype still generates
// `Content map[string]Section` on Resume (referencing the hand-written
// type) without also generating a competing, empty `type Section struct{}`
// — generateGo strips that placeholder struct out of the raw output, since
// section.go already declares the real one.
export function buildGoCodegenSchema(sharedSchema) {
  const clone = structuredClone(sharedSchema);
  clone.$defs.__sectionPlaceholder = {
    title: "Section",
    type: "object",
    additionalProperties: false,
  };
  clone.$defs.content.additionalProperties = {
    $ref: "#/$defs/__sectionPlaceholder",
  };
  return clone;
}

// jstt treats a $ref with a sibling description as a new shape and emits a
// duplicate alias. Drop that use-site annotation only from the TypeScript
// type-copy; the source schema and embedded raw schema retain it.
export function buildTsCodegenSchema(sharedSchema) {
  const clone = structuredClone(sharedSchema);

  const stripDescriptionBesideRef = (node) => {
    if (Array.isArray(node)) {
      node.forEach(stripDescriptionBesideRef);
      return;
    }
    if (node && typeof node === "object") {
      if (typeof node.$ref === "string" && "description" in node) {
        delete node.description;
      }
      for (const value of Object.values(node)) {
        stripDescriptionBesideRef(value);
      }
    }
  };
  stripDescriptionBesideRef(clone);

  return clone;
}
