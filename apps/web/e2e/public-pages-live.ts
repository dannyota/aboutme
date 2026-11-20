import { expect, type Page, test } from '@playwright/test';
import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';

import { mutationTest, proveMutation } from './style-mutation';
import {
  expectContinuous, expectPageIndex, expectPagesState, FOCUS, PAGE,
  SCROLLER,
} from './public-pages';

const SLUG = 'harness-live';
const FIRST_NAME = 'Nguyễn Thị Minh Châu';
const NEXT_NAME = 'Trần Bảo Ngọc';
const PUBLIC_CSP = [
  'default-src \'none\'', 'base-uri \'none\'', 'object-src \'none\'',
  'frame-ancestors \'none\'', 'form-action \'none\'', 'script-src \'self\'',
  'style-src \'self\' \'unsafe-inline\'', 'img-src \'self\' data:',
  'font-src \'self\'', 'connect-src \'self\'', 'manifest-src \'self\'',
  'media-src \'none\'', 'worker-src \'none\'',
].join('; ');
const FAILING_MEASUREMENT
  = '.pagination-measurement { --gap-section: unmeasurable !important; }';

interface Entry { readonly isHidden?: boolean }

function liveResume(revision: number, name: string) {
  const document = JSON.parse(readFileSync(resolve(import.meta.dirname,
    '../../../packages/schema/fixtures/vn-full.json'), 'utf8'));
  delete document.personalDetails.photo;
  document.personalDetails.fullName = name;
  const visible = (items: Entry[]) => items.filter((item) => !item.isHidden)
    .map(({ isHidden: _hidden, ...rest }) => rest);
  document.personalDetails.details = visible(document.personalDetails.details);
  for (const section of Object.values(document.content) as
    { entries: Entry[] }[]) {
    section.entries = visible(section.entries);
  }
  return {
    slug: SLUG, revision: String(revision), lng: 'vi', downloadEnabled: true,
    document,
  };
}

interface LivePage {
  update(revision: number, name: string): Promise<void>;
}

async function openLivePage(
  page: Page, width: number, options: { failFirst?: boolean } = {},
): Promise<LivePage> {
  let current = liveResume(1, FIRST_NAME);
  const response = await page.request.post('/internal-render/public', {
    data: {
      publicResume: current,
      mode: 'continuous',
      canonicalOrigin: 'https://aboutme.example',
      discoveryEnabled: false,
      pageTitle: `${FIRST_NAME} CV`,
      faviconHref: '',
    },
  });
  expect(response.ok(), 'Public render.').toBe(true);
  const html = await response.text();
  await page.route(`**/${SLUG}`, (route) => route.fulfill({
    status: 200,
    contentType: 'text/html; charset=utf-8',
    headers: { 'content-security-policy': PUBLIC_CSP },
    body: html,
  }));
  await page.route('**/api/v1/**', (route) => {
    const path = new URL(route.request().url()).pathname;
    if (path === `/api/v1/public/resumes/${SLUG}`) {
      return route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ data: current }),
      });
    }
    return route.fulfill({ status: 404, body: '' });
  });
  await page.addInitScript((failing) => {
    type Listener = (event: MessageEvent<string>) => void;
    const sources: Map<string, Set<Listener>>[] = [];
    class FakeEventSource {
      onopen: (() => void) | null = null;
      onerror: (() => void) | null = null;
      private readonly listeners = new Map<string, Set<Listener>>();
      constructor() {
        sources.push(this.listeners);
      }

      addEventListener(type: string, listener: Listener): void {
        const set = this.listeners.get(type) ?? new Set();
        set.add(listener);
        this.listeners.set(type, set);
      }

      removeEventListener(type: string, listener: Listener): void {
        this.listeners.get(type)?.delete(listener);
      }

      close(): void {}
    }
    Object.assign(window, {
      EventSource: FakeEventSource,
      emitRevision: (revision: string) => {
        const event = new MessageEvent('revision', {
          data: JSON.stringify({ version: 1, revision }),
        });
        for (const listeners of sources) {
          for (const listener of listeners.get('revision') ?? []) {
            listener(event);
          }
        }
      },
    });
    if (failing !== null) {
      document.addEventListener('DOMContentLoaded', () => {
        const style = document.createElement('style');
        style.textContent = failing;
        document.head.append(style);
      });
    }
  }, options.failFirst === true ? FAILING_MEASUREMENT : null);
  await page.setViewportSize({ width, height: 844 });
  const loaded = await page.goto(`/${SLUG}`);
  expect(loaded?.ok()).toBe(true);
  return {
    update: async (revision, name) => {
      current = liveResume(revision, name);
      await page.evaluate((value) => {
        (window as unknown as { emitRevision: (value: string) => void })
          .emitRevision(value);
      }, String(revision));
      await expect(page.locator('#public-resume'))
        .toHaveAttribute('data-revision', String(revision));
    },
  };
}

const PREVIOUS = '.public-pager > button:first-of-type';
const PROBE = '#focus-probe';

interface PhonePages {
  readonly live: LivePage;
  readonly count: number;
}

async function openPhone(page: Page): Promise<PhonePages> {
  const live = await openLivePage(page, 390);
  await expectPagesState(page, 'shown');
  const count = await page.locator(PAGE).count();
  expect(count).toBeGreaterThan(1);
  return { live, count };
}

async function pageForward(page: Page, count: number): Promise<void> {
  await page.locator(SCROLLER).focus();
  await page.keyboard.press('ArrowRight');
  await expectPageIndex(page, 'vi', 1, count);
}

