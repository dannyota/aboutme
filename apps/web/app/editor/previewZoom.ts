/**
 * Zoom steps for the editor's paged resume preview. `'fit'` keeps the
 * panel's automatic scale (DESIGN.md's editor "Responsive behavior": 0.72 on
 * narrow layouts, 0.84 on wide ones, computed from the current width in
 * EditorPreview.vue); every other step is a fixed percent of the page's
 * physical A4 size, independent of panel width.
 */
export const PREVIEW_ZOOM_STEPS = [
  'fit', 50, 67, 75, 90, 100, 110, 125, 150, 175, 200,
] as const;

export type PreviewZoomStep = (typeof PREVIEW_ZOOM_STEPS)[number];

export const PREVIEW_ZOOM_STORAGE_KEY = 'aboutme-editor-preview-zoom';

export function isPreviewZoomStep(value: unknown): value is PreviewZoomStep {
  return (PREVIEW_ZOOM_STEPS as readonly unknown[]).includes(value);
}

/**
 * Reads the owner's saved zoom choice for this browser. `undefined` means
 * nothing is stored yet, so the caller keeps its own initial value (the
 * `zoom` prop). A corrupted stored value or a blocked store both resolve to
 * Fit rather than a scale the controls could not have produced.
 */
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

/**
 * Moves one step toward a larger (`1`) or smaller (`-1`) zoom, clamped at
 * the ends of `PREVIEW_ZOOM_STEPS` instead of wrapping.
 */
export function stepPreviewZoom(
  current: PreviewZoomStep,
  direction: 1 | -1,
): PreviewZoomStep {
  const index = PREVIEW_ZOOM_STEPS.indexOf(current);
  const nextIndex = Math.min(
    PREVIEW_ZOOM_STEPS.length - 1,
    Math.max(0, index + direction),
  );
  return PREVIEW_ZOOM_STEPS[nextIndex] ?? current;
}

/** The percent the controls display: the step itself, or Fit's live scale. */
export function previewZoomPercent(
  step: PreviewZoomStep,
  fitPercent: number,
): number {
  return step === 'fit' ? fitPercent : step;
}
