import { mountSuspended } from '@nuxt/test-utils/runtime';
import { flushPromises } from '@vue/test-utils';
import { afterEach, beforeEach, describe, expect, it } from 'vitest';

import ErrorPage from '../app/error.vue';
import { setSiteLocale } from './support/locale';
import { RETIRED_STYLE, webSource } from './support/sfcStyles';

let current: Awaited<ReturnType<typeof mountSuspended>> | undefined;

beforeEach(() => {
  setSiteLocale(undefined);
});

afterEach(() => {
  current?.unmount();
  current = undefined;
  setSiteLocale(undefined);
});

async function mountError(statusCode: number, message = 'Not found') {
  const error = createError({ statusCode, message });
  error.stack = 'Error: private stack\n    at secret (/srv/app.js:1:1)';
  current = await mountSuspended(ErrorPage, {
    route: false,
    props: { error },
  });
  await flushPromises();
  return current;
}

describe('error page panel', () => {
  it('holds the h1 and one main pill home on a mist panel', async () => {
    const wrapper = await mountError(404);
    const page = wrapper.get('[data-testid="error-page"]');
    const panel = page.get('[data-testid="error-panel"]');
    for (const name of [
      'rounded-[var(--radius-panel)]', 'bg-surface-mist', 'text-center',
    ]) {
      expect(panel.classes(), name).toContain(name);
    }
    expect(panel.classes().join(' ')).not.toMatch(
      /(?:^|\s)(?:border|shadow-\S+|bg-card)(?:\s|$)/u,
    );
    const title = panel.get('h1');
    expect(title.attributes('data-page-title')).toBeDefined();
    expect(title.text()).toBe('Không tìm thấy trang');
    for (const name of [
      'text-2xl', 'font-extrabold', 'tracking-[-0.02em]',
    ]) {
      expect(title.classes(), name).toContain(name);
    }
    expect(panel.get('p').classes()).toEqual(
      expect.arrayContaining(['text-md', 'text-muted-foreground']),
    );
    const buttons = panel.findAll('button, a');
    expect(buttons).toHaveLength(1);
    const home = buttons[0]!;
    expect(home.text()).toBe('Về trang chủ');
    for (const name of [
      'rounded-full', 'bg-primary', 'text-primary-foreground', 'h-12',
    ]) {
      expect(home.classes(), name).toContain(name);
    }
    expect(panel.findAll('img, svg, picture')).toHaveLength(0);
  });

  it('never shows the message or the stack', async () => {
    const wrapper = await mountError(500, 'database password is hunter2');
    const text = wrapper.text();
    expect(text).toContain('Đã xảy ra lỗi');
    expect(text).not.toContain('hunter2');
    expect(text).not.toContain('private stack');
    expect(text).not.toContain('/srv/app.js');
  });

  it('ends with the footer band after the panel', async () => {
    setSiteLocale('en');
    const wrapper = await mountError(404);
    const footers = wrapper.findAll('[data-testid="landing-footer"]');
    expect(footers).toHaveLength(1);
    const main = wrapper.get('[data-testid="error-page"]').element;
    const footer = footers[0]!.element;
    expect(main.contains(footer)).toBe(false);
    expect(main.compareDocumentPosition(footer)
      & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
    expect(footers[0]!.findAll('a').map((link) => link.text())).toEqual([
      'Terms of Service', 'Privacy Policy', 'Verify', 'Connect AI',
    ]);
  });

  it('takes the application chrome ground and stored theme', () => {
    const source = webSource('app/error.vue');
    expect(source).toMatch(/'data-ui':\s*'app'/u);
    expect(source).toMatch(/const theme = useStoredTheme\(\);/u);
    expect(source).toMatch(/'data-theme':\s*theme\.value/u);
  });

  it('keeps hex and retired tokens out of its markup', () => {
    expect(webSource('app/error.vue')).not.toMatch(RETIRED_STYLE);
  });
});