async function deliverUpdate(page: Page, live: LivePage): Promise<void> {
  await live.update(2, NEXT_NAME);
  await expect(page.locator(`${PAGE} .resume-name`).first())
    .toHaveText(NEXT_NAME);
  await expectPagesState(page, 'shown');
}

async function expectFocusOn(page: Page, selector: string): Promise<void> {
  await expect.poll(() => page.evaluate(
    (target) => document.activeElement === document.querySelector(target),
    selector,
  ), FOCUS).toBe(true);
}

async function armFocusProbe(page: Page): Promise<void> {
  await page.evaluate(() => {
    const pages = document.querySelector('.public-pages')!;
    const probe = document.createElement('button');
    probe.id = 'focus-probe';
    probe.type = 'button';
    probe.textContent = 'probe';
    pages.append(probe);
    probe.focus();
    const observer = new MutationObserver((records) => {
      if (records.every((record) => probe.contains(record.target))) return;
      observer.disconnect();
      probe.remove();
    });
    observer.observe(pages, {
      childList: true, characterData: true, subtree: true,
    });
  });
  await expect(page.locator(PROBE)).toBeFocused();
}

async function lostFocusUpdate(page: Page): Promise<void> {
  const { live, count } = await openPhone(page);
  await pageForward(page, count);
  await armFocusProbe(page);
  await deliverUpdate(page, live);
  await expect(page.locator(PROBE), 'The update removes the focused node.')
    .toHaveCount(0);
}

async function keptFocusUpdate(page: Page): Promise<void> {
  const { live, count } = await openPhone(page);
  await pageForward(page, count);
  await page.locator(PREVIOUS).focus();
  await expectFocusOn(page, PREVIOUS);
  await deliverUpdate(page, live);
  await expectPageIndex(page, 'vi', 1, count);
}

async function failedUpdate(page: Page): Promise<string> {
  const live = await openLivePage(page, 1440);
  await expectPagesState(page, 'shown');
  const stale = await page.locator('.public-pages').evaluate((node) =>
    node.outerHTML);
  await page.addStyleTag({ content: FAILING_MEASUREMENT });
  await expectPagesState(page, 'shown');
  await live.update(2, NEXT_NAME);
  await expectPagesState(page, 'continuous');
  return stale;
}

export function publicPagesLiveTests(): void {
  test.describe('public page live', () => {
    test('an SSE update re-paginates and keeps the index and scroller focus',
      async ({ page }) => {
        const { live, count } = await openPhone(page);
        await pageForward(page, count);
        await expectFocusOn(page, SCROLLER);
        await deliverUpdate(page, live);
        await expectPageIndex(page, 'vi', 1, count);
        await expectFocusOn(page, SCROLLER);
        await expect(page.locator('style[data-public-page-rule]'))
          .toHaveCount(1);
      });

    test('an SSE update leaves focus on a pager button the reader chose',
      async ({ page }) => {
        await keptFocusUpdate(page);
        await expectFocusOn(page, PREVIOUS);
      });

    test('an SSE update that removes the focused node refocuses the scroller',
      async ({ page }) => {
        await lostFocusUpdate(page);
        await expectFocusOn(page, SCROLLER);
      });

    test('a failed SSE update shows the newest continuous article',
      async ({ page }) => {
        await failedUpdate(page);
        await expectContinuous(page, NEXT_NAME);
      });

    test('a failed first load shows the continuous article', async ({
      page,
    }) => {
      await openLivePage(page, 1440, { failFirst: true });
      await expectPagesState(page, 'continuous');
      await expectContinuous(page, FIRST_NAME);
    });

    test.describe('without script', () => {
      test.use({ javaScriptEnabled: false });
      test('the continuous article renders', async ({ page }) => {
        await openLivePage(page, 390);
        await expectContinuous(page, FIRST_NAME);
        await expect(page.locator('.public-resume-page'))
          .not.toHaveAttribute('data-pages');
        await expect(page.locator('.public-pages')).toHaveCount(0);
      });
    });
  });

  mutationTest('public live update rejects focus left on body after loss',
    async ({ page }) => {
      await lostFocusUpdate(page);
      await proveMutation(() => expectFocusOn(page, SCROLLER),
        async () => {
          await page.evaluate(() => {
            (document.activeElement as HTMLElement | null)?.blur();
          });
          await expect.poll(() => page.evaluate(() =>
            document.activeElement === document.body),
          'The mutation leaves focus on the body.').toBe(true);
          return async () => {
            await page.locator(SCROLLER).focus();
          };
        }, FOCUS);
    });

  mutationTest('public live update rejects focus pulled off a pager button',
    async ({ page }) => {
      await keptFocusUpdate(page);
      await proveMutation(() => expectFocusOn(page, PREVIOUS),
        async () => {
          await page.locator(SCROLLER).focus();
          await expectFocusOn(page, SCROLLER);
          return async () => {
            await page.locator(PREVIOUS).focus();
          };
        }, FOCUS);
    });

  mutationTest('public fallback rejects a stale page after a failed update',
    async ({ page }) => {
      const stale = await failedUpdate(page);
      await proveMutation(() => expectContinuous(page, NEXT_NAME),
        async () => {
          const root = page.locator('.public-resume-page');
          await root.evaluate((node, html) => {
            node.insertAdjacentHTML('beforeend', html);
            node.setAttribute('data-pages', 'shown');
          }, stale);
          await expect(page.locator(`${PAGE} .resume-name`).first())
            .toHaveText(FIRST_NAME);
          return async () => {
            await root.evaluate((node) => {
              node.querySelector(':scope > .public-pages')?.remove();
              node.setAttribute('data-pages', 'continuous');
            });
          };
        }, 'Public fallback.');
    });
}
