// icons.test.ts — the favicon, home-screen icon, and manifest set iOS and
// Android need to show a real icon instead of a generic tile
// (docs/adr/0050-aurora-application-identity.md).
import { readFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { describe, expect, it, vi } from 'vitest';
import {
  mockNuxtImport,
  mountSuspended,
  registerEndpoint,
} from '@nuxt/test-utils/runtime';
import { flushPromises } from '@vue/test-utils';
import { setResponseStatus } from 'h3';
import AppRoot from '../app/app.vue';
import { registerCapabilities } from './support/capabilities';
import { setSiteLocale } from './support/locale';

mockNuxtImport('navigateTo', () => vi.fn());
registerCapabilities({ providerLogin: false, agentAccess: false });
registerEndpoint('/api/v1/me', (event) => {
  setResponseStatus(event, 401);
  return { error: { code: 'session_required', message: 'Sign in.' } };
});

const here = dirname(fileURLToPath(import.meta.url));
const publicDir = join(here, '..', 'public');

describe('application icon links', () => {
  it('links a PNG, then the SVG, then the touch icon, then the manifest',
    async () => {
      setSiteLocale('en');
      const wrapper = await mountSuspended(AppRoot, { route: '/' });
      await flushPromises();

      const links = [...document.head.querySelectorAll(
        'link[rel="icon"], link[rel="apple-touch-icon"], '
        + 'link[rel="manifest"]',
      )];
      expect(links.map((link) => ({
        rel: link.getAttribute('rel'),
        type: link.getAttribute('type'),
        sizes: link.getAttribute('sizes'),
        href: link.getAttribute('href'),
      }))).toEqual([
        {
          rel: 'icon', type: 'image/png', sizes: '32x32',
          href: '/icon-32.png',
        },
        {
          rel: 'icon', type: 'image/svg+xml', sizes: null,
          href: '/favicon.svg',
        },
        {
          rel: 'apple-touch-icon', type: null, sizes: '180x180',
          href: '/apple-touch-icon.png',
        },
        {
          rel: 'manifest', type: null, sizes: null,
          href: '/site.webmanifest',
        },
      ]);
      expect(
        document.head.querySelector('meta[name="theme-color"]')
          ?.getAttribute('content'),
      ).toBe('#1a5ceb');
      wrapper.unmount();
    });

  it('does not link a favicon.ico in the head (still served by path)',
    () => {
      expect(
        readFileSync(join(publicDir, 'favicon.ico')).subarray(0, 4)
          .toString('hex'),
      ).toBe('00000100');
    });
});

describe('site.webmanifest', () => {
  const manifestPath = join(publicDir, 'site.webmanifest');
  const manifest = JSON.parse(readFileSync(manifestPath, 'utf8')) as {
    name: string;
    short_name: string;
    theme_color: string;
    background_color: string;
    display: string;
    start_url: string;
    icons: readonly {
      src: string;
      sizes: string;
      type: string;
      purpose?: string;
    }[];
  };

  it('names the app and matches the Aurora brand colors', () => {
    expect(manifest.name).toBe('aboutme');
    expect(manifest.short_name).toBe('aboutme');
    expect(manifest.theme_color).toBe('#1a5ceb');
    expect(manifest.background_color).toBe('#f5f8ff');
    expect(manifest.display).toBe('browser');
    expect(manifest.start_url).toBe('/');
  });

  it('lists a 192 and a 512 PNG icon that exist at their stated size',
    () => {
      const bySize = new Map(manifest.icons.map((icon) => [icon.sizes, icon]));
      expect([...bySize.keys()].sort()).toEqual(['192x192', '512x512']);
      for (const [sizes, icon] of bySize) {
        const [width, height] = sizes.split('x').map(Number);
        expect(icon.type).toBe('image/png');
        expect(icon.src).toBe(`/${icon.src.replace(/^\/+/u, '')}`);
        const bytes = readFileSync(join(publicDir, icon.src));
        expect(bytes.subarray(0, 8).toString('hex'))
          .toBe('89504e470d0a1a0a');
        expect(bytes.subarray(12, 16).toString('ascii')).toBe('IHDR');
        expect(bytes.readUInt32BE(16)).toBe(width);
        expect(bytes.readUInt32BE(20)).toBe(height);
      }
    });
});

describe('apple-touch-icon.png', () => {
  it('is a 180x180 truecolor PNG with no palette and no alpha', () => {
    const bytes = readFileSync(join(publicDir, 'apple-touch-icon.png'));
    expect(bytes.subarray(0, 8).toString('hex')).toBe('89504e470d0a1a0a');
    expect(bytes.subarray(12, 16).toString('ascii')).toBe('IHDR');
    expect(bytes.readUInt32BE(16)).toBe(180);
    expect(bytes.readUInt32BE(20)).toBe(180);
    const bitDepth = bytes.readUInt8(24);
    const colorType = bytes.readUInt8(25);
    // PNG color type 2 is truecolor (RGB, no alpha, no 256-color palette).
    expect(bitDepth).toBe(8);
    expect(colorType).toBe(2);
  });
});

describe('the chrome font preload', () => {
  it('is on the homepage, first in the stack the tab shows', async () => {
    setSiteLocale('en');
    const wrapper = await mountSuspended(AppRoot, { route: '/' });
    await flushPromises();

    const preload = document.head.querySelector(
      'link[rel="preload"][as="font"]',
    );
    expect(preload?.getAttribute('type')).toBe('font/woff2');
    expect(preload?.getAttribute('crossorigin')).toBe('anonymous');
    // Vite's hashed build path for the vendored file, not a second copy.
    expect(preload?.getAttribute('href'))
      .toMatch(/be-vietnam-pro-var[\w.-]*\.woff2$/u);
    wrapper.unmount();
  });

  it('is gated by the same isAppSurface check as the app html attribute',
    () => {
      // The render harness (/_harness/**), the print worker, and public
      // resume pages never mount AppRoot, so they never see this link;
      // this pins the single condition that would otherwise need a
      // second, drifting copy (docs/design/web.md).
      const source = readFileSync(
        join(here, '..', 'app', 'app.vue'), 'utf8',
      );
      const linkBlock = /link:\s*isAppSurface\.value[\s\S]*?:\s*\[\]/u
        .exec(source)?.[0];
      const htmlAttrsBlock = /htmlAttrs:\s*isAppSurface\.value/u
        .exec(source)?.[0];
      expect(linkBlock, 'preload link gated on isAppSurface').toBeDefined();
      expect(htmlAttrsBlock, 'data-ui gated on isAppSurface').toBeDefined();
    });
});

describe('link prefetch', () => {
  it('prefetches route chunks on interaction, not on sight', () => {
    const config = readFileSync(
      join(here, '../nuxt.config.ts'),
      'utf8',
    );
    expect(config).toMatch(
      /nuxtLink: \{ prefetchOn: \{ visibility: false, interaction: true \} \}/u,
    );
  });
});
