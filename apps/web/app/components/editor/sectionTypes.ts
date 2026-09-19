import type { Section } from '@aboutme/schema';

type SectionType = Section['sectionType'];

/** What each section type is, for editor labels. Never shows raw keys. */
export const sectionTypeLabels: Readonly<Record<SectionType, string>> = {
  profile: 'Profile',
  work: 'Work experience',
  education: 'Education',
  skill: 'Skills',
  language: 'Languages',
  certificate: 'Certifications',
  project: 'Projects',
  custom: 'Custom section',
};

/** The section name a new section starts with. */
export const defaultSectionNames: Readonly<Record<SectionType, string>> = {
  profile: 'Summary',
  work: 'Experience',
  education: 'Education',
  skill: 'Skills',
  language: 'Languages',
  certificate: 'Certifications',
  project: 'Projects',
  custom: 'Custom section',
};

/** The heading icon a new section starts with; custom sections have none. */
export const defaultSectionIcons: Readonly<
  Record<SectionType, string | null>
> = {
  profile: 'user',
  work: 'briefcase',
  education: 'graduation-cap',
  skill: 'code',
  language: 'languages',
  certificate: 'award',
  project: 'folder',
  custom: null,
};

/**
 * Heading icons a person can choose: the common ones first, then the rest of
 * the renderer's section icons by name. Every value is a renderer icon key.
 */
export const sectionIconOptions: readonly {
  readonly value: string;
  readonly label: string;
}[] = [
  { value: '', label: 'No icon' },
  { value: 'user', label: 'Person' },
  { value: 'briefcase', label: 'Briefcase' },
  { value: 'graduation-cap', label: 'Graduation cap' },
  { value: 'code', label: 'Code' },
  { value: 'languages', label: 'Languages' },
  { value: 'award', label: 'Award' },
  { value: 'folder', label: 'Folder' },
  { value: 'trophy', label: 'Trophy' },
  { value: 'globe', label: 'Globe' },
  { value: 'badge-check', label: 'Badge' },
  { value: 'book-open', label: 'Book' },
  { value: 'bookmark', label: 'Bookmark' },
  { value: 'building-2', label: 'Building' },
  { value: 'camera', label: 'Camera' },
  { value: 'chart-line', label: 'Chart' },
  { value: 'cpu', label: 'Chip' },
  { value: 'compass', label: 'Compass' },
  { value: 'database', label: 'Database' },
  { value: 'file-text', label: 'Document' },
  { value: 'dumbbell', label: 'Dumbbell' },
  { value: 'flag', label: 'Flag' },
  { value: 'flask-conical', label: 'Flask' },
  { value: 'heart', label: 'Heart' },
  { value: 'heart-handshake', label: 'Helping hands' },
  { value: 'landmark', label: 'Landmark' },
  { value: 'leaf', label: 'Leaf' },
  { value: 'library', label: 'Library' },
  { value: 'lightbulb', label: 'Light bulb' },
  { value: 'link', label: 'Link' },
  { value: 'mail', label: 'Mail' },
  { value: 'map-pin', label: 'Map pin' },
  { value: 'medal', label: 'Medal' },
  { value: 'megaphone', label: 'Megaphone' },
  { value: 'mic', label: 'Microphone' },
  { value: 'microscope', label: 'Microscope' },
  { value: 'music', label: 'Music' },
  { value: 'newspaper', label: 'Newspaper' },
  { value: 'palette', label: 'Palette' },
  { value: 'pen-tool', label: 'Pen' },
  { value: 'users', label: 'People' },
  { value: 'phone', label: 'Phone' },
  { value: 'plane', label: 'Plane' },
  { value: 'presentation', label: 'Presentation' },
  { value: 'puzzle', label: 'Puzzle' },
  { value: 'rocket', label: 'Rocket' },
  { value: 'scale', label: 'Scale' },
  { value: 'school', label: 'School' },
  { value: 'scroll-text', label: 'Scroll' },
  { value: 'sparkles', label: 'Sparkles' },
  { value: 'star', label: 'Star' },
  { value: 'stethoscope', label: 'Stethoscope' },
  { value: 'target', label: 'Target' },
  { value: 'terminal', label: 'Terminal' },
  { value: 'trending-up', label: 'Trend' },
  { value: 'wrench', label: 'Wrench' },
];

/** A person-facing name for an entry: its title field, or its position. */
export function entryLabel(value: object, index: number): string {
  const entry = value as Record<string, unknown>;
  for (const field of ['jobTitle', 'degree', 'name', 'title'] as const) {
    const text = entry[field];
    if (typeof text === 'string' && text !== '') return text;
  }
  return `Entry ${index + 1}`;
}
