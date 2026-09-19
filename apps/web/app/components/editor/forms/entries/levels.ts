import type { Locale } from '@/i18n/locale';
import { editorFieldsCopy } from '@/i18n/editor-fields';

type LevelOption = { readonly value: number | ''; readonly label: string };

function withLabels(
  labels: readonly string[],
  unset: string,
): readonly LevelOption[] {
  return [
    { value: '', label: unset },
    ...labels.map((label, value) => ({ value, label: `${value} · ${label}` })),
  ];
}

export function skillLevelOptions(locale: Locale): readonly LevelOption[] {
  const copy = editorFieldsCopy[locale].levels;
  return withLabels(copy.skill, copy.unset);
}

export function languageLevelOptions(locale: Locale): readonly LevelOption[] {
  const copy = editorFieldsCopy[locale].levels;
  return withLabels(copy.language, copy.unset);
}
