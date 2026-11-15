import { readFileSync } from 'node:fs';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import {
  mockNuxtImport,
  mountSuspended,
  registerEndpoint,
} from '@nuxt/test-utils/runtime';
import { flushPromises } from '@vue/test-utils';
import { setResponseStatus } from 'h3';
import AppRoot from '../app/app.vue';
import LandingPage from '../app/pages/index.vue';
import { setSiteLocale } from './support/locale';

const mocks = vi.hoisted(() => {
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  const real: any = (globalThis as { $fetch: unknown }).$fetch;
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  return { fetchMock: vi.fn((...args: any[]) => real(...args)) };
});
mockNuxtImport('$fetch', () => mocks.fetchMock);

let meRequests = 0;
let meStatus = 401;
registerEndpoint('/api/v1/me', (event) => {
  meRequests += 1;
  setResponseStatus(event, meStatus);
  if (meStatus === 200) {
    return {
      data: {
        user: {
          id: 'user-1',
          email: 'ada@example.com',
          name: 'Ada Lovelace',
          avatarKey: null,
          hasPassword: false,
        },
        csrfToken: 'csrf-token',
        identities: [],
      },
    };
  }
  return { error: { code: 'session_required', message: 'Sign in.' } };
});
function apiPaths(): string[] {
  return mocks.fetchMock.mock.calls
    .map(([url]) => new URL(String(url), 'http://localhost').pathname)
    .filter((path) => path.startsWith('/api/'));
}

async function mountLanding(locale?: 'vi' | 'en') {
  setSiteLocale(locale);
  return mountSuspended(LandingPage);
}

type Wrapper = Awaited<ReturnType<typeof mountLanding>>;

function hrefs(wrapper: Wrapper, selector: string): string[] {
  return wrapper.get(selector).findAll('a')
    .map((link) => link.attributes('href') ?? '');
}

function texts(wrapper: Wrapper, selector: string): string[] {
  return wrapper.get(selector).findAll('a').map((link) => link.text());
}

beforeEach(() => {
  meStatus = 401;
  meRequests = 0;
  mocks.fetchMock.mockClear();
  setSiteLocale(undefined);
  clearNuxtData();
});

const TABLE = {
  vi: {
    hero: 'Làm CV ở đây, gửi đi bằng một đường link.',
    lead: 'Chào bạn, mình là aboutme.vn, chỗ làm CV miễn phí và mã nguồn '
      + 'mở. Bạn viết, chọn mẫu, rồi xuất bản tại đường dẫn riêng khi nào '
      + 'bạn sẵn sàng.',
    signedOut: ['Tạo CV của bạn', 'Xem thư viện', 'Đăng nhập'],
    signedIn: ['Mở CV của bạn', 'Xem thư viện'],
    editor: 'Viết đến đâu, thấy đúng trang đến đó.',
    facts: [
      'Miễn phí và mã nguồn mở.',
      'Tối đa ba CV cho mỗi tài khoản.',
      'Tự xuất PDF lúc nào cũng được.',
      'Giao diện tiếng Việt và tiếng Anh.',
    ],
    templates: 'Chọn mẫu hợp với nghề của bạn.',
    labels: ['Chuẩn ATS', 'Kỹ thuật', 'Mới tốt nghiệp', 'Quản lý'],
    library: 'Mở thư viện',
    publish: 'Không gì công khai cho đến khi bạn xuất bản.',
    ai: 'Dùng trợ lý AI của chính bạn.',
    aiTags: ['Đọc CV', 'Chỉnh sửa CV', 'Xuất bản: chỉ bạn'],
    aiLink: 'Xem cách kết nối',
    data: 'Dữ liệu của bạn lưu tại Việt Nam.',
    dataTags: ['Riêng tư theo mặc định', 'Lập chỉ mục: mặc định tắt'],
    dataLink: 'Đọc chính sách quyền riêng tư',
    maker: 'Chào bạn, mình là Danny.',
    makerSignedOut: [
      'Tạo CV của bạn', 'Xem CV của mình', 'Xem mã nguồn trên GitHub',
    ],
    makerSignedIn: [
      'Mở CV của bạn', 'Xem CV của mình', 'Xem mã nguồn trên GitHub',
    ],
    avatar: 'Tranh vẽ chân dung Danny, bên cạnh là vợ và con anh trong '
      + 'khung hình trái tim',
    footer: 'aboutme.vn là phần mềm mã nguồn mở, phát hành theo giấy phép '
      + 'AGPL-3.0.',
    note: 'Hình minh họa trình chỉnh sửa. Trang xem trước là mẫu Creative '
      + 'Accent với một CV mẫu.',
  },
  en: {
    hero: 'Write your resume here. Send it as one link.',
    lead: 'Hi, I\'m aboutme.vn, a free, open-source resume builder. Write '
      + 'it, pick a template, and publish it at its own link when you\'re '
      + 'ready.',
    signedOut: ['Create your resume', 'Browse the library', 'Sign in'],
    signedIn: ['Open your resumes', 'Browse the library'],
    editor: 'See the real page while you write.',
    facts: [
      'Free and open source.',
      'Up to three resumes per account.',
      'Export your own PDF any time.',
      'Vietnamese and English interface.',
    ],
    templates: 'Pick a template that fits your work.',
    labels: ['ATS-friendly', 'Technical', 'First job', 'Management'],
    library: 'Open the library',
    publish: 'Nothing is public until you publish it.',
    ai: 'Bring your own AI assistant.',
    aiTags: ['Read resumes', 'Write resumes', 'Publish: only you'],
    aiLink: 'See how to connect',
    data: 'Your data is hosted in Vietnam.',
    dataTags: ['Private by default', 'Indexing: off by default'],
    dataLink: 'Read the privacy policy',
    maker: 'Hi, I\'m Danny.',
    makerSignedOut: [
      'Create your resume', 'See my resume', 'View the code on GitHub',
    ],
    makerSignedIn: [
      'Open your resumes', 'See my resume', 'View the code on GitHub',
    ],
    avatar: 'Drawn portrait of Danny, with his wife and child in a '
      + 'heart-shaped frame',
    footer: 'aboutme.vn is open-source software, released under the '
      + 'AGPL-3.0 license.',
    note: 'Illustration of the editor. The preview is the Creative Accent '
      + 'template with a sample resume.',
  },
} as const;

