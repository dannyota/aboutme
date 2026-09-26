/**
 * Each gallery sample's page count in its template's paper size. The
 * template page states it and the pinned browser checks it (samples.spec),
 * so both read it from here.
 */
export const SAMPLE_PAGES: Readonly<Record<string, number>> = Object.freeze({
  'ats-plain': 1,
  'consulting-formal': 1,
  'creative-accent': 1,
  'elegant-serif-two': 1,
  'engineer-compact': 1,
  'executive-band': 1,
  'graduate-friendly': 1,
  'international-lang': 1,
  'minimal-air': 1,
  'modern-sidebar': 1,
  'mono-print': 1,
  'nordic-muted': 1,
  'one-page-tight': 1,
});
