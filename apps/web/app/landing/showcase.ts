// The homepage template showcase: four templates, one per gallery filter
// chip, each with a sample in both site languages (DESIGN.md; ADR 0050).
// `modern-sidebar` is left out because its only filter is "photo", which
// this showcase does not offer as a chip.
import type { GalleryFilter } from '../templates/catalog';

export const SHOWCASE = [
  { id: 'ats-plain', filter: 'ats' },
  { id: 'engineer-compact', filter: 'technical' },
  { id: 'graduate-friendly', filter: 'first-job' },
  { id: 'executive-band', filter: 'management' },
] as const satisfies readonly { id: string; filter: GalleryFilter }[];
