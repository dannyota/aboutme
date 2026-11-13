export const SECTION_TYPE_DEF = ["sectionType", "SectionType"];

export function toPascalCase(key) {
  return key.charAt(0).toUpperCase() + key.slice(1);
}

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

export function buildSharedCodegenSchema(schema) {
  const clone = structuredClone(schema);

  // Neither quicktype nor jstt evaluates JSON Schema unevaluatedProperties.
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

  // jstt collapses dateRange's properties to unknown under its if/then allOf.
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

  // jstt emits duplicate intersection aliases for link's anyOf.
  if (clone.$defs?.link) {
    clone.$defs.link = {
      description: clone.$defs.link.description,
      type: "string",
      maxLength: clone.$defs.link.maxLength,
    };
  }

  // quicktype otherwise names some definitions after the temp input file.
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

// jstt emits a duplicate alias for a $ref with a sibling description.
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
