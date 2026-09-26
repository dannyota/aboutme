import { beforeEach, describe, expect, it } from 'vitest';
import {
  mockNuxtImport,
  mountSuspended,
  registerEndpoint,
} from '@nuxt/test-utils/runtime';
import { flushPromises } from '@vue/test-utils';
import { setResponseStatus } from 'h3';
import ViewsDetailPage from '../../app/pages/app/views/[id].vue';
import { setSiteLocale } from '../support/locale';

mockNuxtImport('useRoute', () => () => ({ params: { id: 'resume-1' } }));

function day(
  date: string,
  real: number,
  overrides: Record<string, number> = {},
) {
  return {
    date,
    real,
    bot: 0,
    datacenter: 0,
    anomaly: 0,
    invalid: 0,
    crawler: 0,
    ...overrides,
  };
}

const detail = {
  id: 'resume-1',
  title: 'Backend engineer',
  slug: 'ada-lovelace',
  live: true,
  today: '2026-09-26',
  days: [
    day('2026-09-24', 2, { bot: 1 }),
    day('2026-09-25', 3, { datacenter: 2 }),
    day('2026-09-26', 1, { anomaly: 1, invalid: 1, crawler: 4 }),
  ],
  months: [{ month: '2026-09', real: 6, filtered: 9 }],
  previews: [{ platform: 'zalo', fetches: 3 }],
};

describe('views detail page', () => {
  beforeEach(() => {
    setSiteLocale('en');
    clearNuxtData();
  });

  it('renders the headline, definition, chart, breakdown, and previews',
    async () => {
      registerEndpoint('/api/v1/views/resume-1', () => ({ data: detail }));
      const wrapper = await mountSuspended(ViewsDetailPage);
      await flushPromises();

      expect(wrapper.get('[data-testid="views-headline"]').text()).toBe(
        '6 real views · 9 filtered',
      );
      expect(wrapper.get('[data-testid="views-definition"]').text()).toBe(
        'One view per network per day. Two people on one network in one '
        + 'day count once; one person on two days counts twice.',
      );
      const bars = wrapper.findAll('[data-testid^="views-bar-"]');
      expect(bars).toHaveLength(3);
      const breakdown = wrapper.get('[data-testid="views-filtered-breakdown"]');
      expect(breakdown.text()).toContain('Bots');
      expect(breakdown.text()).toContain('Hosting networks');
      expect(breakdown.text()).toContain('Anomalies');
      expect(breakdown.text()).toContain('Failed checks');
      expect(breakdown.text()).toContain('Crawlers');
      const previews = wrapper.get('[data-testid="views-link-previews"]');
      expect(previews.text()).toContain('Link previews on Zalo × 3');
      expect(previews.text()).toContain(
        'One share can fetch the preview more than once.',
      );
      expect(wrapper.get('[data-testid="views-honest-limit"]').text()).toBe(
        'Counts exclude only what the filters detect; sophisticated '
        + 'automation can still be counted.',
      );
    });

  it('hides the link-previews section when there are none', async () => {
    registerEndpoint('/api/v1/views/resume-1', () => (
      { data: { ...detail, previews: [] } }
    ));
    const wrapper = await mountSuspended(ViewsDetailPage);
    await flushPromises();

    expect(wrapper.find('[data-testid="views-link-previews"]').exists())
      .toBe(false);
  });

  it('shows the app not-found state on a 404', async () => {
    registerEndpoint('/api/v1/views/resume-1', (event) => {
      setResponseStatus(event, 404);
      return { error: { code: 'public_not_found', message: 'not found' } };
    });
    const wrapper = await mountSuspended(ViewsDetailPage);
    await flushPromises();

    const notFound = wrapper.get('[data-testid="views-detail-not-found"]');
    expect(notFound.text()).toContain('Resume not found');
    expect(notFound.get('a').attributes('href')).toBe('/app/views');
  });

  it('handles the singular "1 real view" in English', async () => {
    registerEndpoint('/api/v1/views/resume-1', () => (
      { data: { ...detail, days: [day('2026-09-26', 1)], months: [] } }
    ));
    const wrapper = await mountSuspended(ViewsDetailPage);
    await flushPromises();

    expect(wrapper.get('[data-testid="views-headline"]').text()).toBe(
      '1 real view · 0 filtered',
    );
  });

  it('speaks Vietnamese by default', async () => {
    setSiteLocale('vi');
    registerEndpoint('/api/v1/views/resume-1', () => ({ data: detail }));
    const wrapper = await mountSuspended(ViewsDetailPage);
    await flushPromises();

    expect(wrapper.get('[data-testid="views-headline"]').text()).toBe(
      '6 lượt xem thật · 9 bị lọc',
    );
    expect(wrapper.get('[data-testid="views-definition"]').text()).toBe(
      'Mỗi kết nối mạng được tính tối đa một lượt xem mỗi ngày. Hai người '
      + 'dùng chung một kết nối mạng trong một ngày được tính một lần; một '
      + 'người xem vào hai ngày được tính hai lần.',
    );
  });
});