const LANDING_SOURCES = [
  'app/pages/index.vue',
  'app/landing/copy.ts',
  'app/landing/components/EditorIllustration.vue',
  'app/landing/components/HeroArt.vue',
  'app/landing/components/HomeCards.vue',
  'app/landing/components/HomeEditor.vue',
  'app/landing/components/HomeFooter.vue',
  'app/landing/components/HomeGlyph.vue',
  'app/landing/components/HomeHero.vue',
  'app/landing/components/HomeMaker.vue',
  'app/landing/components/HomePublish.vue',
  'app/landing/components/HomeTemplates.vue',
  'app/landing/styles/home.css',
];

const RENDERER_GUARD = ':not(:where(.resume-document, .resume-document *))';

const SHOWCASE_IDS = [
  'ats-plain', 'engineer-compact', 'graduate-friendly', 'executive-band',
];

describe('index.vue copy', () => {
  for (const locale of ['vi', 'en'] as const) {
    const table = TABLE[locale];

    it(`renders the ${locale} hero and section headings`, async () => {
      const wrapper = await mountLanding(locale);
      expect(wrapper.findAll('h1')).toHaveLength(1);
      expect(wrapper.get('[data-testid="landing-title"]').text())
        .toBe(table.hero);
      expect(wrapper.get('[data-testid="landing-hero"] p').text())
        .toBe(table.lead);
      expect(wrapper.findAll('h2')
        .filter((h2) => !h2.element.closest('.resume-document'))
        .map((h2) => h2.text())).toEqual([
        table.editor, table.templates, table.publish, table.ai, table.data,
        table.maker,
      ]);
    });

    it(`renders the ${locale} facts, cards, note and footer`, async () => {
      const wrapper = await mountLanding(locale);
      expect(wrapper.findAll('[data-testid="landing-facts"] li')
        .map((li) => li.text())).toEqual(table.facts);
      expect(wrapper.findAll('[data-testid="landing-ai"] li')
        .map((li) => li.text())).toEqual(table.aiTags);
      expect(wrapper.findAll('[data-testid="landing-data"] li')
        .map((li) => li.text())).toEqual(table.dataTags);
      expect(wrapper.get('[data-testid="landing-editor-note"]').text())
        .toBe(table.note);
      expect(wrapper.get('[data-testid="landing-license"]').text())
        .toBe(table.footer);
    });

    it(`links the ${locale} AI and data cards to their pages`, async () => {
      const wrapper = await mountLanding(locale);
      expect(hrefs(wrapper, '[data-testid="landing-ai"]'))
        .toEqual(['/guide/mcp']);
      expect(texts(wrapper, '[data-testid="landing-ai"]'))
        .toEqual([table.aiLink]);
      expect(hrefs(wrapper, '[data-testid="landing-data"]'))
        .toEqual(['/privacy']);
      expect(texts(wrapper, '[data-testid="landing-data"]'))
        .toEqual([table.dataLink]);
    });

    it(`labels the ${locale} maker avatar from the copy table`, async () => {
      const wrapper = await mountLanding(locale);
      const avatar = wrapper.get('[data-testid="landing-maker-avatar"]');
      expect(avatar.attributes('alt')).toBe(table.avatar);
      expect(avatar.attributes('src')).toBe('/landing/maker-avatar-320.webp');
      expect(avatar.attributes('srcset')).toBe(
        '/landing/maker-avatar-320.webp 320w, '
        + '/landing/maker-avatar-640.webp 640w',
      );
      expect(avatar.attributes('sizes')).toBe(
        '(min-width: 64rem) 232px, (min-width: 48rem) 176px, 168px',
      );
      for (const size of [320, 640]) {
        const file = readFileSync(`public/landing/maker-avatar-${size}.webp`);
        expect(file.subarray(8, 12).toString('latin1')).toBe('WEBP');
      }
    });

    it(`lists the ${locale} templates as links to their pages`, async () => {
      const wrapper = await mountLanding(locale);
      const list = wrapper.get('[data-testid="landing-template-list"]');
      const links = list.findAll('a');
      expect(links.map((link) => link.attributes('href'))).toEqual(
        SHOWCASE_IDS.map((id) => `/templates/${id}`),
      );
      expect(links.map((link) => link.get('.tpl-label').text()))
        .toEqual(table.labels);
      expect(links.map((link) => link.get('.tpl-name').text())).toEqual([
        'ATS Plain', 'Engineer Compact', 'Graduate Friendly',
        'Executive Band',
      ]);
      for (const link of links) {
        expect(link.get('.tpl-purpose').text()).toMatch(/\.$/u);
      }
      const all = wrapper.get('[data-testid="landing-browse-all"]');
      expect(all.attributes('href')).toBe('/templates');
      expect(all.text()).toBe(table.library);
      expect(all.classes()).toContain('home-pill--line');
    });

    it(`fans four ${locale} first pages behind a hidden block`, async () => {
      const wrapper = await mountLanding(locale);
      const fan = wrapper.get('[data-testid="landing-template-fan"]');
      expect(fan.attributes('aria-hidden')).toBe('true');
      expect(fan.findAll('img').map((img) => img.attributes('src')))
        .toEqual(SHOWCASE_IDS.map((id) =>
          `/templates/pages/${id}-${locale}-p1.png`));
      for (const img of fan.findAll('img')) {
        expect(img.attributes('alt')).toBe('');
      }
    });
  }
});

