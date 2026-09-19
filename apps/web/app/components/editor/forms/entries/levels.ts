// Level pickers show what each 0–5 value means; the renderer draws the number
// as filled dots, so the value itself stays the schema integer.

type LevelOption = { readonly value: number | ''; readonly label: string };

function withLabels(labels: readonly string[]): readonly LevelOption[] {
  return [
    { value: '', label: 'Not set' },
    ...labels.map((label, value) => ({ value, label: `${value} · ${label}` })),
  ];
}

export const skillLevelOptions = withLabels([
  'None',
  'Beginner',
  'Basic',
  'Intermediate',
  'Advanced',
  'Expert',
]);

export const languageLevelOptions = withLabels([
  'None',
  'Elementary',
  'Limited working',
  'Professional working',
  'Full professional',
  'Native or bilingual',
]);
