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
  months: [{ month: '2026-09', real: 12345, filtered: 9 }],
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
      expect(wrapper.find('[data-testid="views-chart-empty"]').exists())
        .toBe(false);
      // sr-only table row headers read the formatted day, not the ISO date.
      expect(wrapper.text()).toContain('Sep 24, 2026');
      expect(wrapper.text()).toContain('Sep 26, 2026');
      // Formatted month total, with grouped thousands.
      const monthly = wrapper.get('[data-testid="views-monthly-totals"]');
      expect(monthly.get('dt').text()).toBe('Sep 2026');
      expect(monthly.get('dd').text()).toBe('12,345 real views');
      const breakdown = wrapper.get('[data-testid="views-filtered-breakdown"]');
      expect(breakdown.text()).toContain('Bots');
      expect(breakdown.text()).toContain('Hosting networks');
      expect(breakdown.text()).toContain('Anomalies');
      expect(breakdown.text()).toContain('Failed checks');
      expect(breakdown.text()).toContain('Crawlers');
      for (const label of breakdown.findAll('dt')) {
        expect(label.classes()).toContain('text-balance');
      }
      const previews = wrapper.get('[data-testid="views-link-previews"]');
      expect(previews.text()).toContain('Link previews');
      expect(previews.text()).toContain('Link previews on Zalo × 3');
      expect(previews.text()).toContain(
        'One share can fetch the preview more than once.',
      );
      expect(wrapper.get('[data-testid="views-honest-limit"]').text()).toBe(
        'Counts exclude only what the filters detect; sophisticated '
        + 'automation can still be counted.',
      );
      const backLink = wrapper.get('[data-testid="views-detail-page"] > a');
      expect(backLink.text()).toContain('Back to views');
      expect(backLink.attributes('href')).toBe('/app/views');
    });

  it('shows the chart-empty message when every day is zero', async () => {
    registerEndpoint('/api/v1/views/resume-1', () => (
      { data: { ...detail, days: [day('2026-09-26', 0)], months: [] } }
    ));
    const wrapper = await mountSuspended(ViewsDetailPage);
    await flushPromises();

    expect(wrapper.get('[data-testid="views-chart-empty"]').text()).toBe(
      'No real views in the last 90 days',
    );
    expect(wrapper.find('[data-testid="views-daily-bars"]').exists())
      .toBe(false);
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

  it('shows the app not-found state on a 404, with the back link above it',
    async () => {
      registerEndpoint('/api/v1/views/resume-1', (event) => {
        setResponseStatus(event, 404);
        return { error: { code: 'public_not_found', message: 'not found' } };
      });
      const wrapper = await mountSuspended(ViewsDetailPage);
      await flushPromises();

      const notFound = wrapper.get('[data-testid="views-detail-not-found"]');
      expect(notFound.text()).toContain('Resume not found');
      const backLink = wrapper.get('[data-testid="views-detail-page"] > a');
      expect(backLink.attributes('href')).toBe('/app/views');
    });

  it('retries the failed load from the error state', async () => {
    registerEndpoint('/api/v1/views/resume-1', (event) => {
      setResponseStatus(event, 500);
      return { error: { code: 'internal_error', message: 'failed' } };
    });
    const wrapper = await mountSuspended(ViewsDetailPage);
    await flushPromises();

    const unavailable = wrapper.get('[data-testid="views-detail-unavailable"]');
    expect(unavailable.text()).toContain('Could not load views.');
    expect(unavailable.get('button').text()).toBe('Try again');
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
    const monthly = wrapper.get('[data-testid="views-monthly-totals"]');
    expect(monthly.get('dt').text()).toBe('tháng 9 năm 2026');
    expect(monthly.get('dd').text()).toBe('12.345 lượt xem thật');
  });
});
