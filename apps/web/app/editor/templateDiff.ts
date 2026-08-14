import type { Customization } from '@aboutme/schema';

import type { CustomizationDelta, StructureEdit } from './commands';
import type { JsonValue, PlacementProjection } from './types';

export function diffPlacement(
  current: PlacementProjection,
  intended: PlacementProjection,
): readonly StructureEdit[] {
  const working = {
    main: [...current.main],
    sidebar: [...current.sidebar],
  };
  const edits: StructureEdit[] = [];
  for (const column of ['main', 'sidebar'] as const) {
    for (const [index, key] of intended[column].entries()) {
      if (working[column][index] === key) continue;
      remove(working, key);
      working[column].splice(index, 0, key);
      edits.push({ op: 'moveSection', key, column, index });
    }
  }
  return edits;
}

export function diffCustomization(
  current: Customization,
  intended: Customization,
): readonly CustomizationDelta[] {
  const deltas: CustomizationDelta[] = [];
  diffValue(current, intended, [], deltas);
  return deltas.sort((left, right) =>
    left.path < right.path ? -1 : left.path > right.path ? 1 : 0,
  );
}

function diffValue(
  current: unknown,
  intended: unknown,
  parts: readonly string[],
  deltas: CustomizationDelta[],
): void {
  const path = parts.join('.');
  if (path === 'layout.sections') return;
  if (isUnsetPath(path, intended)) {
    if (current !== undefined) deltas.push({ op: 'unset', path });
    return;
  }
  if (isRecord(current) && isRecord(intended)) {
    for (const key of new Set([
      ...Object.keys(current),
      ...Object.keys(intended),
    ])) {
      diffValue(current[key], intended[key], [...parts, key], deltas);
    }
    return;
  }
  if (isRecord(intended)) {
    for (const key of Object.keys(intended)) {
      diffValue(undefined, intended[key], [...parts, key], deltas);
    }
    return;
  }
  if (
    !equalJson(current, intended)
    && isSetPath(path)
    && intended !== undefined
  ) {
    deltas.push({ op: 'set', path, value: intended as JsonValue });
  }
}

function remove(
  placement: { main: string[]; sidebar: string[] },
  key: string,
): void {
  for (const column of ['main', 'sidebar'] as const) {
    const index = placement[column].indexOf(key);
    if (index !== -1) placement[column].splice(index, 1);
  }
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return value !== null && typeof value === 'object' && !Array.isArray(value);
}

function isSetPath(
  path: string,
): path is Extract<CustomizationDelta, { op: 'set' }>['path'] {
  return (
    path !== 'layout.sections'
    && path !== 'spacing.pageMargin'
    && path !== 'header'
  );
}

function isUnsetPath(
  path: string,
  intended: unknown,
): path is Extract<CustomizationDelta, { op: 'unset' }>['path'] {
  return (
    intended === undefined
    && (path === 'colors.accent'
      || path === 'colors.surface'
      || path === 'spacing.pageMargin'
      || path === 'header'
      || path === 'layout.surfaceTarget')
  );
}

function equalJson(left: unknown, right: unknown): boolean {
  if (Object.is(left, right)) return true;
  if (!isRecord(left) || !isRecord(right)) return false;
  const leftKeys = Object.keys(left);
  const rightKeys = Object.keys(right);
  return (
    leftKeys.length === rightKeys.length
    && leftKeys.every((key) => key in right && equalJson(left[key], right[key]))
  );
}
