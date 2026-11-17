import { afterEach, beforeEach, describe, expect, it } from 'vitest';
import {
  mockNuxtImport,
  mountSuspended,
  registerEndpoint,
} from '@nuxt/test-utils/runtime';
import { flushPromises } from '@vue/test-utils';
import ViewsDetailPage from '../../app/pages/app/views/[id].vue';
import ViewsIndexPage from '../../app/pages/app/views/index.vue';
import { setSiteLocale } from '../support/locale';

mockNuxtImport('useRoute', () => () => ({
  path: '/app/views/resume-1',
  params: { id: 'resume-1' },
}));

const counts = { real: 4, filtered: 1 };

registerEndpoint('/api/v1/views', () => ({
  data: {
    today: '2026-11-16',
    resumes: [{
      id: 'resume-1',
      title: 'Backend engineer',
      slug: 'ada',
      live: true,
      last7: counts,
      last30: counts,
      last90: counts,
    }],
  },
}));
registerEndpoint('/api/v1/views/resume-1', () => ({
  data: {
    id: 'resume-1',
    title: 'Backend engineer',
    slug: 'ada',
    live: true,
    today: '2026-11-16',
    days: [
      {
        date: '2026-11-15', real: 2, bot: 1, datacenter: 0, anomaly: 0,
        invalid: 0, crawler: 0,
      },
      {
        date: '2026-11-16', real: 3, bot: 0, datacenter: 0, anomaly: 0,
        invalid: 0, crawler: 0,
      },
    ],
    months: [{ month: '2026-11', real: 5, filtered: 1 }],
    previews: [{ platform: 'zalo', fetches: 2 }],
  },
}));

const mounted: { unmount(): void }[] = [];

afterEach(() => {
  for (const wrapper of mounted.splice(0)) wrapper.unmount();
});

async function mountIndex() {
  const wrapper = await mountSuspended(ViewsIndexPage);
  mounted.push(wrapper);
  await flushPromises();
  return wrapper;
}

async function mountDetail() {
  const wrapper = await mountSuspended(ViewsDetailPage);
  mounted.push(wrapper);
  await flushPromises();
  return wrapper;
}

function expectPageHeading(classes: string[]): void {
  for (const name of ['text-2xl', 'font-extrabold', 'tracking-[-0.02em]']) {
    expect(classes, name).toContain(name);
  }
  expect(classes).not.toContain('font-bold');
}

describe('views identity', () => {
  beforeEach(() => {
    setSiteLocale('en');
    clearNuxtData();
  });

  it('sets the list heading at 32 px, weight 800', async () => {
    const wrapper = await mountIndex();
    expectPageHeading(wrapper.get('h1').classes());
  });

  it('draws each resume as a flat card on the card radius', async () => {
    const wrapper = await mountIndex();
    const card = wrapper.get('[data-testid="views-resume-resume-1"]');
    for (const name of [
      'rounded-[var(--radius-card)]', 'border', 'border-border', 'bg-card',
    ]) {
      expect(card.classes(), name).toContain(name);
    }
    expect(card.classes().filter((name) => /shadow/u.test(name))).toEqual([]);
    expect(card.classes()).not.toContain('rounded-[var(--radius)]');
  });

  it('sets the resume heading at 32 px and every section title at 20 px, '
    + 'weight 700', async () => {
    const wrapper = await mountDetail();
    expectPageHeading(wrapper.get('h1').classes());
    for (const id of [
      'views-chart-heading',
      'views-monthly-heading',
      'views-filtered-heading',
      'views-previews-heading',
    ]) {
      const title = wrapper.get(`#${id}`);
      expect(title.classes(), id).toEqual(
        expect.arrayContaining(['text-lg', 'font-bold']),
      );
      expect(title.classes(), id).not.toContain('font-semibold');
    }
  });

  it('keeps the headline count off the home-only sizes', async () => {
    const wrapper = await mountDetail();
    const count = wrapper.get('[data-testid="views-headline"] span');
    for (const name of ['text-3xl', 'font-extrabold', 'tabular-nums']) {
      expect(count.classes(), name).toContain(name);
    }
    expect(count.classes().join(' ')).not.toMatch(/text-[46]xl/u);
  });

  it('frames the chart as a card with chart-bar bars', async () => {
    const wrapper = await mountDetail();
    const bars = wrapper.get('[data-testid="views-daily-bars"]');
    const frame = bars.element.parentElement!;
    for (const name of [
      'rounded-[var(--radius-card)]', 'border', 'border-border', 'bg-card',
    ]) {
      expect(frame.classList.contains(name), name).toBe(true);
    }
    for (const bar of bars.findAll('[data-testid^="views-bar-"]')) {
      expect(bar.classes()).toContain('bg-chart-bar');
    }
  });
});
