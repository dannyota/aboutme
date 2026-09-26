import { beforeEach, describe, expect, it } from 'vitest';
import { mountSuspended, registerEndpoint } from '@nuxt/test-utils/runtime';
import { flushPromises } from '@vue/test-utils';
import { setResponseStatus } from 'h3';
import ViewsIndexPage from '../../app/pages/app/views/index.vue';
import { setSiteLocale } from '../support/locale';

const summary = {
  today: '2026-09-26',
  resumes: [
    {
      id: 'resume-1',
      title: 'Backend engineer',
      slug: 'ada-lovelace',
      live: true,
      last7: { real: 4, filtered: 9 },
      last30: { real: 12, filtered: 31 },
      last90: { real: 20, filtered: 55 },
    },
  ],
};

describe('views index page', () => {
  beforeEach(() => {
    setSiteLocale('en');
    clearNuxtData();
  });

  it('lists a resume with its 7/30/90-day totals, linking to its page',
    async () => {
      registerEndpoint('/api/v1/views', () => ({ data: summary }));
      const wrapper = await mountSuspended(ViewsIndexPage);
      await flushPromises();

      const row = wrapper.get('[data-testid="views-resume-resume-1"]');
      expect(row.text()).toContain('Backend engineer');
      expect(row.text()).toContain('Live');
      expect(row.text()).toContain('4 real views · 9 filtered');
      expect(row.text()).toContain('12 real views · 31 filtered');
      expect(row.text()).toContain('20 real views · 55 filtered');
      expect(row.get('a').attributes('href')).toBe('/app/views/resume-1');
    });

  it('shows an empty state when no resume has stored counts', async () => {
    registerEndpoint('/api/v1/views', () => (
      { data: { today: '2026-09-26', resumes: [] } }
    ));
    const wrapper = await mountSuspended(ViewsIndexPage);
    await flushPromises();

    expect(wrapper.get('[data-testid="views-empty"]').text()).toContain(
      'No resume has been published yet',
    );
  });

  it('shows an error state when the list fails to load', async () => {
    registerEndpoint('/api/v1/views', (event) => {
      setResponseStatus(event, 500);
      return { error: { code: 'internal_error', message: 'failed' } };
    });
    const wrapper = await mountSuspended(ViewsIndexPage);
    await flushPromises();

    expect(wrapper.get('[data-testid="views-unavailable"]').text()).toContain(
      'Could not load views',
    );
  });

  it('speaks Vietnamese by default', async () => {
    setSiteLocale('vi');
    registerEndpoint('/api/v1/views', () => ({ data: summary }));
    const wrapper = await mountSuspended(ViewsIndexPage);
    await flushPromises();

    expect(wrapper.text()).toContain('Lượt xem');
    expect(wrapper.text()).toContain('4 lượt xem thật · 9 bị lọc');
  });
});