describe('index.vue actions', () => {
  for (const locale of ['vi', 'en'] as const) {
    it(`offers ${locale} registration before sign-in when signed out`,
      async () => {
        const wrapper = await mountLanding(locale);
        const hero = '[data-testid="landing-hero-actions"]';
        expect(texts(wrapper, hero)).toEqual(TABLE[locale].signedOut);
        expect(hrefs(wrapper, hero))
          .toEqual(['/register', '/templates', '/login']);
        expect(wrapper.find('[data-testid="landing-open-resumes"]').exists())
          .toBe(false);
        const maker = '[data-testid="landing-maker-actions"]';
        expect(texts(wrapper, maker)).toEqual(TABLE[locale].makerSignedOut);
        expect(hrefs(wrapper, maker)).toEqual([
          '/register', '/danny', 'https://github.com/dannyota/aboutme',
        ]);
      });

    it(`shows only the ${locale} resume entry when signed in`, async () => {
      meStatus = 200;
      const wrapper = await mountLanding(locale);
      await flushPromises();
      const hero = '[data-testid="landing-hero-actions"]';
      expect(texts(wrapper, hero)).toEqual(TABLE[locale].signedIn);
      expect(hrefs(wrapper, hero)).toEqual(['/app/resumes', '/templates']);
      expect(wrapper.find('[data-testid="landing-sign-in"]').exists())
        .toBe(false);
      expect(wrapper.find('[data-testid="landing-create-account"]').exists())
        .toBe(false);
      const maker = '[data-testid="landing-maker-actions"]';
      expect(texts(wrapper, maker)).toEqual(TABLE[locale].makerSignedIn);
      expect(hrefs(wrapper, maker)).toEqual([
        '/app/resumes', '/danny', 'https://github.com/dannyota/aboutme',
      ]);
    });
  }

  it('styles hero and maker actions as the spec draws them', async () => {
    const wrapper = await mountLanding('en');
    const hero = wrapper.get('[data-testid="landing-hero-actions"]')
      .findAll('a').map((link) => link.classes());
    expect(hero[0]).toContain('home-pill--white');
    expect(hero[1]).toContain('home-pill--ghost');
    expect(hero[2]).toContain('home-textlink');
    const maker = wrapper.get('[data-testid="landing-maker-actions"]')
      .findAll('a').map((link) => link.classes());
    expect(maker[0]).toContain('home-pill');
    expect(maker[0]).not.toContain('home-pill--line');
    expect(maker[1]).toContain('home-pill--line');
    expect(maker[2]).toContain('home-pill--line');
    const source = wrapper.get('[data-testid="landing-source-link"]');
    expect(source.attributes('rel')).toBe('noopener noreferrer');
  });
});

