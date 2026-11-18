import type {
  GitHubEvidenceFailure,
} from '../../composables/githubEvidenceApi';
import {
  githubEvidenceSettingsCopy,
} from '../../i18n/github-evidence-settings';
import type { Locale } from '../../i18n/locale';
import { projectEvidenceCopy } from '../../i18n/project-evidence';
import { projectsCopy } from '../../i18n/projects';
import type { EvidenceNotice } from './projectEvidenceState';

function intlLocale(locale: Locale): string {
  return locale === 'vi' ? 'vi-VN' : 'en-US';
}

export function formatEvidenceDate(value: string, locale: Locale): string {
  return new Intl.DateTimeFormat(intlLocale(locale), {
    timeZone: 'UTC',
    year: 'numeric',
    month: 'long',
    day: 'numeric',
  }).format(new Date(value));
}

export function formatLiftTime(until: number, locale: Locale): string {
  return new Intl.DateTimeFormat(intlLocale(locale), {
    hour: 'numeric',
    minute: '2-digit',
  }).format(new Date(until));
}

export function evidenceNoticeText(
  notice: EvidenceNotice,
  locale: Locale,
): string {
  const evidence = projectEvidenceCopy[locale];
  const errors = githubEvidenceSettingsCopy[locale].errors;
  const projects = projectsCopy[locale];
  switch (notice.kind) {
    case 'rate-limited':
      return notice.until === null
        ? errors.rateLimited
        : evidence.rateLimit(formatLiftTime(notice.until, locale));
    case 'not-found': return evidence.notFound;
    case 'duplicate': return evidence.duplicate;
    case 'limit': return evidence.limit;
    case 'size': return evidence.size;
    case 'upstream': return evidence.upstream;
    case 'not-confirmed': return evidence.notConfirmed;
    case 'unsupported-link': return evidence.unsupportedLink;
    case 'try-again': return projects.tryAgain;
    case 'evidence-unavailable': return errors.unavailable;
    case 'csrf-rejected': return errors.csrfRejected;
    case 'session-lost': return projects.sessionLost;
    default: return errors.failed;
  }
}

export function startFailureText(
  failure: GitHubEvidenceFailure,
  locale: Locale,
): string {
  const errors = githubEvidenceSettingsCopy[locale].errors;
  switch (failure) {
    case 'rate-limited': return errors.rateLimited;
    case 'unavailable': return errors.unavailable;
    case 'csrf-rejected': return errors.csrfRejected;
    case 'session-lost': return projectsCopy[locale].sessionLost;
    default: return errors.failed;
  }
}
