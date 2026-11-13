import type { ProjectsCopy } from '@/i18n/projects';
import {
  TECHNOLOGY_LENGTH,
  TECHNOLOGY_LIMIT,
  TEXT_LIMITS,
  type ProjectIssue,
} from './projectDocument';

const TOP_LEVEL_IDS: Readonly<Record<string, string>> = {
  dates: 'project-dates',
  repositoryUrl: 'project-repository-url',
  demoUrl: 'project-demo-url',
  technologies: 'project-technologies',
};

export function issueTarget(path: string): {
  readonly id: string;
  readonly language: string | null;
} {
  const [root, language, field] = path.split('.');
  if (root === 'text' && language !== undefined && field !== undefined) {
    return { id: `project-${language}-${field}`, language };
  }
  return { id: TOP_LEVEL_IDS[path] ?? 'project-form', language: null };
}

export function issueMessage(issue: ProjectIssue, copy: ProjectsCopy): string {
  const field = issue.path.split('.').at(-1) ?? '';
  if (issue.code === 'date-range-order') return '';
  if (issue.code === 'required') return copy.errors.nameRequired;
  if (issue.code === 'format') return copy.errors.https;
  if (issue.path === 'technologies') {
    return copy.errors.technologies(TECHNOLOGY_LIMIT, TECHNOLOGY_LENGTH);
  }
  if (field === 'description') return copy.errors.descriptionTooLong;
  if (field in TEXT_LIMITS) {
    return copy.errors.tooLong(TEXT_LIMITS[field as keyof typeof TEXT_LIMITS]);
  }
  return copy.saveInvalid;
}
