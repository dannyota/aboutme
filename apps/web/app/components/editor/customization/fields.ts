import currentSchema from '@aboutme/schema/current-schema';

import type { CustomizationSetPath } from '../../../editor/commands';

type SchemaNode = {
  readonly $ref?: string;
  readonly enum?: readonly (string | number)[];
  readonly maximum?: number;
  readonly minimum?: number;
  readonly pattern?: string;
  readonly properties?: Readonly<Record<string, SchemaNode>>;
  readonly required?: readonly string[];
  readonly type?: 'boolean' | 'integer' | 'number' | 'object' | 'string';
};

type ResumeSchema = SchemaNode & {
  readonly $defs: Readonly<Record<string, SchemaNode>>;
};

export interface CustomizationField {
  readonly path: CustomizationSetPath;
  readonly kind: 'boolean' | 'color' | 'enum' | 'integer' | 'number';
  readonly required: boolean;
  readonly values?: readonly (string | number)[];
  readonly minimum?: number;
  readonly maximum?: number;
}

const paths = [
  'font.family',
  'font.baseSizePx',
  'colors.primary',
  'colors.text',
  'colors.background',
  'colors.accent',
  'colors.surface',
  'spacing.sectionGap',
  'spacing.entryGap',
  'spacing.lineHeight',
  'spacing.pageMargin.x',
  'spacing.pageMargin.y',
  'heading.style',
  'heading.showRule',
  'header.align',
  'header.detailsLayout',
  'header.iconStyle',
  'layout.columns',
  'layout.surfaceTarget',
  'sectionDisplay.skill.style',
  'sectionDisplay.language.style',
  'pageFormat',
  'dateFormat',
] as const satisfies readonly CustomizationSetPath[];

const schema = currentSchema as ResumeSchema;

export const CUSTOMIZATION_FIELDS: readonly CustomizationField[]
  = Object.freeze(paths.map((path) => Object.freeze(fieldFor(path))));

function fieldFor(path: CustomizationSetPath): CustomizationField {
  const leaf = resolveCustomizationLeaf(path);
  if (leaf === undefined) {
    throw new Error(`missing customization schema path: ${path}`);
  }
  const node = dereference(leaf.node);
  if (node === undefined) {
    throw new Error(`missing customization schema path: ${path}`);
  }
  const kind = primitiveKind(node, path);
  return {
    path,
    kind,
    required: leaf.required,
    ...(node.enum === undefined ? {} : { values: [...node.enum] }),
    ...(node.minimum === undefined ? {} : { minimum: node.minimum }),
    ...(node.maximum === undefined ? {} : { maximum: node.maximum }),
  };
}

function resolveCustomizationLeaf(
  path: CustomizationSetPath,
): { readonly node: SchemaNode; readonly required: boolean } | undefined {
  const root = dereference(schema.properties?.customization);
  if (root === undefined) return undefined;
  let node = root;
  let required = true;
  for (const part of path.split('.')) {
    const current = dereference(node);
    if (current === undefined) return undefined;
    const child = current.properties?.[part];
    if (child === undefined) return undefined;
    required = required && (current.required?.includes(part) ?? false);
    node = child;
  }
  return { node, required };
}

function dereference(node: SchemaNode | undefined): SchemaNode | undefined {
  if (node === undefined || node.$ref === undefined) return node;
  const prefix = '#/$defs/';
  if (!node.$ref.startsWith(prefix)) return undefined;
  return schema.$defs[node.$ref.slice(prefix.length)];
}

function primitiveKind(
  node: SchemaNode,
  path: CustomizationSetPath,
): CustomizationField['kind'] {
  if (node.pattern === '^#[0-9a-fA-F]{6}$') return 'color';
  if (node.enum !== undefined) return 'enum';
  switch (node.type) {
    case 'boolean':
    case 'integer':
    case 'number':
      return node.type;
    default:
      throw new Error(`unsupported customization schema type: ${path}`);
  }
}
