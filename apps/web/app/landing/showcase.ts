import type { GalleryFilter } from '../templates/catalog';

export const SHOWCASE = [
  { id: 'ats-plain', filter: 'ats' },
  { id: 'engineer-compact', filter: 'technical' },
  { id: 'graduate-friendly', filter: 'first-job' },
  { id: 'executive-band', filter: 'management' },
] as const satisfies readonly { id: string; filter: GalleryFilter }[];
