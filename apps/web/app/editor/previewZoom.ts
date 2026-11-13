export const PREVIEW_ZOOM_STEPS = [
  'fit', 50, 67, 75, 90, 100, 110, 125, 150, 175, 200,
] as const;

export type PreviewZoomStep = (typeof PREVIEW_ZOOM_STEPS)[number];

const FIXED_ZOOM_STEPS = PREVIEW_ZOOM_STEPS.filter(
  (step): step is Exclude<PreviewZoomStep, 'fit'> => step !== 'fit',
);

export const PREVIEW_ZOOM_STORAGE_KEY = 'aboutme-editor-preview-zoom';

export function isPreviewZoomStep(value: unknown): value is PreviewZoomStep {
  return (PREVIEW_ZOOM_STEPS as readonly unknown[]).includes(value);
}

export function readStoredPreviewZoom(): PreviewZoomStep | undefined {
  try {
    const raw = window.localStorage.getItem(PREVIEW_ZOOM_STORAGE_KEY);
    if (raw === null) return undefined;
    if (raw === 'fit') return 'fit';
    const parsed = Number(raw);
    return isPreviewZoomStep(parsed) ? parsed : 'fit';
  } catch {
    return 'fit';
  }
}

export function writeStoredPreviewZoom(step: PreviewZoomStep): void {
  try {
    window.localStorage.setItem(PREVIEW_ZOOM_STORAGE_KEY, String(step));
  } catch {
    // A blocked or full store keeps today's in-memory choice for this tab.
  }
}

export function stepPreviewZoom(
  current: PreviewZoomStep,
  direction: 1 | -1,
  fitPercent: number,
): PreviewZoomStep {
  if (current === 'fit') {
    const beyond = direction === 1
      ? FIXED_ZOOM_STEPS.find((step) => step > fitPercent)
      : [...FIXED_ZOOM_STEPS].reverse().find((step) => step < fitPercent);
    const end = direction === 1
      ? FIXED_ZOOM_STEPS[FIXED_ZOOM_STEPS.length - 1]
      : FIXED_ZOOM_STEPS[0];
    return beyond ?? end ?? current;
  }
  const index = FIXED_ZOOM_STEPS.indexOf(current);
  const nextIndex = Math.min(
    FIXED_ZOOM_STEPS.length - 1,
    Math.max(0, index + direction),
  );
  return FIXED_ZOOM_STEPS[nextIndex] ?? current;
}

export function previewZoomPercent(
  step: PreviewZoomStep,
  fitPercent: number,
): number {
  return step === 'fit' ? fitPercent : step;
}