describe('index.vue structure', () => {
  it('renders the shared sample resume on the hero sheet', async () => {
    const wrapper = await mountLanding('en');
    const sample = wrapper.get('[data-testid="landing-sample"]');
    expect(sample.element.tagName).toBe('FIGURE');
    expect(sample.attributes('aria-label'))
      .toBe('Sample resume published at aboutme.vn/danny');
    expect(sample.find('.resume-document').exists()).toBe(true);
    expect(sample.find('[data-app-seal]').exists()).toBe(false);
  });

  it('hides every decorative chip from screen readers', async () => {
    const wrapper = await mountLanding('vi');
    const chips = wrapper.get('[data-testid="landing-chips"]');
    expect(chips.attributes('aria-hidden')).toBe('true');
    expect(chips.findAll('[data-chip]').map((chip) =>
      chip.attributes('data-chip'))).toEqual(['link', 'coin', 'mark', 'seal']);
    expect(chips.get('[data-chip="link"]').text()).toBe('aboutme.vn/danny');
    expect(chips.get('[data-chip="coin"]').text()).toBe('0đ');
    expect(wrapper.get('[data-testid="landing-rings"]')
      .attributes('aria-hidden')).toBe('true');
    const data = wrapper.get('[data-testid="landing-data"] .home-coin');
    expect(data.attributes('aria-hidden')).toBe('true');
    for (const glyph of wrapper.findAll('[data-glyph]')) {
      expect(glyph.attributes('aria-hidden')).toBe('true');
    }
  });

  it('uses the ticket shape and uppercase only inside the seal', async () => {
    for (const locale of ['vi', 'en'] as const) {
      const wrapper = await mountLanding(locale);
      const stamps = wrapper.findAll('[data-app-seal="stamp"]');
      expect(stamps).toHaveLength(2);
      for (const ticket of wrapper.findAll('[data-seal-ticket]')) {
        expect(ticket.element.closest('[data-app-seal="stamp"]'))
          .not.toBeNull();
      }
      const word = locale === 'vi' ? 'CÔNG KHAI' : 'PUBLIC';
      const outside = wrapper.element.cloneNode(true) as HTMLElement;
      for (const seal of outside.querySelectorAll('[data-app-seal]')) {
        seal.remove();
      }
      for (const sheet of outside.querySelectorAll('.resume-document')) {
        sheet.remove();
      }
      expect(outside.textContent).not.toContain(word);
      expect(outside.querySelector('[data-seal-ticket]')).toBeNull();
    }
    for (const file of LANDING_SOURCES) {
      const source = readFileSync(file, 'utf8');
      expect(source, file).not.toMatch(/uppercase/iu);
      expect(source, file).not.toMatch(/rotate\(-6deg\)/u);
    }
  });

  it('shows the publish example with the stamp and three states',
    async () => {
      const wrapper = await mountLanding('en');
      const choices = wrapper.findAll(
        '[data-testid="landing-publish-choice"]',
      );
      expect(choices.map((choice) => choice.attributes('data-state')))
        .toEqual(['on', 'on', 'off']);
      expect(choices.map((choice) => choice.get('dt').text())).toEqual([
        'Public resume', 'PDF download', 'SEO and GEO',
      ]);
      expect(wrapper.findAll('[data-testid="landing-publish-state"]')
        .map((state) => state.text())).toEqual(['On', 'On', 'Off']);
      expect(choices[0]!.text()).toContain('aboutme.vn/danny');
      const seal = wrapper.get('[data-testid="landing-publish-seal"]');
      expect(seal.attributes('role')).toBe('img');
      expect(seal.attributes('aria-label')).toBe('Public at aboutme.vn/danny');
      expect(seal.element.closest('[aria-hidden="true"]')).toBeNull();
      expect(wrapper.text()).toContain('Example settings');
    });

  it('draws the editor as a labeled illustration', async () => {
    for (const locale of ['vi', 'en'] as const) {
      const wrapper = await mountLanding(locale);
      const root = wrapper.get(
        '[data-testid="landing-editor-illustration"]',
      );
      const picture = root.get('[role="img"]');
      expect(picture.attributes('aria-label')).toBe(locale === 'vi'
        ? 'Minh họa trình chỉnh sửa: biểu mẫu và bản xem trước trang CV'
        : 'Illustration of the editor: a form and the resume page preview');
      expect(picture.get('img').attributes('src'))
        .toBe(`/templates/pages/creative-accent-${locale}-p1.png`);
      expect(picture.get('img').attributes('alt')).toBe('');
      expect(root.findAll('input, textarea, button')).toHaveLength(0);
    }
  });

  it('scopes the home tokens to the root element of this route',
    async () => {
      await mountLanding('en');
      await flushPromises();
      expect(document.documentElement.dataset.page).toBe('home');
    });

  it('renders Vietnamese by default and for an unknown cookie', async () => {
    for (const value of [undefined, 'fr' as 'vi']) {
      const wrapper = await mountLanding(value);
      expect(wrapper.get('[data-testid="landing-title"]').text())
        .toBe(TABLE.vi.hero);
    }
  });

  it('marks the page language for the rendered copy', async () => {
    for (const locale of ['vi', 'en'] as const) {
      setSiteLocale(locale === 'vi' ? undefined : locale);
      const wrapper = await mountSuspended(AppRoot, { route: '/' });
      await flushPromises();
      expect(wrapper.get('[data-testid="landing-title"]').text())
        .toBe(TABLE[locale].hero);
      await vi.waitFor(() =>
        expect(document.documentElement.lang).toBe(locale));
      expect(document.documentElement.dataset.page).toBe('home');
    }
  });

  it('switches to English from the header and remembers it', async () => {
    const wrapper = await mountSuspended(AppRoot, { route: '/' });
    await flushPromises();
    const toggle = wrapper
      .get('[data-testid="app-shell"]')
      .get('[data-testid="landing-locale"]');
    expect(toggle.attributes('role')).toBe('group');
    expect(toggle.attributes('aria-label')).toBe('Ngôn ngữ');
    const vi = toggle.get('[data-testid="landing-locale-vi"]');
    const en = toggle.get('[data-testid="landing-locale-en"]');
    expect(vi.attributes('aria-pressed')).toBe('true');
    expect(en.attributes('aria-pressed')).toBe('false');
    expect(en.attributes('lang')).toBe('en');
    expect(vi.attributes('lang')).toBe('vi');

    await en.trigger('click');
    await flushPromises();
    expect(en.attributes('aria-pressed')).toBe('true');
    expect(document.cookie).toContain('aboutme-locale=en');
    wrapper.unmount();
    // Nuxt syncs useCookie via cookieStore or BroadcastChannel; none here.
    const page = await mountSuspended(LandingPage);
    expect(page.get('[data-testid="landing-title"]').text())
      .toBe(TABLE.en.hero);
  });

  it('names no unshipped feature and no em dash', async () => {
    for (const locale of ['en', 'vi'] as const) {
      const wrapper = await mountLanding(locale);
      const text = wrapper.text().toLowerCase();
      expect(text).not.toMatch(/realtime|real-time|thời gian thực/u);
      expect(text).not.toContain('ai ready');
      expect(text).not.toContain('\u2014');
    }
  });
});

