import type { Customization } from '@aboutme/schema';

import type {
  ProjectEvidence,
  ProjectEvidenceKind,
} from '../../../server/utils/public-render/project-envelope';
import {
  PROJECT_PAGE_COPY,
  type ProjectPageLanguage,
} from '../../i18n/project-pages';
import { formatYearMonth } from '../resume/formatDate';

export interface EvidenceRow {
  readonly kind: ProjectEvidenceKind;
  readonly title: string;
  readonly url: string;
  readonly label?: string;
  readonly meta?: string;
}

const yearMonth = (
  instant: string, format: Customization['dateFormat'], lng: string,
): string => formatYearMonth({
  y: Number(instant.slice(0, 4)), m: Number(instant.slice(5, 7)),
}, format, lng);

export function evidenceRows(
  items: readonly ProjectEvidence[],
  format: Customization['dateFormat'],
  lng: string,
  uiLng: ProjectPageLanguage,
): EvidenceRow[] {
  const copy = PROJECT_PAGE_COPY[uiLng];
  return items.map((item) => {
    const repository = item.repositoryName === item.title
      ? []
      : [item.repositoryName];
    const parts = item.confirmed
      ? [...repository, yearMonth(item.itemDate, format, lng),
          copy.checked(yearMonth(item.checkedAt, format, lng))]
      : repository;
    return {
      kind: item.kind,
      title: item.title,
      url: item.url,
      ...(item.confirmed ? { label: copy.evidenceLabels[item.kind] } : {}),
      ...(parts.length === 0 ? {} : { meta: parts.join(' · ') }),
    };
  });
}
