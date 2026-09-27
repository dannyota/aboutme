import { afterEach, describe, expect, it, vi } from 'vitest';

import {
  isPreviewZoomStep,
  PREVIEW_ZOOM_STEPS,
  PREVIEW_ZOOM_STORAGE_KEY,
  previewZoomPercent,
  readStoredPreviewZoom,
  stepPreviewZoom,
  writeStoredPreviewZoom,
} from '../../app/editor/previewZoom';

afterEach(() => {
  window.localStorage.clear();
  vi.restoreAllMocks();
});

describe('isPreviewZoomStep', () => {
  it('accepts every declared step and rejects anything else', () => {
    for (const step of PREVIEW_ZOOM_STEPS) {
      expect(isPreviewZoomStep(step)).toBe(true);
    }
    expect(isPreviewZoomStep(51)).toBe(false);
    expect(isPreviewZoomStep('Fit')).toBe(false);
    expect(isPreviewZoomStep(undefined)).toBe(false);
    expect(isPreviewZoomStep(null)).toBe(false);
  });
});

describe('stepPreviewZoom', () => {
  it('walks the steps in order, both directions', () => {
    expect(stepPreviewZoom('fit', 1)).toBe(50);
    expect(stepPreviewZoom(50, 1)).toBe(67);
    expect(stepPreviewZoom(100, 1)).toBe(110);
    expect(stepPreviewZoom(100, -1)).toBe(90);
    expect(stepPreviewZoom(50, -1)).toBe('fit');
  });

  it('clamps at Fit and at 200%, never wrapping around', () => {
    expect(stepPreviewZoom('fit', -1)).toBe('fit');
    expect(stepPreviewZoom(200, 1)).toBe(200);
  });
});

describe('previewZoomPercent', () => {
  it('reports the step itself when not at Fit', () => {
    expect(previewZoomPercent(150, 84)).toBe(150);
  });

  it('reports the live fit percent while at Fit', () => {
    expect(previewZoomPercent('fit', 72)).toBe(72);
    expect(previewZoomPercent('fit', 84)).toBe(84);
  });
});

describe('readStoredPreviewZoom', () => {
  it('returns undefined when nothing is stored yet', () => {
    expect(readStoredPreviewZoom()).toBeUndefined();
  });

  it('returns the stored step when it is a valid percent', () => {
    window.localStorage.setItem(PREVIEW_ZOOM_STORAGE_KEY, '150');
    expect(readStoredPreviewZoom()).toBe(150);
  });

  it('returns the stored Fit value', () => {
    window.localStorage.setItem(PREVIEW_ZOOM_STORAGE_KEY, 'fit');
    expect(readStoredPreviewZoom()).toBe('fit');
  });

  it('falls back to Fit for a corrupted or out-of-range value', () => {
    window.localStorage.setItem(PREVIEW_ZOOM_STORAGE_KEY, 'not-a-zoom');
    expect(readStoredPreviewZoom()).toBe('fit');
    window.localStorage.setItem(PREVIEW_ZOOM_STORAGE_KEY, '999');
    expect(readStoredPreviewZoom()).toBe('fit');
  });

  it('falls back to Fit when storage throws', () => {
    const getItem = vi.spyOn(window.localStorage, 'getItem').mockImplementation(
      () => { throw new Error('blocked'); },
    );
    expect(readStoredPreviewZoom()).toBe('fit');
    getItem.mockRestore();
  });
});

describe('writeStoredPreviewZoom', () => {
  it('writes the exact step under the documented key', () => {
    writeStoredPreviewZoom(125);
    expect(window.localStorage.getItem(PREVIEW_ZOOM_STORAGE_KEY)).toBe('125');
    writeStoredPreviewZoom('fit');
    expect(window.localStorage.getItem(PREVIEW_ZOOM_STORAGE_KEY)).toBe('fit');
  });

  it('swallows a blocked or full store', () => {
    const setItem = vi.spyOn(window.localStorage, 'setItem').mockImplementation(
      () => { throw new Error('blocked'); },
    );
    expect(() => writeStoredPreviewZoom(100)).not.toThrow();
    setItem.mockRestore();
  });
});