describe('index.vue sources', () => {
  it(
    'keeps the shell read to one /me request and no other API calls',
    async () => {
      mocks.fetchMock.mockClear();
      meRequests = 0;
      const wrapper = await mountSuspended(AppRoot, { route: '/' });
      await flushPromises();

      expect(meRequests).toBeLessThanOrEqual(2);
      expect(apiPaths().every((path) => path === '/api/v1/me')).toBe(true);
      expect(wrapper.get('[data-testid="landing-license"]').exists())
        .toBe(true);
    },
  );

  it('does not request API data from the landing page', async () => {
    mocks.fetchMock.mockClear();
    await mountSuspended(LandingPage);
    await flushPromises();

    expect(apiPaths().every((path) => path === '/api/v1/me')).toBe(true);
  });

  it('has no client data fetch, CSP change, or added script', () => {
    for (const file of LANDING_SOURCES) {
      const source = readFileSync(file, 'utf8');
      expect(source, file).not.toMatch(/(?:useFetch|useAsyncData|\$fetch)/u);
      expect(source, file).not.toContain('Content-Security-Policy');
      expect(source, file).not.toMatch(/<script(?! setup lang="ts">)/u);
    }
  });

  it('keeps hex colors in the token file, never in components', () => {
    for (const file of LANDING_SOURCES) {
      const source = readFileSync(file, 'utf8');
      expect(source, file).not.toMatch(/#[0-9a-fA-F]{3,8}\b/u);
    }
  });

  it('never animates on load and honors reduced motion', () => {
    for (const file of LANDING_SOURCES) {
      const source = readFileSync(file, 'utf8');
      expect(source, file).not.toMatch(/@keyframes|animation\s*:/u);
      for (const match of source.matchAll(/transition:\s*([^;]+);/gu)) {
        expect(match[1], file)
          .toMatch(/^(?:none !important|[\s\S]*150ms[\s\S]*)$/u);
      }
    }
    const css = readFileSync('app/landing/styles/home.css', 'utf8');
    const reduced = css.slice(
      css.indexOf('@media (prefers-reduced-motion: reduce)'),
    );
    expect(reduced).toMatch(/^@media/u);
    expect(reduced).toContain('.home *' + RENDERER_GUARD + ' {\n'
      + '    transition: none !important;');
  });

  it('draws a visible focus ring, white on brand, panel and footer', () => {
    const css = readFileSync('app/landing/styles/home.css', 'utf8');
    expect(css).toContain('.home :focus-visible' + RENDERER_GUARD + ' {\n'
      + '  outline: 3px solid var(--ring);');
    expect(css).toMatch(
      /\.home \.home-inverse \{\s*--ring: var\(--ring-inverse\);/u,
    );
    for (const [file, marker] of [
      ['app/landing/components/HomeHero.vue', 'hero home-panel home-inverse'],
      [
        'app/landing/components/HomePublish.vue',
        'publish home-panel home-inverse',
      ],
      ['app/landing/components/HomeFooter.vue', 'footer home-inverse'],
    ] as const) {
      expect(readFileSync(file, 'utf8'), file).toContain(marker);
    }
  });

  it('stops the hero margin collapsing so the sheet hangs below', () => {
    const hero = cssBlock('app/landing/components/HomeHero.vue', '.hero');
    expect(hero).toContain('display: flow-root;');
    const art = cssBlock('app/landing/components/HeroArt.vue', '.art');
    expect(art).toContain(
      'margin: 48px 0 -48px calc((100% - 210mm * var(--sheet-zoom)) / 2);',
    );
  });

  it('resets every inherited chrome type property on the sheet', () => {
    const sheet = cssBlock('app/landing/components/HeroArt.vue', '.sheet');
    for (const declaration of [
      'color: var(--paper-ink);',
      'font: 400 15px/1.5 var(--font-paper);',
      'font-feature-settings: normal;',
      'letter-spacing: normal;',
      'text-align: start;',
      'text-transform: none;',
      'text-wrap: wrap;',
    ]) {
      expect(sheet, declaration).toContain(declaration);
    }
  });

  it('wraps the shared renderer in the reset sheet', async () => {
    const wrapper = await mountLanding('en');
    const sheet = wrapper.get('[data-testid="landing-sheet"]');
    expect(sheet.classes()).toContain('sheet');
    expect(sheet.find('.resume-document').exists()).toBe(true);
  });

  it('keeps chrome focus styling off the renderer subtree', () => {
    const css = readFileSync('app/landing/styles/home.css', 'utf8');
    const rules = [...css.matchAll(/([^{}]*:focus-visible[^{}]*)\{/gu)]
      .map((match) => match[1]!.trim());
    expect(rules.length).toBeGreaterThan(0);
    for (const selector of rules) {
      expect(selector, selector).toContain(RENDERER_GUARD);
    }
  });
});

function cssBlock(file: string, selector: string): string {
  const source = readFileSync(file, 'utf8');
  const start = source.indexOf(`\n${selector} {`);
  expect(start, `${file} ${selector}`).toBeGreaterThanOrEqual(0);
  return source.slice(source.indexOf('{', start) + 1,
    source.indexOf('}', start)).replace(/\s+/gu, ' ');
}
