import catalog from '../../../assets/fonts/catalog.json';
import type { CustomizationSetPath } from '../../../editor/commands';

export const FIELD_LABELS: Readonly<Record<CustomizationSetPath, string>> = {
  'font.family': 'Font',
  'font.baseSizePx': 'Base size (px)',
  'colors.primary': 'Primary',
  'colors.text': 'Text',
  'colors.background': 'Background',
  'colors.accent': 'Accent',
  'colors.surface': 'Surface',
  'spacing.sectionGap': 'Section gap',
  'spacing.entryGap': 'Entry gap',
  'spacing.lineHeight': 'Line height',
  'spacing.pageMargin.x': 'Horizontal margin',
  'spacing.pageMargin.y': 'Vertical margin',
  'heading.style': 'Heading style',
  'heading.showRule': 'Heading rule',
  'header.align': 'Header alignment',
  'header.detailsLayout': 'Contact layout',
  'header.iconStyle': 'Icon style',
  'layout.columns': 'Columns',
  'layout.surfaceTarget': 'Surface target',
  'sectionDisplay.skill.style': 'Skill display',
  'sectionDisplay.language.style': 'Language display',
  'pageFormat': 'Page size',
  'dateFormat': 'Date format',
};

export const FIELD_GROUPS = [
  { title: 'Type', paths: ['font.family', 'font.baseSizePx'] },
  {
    title: 'Spacing',
    paths: [
      'spacing.sectionGap',
      'spacing.entryGap',
      'spacing.lineHeight',
      'spacing.pageMargin.x',
      'spacing.pageMargin.y',
    ],
  },
  {
    title: 'Headings',
    paths: [
      'heading.style',
      'heading.showRule',
      'header.align',
      'header.detailsLayout',
      'header.iconStyle',
    ],
  },
  {
    title: 'Layout',
    paths: [
      'layout.columns',
      'layout.surfaceTarget',
      'sectionDisplay.skill.style',
      'sectionDisplay.language.style',
      'pageFormat',
      'dateFormat',
    ],
  },
  {
    title: 'Colors',
    paths: [
      'colors.primary',
      'colors.text',
      'colors.background',
      'colors.accent',
      'colors.surface',
    ],
  },
] as const satisfies ReadonlyArray<{
  title: string;
  paths: readonly CustomizationSetPath[];
}>;

const KNOWN_ENUM_LABELS: Readonly<Record<string, string>> = {
  a4: 'A4',
  bar: 'Bar',
  center: 'Center',
  dots: 'Dots',
  header: 'Header',
  inline: 'Inline',
  left: 'Left',
  letter: 'Letter',
  none: 'None',
  normal: 'Normal',
  outline: 'Outline',
  sidebar: 'Sidebar',
  stacked: 'Stacked',
  tag: 'Tag',
  text: 'Text',
  titlecase: 'Titlecase',
  uppercase: 'Uppercase',
};

export function enumLabel(
  path: string,
  value: string | number | boolean,
): string {
  if (typeof value !== 'string') return String(value);
  if (path === 'font.family') {
    return catalog.entries.find((entry) => entry.id === value)?.displayName
      ?? value;
  }
  return KNOWN_ENUM_LABELS[value] ?? value;
}
