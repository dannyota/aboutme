import { beforeEach, describe, expect, it, vi } from 'vitest';
import { mockNuxtImport, mountSuspended } from '@nuxt/test-utils/runtime';
import { ref } from 'vue';
import ViewsDetailPage from '../../app/pages/app/views/[id].vue';
import ViewsIndexPage from '../../app/pages/app/views/index.vue';
import { viewsDetailCopy, viewsIndexCopy } from '../../app/i18n/views';
import { literalGuard, parityViolations } from '../support/localizationSource';
import { setSiteLocale } from '../support/locale';

// server:false leaves `status` 'idle' during SSR, but Nuxt starts the fetch
// during hydration (setting `status` 'pending') before the first client
// render. Both pages must render their loading branch for 'idle' too, or a
// direct load hydrates against a different branch and the content never
// appears. Mocking `useFetch` with the idle status checks that branch.
mockNuxtImport('useFetch', () => () => ({
  data: ref(undefined),
  error: ref(null),
  status: ref('idle'),
  refresh: vi.fn(),
}));
mockNuxtImport('useRoute', () => () => ({ params: { id: 'resume-1' } }));

const approvedLiterals: Readonly<Record<string, readonly string[]>> = {
  'pages/app/views/[id].vue': [' '],
};
const { sourceViolations } = literalGuard(approvedLiterals);

const viewsSources = [
  'pages/app/views/index.vue',
  'pages/app/views/[id].vue',
  'components/views/ViewsResumeCard.vue',
  'components/views/ViewsDailyChart.vue',
  'components/views/ViewsFilteredBreakdown.vue',
  'components/views/ViewsLinkPreviews.vue',
];

const indexFixtures: Readonly<Record<string, readonly unknown[]>> = {
  'viewsIndexCopy.realFiltered': [4, 9],
};
const detailFixtures: Readonly<Record<string, readonly unknown[]>> = {
  'viewsDetailCopy.headlineCount': [6],
  'viewsDetailCopy.headlineRest': [6, 9],
  'viewsDetailCopy.chartDay': ['2026-09-26', 4],
  'viewsDetailCopy.axisDay': ['2026-09-26'],
  'viewsDetailCopy.fullDay': ['2026-09-26'],
  'viewsDetailCopy.monthName': ['2026-09'],
  'viewsDetailCopy.realCount': [6],
  'viewsDetailCopy.count': [9],
  'viewsDetailCopy.previewLine': ['Zalo', 3],
};

describe('views copy parity', () => {
  it('keeps viewsIndexCopy typed, complete, and nonempty', () => {
    expect(Object.keys(viewsIndexCopy).sort()).toEqual(['en', 'vi']);
    expect(parityViolations(
      viewsIndexCopy.vi, viewsIndexCopy.en, 'viewsIndexCopy', indexFixtures,
    )).toEqual([]);
  });

  it('keeps viewsDetailCopy typed, complete, and nonempty', () => {
    expect(Object.keys(viewsDetailCopy).sort()).toEqual(['en', 'vi']);
    expect(parityViolations(
      viewsDetailCopy.vi, viewsDetailCopy.en, 'viewsDetailCopy',
      detailFixtures,
    )).toEqual([]);
  });
});

describe('views source literal guard', () => {
  it('has no display literal outside the views copy', () => {
    const violations = viewsSources.flatMap((path) => sourceViolations(path));
    expect(violations).toEqual([]);
  });
});

describe('views hydration loading state', () => {
  beforeEach(() => {
    setSiteLocale('en');
    clearNuxtData();
  });

  it('renders the index page loading state when status is idle',
    async () => {
      const wrapper = await mountSuspended(ViewsIndexPage);

      expect(wrapper.find('[data-testid="views-loading"]').exists())
        .toBe(true);
      expect(wrapper.find('[data-testid="views-empty"]').exists())
        .toBe(false);
      expect(wrapper.find('[data-testid="views-unavailable"]').exists())
        .toBe(false);
    });

  it('renders the detail page loading state when status is idle',
    async () => {
      const wrapper = await mountSuspended(ViewsDetailPage);

      expect(wrapper.find('[data-testid="views-detail-loading"]').exists())
        .toBe(true);
      expect(
        wrapper.find('[data-testid="views-detail-not-found"]').exists(),
      ).toBe(false);
      expect(
        wrapper.find('[data-testid="views-detail-unavailable"]').exists(),
      ).toBe(false);
    });
});
