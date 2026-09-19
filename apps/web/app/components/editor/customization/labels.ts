import catalog from '../../../assets/fonts/catalog.json';
import type { CustomizationSetPath } from '../../../editor/commands';
import {
  editorControlsCopy,
  type CustomizationGroupId,
  type EditorControlEnum,
} from '../../../i18n/editor-controls';
import type { Locale } from '../../../i18n/locale';

export function fieldLabel(locale: Locale, path: CustomizationSetPath): string {
  return editorControlsCopy[locale].customization[path] ?? path;
}

export const FIELD_GROUPS = [
  {
    id: 'page',
    hook: 'Page & PDF',
    paths: ['pageFormat', 'spacing.pageMargin.x', 'spacing.pageMargin.y'],
  },
  {
    id: 'type',
    hook: 'Type',
    paths: ['font.family', 'font.baseSizePx', 'font.textAlign'],
  },
  {
    id: 'spacing',
    hook: 'Spacing',
    paths: ['spacing.sectionGap', 'spacing.entryGap', 'spacing.lineHeight'],
  },
  {
    id: 'headings',
    hook: 'Headings',
    paths: [
      'heading.style',
      'heading.showRule',
      'header.align',
      'header.detailsLayout',
      'header.iconStyle',
      'header.photoPosition',
    ],
  },
  {
    id: 'layout',
    hook: 'Layout',
    paths: [
      'layout.columns',
      'layout.surfaceTarget',
      'sectionDisplay.skill.style',
      'sectionDisplay.language.style',
      'dateFormat',
    ],
  },
  {
    id: 'colors',
    hook: 'Colors',
    paths: [
      'colors.primary',
      'colors.text',
      'colors.background',
      'colors.accent',
      'colors.surface',
    ],
  },
] as const satisfies ReadonlyArray<{
  id: CustomizationGroupId;
  hook: string;
  paths: readonly CustomizationSetPath[];
}>;

export function enumLabel(
  locale: Locale,
  path: string,
  value: string | number | boolean,
): string {
  if (typeof value !== 'string') return String(value);
  if (path === 'font.family') {
    return (
      catalog.entries.find((entry) => entry.id === value)?.displayName ?? value
    );
  }
  return editorControlsCopy[locale].enums[value as EditorControlEnum] ?? value;
}
