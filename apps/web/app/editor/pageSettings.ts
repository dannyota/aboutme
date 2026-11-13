import type { Customization } from '@aboutme/schema';
import type { Locale } from '../i18n/locale';
import { editorControlsCopy } from '../i18n/editor-controls';

import type { CustomizationDelta } from './commands';

export type PageFormat = Customization['pageFormat'];
export type PageMargin = NonNullable<Customization['spacing']['pageMargin']>;
export type MarginChoice = 'narrow' | 'normal' | 'wide' | 'custom';
export type MarginAxis = 'x' | 'y';

const DEFAULT_MARGIN_MM = 15;
export const MAX_MARGIN_MM = 40;
export const PRINTABLE_EDGE_MM = 5;
const MM_PER_INCH = 25.4;

export const MARGIN_PRESETS: readonly {
  readonly choice: Exclude<MarginChoice, 'custom'>;
  readonly mm: number;
}[] = [
  { choice: 'narrow', mm: 10 },
  { choice: 'normal', mm: DEFAULT_MARGIN_MM },
  { choice: 'wide', mm: 25 },
];

export function marginLabel(
  choice: MarginChoice,
  locale: Locale = 'en',
): string {
  return editorControlsCopy[locale].page.marginLabels[choice];
}

export const PAGE_SIZE_SHORT: Readonly<Record<PageFormat, string>> = {
  a4: 'A4',
  letter: 'Letter',
};

export function unitFor(format: PageFormat): 'mm' | 'in' {
  return format === 'letter' ? 'in' : 'mm';
}

export function effectiveMargin(margin: PageMargin | undefined): PageMargin {
  return margin ?? { x: DEFAULT_MARGIN_MM, y: DEFAULT_MARGIN_MM };
}

export function marginChoice(margin: PageMargin | undefined): MarginChoice {
  const { x, y } = effectiveMargin(margin);
  if (x !== y) return 'custom';
  return MARGIN_PRESETS.find(({ mm }) => mm === x)?.choice ?? 'custom';
}

export function presetDeltas(
  choice: MarginChoice,
  margin: PageMargin | undefined,
): readonly CustomizationDelta[] {
  if (choice === 'custom') return [];
  if (choice === 'normal') {
    return margin === undefined
      ? []
      : [{ op: 'unset', path: 'spacing.pageMargin' }];
  }
  const mm = MARGIN_PRESETS.find((preset) => preset.choice === choice)!.mm;
  if (margin?.x === mm && margin.y === mm) return [];
  return [
    { op: 'set', path: 'spacing.pageMargin.x', value: mm },
    { op: 'set', path: 'spacing.pageMargin.y', value: mm },
  ];
}

export function axisDeltas(
  axis: MarginAxis,
  mm: number,
  margin: PageMargin | undefined,
): readonly CustomizationDelta[] {
  if (margin === undefined) {
    const next = { ...effectiveMargin(undefined), [axis]: mm };
    return [
      { op: 'set', path: 'spacing.pageMargin.x', value: next.x },
      { op: 'set', path: 'spacing.pageMargin.y', value: next.y },
    ];
  }
  if (margin[axis] === mm) return [];
  return [{ op: 'set', path: `spacing.pageMargin.${axis}`, value: mm }];
}

function round(value: number, places: number): number {
  const factor = 10 ** places;
  return Math.round(value * factor) / factor;
}

export function toDisplay(mm: number, format: PageFormat): number {
  return format === 'letter' ? round(mm / MM_PER_INCH, 2) : round(mm, 2);
}

export function fromDisplay(
  value: number,
  format: PageFormat,
): number | undefined {
  if (!Number.isFinite(value)) return undefined;
  const mm = format === 'letter' ? round(value * MM_PER_INCH, 2) : value;
  return mm >= 0 && mm <= MAX_MARGIN_MM ? mm : undefined;
}

export function formatLength(mm: number, format: PageFormat): string {
  return format === 'letter'
    ? `${round(mm / MM_PER_INCH, 1)} in`
    : `${round(mm, 1)} mm`;
}

export function presetLabel(
  choice: MarginChoice,
  format: PageFormat,
  locale: Locale = 'en',
): string {
  if (choice === 'custom') return marginLabel(choice, locale);
  const preset = MARGIN_PRESETS.find((item) => item.choice === choice)!;
  return `${marginLabel(choice, locale)} · ${formatLength(preset.mm, format)}`;
}
