/**
 * Each gallery sample's page count in its template's paper size. The
 * template page states it and the pinned browser checks it (samples.spec),
 * so both read it from here.
 */
export const SAMPLE_PAGES: Readonly<Record<string, number>> = Object.freeze({
  'ats-plain': 1,
  'engineer-compact': 1,
  'executive-band': 2,
  'graduate-friendly': 1,
  'modern-sidebar': 1,
});
